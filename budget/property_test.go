package budget

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"
)

// A time budget is only worth having if it cannot be overrun, and "cannot" is
// a claim about every plan rather than the four in the table. These properties
// generate plans -- stages, caps, floors, margins -- and check the guarantees
// that callers rely on without reading the code.

func randPlan(r *rand.Rand) Plan {
	n := 1 + r.IntN(6)
	phases := make([]Phase, n)
	for i := range phases {
		phases[i] = Phase{
			Name:  string(rune('a' + i)),
			Stage: r.IntN(3),
			Cap:   time.Duration(r.IntN(400)) * time.Millisecond,
			Floor: time.Duration(r.IntN(120)) * time.Millisecond,
		}
	}
	return Plan{
		Total:    time.Duration(500+r.IntN(4000)) * time.Millisecond,
		Margin:   time.Duration(r.IntN(200)) * time.Millisecond,
		MinSlice: time.Duration(r.IntN(50)) * time.Millisecond,
		Phases:   phases,
	}
}

// A frozen clock so the arithmetic is exact and nothing depends on how long
// the test itself takes to run.
func frozen(at *time.Time) func() time.Time {
	return func() time.Time { return *at }
}

// No phase is ever allowed more time than the budget has left. This is the
// whole promise: a phase that could be granted more than remains would blow
// the deadline it was created to protect.
func TestPropertyAllowanceNeverExceedsWhatIsLeft(t *testing.T) {
	r := rand.New(rand.NewPCG(101, 102))
	for i := 0; i < 3000; i++ {
		plan := randPlan(r)
		now := time.Unix(1700000000, 0)
		b, err := New(plan, frozen(&now))
		if err != nil {
			continue // an impossible plan is refused, which is correct
		}
		// Walk the clock forward through the budget, including past its end.
		for step := 0; step < 12; step++ {
			for _, ph := range plan.Phases {
				got := b.Allowance(ph.Name)
				if got < 0 {
					t.Fatalf("%q was allowed %v", ph.Name, got)
				}
				if rem := b.Remaining(); got > rem {
					t.Fatalf("%q was allowed %v with %v left", ph.Name, got, rem)
				}
				if ph.Cap > 0 && got > ph.Cap {
					t.Fatalf("%q has cap %v but was allowed %v", ph.Name, ph.Cap, got)
				}
			}
			now = now.Add(plan.Total / 8)
		}
	}
}

// The deadline is fixed at New and never moves, whatever happens to the
// phases. A budget whose deadline drifted would be no budget at all.
func TestPropertyTheDeadlineNeverMoves(t *testing.T) {
	r := rand.New(rand.NewPCG(103, 104))
	for i := 0; i < 2000; i++ {
		plan := randPlan(r)
		now := time.Unix(1700000000, 0)
		b, err := New(plan, frozen(&now))
		if err != nil {
			continue
		}
		want := b.Deadline()
		for _, ph := range plan.Phases {
			ctx, cancel, ok := b.Begin(context.Background(), ph.Name)
			if ok {
				b.End(ctx, ph.Name, nil)
			}
			cancel()
			now = now.Add(37 * time.Millisecond)
			if got := b.Deadline(); !got.Equal(want) {
				t.Fatalf("deadline moved from %v to %v", want, got)
			}
		}
		b.Finalize()
		if got := b.Deadline(); !got.Equal(want) {
			t.Fatalf("Finalize moved the deadline to %v", got)
		}
	}
}

