package drift

import (
	"errors"
	"math"
	"testing"
)

func TestEWMAKnownAnswer(t *testing.T) {
	e, err := NewEWMA(0.5)
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(e.Value()) {
		t.Fatal("value before the first sample must be NaN")
	}
	// 10 initialises; 20 → 10 + 0.5·10 = 15; 40 → 15 + 0.5·25 = 27.5;
	// 27.5 → unchanged.
	for i, c := range []struct{ x, want float64 }{{10, 10}, {20, 15}, {40, 27.5}, {27.5, 27.5}} {
		e.Add(c.x)
		if e.Value() != c.want {
			t.Fatalf("step %d: value %v want %v", i, e.Value(), c.want)
		}
	}
	if e.N() != 4 || e.Alpha() != 0.5 {
		t.Fatalf("N %d alpha %v", e.N(), e.Alpha())
	}
}

func TestNewEWMAHalfLife(t *testing.T) {
	// samples = 1: alpha = 1 − 2^−1 = 0.5.
	e, err := NewEWMAHalfLife(1)
	if err != nil || relErr(e.Alpha(), 0.5) > 1e-15 {
		t.Fatalf("alpha %v err %v", e.Alpha(), err)
	}
	// The defining property: after `samples` samples the first value's
	// weight (1−alpha)^samples is one half. Start at 1, feed zeros.
	e, _ = NewEWMAHalfLife(10)
	e.Add(1)
	for range 10 {
		e.Add(0)
	}
	if relErr(e.Value(), 0.5) > 1e-12 {
		t.Fatalf("after one half-life the first value weighs %v, want 0.5", e.Value())
	}
	// A very long half-life: 1 − 2^(−1/s) rounds to exactly 0 in the naive
	// form, which would be refused as an invalid alpha. expm1 keeps it.
	const s = 1e17
	if naive := 1 - math.Pow(2, -1/s); naive != 0 {
		t.Fatalf("naive alpha %v: the test no longer exercises the cancellation", naive)
	}
	e, err = NewEWMAHalfLife(s)
	if err != nil || relErr(e.Alpha(), math.Ln2/s) > 1e-9 {
		t.Fatalf("alpha %v err %v, want ln2/s", e.Alpha(), err)
	}
}

func TestEWMARejects(t *testing.T) {
	for _, a := range []float64{0, -0.1, 1.0000001, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := NewEWMA(a); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewEWMA(%v) err %v", a, err)
		}
	}
	if _, err := NewEWMA(1); err != nil {
		t.Errorf("alpha 1 is valid: %v", err)
	}
	for _, s := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := NewEWMAHalfLife(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewEWMAHalfLife(%v) err %v", s, err)
		}
	}
}

func TestEWMASkipsNonFinite(t *testing.T) {
	e, _ := NewEWMA(0.5)
	e.Add(math.NaN()) // must not be taken as the initialising value
	e.Add(4)
	e.Add(math.Inf(1))
	e.Add(math.Inf(-1))
	if e.Value() != 4 || e.N() != 1 || e.Skipped() != 3 {
		t.Fatalf("value %v N %d skipped %d", e.Value(), e.N(), e.Skipped())
	}
	e.Reset()
	if e.N() != 0 || e.Skipped() != 0 || e.Alpha() != 0.5 || !math.IsNaN(e.Value()) {
		t.Fatalf("Reset left %+v", e)
	}
}
