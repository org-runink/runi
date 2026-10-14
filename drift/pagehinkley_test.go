package drift

import (
	"errors"
	"math"
	"testing"
)

func TestPageHinkleyKnownAnswer(t *testing.T) {
	// δ 0.5, λ 2. d = x − mean (mean including x).
	p, err := NewPageHinkley(0.5, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range []struct {
		x, up, down float64
		want        Signal
	}{
		// x=1: mean 1, d 0. m⁺ = −0.5, M⁺ = −0.5; m⁻ = 0.5, M⁻ = 0.5.
		{1, 0, 0, None},
		// x=3: mean 2, d 1. m⁺ = 0, M⁺ −0.5 → 0.5; m⁻ = 2, M⁻ 2 → 0.
		{3, 0.5, 0, None},
		// x=5: mean 3, d 2. m⁺ = 1.5 → 2, at λ is not over λ; m⁻ 4.5 = M⁻.
		{5, 2, 0, None},
		// x=7: mean 4, d 3. m⁺ = 4 → 4.5 > 2.
		{7, 4.5, 0, Up},
		// x=0: mean 3.2, d −3.2. m⁺ = 0.3 → 0.8; m⁻ = 8 − 2.7 = 5.3, M⁻ 8 → 2.7.
		{0, 0.8, 2.7, Down},
	} {
		got := p.Add(s.x)
		up, down := p.Statistic()
		if got != s.want || math.Abs(up-s.up) > 1e-12 || math.Abs(down-s.down) > 1e-12 {
			t.Fatalf("step %d x=%v: %v (%v, %v), want %v (%v, %v)", i, s.x, got, up, down, s.want, s.up, s.down)
		}
	}
	if p.N() != 5 {
		t.Fatalf("N %d", p.N())
	}
}

func TestPageHinkleyRejects(t *testing.T) {
	bad := []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1}
	for _, v := range bad {
		if _, err := NewPageHinkley(v, 1); !errors.Is(err, ErrInvalid) {
			t.Errorf("delta %v: %v", v, err)
		}
	}
	for _, v := range append(bad, 0) {
		if _, err := NewPageHinkley(0, v); !errors.Is(err, ErrInvalid) {
			t.Errorf("lambda %v: %v", v, err)
		}
	}
	if _, err := NewPageHinkley(0, 1); err != nil {
		t.Errorf("delta 0 is valid: %v", err)
	}
}

func TestPageHinkleySkipsNonFinite(t *testing.T) {
	p, _ := NewPageHinkley(0, 1)
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if s := p.Add(x); s != None {
			t.Fatalf("%v signalled %v", x, s)
		}
	}
	if p.N() != 0 || p.Skipped() != 3 {
		t.Fatalf("%+v", p)
	}
	p.Add(1)
	p.Add(9)
	p.Reset()
	if up, down := p.Statistic(); p.N() != 0 || p.Skipped() != 0 || up != 0 || down != 0 || p.lambda != 1 {
		t.Fatalf("Reset left %+v", p)
	}
}
