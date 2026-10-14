package rate

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func relErr(got, want float64) float64 {
	if want == 0 {
		return math.Abs(got)
	}
	return math.Abs(got-want) / math.Abs(want)
}

func mustCounter(t testing.TB, width uint) Counter {
	t.Helper()
	c, err := NewCounter(width)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// want checks one Update against an expected Reading.
func want(t *testing.T, c *Counter, total uint64, at time.Time, delta uint64, ok bool) Reading {
	t.Helper()
	r, gotOK := c.Update(total, at)
	if gotOK != ok || r.Delta != delta {
		t.Fatalf("Update(%d, +%v) = %+v, %v; want delta %d, %v", total, at.Sub(t0), r, gotOK, delta, ok)
	}
	if !ok && r != (Reading{}) {
		t.Fatalf("Update(%d) not ok but returned %+v", total, r)
	}
	return r
}

func TestCounterKnownAnswer(t *testing.T) {
	c := mustCounter(t, 64)
	want(t, &c, 1000, t0, 0, false) // baseline
	r := want(t, &c, 1500, t0.Add(2*time.Second), 500, true)
	if r.Interval != 2*time.Second || r.PerSecond != 250 {
		t.Fatalf("%+v, want 500 over 2s = 250/s", r)
	}
	// No change is a valid reading with rate 0.
	r = want(t, &c, 1500, t0.Add(3*time.Second), 0, true)
	if r.PerSecond != 0 || r.Interval != time.Second {
		t.Fatalf("%+v", r)
	}
	// Near 2^64 the integer difference is exact where float64 would lose it.
	c.Reset()
	want(t, &c, math.MaxUint64-10, t0, 0, false)
	if r = want(t, &c, math.MaxUint64-3, t0.Add(time.Second), 7, true); r.PerSecond != 7 {
		t.Fatalf("%+v", r)
	}
	if c.Width() != 64 || c.Resets() != 0 || c.Wraps() != 0 || c.Skipped() != 0 || c.Invalid() != 0 {
		t.Fatalf("counts %+v", c)
	}
}

// Irregular intervals: each reading's rate is its own increase over its own
// interval, and nothing carries over between intervals.
func TestCounterIrregular(t *testing.T) {
	c := mustCounter(t, 32)
	steps := []struct {
		after time.Duration
		total uint64
		rate  float64
	}{
		{0, 0, 0},
		{100 * time.Millisecond, 10, 100},
		{1100 * time.Millisecond, 10, 0},
		{1350 * time.Millisecond, 1010, 4000},
		{4350 * time.Millisecond, 1013, 1},
	}
	for i, s := range steps {
		r, ok := c.Update(s.total, t0.Add(s.after))
		if ok != (i > 0) || ok && relErr(r.PerSecond, s.rate) > 1e-15 {
			t.Fatalf("step %d: %+v %v, want rate %v", i, r, ok, s.rate)
		}
	}
}

// PerSecond is Delta over the interval at full nanosecond resolution: an
// interval of 1 s + 1 ns is not rounded to 1 s. The exact quotient is
// computed in big.Rat and PerSecond must be within two roundings of it. An
// implementation that dropped the nanosecond would give 1.000000001e9 for the
// first case, a relative error of 1e-9, far outside the tolerance.
func TestCounterNanosecondInterval(t *testing.T) {
	for _, tc := range []struct {
		delta uint64
		dt    time.Duration
	}{
		{1_000_000_001, time.Second + time.Nanosecond}, // exactly 1e9 per s
		{1, time.Nanosecond},
		{3, 7 * time.Nanosecond},
		{1 << 40, 1234567891 * time.Nanosecond},
		{math.MaxUint64, time.Nanosecond},
		{1, math.MaxInt64},
		{12345, 3*time.Hour + 17*time.Nanosecond},
	} {
		c := mustCounter(t, 64)
		c.Update(0, t0)
		r, ok := c.Update(tc.delta, t0.Add(tc.dt))
		exact, _ := new(big.Rat).SetFrac(
			new(big.Int).Mul(new(big.Int).SetUint64(tc.delta), big.NewInt(1e9)),
			big.NewInt(int64(tc.dt)),
		).Float64()
		if !ok || r.Interval != tc.dt || relErr(r.PerSecond, exact) > 4e-16 {
			t.Errorf("delta %d over %v: %+v, want %v", tc.delta, tc.dt, r, exact)
		}
	}
}

// Wraps at widths 8 and 32: a decrease implying an increase of at most half
// the range is a wrap, with the modular increase.
func TestCounterWrap(t *testing.T) {
	c := mustCounter(t, 8)
	want(t, &c, 250, t0, 0, false)
	// 250 → 255 → 0 → 4: up 10.
	r := want(t, &c, 4, t0.Add(time.Second), 10, true)
	if r.PerSecond != 10 || c.Wraps() != 1 || c.Resets() != 0 {
		t.Fatalf("%+v wraps %d resets %d", r, c.Wraps(), c.Resets())
	}
	// The baseline is now 4.
	want(t, &c, 14, t0.Add(2*time.Second), 10, true)

	c32 := mustCounter(t, 32)
	want(t, &c32, math.MaxUint32-99, t0, 0, false)
	want(t, &c32, 900, t0.Add(time.Second), 1000, true)
	if c32.Wraps() != 1 {
		t.Fatalf("wraps %d", c32.Wraps())
	}
}

// The half-range boundary, exactly, on both sides: an implied increase of
// 2^(w−1) is a wrap, of 2^(w−1) + 1 a reset.
func TestCounterHalfRangeBoundary(t *testing.T) {
	for _, w := range []uint{1, 2, 8, 16, 32, 63} {
		mod := new(big.Int).Lsh(big.NewInt(1), w)
		half := uint64(1) << (w - 1)
		mask := ^uint64(0) >> (64 - w)
		// prev = mask, total = half − 1: increase (2^w − mask) + half − 1 = half.
		c := mustCounter(t, w)
		c.Update(mask, t0)
		r, ok := c.Update(half-1, t0.Add(time.Second))
		if !ok || r.Delta != half || c.Wraps() != 1 || c.Resets() != 0 {
			t.Fatalf("w=%d at half: %+v %v wraps %d", w, r, ok, c.Wraps())
		}
		// Same arithmetic in big.Int: (2^w − prev) + total.
		d := new(big.Int).Sub(mod, new(big.Int).SetUint64(mask))
		d.Add(d, new(big.Int).SetUint64(half-1))
		if d.Uint64() != half {
			t.Fatalf("w=%d: modular arithmetic %v, want %d", w, d, half)
		}
		// One more: prev = mask − 1 makes the increase half + 1, a reset.
		if w == 1 {
			continue // 1 bit: the only fall, 1 → 0, implies an increase of exactly half
		}
		c = mustCounter(t, w)
		c.Update(mask-1, t0)
		r, ok = c.Update(half-1, t0.Add(time.Second))
		if ok || c.Resets() != 1 || c.Wraps() != 0 {
			t.Fatalf("w=%d above half: %+v %v resets %d", w, r, ok, c.Resets())
		}
		// The reset made half − 1 the baseline.
		if r, ok = c.Update(half, t0.Add(2*time.Second)); !ok || r.Delta != 1 {
			t.Fatalf("w=%d after reset: %+v %v", w, r, ok)
		}
	}
}

// A 64-bit counter never wraps: every decrease is a reset, even by one.
func TestCounterWidth64AlwaysResets(t *testing.T) {
	c := mustCounter(t, 64)
	c.Update(math.MaxUint64, t0)
	if _, ok := c.Update(0, t0.Add(time.Second)); ok || c.Resets() != 1 || c.Wraps() != 0 {
		t.Fatalf("MaxUint64 → 0 gave a rate; resets %d wraps %d", c.Resets(), c.Wraps())
	}
	c.Update(10, t0.Add(2*time.Second))
	if _, ok := c.Update(9, t0.Add(3*time.Second)); ok || c.Resets() != 2 {
		t.Fatal("10 → 9 gave a rate")
	}
	if r, ok := c.Update(20, t0.Add(4*time.Second)); !ok || r.Delta != 11 {
		t.Fatalf("%+v %v, want +11 from the new baseline 9", r, ok)
	}
}

func TestCounterReset(t *testing.T) {
	c := mustCounter(t, 32)
	c.Update(5_000_000, t0)
	c.Update(5_001_000, t0.Add(time.Second))
	// The device restarted: a small fall is a reset, not a wrap of ~2^32.
	want(t, &c, 40, t0.Add(2*time.Second), 0, false)
	if c.Resets() != 1 || c.Wraps() != 0 {
		t.Fatalf("resets %d wraps %d", c.Resets(), c.Wraps())
	}
	want(t, &c, 140, t0.Add(3*time.Second), 100, true)

	c.Reset()
	if c.Resets() != 0 || c.Width() != 32 {
		t.Fatalf("Reset left %+v", c)
	}
	want(t, &c, 7, t0, 0, false) // a baseline again
	want(t, &c, 8, t0.Add(time.Second), 1, true)
}

// Readings not after the previous one are skipped and keep the baseline.
func TestCounterNonPositiveInterval(t *testing.T) {
	c := mustCounter(t, 64)
	c.Update(100, t0)
	want(t, &c, 200, t0, 0, false)                   // duplicate time
	want(t, &c, 300, t0.Add(-time.Second), 0, false) // clock stepped back
	want(t, &c, 0, t0.Add(-time.Second), 0, false)   // and not a reset either
	if c.Skipped() != 3 || c.Resets() != 0 {
		t.Fatalf("skipped %d resets %d", c.Skipped(), c.Resets())
	}
	if r := want(t, &c, 150, t0.Add(time.Second), 50, true); r.Interval != time.Second {
		t.Fatalf("%+v", r)
	}
}

// A total that cannot come from a counter of the width is invalid, counted,
// and leaves the baseline alone, before it could be a baseline itself.
func TestCounterInvalidTotal(t *testing.T) {
	c := mustCounter(t, 8)
	want(t, &c, 256, t0, 0, false) // not a baseline
	want(t, &c, 10, t0.Add(time.Second), 0, false)
	want(t, &c, 1<<40, t0.Add(2*time.Second), 0, false)
	want(t, &c, 30, t0.Add(3*time.Second), 20, true)
	if c.Invalid() != 2 || c.Resets() != 0 || c.Skipped() != 0 {
		t.Fatalf("invalid %d resets %d skipped %d", c.Invalid(), c.Resets(), c.Skipped())
	}
	// Invalid takes precedence over a bad time.
	want(t, &c, 300, t0, 0, false)
	if c.Invalid() != 3 || c.Skipped() != 0 {
		t.Fatalf("invalid %d skipped %d", c.Invalid(), c.Skipped())
	}
}

func TestCounterRejects(t *testing.T) {
	for _, w := range []uint{0, 65, 128, math.MaxUint} {
		if _, err := NewCounter(w); !errors.Is(err, ErrInvalid) {
			t.Errorf("width %d: %v", w, err)
		}
	}
	for w := uint(1); w <= 64; w++ {
		if _, err := NewCounter(w); err != nil {
			t.Errorf("width %d: %v", w, err)
		}
	}
}

// From a known series of true increments, each below half the range, the
// counter is read modulo 2^w at irregular times; wraps happen wherever the
// running total crosses a multiple of 2^w, sometimes several in a row.
// Every increment comes back exactly, and the wrap count equals the number of
// multiples crossed.
func TestCounterRecoversIncrements(t *testing.T) {
	for _, w := range []uint{1, 3, 8, 16} {
		for seed := range uint64(20) {
			r := rand.New(rand.NewPCG(uint64(w), seed))
			c := mustCounter(t, w)
			half := uint64(1) << (w - 1)
			mask := ^uint64(0) >> (64 - w)
			truth := r.Uint64N(1 << 20) // a running total, never wrapped
			at := t0
			c.Update(truth&mask, at)
			var wraps int64
			for i := range 2000 {
				inc := r.Uint64N(half + 1) // 0..half, the boundary included
				if i%7 == 0 {
					inc = half
				}
				wraps += int64((truth+inc)>>w - truth>>w)
				truth += inc
				dt := time.Duration(1 + r.Int64N(int64(3*time.Second)))
				at = at.Add(dt)
				got, ok := c.Update(truth&mask, at)
				if !ok || got.Delta != inc || got.Interval != dt {
					t.Fatalf("w=%d seed=%d step %d: %+v %v, want +%d over %v", w, seed, i, got, ok, inc, dt)
				}
				if exact := float64(inc) / dt.Seconds(); got.PerSecond != exact {
					t.Fatalf("w=%d: PerSecond %v, want %v", w, got.PerSecond, exact)
				}
			}
			// An increment below the modulus crosses at most one multiple,
			// and every crossing reads as a fall: one wrap each.
			if c.Wraps() != wraps || c.Resets() != 0 {
				t.Fatalf("w=%d seed=%d: wraps %d (crossings %d) resets %d", w, seed, c.Wraps(), wraps, c.Resets())
			}
		}
	}
}
