package sustain

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

func mustHysteresis(t testing.TB, dir Direction, set, clear float64, dwell time.Duration) Hysteresis {
	t.Helper()
	h, err := NewHysteresis(dir, set, clear, dwell)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// feed offers xs one second apart starting at t0 + start seconds and returns
// the state after each.
func feedH(h *Hysteresis, start float64, xs ...float64) []bool {
	out := make([]bool, len(xs))
	for i, x := range xs {
		out[i] = h.Update(x, ts(start+float64(i)))
	}
	return out
}

func eqBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestHysteresisKnownAnswer(t *testing.T) {
	// High: on at ≥ 90, off at ≤ 80; equality counts.
	h := mustHysteresis(t, High, 90, 80, 0)
	got := feedH(&h, 0, 85, 89.9, 90, 85, 80.1, 80, 85, 95)
	want := []bool{false, false, true, true, true, false, false, true}
	if !eqBools(got, want) {
		t.Fatalf("High: %v, want %v", got, want)
	}
	if h.Transitions() != 3 || !h.Since().Equal(ts(7)) {
		t.Fatalf("transitions %d since %v", h.Transitions(), h.Since())
	}
	// Low: on at ≤ 10, off at ≥ 20.
	l := mustHysteresis(t, Low, 10, 20, 0)
	got = feedH(&l, 0, 15, 10.1, 10, 19.9, 20, 15, 5)
	want = []bool{false, false, true, true, false, false, true}
	if !eqBools(got, want) {
		t.Fatalf("Low: %v, want %v", got, want)
	}
	// The first reading is judged like any other.
	f := mustHysteresis(t, High, 90, 80, time.Hour)
	if !f.Update(100, t0) || f.Transitions() != 1 {
		t.Fatal("a first reading past set did not turn the state on at once")
	}
}

func TestHysteresisInvalid(t *testing.T) {
	for _, c := range []struct {
		dir        Direction
		set, clear float64
		dwell      time.Duration
	}{
		{0, 2, 1, 0},
		{3, 2, 1, 0},
		{High, math.NaN(), 1, 0},
		{High, math.Inf(1), 1, 0},
		{High, 2, math.NaN(), 0},
		{High, 2, math.Inf(-1), 0},
		{High, 1, 1, 0},
		{High, 1, 2, 0},
		{Low, 2, 1, 0},
		{Low, 1, 1, 0},
		{High, 2, 1, -1},
	} {
		if _, err := NewHysteresis(c.dir, c.set, c.clear, c.dwell); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewHysteresis(%v, %v, %v, %v): %v, want ErrInvalid", c.dir, c.set, c.clear, c.dwell, err)
		}
	}
	if _, err := NewHysteresis(Low, 1, 2, 0); err != nil {
		t.Fatal(err)
	}
}

func TestDirectionString(t *testing.T) {
	for d, want := range map[Direction]string{High: "high", Low: "low", 0: "Direction(0)", -3: "Direction(-3)"} {
		if got := d.String(); got != want {
			t.Errorf("%d: %q, want %q", int(d), got, want)
		}
	}
}

// The package doc's central claim: noise whose peak-to-peak amplitude is
// below the deadband causes at most one change, wherever its mean sits —
// below clear, between the levels, on either level, or above set. A single
// threshold at the midpoint, fed the same series, flaps; that half of the
// test shows the series really does cross and the check could fail.
func TestHysteresisNoFlapUnderNoise(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	const set, clear = 90.0, 80.0
	comparatorFlaps := 0
	for range 2000 / raceScale {
		dir := Direction(1 + r.IntN(2))
		s, c := set, clear
		if dir == Low {
			s, c = clear, set
		}
		h := mustHysteresis(t, dir, s, c, 0)
		mean := 70 + 30*r.Float64()
		amp := 0.4999 * (set - clear) * r.Float64() // peak-to-peak < set − clear
		mid, prev := (set+clear)/2, false
		for i := range 500 {
			x := mean + amp*(2*r.Float64()-1)
			h.Update(x, ts(float64(i)))
			if above := x >= mid; i > 0 && above != prev {
				comparatorFlaps++
			}
			prev = x >= mid
		}
		if h.Transitions() > 1 {
			t.Fatalf("%v mean %v amp %v: %d transitions, want at most 1", dir, mean, amp, h.Transitions())
		}
	}
	if comparatorFlaps < 1000 {
		t.Fatalf("a single threshold flapped only %d times; the noise does not test the claim", comparatorFlaps)
	}
}

// Dwell: two changes are never closer than the dwell, the readings it holds
// back are counted, and the change happens at the first reading after the
// dwell that is itself past the opposite level.
func TestHysteresisDwell(t *testing.T) {
	h := mustHysteresis(t, High, 1, 0, 10*time.Second)
	// on at 0; 0 at 3 s and 9 s held; 0.5 at 10 s is inside the deadband, so
	// nothing happens although the dwell has passed; 0 at 11 s turns off.
	got := feedH(&h, 0, 1)
	got = append(got, h.Update(0, ts(3)), h.Update(0, ts(9)), h.Update(0.5, ts(10)), h.Update(0, ts(11)))
	want := []bool{true, true, true, true, false}
	if !eqBools(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
	if h.Held() != 2 || h.Transitions() != 2 || !h.Since().Equal(ts(11)) {
		t.Fatalf("held %d transitions %d since %v", h.Held(), h.Transitions(), h.Since().Sub(t0))
	}
	// Exactly at the dwell is allowed.
	if !h.Update(1, ts(21)) {
		t.Fatal("a change exactly one dwell after the last was refused")
	}

	r := rand.New(rand.NewPCG(7, 8))
	held := int64(0)
	for range 200 / raceScale {
		dwell := time.Duration(1+r.IntN(20)) * time.Second
		h := mustHysteresis(t, High, 1, -1, dwell)
		var last time.Time
		at := t0
		for range 400 {
			at = at.Add(time.Duration(1+r.IntN(3000)) * time.Millisecond)
			n := h.Transitions()
			h.Update(float64(r.IntN(5)-2), at) // crosses both levels constantly
			if h.Transitions() > n {
				if n > 0 && at.Sub(last) < dwell {
					t.Fatalf("changes %v apart, dwell %v", at.Sub(last), dwell)
				}
				last = at
			}
		}
		held += h.Held()
	}
	if held == 0 {
		t.Fatal("the dwell never held a change back; the test proves nothing")
	}
}

func TestHysteresisSinceInStateReset(t *testing.T) {
	h := mustHysteresis(t, High, 2, 1, time.Second)
	if h.InState(ts(5)) != 0 || !h.Since().IsZero() || h.Active() {
		t.Fatal("not empty before the first reading")
	}
	h.Update(0, ts(1))
	if !h.Since().Equal(ts(1)) || h.InState(ts(4)) != 3*time.Second || h.InState(ts(0)) != 0 {
		t.Fatalf("since %v in state %v", h.Since(), h.InState(ts(4)))
	}
	h.Update(3, ts(2))
	h.Update(0, ts(2.5))
	h.Update(math.NaN(), ts(3))
	if !h.Active() || h.Held() != 1 || h.Skipped() != 1 || h.Transitions() != 1 {
		t.Fatalf("active %v held %d skipped %d transitions %d", h.Active(), h.Held(), h.Skipped(), h.Transitions())
	}
	h.Reset()
	fresh := mustHysteresis(t, High, 2, 1, time.Second)
	if h != fresh {
		t.Fatalf("after Reset %+v, want %+v", h, fresh)
	}
}
