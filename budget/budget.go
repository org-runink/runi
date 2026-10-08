// SPDX-License-Identifier: BSD-3-Clause

package budget

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Phase is one row of a Plan.
type Phase struct {
	// Name identifies the phase in Begin, End and the record. Unique, non-empty.
	Name string
	// Stage orders the phases. Phases sharing a stage run concurrently, so a
	// phase holds back only the floors of strictly greater stages.
	Stage int
	// Cap is the most this phase may take even when more time is left. Zero
	// means no cap beyond what is left.
	Cap time.Duration
	// Floor is time every earlier stage must leave for this phase.
	Floor time.Duration
}

// Plan is a deadline and the phases that share it.
type Plan struct {
	// Total is the whole deadline, from New to the answer.
	Total time.Duration
	// Margin is kept back after the last phase, for assembling and sending the
	// answer. It is not a phase.
	Margin time.Duration
	// MinSlice is the smallest slice worth starting a phase in.
	MinSlice time.Duration
	Phases   []Phase
}

// Validate reports why a plan cannot be honoured: a non-positive Total, a
// negative duration, a missing or repeated name, or a margin and floors that
// together exceed the deadline.
func (p Plan) Validate() error {
	if p.Total <= 0 {
		return fmt.Errorf("budget: total %v is not positive", p.Total)
	}
	if p.Margin < 0 || p.MinSlice < 0 {
		return errors.New("budget: margin and minimum slice must not be negative")
	}
	reserved := p.Margin
	seen := make(map[string]bool, len(p.Phases))
	for _, ph := range p.Phases {
		if ph.Name == "" {
			return errors.New("budget: a phase has no name")
		}
		if seen[ph.Name] {
			return fmt.Errorf("budget: phase %q appears twice", ph.Name)
		}
		seen[ph.Name] = true
		if ph.Cap < 0 || ph.Floor < 0 {
			return fmt.Errorf("budget: phase %q has a negative cap or floor", ph.Name)
		}
		reserved += ph.Floor
	}
	if reserved > p.Total {
		return fmt.Errorf("budget: margin and floors need %v, more than the %v total", reserved, p.Total)
	}
	return nil
}

// Scaled returns the plan for a different total deadline, with every cap and
// floor scaled in proportion. Margin and MinSlice are not scaled: they are
// costs of the answer and of starting a phase, not shares of the deadline.
func (p Plan) Scaled(total time.Duration) Plan {
	q := p
	q.Total = total
	q.Phases = make([]Phase, len(p.Phases))
	f := 0.0
	if p.Total > 0 {
		f = float64(total) / float64(p.Total)
	}
	for i, ph := range p.Phases {
		ph.Cap = time.Duration(float64(ph.Cap) * f)
		ph.Floor = time.Duration(float64(ph.Floor) * f)
		q.Phases[i] = ph
	}
	return q
}

// CriticalPath is the time the plan needs when every phase takes its cap:
// the longest cap in each stage, summed over the stages, plus the margin. A
// phase with no cap counts as its floor. A plan whose critical path exceeds
// Total cannot give every phase its cap, so later stages get less.
func (p Plan) CriticalPath() time.Duration {
	longest := map[int]time.Duration{}
	for _, ph := range p.Phases {
		d := ph.Cap
		if d == 0 {
			d = ph.Floor
		}
		if d > longest[ph.Stage] {
			longest[ph.Stage] = d
		}
	}
	sum := p.Margin
	for _, d := range longest {
		sum += d
	}
	return sum
}

// Status is how a phase ended, or that it has not ended yet.
type Status string

const (
	Running Status = "running" // begun, not ended; never survives Finalize
	Done    Status = "done"    // ended without error inside its slice
	Failed  Status = "failed"  // ended with an error that was not a deadline
	Overrun Status = "overrun" // did not finish inside its slice
	Skipped Status = "skipped" // never started: its slice was below MinSlice
)

// Record is what the ledger knows about one phase.
type Record struct {
	Phase   Phase
	Slice   time.Duration // the allowance it was given at Begin
	Started time.Time
	Elapsed time.Duration // time from Begin to End, or to Finalize
	Status  Status
	// Cut is true for a phase Finalize found still running: it never ended,
	// so it has not reported its own outcome anywhere.
	Cut bool
}

// Budget is one deadline and the ledger of how its phases spent it. It is
// safe for concurrent use.
type Budget struct {
	plan     Plan
	byName   map[string]Phase
	now      func() time.Time
	start    time.Time
	deadline time.Time

	mu    sync.Mutex
	recs  map[string]*Record
	order []string
	final bool
}

// New starts the deadline now. A nil clock means time.Now.
func New(plan Plan, now func() time.Time) (*Budget, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	byName := make(map[string]Phase, len(plan.Phases))
	for _, ph := range plan.Phases {
		byName[ph.Name] = ph
	}
	start := now()
	return &Budget{
		plan:     plan,
		byName:   byName,
		now:      now,
		start:    start,
		deadline: start.Add(plan.Total),
		recs:     map[string]*Record{},
	}, nil
}