// A phase's context never outlives the budget. Whatever slice it is given, its
// deadline is at or before the budget's -- that is what makes a runaway phase
// stop by itself rather than by being noticed.
func TestPropertyAPhaseContextNeverOutlivesTheBudget(t *testing.T) {
	r := rand.New(rand.NewPCG(105, 106))
	for i := 0; i < 3000; i++ {
		plan := randPlan(r)
		// The real clock, not an injected one: a phase context's deadline is
		// a context.WithTimeout and so is measured against time.Now, while an
		// injected clock governs only the ledger's arithmetic. Comparing the
		// two would compare a real instant with a fictional one.
		b, err := New(plan, nil)
		if err != nil {
			continue
		}
		for _, ph := range plan.Phases {
			ctx, cancel, ok := b.Begin(context.Background(), ph.Name)
			if !ok {
				cancel()
				continue
			}
			dl, has := ctx.Deadline()
			if !has {
				t.Fatalf("%q got a context with no deadline", ph.Name)
			}
			if dl.After(b.Deadline()) {
				t.Fatalf("%q may run until %v, past the budget's %v", ph.Name, dl, b.Deadline())
			}
			b.End(ctx, ph.Name, nil)
			cancel()
		}
	}
}

// Finalize freezes the budget. It reports the phases that did not finish --
// overrun or skipped -- and reports the same ones however often it is called:
// a ledger that changed after being closed is not a ledger. Nothing that
// completed may appear there, because a phase listed as unfinished is one
// somebody will go looking into.
func TestPropertyFinalizeIsIdempotentAndReportsOnlyUnfinishedPhases(t *testing.T) {
	r := rand.New(rand.NewPCG(107, 108))
	for i := 0; i < 2000; i++ {
		plan := randPlan(r)
		now := time.Unix(1700000000, 0)
		b, err := New(plan, frozen(&now))
		if err != nil {
			continue
		}
		// Run an arbitrary subset, leaving the rest unfinished.
		for _, ph := range plan.Phases {
			if r.IntN(2) == 0 {
				continue
			}
			ctx, cancel, ok := b.Begin(context.Background(), ph.Name)
			if ok {
				now = now.Add(time.Duration(r.IntN(50)) * time.Millisecond)
				b.End(ctx, ph.Name, nil)
			}
			cancel()
		}
		first := b.Finalize()
		seen := map[string]int{}
		for _, rec := range first {
			seen[rec.Phase.Name]++
			if seen[rec.Phase.Name] > 1 {
				t.Fatalf("phase %q appears twice in the record", rec.Phase.Name)
			}
			if rec.Status != Overrun && rec.Status != Skipped {
				t.Fatalf("phase %q is reported unfinished with status %q", rec.Phase.Name, rec.Status)
			}
			if rec.Status == Running {
				t.Fatalf("phase %q is still Running after Finalize", rec.Phase.Name)
			}
		}
		again := b.Finalize()
		if len(again) != len(first) {
			t.Fatalf("Finalize gave %d records then %d", len(first), len(again))
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("record %d changed after Finalize: %+v then %+v", j, first[j], again[j])
			}
		}
	}
}

// Scaling a plan to a new total keeps it valid and keeps the shape: the same
// phases, in the same stages, and a critical path that scales with it. A plan
// that became invalid under Scaled would fail at New, far from the cause.
func TestPropertyScaledStaysValid(t *testing.T) {
	r := rand.New(rand.NewPCG(109, 110))
	for i := 0; i < 3000; i++ {
		plan := randPlan(r)
		if plan.Validate() != nil {
			continue
		}
		total := time.Duration(100+r.IntN(20000)) * time.Millisecond
		got := plan.Scaled(total)
		if err := got.Validate(); err != nil {
			t.Fatalf("scaling a valid plan to %v made it invalid: %v", total, err)
		}
		if got.Total != total {
			t.Fatalf("Scaled(%v).Total = %v", total, got.Total)
		}
		if len(got.Phases) != len(plan.Phases) {
			t.Fatalf("Scaled changed the phase count: %d -> %d", len(plan.Phases), len(got.Phases))
		}
		for j := range got.Phases {
			if got.Phases[j].Name != plan.Phases[j].Name || got.Phases[j].Stage != plan.Phases[j].Stage {
				t.Fatalf("Scaled changed phase %d: %+v -> %+v", j, plan.Phases[j], got.Phases[j])
			}
		}
	}
}
