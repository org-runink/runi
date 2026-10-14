package drift

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestDecayedKnownAnswer(t *testing.T) {
	d, err := NewDecayed(10 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// The first reading is only a baseline.
	if v, ok := d.Update(0, t0); ok || !math.IsNaN(v) || !math.IsNaN(d.Value()) {
		t.Fatalf("first reading: %v %v", v, ok)
	}
	// +5 over 1 s: rate 5, and the first rate initialises the average.
	if v, ok := d.Update(5, t0.Add(time.Second)); !ok || v != 5 {
		t.Fatalf("first rate: %v %v", v, ok)
	}
	// +20 over 2 s: rate 10. e = exp(−2/10) = 0.818731;
	// a = 5·0.818731 + 10·0.181269 = 5.906346.
	v, ok := d.Update(25, t0.Add(3*time.Second))
	if want := 5*math.Exp(-0.2) + 10*(1-math.Exp(-0.2)); !ok || relErr(v, want) > 1e-15 || relErr(v, 5.906346) > 1e-6 {
		t.Fatalf("second rate: %v %v", v, ok)
	}
	if d.Window() != 10*time.Second || d.Value() != v {
		t.Fatalf("window %v value %v", d.Window(), d.Value())
	}
}

// irregular returns sample times after start up to and including end, spaced
// uniformly at random between lo and hi.
func irregular(r *rand.Rand, start, end time.Time, lo, hi time.Duration) []time.Time {
	var ts []time.Time
	for at := start; ; {
		at = at.Add(lo + time.Duration(r.Int64N(int64(hi-lo))))
		if !at.Before(end) {
			return append(ts, end)
		}
		ts = append(ts, at)
	}
}

// A counter of nanoseconds stalled, growing at a quarter of wall time, read
// at irregular times: the average is 0.25e9 ns/s, a 25% share, from the first
// rate on.
func TestDecayedConstantRate(t *testing.T) {
	r := rand.New(rand.NewPCG(71, 72))
	d, _ := NewDecayed(time.Minute)
	stalled := func(at time.Time) uint64 { return uint64(at.Sub(t0)) / 4 }
	d.Update(stalled(t0), t0)
	for _, at := range irregular(r, t0, t0.Add(10*time.Minute), 100*time.Millisecond, 3*time.Second) {
		v, ok := d.Update(stalled(at), at)
		if !ok || relErr(v, 0.25e9) > 1e-7 {
			t.Fatalf("at %v: %v %v, want 2.5e8", at.Sub(t0), v, ok)
		}
	}
}

// A step in the rate is absorbed with time constant window whatever the
// spacing: one window after the step the average is 1 − 1/e of the way there.
//
// Readings come every 0.1 to 3 s before the step and every 0.1 to 0.5 s after
// it, as a poller that is itself slowed by load might deliver them. A fixed
// per-sample weight tuned to the mean spacing before the step is computed
// alongside: it counts the bunched readings as if each covered 1.55 s and
// overshoots, which shows the test would catch an implementation that ignored
// the time between readings.
func TestDecayedStepTimeConstant(t *testing.T) {
	const window = 60 * time.Second
	const before, after = 100.0, 1100.0 // units per second
	step := t0.Add(10 * time.Minute)
	total := func(at time.Time) uint64 {
		if at.Before(step) {
			return uint64(before * at.Sub(t0).Seconds())
		}
		return uint64(before*step.Sub(t0).Seconds() + after*at.Sub(step).Seconds())
	}
	for seed := range uint64(20) {
		r := rand.New(rand.NewPCG(73, seed))
		d, _ := NewDecayed(window)
		d.Update(total(t0), t0)
		times := append(irregular(r, t0, step, 100*time.Millisecond, 3*time.Second),
			irregular(r, step, step.Add(window), 100*time.Millisecond, 500*time.Millisecond)...)
		// Fixed-weight EMA for the mean spacing of 1.55 s.
		alpha := 1 - math.Exp(-1.55/window.Seconds())
		fixed, prev, prevAt := before, total(t0), t0
		var got float64
		for _, at := range times {
			got, _ = d.Update(total(at), at)
			rate := float64(total(at)-prev) / at.Sub(prevAt).Seconds()
			fixed += alpha * (rate - fixed)
			prev, prevAt = total(at), at
		}
		want := before + (after-before)*(1-1/math.E) // 732.12
		// Tolerance: the integer counter truncates each reading by under one
		// unit. Measured error is about 0.008 on a step of 1000; the
		// fixed-weight EMA misses by about 360.
		if math.Abs(got-want) > 0.05 {
			t.Fatalf("seed %d: one window after the step %v, want %v", seed, got, want)
		}
		if math.Abs(fixed-want) < 100 {
			t.Fatalf("seed %d: fixed-weight EMA %v is within 100 of %v: the spacing no longer shows the jitter error", seed, fixed, want)
		}
	}
}

func TestDecayedCounterReset(t *testing.T) {
	d, _ := NewDecayed(time.Second)
	d.Update(1000, t0)
	d.Update(2000, t0.Add(time.Second)) // rate 1000
	// The counter restarts: no rate, a new baseline, the average kept.
	if v, ok := d.Update(10, t0.Add(2*time.Second)); ok || v != 1000 || d.Resets() != 1 {
		t.Fatalf("reset: %v %v resets %d", v, ok, d.Resets())
	}
	// Measured from the new baseline: +10 over 1 s is rate 10, not
	// (10 − 2000)/1, and not the 2^64 − 1990 an unsigned subtraction gives.
	v, ok := d.Update(20, t0.Add(3*time.Second))
	if want := 1000 + (10-1000)*(1-math.Exp(-1)); !ok || relErr(v, want) > 1e-12 {
		t.Fatalf("after reset: %v %v, want %v", v, ok, want)
	}
}

// Totals near 2^64 differ by amounts float64 cannot see; the difference is
// taken in integers first.
func TestDecayedLargeTotals(t *testing.T) {
	d, _ := NewDecayed(time.Second)
	d.Update(math.MaxUint64-10, t0)
	if v, ok := d.Update(math.MaxUint64-5, t0.Add(time.Second)); !ok || v != 5 {
		t.Fatalf("%v %v, want 5", v, ok)
	}
	if float64(uint64(math.MaxUint64-5))-float64(uint64(math.MaxUint64-10)) != 0 {
		t.Fatal("float64 resolves these totals: the test no longer shows the integer difference matters")
	}
}

// Decayed takes no float input, so nothing can be NaN or Inf; what it skips
// are readings not after the one before.
func TestDecayedSkipsNonPositiveInterval(t *testing.T) {
	d, _ := NewDecayed(time.Second)
	d.Update(100, t0)
	if _, ok := d.Update(200, t0); ok {
		t.Fatal("zero interval produced a rate")
	}
	if _, ok := d.Update(300, t0.Add(-time.Second)); ok {
		t.Fatal("negative interval produced a rate")
	}
	if d.Skipped() != 2 || d.Resets() != 0 {
		t.Fatalf("skipped %d resets %d", d.Skipped(), d.Resets())
	}
	// The baseline is still (100, t0): +50 over 1 s is 50.
	if v, ok := d.Update(150, t0.Add(time.Second)); !ok || v != 50 {
		t.Fatalf("%v %v, want 50", v, ok)
	}
	d.Reset()
	if d.Skipped() != 0 || d.Window() != time.Second || !math.IsNaN(d.Value()) {
		t.Fatalf("Reset left %+v", d)
	}
	if _, ok := d.Update(0, t0); ok {
		t.Fatal("after Reset the first reading must be a baseline again")
	}
}

func TestDecayedRejects(t *testing.T) {
	for _, w := range []time.Duration{0, -time.Second} {
		if _, err := NewDecayed(w); !errors.Is(err, ErrInvalid) {
			t.Errorf("window %v: %v", w, err)
		}
	}
}