// Deadline is the instant the answer is due, on the budget's clock.
func (b *Budget) Deadline() time.Time { return b.deadline }

// Elapsed is the time since New.
func (b *Budget) Elapsed() time.Duration { return b.now().Sub(b.start) }

// Remaining is what is left of the deadline, never negative.
func (b *Budget) Remaining() time.Duration {
	if r := b.deadline.Sub(b.now()); r > 0 {
		return r
	}
	return 0
}

// Allowance is the slice the named phase would get if it started now: zero
// for a name the plan does not have.
func (b *Budget) Allowance(name string) time.Duration {
	ph, ok := b.byName[name]
	if !ok {
		return 0
	}
	return b.allowance(ph)
}

func (b *Budget) allowance(ph Phase) time.Duration {
	avail := b.Remaining() - b.plan.Margin
	for _, o := range b.plan.Phases {
		if o.Stage > ph.Stage {
			avail -= o.Floor
		}
	}
	if ph.Cap > 0 && ph.Cap < avail {
		avail = ph.Cap
	}
	if avail < 0 {
		return 0
	}
	return avail
}

// Begin starts the named phase. It returns a context that expires when the
// phase's slice does (never later than parent), and ok=true. The caller must
// then call End or EndWith once, and cancel.
//
// ok is false, and the returned context already cancelled, when the phase is
// not started. A phase whose slice is below MinSlice (or zero), or a name the
// plan does not have, is recorded as skipped. A phase already running is left
// alone, and after Finalize nothing is recorded at all. Beginning a phase
// that has ended starts it again with a fresh record.
func (b *Budget) Begin(parent context.Context, name string) (context.Context, context.CancelFunc, bool) {
	ph, known := b.byName[name]
	var slice time.Duration
	if known {
		slice = b.allowance(ph)
	} else {
		ph = Phase{Name: name}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	rec, seen := b.recs[name]
	if b.final || (seen && rec.Status == Running) {
		return cancelled(parent)
	}
	if !seen {
		b.order = append(b.order, name)
	}
	rec = &Record{Phase: ph, Slice: slice, Started: b.now()}
	b.recs[name] = rec
	if slice == 0 || slice < b.plan.MinSlice {
		rec.Status = Skipped
		return cancelled(parent)
	}
	rec.Status = Running
	ctx, cancel := context.WithTimeout(parent, slice)
	return ctx, cancel, true
}

func cancelled(parent context.Context) (context.Context, context.CancelFunc, bool) {
	ctx, cancel := context.WithCancel(parent)
	cancel()
	return ctx, func() {}, false
}

// End closes the named phase from its outcome. A nil error is done, unless
// the phase's context had already run out of time, which is overrun; an error
// that is or wraps context.DeadlineExceeded is overrun; any other error is
// failed. An RPC library's own deadline error is not recognised: map it to
// context.DeadlineExceeded, or call EndWith.
func (b *Budget) End(ctx context.Context, name string, err error) Status {
	expired := ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)
	switch {
	case expired, errors.Is(err, context.DeadlineExceeded):
		return b.EndWith(name, Overrun)
	case err != nil:
		return b.EndWith(name, Failed)
	default:
		return b.EndWith(name, Done)
	}
}

// EndWith closes the named phase with the given status, for a phase that
// knows better than its error (one that finished part of its work and
// stopped at its deadline reports Overrun itself). It returns the status the
// record holds: only a running phase is changed, and nothing is changed after
// Finalize.
func (b *Budget) EndWith(name string, s Status) Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	rec, ok := b.recs[name]
	if !ok {
		return ""
	}
	if rec.Status != Running || b.final {
		return rec.Status
	}
	rec.Elapsed = b.now().Sub(rec.Started)
	rec.Status = s
	return s
}

// Status is the named phase's status, or "" if it never began.
func (b *Budget) Status(name string) Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	if rec, ok := b.recs[name]; ok {
		return rec.Status
	}
	return ""
}

// Records is every phase that began, in the order it first began.
func (b *Budget) Records() []Record {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Record, 0, len(b.order))
	for _, n := range b.order {
		out = append(out, *b.recs[n])
	}
	return out
}

// Finalize freezes the ledger when the answer is assembled. Every phase still
// running becomes overrun, with Cut set and the time it had used. It returns
// the phases that did not finish (overrun or skipped) in the order they
// began. Afterwards Begin starts nothing and End changes nothing. Calling it
// again returns the same phases.
func (b *Budget) Finalize() []Record {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	var unfinished []Record
	for _, n := range b.order {
		rec := b.recs[n]
		if rec.Status == Running {
			rec.Status = Overrun
			rec.Elapsed = now.Sub(rec.Started)
			rec.Cut = true
		}
		if rec.Status == Overrun || rec.Status == Skipped {
			unfinished = append(unfinished, *rec)
		}
	}
	b.final = true
	return unfinished
}
