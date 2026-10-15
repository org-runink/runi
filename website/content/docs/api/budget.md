---
title: "runi/budget"
weight: 10
---

```go
import "github.com/org-runink/runi/budget"
```

Package budget splits one deadline between the phases of a piece of work,
and keeps an honest record of how each phase spent its share.

A request that must answer in three minutes usually grows a timeout per
step: two minutes for this call, ninety seconds for that scrape. Each number
is defensible on its own, nothing adds them up, and the only real bound on
the whole is whatever the client gives up at. This package replaces that
with one deadline and a slice for every phase derived from what is left when
the phase starts:

slice(phase) = min( cap(phase), remaining − margin − Σ floor(later stages) )

A floor is time an earlier stage may not take: give the phase that writes the
answer a floor, and no amount of slowness earlier on can leave it with
nothing. Phases that share a stage run concurrently, so only strictly later
stages hold time back. A phase whose slice is below the plan's MinSlice is
not started at all and is recorded as skipped, rather than started into a
guaranteed overrun.

b, err := budget.New(plan)
ctx, cancel, ok := b.Begin(parent, "search")
if ok {
	err := search(ctx)
	cancel()
	b.End(ctx, "search", err)
}
...
for _, r := range b.Finalize() {
	// r did not finish: say so in the answer
}

Every phase ends as done, failed, overrun or skipped. Finalize freezes that
record when the answer is assembled: a phase still running then is overrun,
and nothing that happens afterwards changes the record. The answer can then
say which parts it is missing instead of silently leaving them out.

# What it does not do

It does not schedule. It never runs, orders, retries or parallelises your
phases; it answers "how long may this phase take if it starts now?" and
records what happened. Stage numbers only tell it which floors are still
ahead.

It does not stop work. A slice is a context deadline, and cancellation in Go
is cooperative: a phase that ignores its context keeps running and keeps
whatever it holds. Finalize reports it as overrun, which is true, but the
goroutine is yours to stop.

Its caps are a plan, not a measurement. Nothing here knows whether a phase
can finish in its slice; measure that on the hardware that will run it.

The ledger reads time through the clock you give New with WithClock, so the
arithmetic can be tested with a fake clock. Context deadlines are set from durations and
expire on the real clock.

## Index

