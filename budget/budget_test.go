// SPDX-License-Identifier: BSD-3-Clause

package budget

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

const s = time.Second

// plan is shaped like a real one: a lookup, a short prep, three concurrent
// analyses, and an answer that must never be starved.
func plan() Plan {
	return Plan{
		Total:    170 * s,
		Margin:   5 * s,
		MinSlice: 3 * s,
		Phases: []Phase{
			{Name: "lookup", Stage: 0, Cap: 25 * s},
			{Name: "prep", Stage: 1, Cap: 8 * s},
			{Name: "health", Stage: 2, Cap: 40 * s},
			{Name: "rules", Stage: 2, Cap: 40 * s},
			{Name: "trends", Stage: 2, Cap: 40 * s},
			{Name: "answer", Stage: 3, Cap: 45 * s, Floor: 35 * s},
		},
	}
}

func mustNew(t *testing.T, p Plan, c *fakeClock) *Budget {
	t.Helper()
	b, err := New(p, c.now)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAllowanceIsTheCapWhileTimeIsPlentiful(t *testing.T) {
	b := mustNew(t, plan(), newFakeClock())
	for name, want := range map[string]time.Duration{"lookup": 25 * s, "prep": 8 * s, "health": 40 * s, "answer": 45 * s} {
		if got := b.Allowance(name); got != want {
			t.Errorf("Allowance(%s) = %v, want %v", name, got, want)
		}
	}
}

func TestAllowanceKeepsTheLaterFloors(t *testing.T) {
	c := newFakeClock()
	b := mustNew(t, plan(), c)
	c.advance(120 * s) // 50 s left
	// An analysis gets what is left after the margin and the answer's floor.
	if got, want := b.Allowance("health"), 10*s; got != want {
		t.Fatalf("health = %v, want %v", got, want)
	}
	// The answer holds back nothing for itself: no stage follows it.
	if got, want := b.Allowance("answer"), 45*s; got != want {
		t.Fatalf("answer = %v, want %v", got, want)
	}
	c.advance(30 * s) // 20 s left: nothing for analysis at all
	if got := b.Allowance("health"); got != 0 {
		t.Fatalf("health = %v, want 0", got)
	}
	if got, want := b.Allowance("answer"), 15*s; got != want {
		t.Fatalf("answer = %v, want %v", got, want)
	}
}

func TestPhasesInTheSameStageDoNotHoldTimeForEachOther(t *testing.T) {
	p := plan()
	p.Phases[3].Floor = 20 * s // rules, stage 2, same as health
	c := newFakeClock()
	b := mustNew(t, p, c)
	c.advance(105 * s) // 65 s left
	// health: 65 - 5 margin - 35 answer = 25; rules is beside it, not after it.
	if got, want := b.Allowance("health"), 25*s; got != want {
		t.Fatalf("health = %v, want %v (a same-stage floor must not count)", got, want)
	}
	// prep comes before rules, so it leaves room for both floors: 65 - 5 - 20 - 35.
	if got, want := b.Allowance("prep"), 5*s; got != want {
		t.Fatalf("prep = %v, want %v", got, want)
	}
}

func TestZeroCapMeansWhatIsLeft(t *testing.T) {
	p := Plan{Total: 60 * s, Margin: 2 * s, Phases: []Phase{{Name: "all"}}}
	b := mustNew(t, p, newFakeClock())
	if got, want := b.Allowance("all"), 58*s; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestUnknownPhaseHasNoAllowanceAndIsSkipped(t *testing.T) {
	b := mustNew(t, plan(), newFakeClock())
	if got := b.Allowance("nope"); got != 0 {
		t.Fatalf("Allowance = %v", got)
	}
	ctx, cancel, ok := b.Begin(context.Background(), "nope")
	defer cancel()
	if ok || ctx.Err() == nil {
		t.Fatal("an unknown phase was started")
	}
	if got := b.Status("nope"); got != Skipped {
		t.Fatalf("status = %q, want skipped", got)
	}
}

func TestBeginSkipsASliceBelowTheMinimum(t *testing.T) {
	c := newFakeClock()
	b := mustNew(t, plan(), c)
	c.advance(128 * s) // 42 s left: health gets 42-5-35 = 2 s, under 3
	ctx, cancel, ok := b.Begin(context.Background(), "health")
	defer cancel()
	if ok {
		t.Fatal("a 2 s slice was started")
	}
	if ctx.Err() == nil {
		t.Fatal("a skipped phase's context is live")
	}
	recs := b.Records()
	if len(recs) != 1 || recs[0].Status != Skipped || recs[0].Slice != 2*s {
		t.Fatalf("records = %+v", recs)
	}
}

func TestBeginSkipsAZeroSliceEvenWithNoMinimum(t *testing.T) {
	p := plan()
	p.MinSlice = 0
	c := newFakeClock()
	b := mustNew(t, p, c)
	c.advance(170 * s)
	if _, _, ok := b.Begin(context.Background(), "answer"); ok {
		t.Fatal("a phase was started with no time left")
	}
}

func TestPhaseContextNeverOutlivesItsSliceOrItsParent(t *testing.T) {
	b, err := New(plan(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel, ok := b.Begin(context.Background(), "prep")
	defer cancel()
	dl, has := ctx.Deadline()
	if !ok || !has || time.Until(dl) > 8*s {
		t.Fatalf("ok=%v deadline in %v, want at most 8s", ok, time.Until(dl))
	}
	parent, pcancel := context.WithTimeout(context.Background(), time.Second)
	defer pcancel()
	ctx2, cancel2, ok := b.Begin(parent, "lookup")
	defer cancel2()
	dl2, _ := ctx2.Deadline()
	pdl, _ := parent.Deadline()
	if !ok || dl2.After(pdl) {
		t.Fatal("the phase context outlives its parent")
	}
}

func TestEndClassifiesTheOutcome(t *testing.T) {
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want Status
	}{
		{"done", context.Background(), nil, Done},
		{"failed", context.Background(), errors.New("boom"), Failed},
		{"cancelled is a failure, not an overrun", context.Background(), context.Canceled, Failed},
		{"deadline", context.Background(), context.DeadlineExceeded, Overrun},
		{"wrapped deadline", context.Background(), fmt.Errorf("query: %w", context.DeadlineExceeded), Overrun},
		{"returned nil after its slice ran out", expired, nil, Overrun},
		{"nil context", nil, nil, Done},
	}
	for _, tc := range cases {
		b := mustNew(t, plan(), newFakeClock())
		_, c, _ := b.Begin(context.Background(), "lookup")
		if got := b.End(tc.ctx, "lookup", tc.err); got != tc.want {
			t.Errorf("%s: End = %q, want %q", tc.name, got, tc.want)
		}
		c()
	}
}

func TestEndRecordsElapsedAndOnlyOnce(t *testing.T) {
	c := newFakeClock()
	b := mustNew(t, plan(), c)
	_, cancel, _ := b.Begin(context.Background(), "lookup")
	defer cancel()
	c.advance(7 * s)
	b.End(context.Background(), "lookup", nil)
	c.advance(7 * s)
	if got := b.EndWith("lookup", Failed); got != Done {
		t.Fatalf("second end changed the record to %q", got)
	}
	if r := b.Records()[0]; r.Elapsed != 7*s || r.Status != Done {
		t.Fatalf("record = %+v", r)
	}
	if got := b.EndWith("never-began", Done); got != "" {
		t.Fatalf("EndWith on a phase that never began = %q", got)
	}
}

func TestBeginLeavesARunningPhaseAloneAndRestartsAnEndedOne(t *testing.T) {
	c := newFakeClock()
	b := mustNew(t, plan(), c)
	_, cancel, _ := b.Begin(context.Background(), "lookup")
	defer cancel()
	c.advance(4 * s)
	if _, _, ok := b.Begin(context.Background(), "lookup"); ok {
		t.Fatal("a running phase was started twice")
	}
	if r := b.Records()[0]; r.Status != Running || !r.Started.Equal(c.now().Add(-4*s)) {
		t.Fatalf("the running record was touched: %+v", r)
	}
	b.End(context.Background(), "lookup", errors.New("transient"))
	_, cancel2, ok := b.Begin(context.Background(), "lookup")
	defer cancel2()
	if !ok || b.Status("lookup") != Running || len(b.Records()) != 1 {
		t.Fatalf("retry: ok=%v status=%q records=%d", ok, b.Status("lookup"), len(b.Records()))
	}
}

func TestFinalizeReportsUnfinishedPhasesAndFreezes(t *testing.T) {
	c := newFakeClock()
	b := mustNew(t, plan(), c)
	_, c1, _ := b.Begin(context.Background(), "lookup")
	defer c1()
	b.End(context.Background(), "lookup", nil)
	_, c2, _ := b.Begin(context.Background(), "health")
	defer c2()
	c.advance(130 * s) // rules now gets 40-5-35 = 0
	_, c3, _ := b.Begin(context.Background(), "rules")
	defer c3()

	got := b.Finalize()
	if len(got) != 2 || got[0].Phase.Name != "health" || got[1].Phase.Name != "rules" {
		t.Fatalf("unfinished = %+v", got)
	}
	if h := got[0]; h.Status != Overrun || !h.Cut || h.Elapsed != 130*s {
		t.Fatalf("health = %+v", h)
	}
	if r := got[1]; r.Status != Skipped || r.Cut {
		t.Fatalf("rules = %+v", r)
	}

	// Frozen: late ends, late begins and a second Finalize change nothing.
	if s := b.End(context.Background(), "health", nil); s != Overrun {
		t.Fatalf("End after Finalize = %q", s)
	}
	if _, _, ok := b.Begin(context.Background(), "answer"); ok {
		t.Fatal("a phase started after Finalize")
	}
	if b.Status("answer") != "" || len(b.Records()) != 3 {
		t.Fatal("Begin after Finalize was recorded")
	}
	if again := b.Finalize(); len(again) != 2 {
		t.Fatalf("second Finalize = %+v", again)
	}
}

func TestClockAccessors(t *testing.T) {
	c := newFakeClock()
	start := c.now()
	b := mustNew(t, plan(), c)
	c.advance(200 * s)
	if b.Remaining() != 0 {
		t.Fatalf("Remaining = %v, want 0 past the deadline", b.Remaining())
	}
	if b.Elapsed() != 200*s || !b.Deadline().Equal(start.Add(170*s)) {
		t.Fatalf("Elapsed %v Deadline %v", b.Elapsed(), b.Deadline())
	}
}

func TestValidate(t *testing.T) {
	bad := map[string]func(*Plan){
		"not positive":    func(p *Plan) { p.Total = 0 },
		"not be negative": func(p *Plan) { p.Margin = -s },
		"has no name":     func(p *Plan) { p.Phases[0].Name = "" },
		"appears twice":   func(p *Plan) { p.Phases[1].Name = "lookup" },
		"negative cap":    func(p *Plan) { p.Phases[2].Floor = -s },
		"more than":       func(p *Plan) { p.Phases[5].Floor = 166 * s },
	}
	for want, mutate := range bad {
		p := plan()
		mutate(&p)
		err := p.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", want, err)
		}
		if _, err := New(p, nil); err == nil {
			t.Errorf("%s: New accepted the plan", want)
		}
	}
	if err := plan().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestScaled(t *testing.T) {
	p := plan()
	q := p.Scaled(340 * s)
	if q.Total != 340*s || q.Margin != p.Margin || q.MinSlice != p.MinSlice {
		t.Fatalf("scaled header = %+v", q)
	}
	if q.Phases[5].Cap != 90*s || q.Phases[5].Floor != 70*s || q.Phases[0].Cap != 50*s {
		t.Fatalf("scaled answer = %+v", q.Phases[5])
	}
	if p.Phases[5].Cap != 45*s {
		t.Fatal("Scaled changed the original plan")
	}
	if z := (Plan{Phases: []Phase{{Name: "a", Cap: s}}}).Scaled(s); z.Phases[0].Cap != 0 {
		t.Fatalf("scaling a zero-total plan = %+v", z)
	}
}

func TestCriticalPath(t *testing.T) {
	// 25 + 8 + max(40, 40, 40) + 45 + 5 margin.
	if got, want := plan().CriticalPath(), 123*s; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	p := Plan{Margin: s, Phases: []Phase{{Name: "a", Floor: 4 * s}, {Name: "b", Stage: 1, Cap: 2 * s}}}
	if got, want := p.CriticalPath(), 7*s; got != want {
		t.Fatalf("uncapped phase: got %v, want %v", got, want)
	}
}

func TestConcurrentUse(t *testing.T) {
	b, err := New(Plan{Total: time.Minute, Phases: []Phase{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, n := range []string{"a", "b", "c", "d"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel, ok := b.Begin(context.Background(), n)
			defer cancel()
			if ok {
				_ = b.Allowance(n)
				_ = b.Status(n)
				_ = b.Records()
				b.End(ctx, n, nil)
			}
		}()
	}
	wg.Wait()
	if got := b.Finalize(); len(got) != 0 {
		t.Fatalf("unfinished = %+v", got)
	}
}
