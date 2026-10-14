package drift

import (
	"errors"
	"math"
	"testing"
)

func TestCUSUMKnownAnswer(t *testing.T) {
	// target 10, k 1, h 3.
	c, err := NewCUSUM(10, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range []struct {
		x, up, down float64
		since       int64
		want        Signal
	}{
		// S⁺ = max(0, S⁺ + x − 11), S⁻ = max(0, S⁻ + 9 − x).
		{12, 1, 0, 1, None}, // S⁺ 0+1 = 1
		{13, 3, 0, 2, None}, // S⁺ 1+2 = 3, at h is not over h
		{11, 3, 0, 3, None}, // S⁺ 3+0 = 3, still positive: the run goes on
		{12, 4, 0, 4, Up},   // S⁺ 3+1 = 4 > 3. Since 4: began at sample 1
		{12, 5, 0, 5, Up},   // a signal resets nothing
		{4, 0, 5, 1, Down},  // S⁺ max(0, 5−7) = 0, S⁻ 0+5 = 5 > 3
		{9, 0, 5, 2, Down},  // S⁻ 5+0 = 5
		{10, 0, 4, 3, Down}, // S⁻ 5−1 = 4
		{14, 3, 0, 1, None}, // S⁺ 0+3, S⁻ max(0, 4−5) = 0
	} {
		got := c.Add(s.x)
		if got != s.want || c.Upper() != s.up || c.Lower() != s.down || c.Since() != s.since {
			t.Fatalf("step %d x=%v: %v up %v down %v since %d, want %v %v %v %d",
				i, s.x, got, c.Upper(), c.Lower(), c.Since(), s.want, s.up, s.down, s.since)
		}
	}
	if c.N() != 9 {
		t.Fatalf("N %d", c.N())
	}
}

// Both sides over h at once needs a large swing with k = 0: S⁺ climbs to 10,
// then one low reading moves the same amount from S⁺ to S⁻.
func TestCUSUMBothSides(t *testing.T) {
	c, _ := NewCUSUM(0, 0, 3)
	c.Add(10)                                      // S⁺ 10
	if s := c.Add(-5); s != Up || c.Lower() != 5 { // S⁺ 5, S⁻ 5: tie goes to Up
		t.Fatalf("tie: %v up %v down %v", s, c.Upper(), c.Lower())
	}
	c.Reset()
	c.Add(10)
	if s := c.Add(-6); s != Down { // S⁺ 4, S⁻ 6: both over 3, Down larger
		t.Fatalf("both over: %v up %v down %v", s, c.Upper(), c.Lower())
	}
	if c.Since() != 1 { // the leading side is S⁻, positive for one sample
		t.Fatalf("since %d", c.Since())
	}
}

// Inputs at the edge of float64 cannot push a side to Inf, and a capped side
// drains like any other instead of signalling forever.
func TestCUSUMCapsAtMaxFloat(t *testing.T) {
	c, _ := NewCUSUM(0, 0, 1)
	c.Add(math.MaxFloat64)
	c.Add(math.MaxFloat64) // uncapped this would be Inf
	if c.Upper() != math.MaxFloat64 {
		t.Fatalf("upper %v, want MaxFloat64", c.Upper())
	}
	if big := math.MaxFloat64; big+big != math.Inf(1) {
		t.Fatal("the test no longer overflows")
	}
	// The same step that drains S⁺ fills S⁻, hence Down.
	if s := c.Add(-math.MaxFloat64); s != Down || c.Upper() != 0 {
		t.Fatalf("did not drain: %v upper %v", s, c.Upper())
	}
}

func TestCUSUMRejects(t *testing.T) {
	bad := []float64{math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, v := range bad {
		if _, err := NewCUSUM(v, 1, 1); !errors.Is(err, ErrInvalid) {
			t.Errorf("target %v: %v", v, err)
		}
	}
	for _, v := range append(bad, -1) {
		if _, err := NewCUSUM(0, v, 1); !errors.Is(err, ErrInvalid) {
			t.Errorf("k %v: %v", v, err)
		}
	}
	for _, v := range append(bad, 0, -1) {
		if _, err := NewCUSUM(0, 1, v); !errors.Is(err, ErrInvalid) {
			t.Errorf("h %v: %v", v, err)
		}
		if _, err := DefaultCUSUM(0, v); !errors.Is(err, ErrInvalid) {
			t.Errorf("default sigma %v: %v", v, err)
		}
	}
	if _, err := DefaultCUSUM(math.NaN(), 1); !errors.Is(err, ErrInvalid) {
		t.Errorf("default target NaN: %v", err)
	}
	if _, err := NewCUSUM(0, 0, 1); err != nil {
		t.Errorf("k = 0 is valid: %v", err)
	}
	c, _ := DefaultCUSUM(3, 2)
	if c.k != 1 || c.h != 10 || c.target != 3 {
		t.Errorf("DefaultCUSUM(3, 2) = %+v, want k 1 h 10", c)
	}
}

func TestCUSUMSkipsNonFinite(t *testing.T) {
	c, _ := NewCUSUM(0, 0, 1)
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if s := c.Add(x); s != None {
			t.Fatalf("%v signalled %v", x, s)
		}
	}
	if c.N() != 0 || c.Skipped() != 3 || c.Upper() != 0 || c.Lower() != 0 {
		t.Fatalf("%+v", c)
	}
	c.Add(5)
	c.Reset()
	if c.N() != 0 || c.Skipped() != 0 || c.Upper() != 0 || c.Since() != 0 || c.h != 1 {
		t.Fatalf("Reset left %+v", c)
	}
}