- `type` [Budget](#budget)
- `type` [Option](#option)
- `type` [Phase](#phase)
- `type` [Plan](#plan)
- `type` [Record](#record)
- `type` [Status](#status)
- `method` [Budget.Allowance](#budgetallowance)
- `method` [Budget.Begin](#budgetbegin)
- `method` [Budget.Deadline](#budgetdeadline)
- `method` [Budget.Elapsed](#budgetelapsed)
- `method` [Budget.End](#budgetend)
- `method` [Budget.EndWith](#budgetendwith)
- `method` [Budget.Finalize](#budgetfinalize)
- `method` [Budget.Records](#budgetrecords)
- `method` [Budget.Remaining](#budgetremaining)
- `method` [Budget.Status](#budgetstatus)
- `const` [ErrInvalid](#errinvalid)
- `func` [New](#new)
- `method` [Plan.CriticalPath](#plancriticalpath)
- `method` [Plan.Scaled](#planscaled)
- `method` [Plan.Validate](#planvalidate)
- `func` [WithClock](#withclock)

### Budget

```go
type Budget struct
```

Budget is one deadline and the ledger of how its phases spent it. It is
safe for concurrent use.

The zero value is not usable: create a Budget with New.

<small>Defined in <code>budget/budget.go</code>.</small>

### Option

```go
type Option func(*config)
```

Option configures a Budget.

<small>Defined in <code>budget/budget.go</code>.</small>

### Phase

```go
type Phase struct
```

Phase is one row of a Plan.

<small>Defined in <code>budget/budget.go</code>.</small>

### Plan

```go
type Plan struct
```

Plan is a deadline and the phases that share it.

<small>Defined in <code>budget/budget.go</code>.</small>

### Record

```go
type Record struct
```

Record is what the ledger knows about one phase.

<small>Defined in <code>budget/budget.go</code>.</small>

### Status

```go
type Status string
```

Status is how a phase ended, or that it has not ended yet.

<small>Defined in <code>budget/budget.go</code>.</small>

### Budget.Allowance

```go
func (b *Budget) Allowance(name string) time.Duration
```

Allowance is the slice the named phase would get if it started now: zero
for a name the plan does not have.

<small>Defined in <code>budget/budget.go</code>.</small>

### Budget.Begin

```go
func (b *Budget) Begin(parent context.Context, name string) (context.Context, context.CancelFunc, bool)
```

Begin starts the named phase. It returns a context that expires when the
phase's slice does (never later than parent), and ok=true. The caller must
then call End or EndWith once, and cancel.

ok is false, and the returned context already cancelled, when the phase is
not started. A phase whose slice is below MinSlice (or zero), or a name the
plan does not have, is recorded as skipped. A phase already running is left
alone, and after Finalize nothing is recorded at all. Beginning a phase
that has ended starts it again with a fresh record.

<small>Defined in <code>budget/budget.go</code>.</small>

### Budget.Deadline

```go
func (b *Budget) Deadline() time.Time
```

Deadline is the instant the answer is due, on the budget's clock.

<small>Defined in <code>budget/budget.go</code>.</small>

### Budget.Elapsed

```go
func (b *Budget) Elapsed() time.Duration
```

Elapsed is the time since New.

<small>Defined in <code>budget/budget.go</code>.</small>

### Budget.End

```go
func (b *Budget) End(ctx context.Context, name string, err error) Status
```

End closes the named phase from its outcome. A nil error is done, unless
the phase's context had already run out of time, which is overrun; an error
that is or wraps context.DeadlineExceeded is overrun; any other error is
failed. An RPC library's own deadline error is not recognised: map it to
context.DeadlineExceeded, or call EndWith.

<small>Defined in <code>budget/budget.go</code>.</small>

### Budget.EndWith

```go
func (b *Budget) EndWith(name string, s Status) Status
```

EndWith closes the named phase with the given status, for a phase that
knows better than its error (one that finished part of its work and
stopped at its deadline reports Overrun itself). It returns the status the
record holds: only a running phase is changed, and nothing is changed after
Finalize.

<small>Defined in <code>budget/budget.go</code>.</small>

### Budget.Finalize

```go
func (b *Budget) Finalize() []Record
```

Finalize freezes the ledger when the answer is assembled. Every phase still
running becomes overrun, with Cut set and the time it had used. It returns
the phases that did not finish (overrun or skipped) in the order they
began. Afterwards Begin starts nothing and End changes nothing. Calling it
again returns the same phases.

<small>Defined in <code>budget/budget.go</code>.</small>

### Budget.Records

```go
func (b *Budget) Records() []Record
```

Records is every phase that began, in the order it first began.

<small>Defined in <code>budget/budget.go</code>.</small>

### Budget.Remaining

```go
func (b *Budget) Remaining() time.Duration
```

Remaining is what is left of the deadline, never negative.

<small>Defined in <code>budget/budget.go</code>.</small>

### Budget.Status

```go
func (b *Budget) Status(name string) Status
```

Status is the named phase's status, or "" if it never began.

<small>Defined in <code>budget/budget.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("budget: invalid plan")
```

ErrInvalid is wrapped by every error Validate and New return: a plan with a
non-positive total, a negative duration, a missing or repeated phase name,
or floors and margin that exceed the deadline. Match it with errors.Is; the
message text is not part of the API.

<small>Defined in <code>budget/budget.go</code>.</small>

### New

```go
func New(plan Plan, opts ...Option) (*Budget, error)
```

New starts the deadline now. A plan that fails Validate is refused with
that error, which wraps ErrInvalid.

<small>Defined in <code>budget/budget.go</code>.</small>

### Plan.CriticalPath

```go
func (p Plan) CriticalPath() time.Duration
```

CriticalPath is the time the plan needs when every phase takes its cap:
the longest cap in each stage, summed over the stages, plus the margin. A
phase with no cap counts as its floor. A plan whose critical path exceeds
Total cannot give every phase its cap, so later stages get less.

<small>Defined in <code>budget/budget.go</code>.</small>

### Plan.Scaled

```go
func (p Plan) Scaled(total time.Duration) Plan
```

Scaled returns the plan for a different total deadline, with every cap and
floor scaled in proportion. Margin and MinSlice are not scaled: they are
costs of the answer and of starting a phase, not shares of the deadline.

Margin is kept only while keeping it is possible. A fixed margin plus the
scaled floors eventually needs more than a shrinking total, and Scaled would
then hand back a plan New refuses -- a failure reported a long way from the
Scaled call that caused it. In that case the margin is cut to whatever the
floors leave, so scaling a valid plan always yields a valid plan.

<small>Defined in <code>budget/budget.go</code>.</small>

### Plan.Validate

```go
func (p Plan) Validate() error
```

Validate reports why a plan cannot be honoured: a non-positive Total, a
negative duration, a missing or repeated name, or a margin and floors that
together exceed the deadline.

<small>Defined in <code>budget/budget.go</code>.</small>

### WithClock

```go
func WithClock(now func() time.Time) Option
```

WithClock sets the clock the ledger reads (default time.Now), so the
arithmetic can be tested with a fake clock. A nil clock means time.Now.
Context deadlines are set from durations and still expire on the real clock.

<small>Defined in <code>budget/budget.go</code>.</small>
