package season

import (
	"math"
	"math/rand/v2"
	"testing"
)

// spiked is a trending weekly series with one bad reading at index 40. The
// construction is the one PULSE reported the defect with, kept verbatim so the
// regression it covers stays the regression it was reported as.
func spiked(amp float64) []float64 {
	r := rand.New(rand.NewPCG(1, 7))
	x := make([]float64, 91)
	for i := range x {
		x[i] = 100 + 0.5*float64(i) + 10*math.Sin(2*math.Pi*float64(i)/7) + r.NormFloat64()*0.5
	}
	x[40] += amp
	return x
}

// One bad reading must not hide a weekly season, and must not be reported as a
// structural break. Before the Hampel filter went in, amp=20 upward invented
// breaks, and amp=60 reported no season at all — the two most expensive wrong
// answers this package can give, from a single point in 91.
func TestOneBadReadingChangesNothing(t *testing.T) {
	for _, amp := range []float64{0, 20, 60, 200, 1000} {
		x := spiked(amp)

		if got := Period(x, 0); got != 7 {
			t.Errorf("amp=%g: Period = %d, want 7", amp, got)
		}
		if got := Period(x, 14); got != 7 {
			t.Errorf("amp=%g: Period(maxLag=14) = %d, want 7", amp, got)
		}
		if got := Changepoints(x, 5); len(got) != 0 {
			t.Errorf("amp=%g: Changepoints = %v, want none", amp, got)
		}

		d, err := Decompose(x, Options{})
		if err != nil {
			t.Fatalf("amp=%g: Decompose: %v", amp, err)
		}
		if d.Period != 7 {
			t.Errorf("amp=%g: Decompose period = %d, want 7", amp, d.Period)
		}
		if len(d.Changepoints) != 0 {
			t.Errorf("amp=%g: Decompose changepoints = %v, want none", amp, d.Changepoints)
		}
	}
}

// The spike is filtered out of what the estimators SCORE on, not out of the
// output. A caller looking for the bad day must still find it in the residual,
// which is the whole reason the filter is not applied to the series itself.
func TestOutlierSurvivesInResidual(t *testing.T) {
	const amp = 60
	d, err := Decompose(spiked(amp), Options{})
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	worst, at := 0.0, -1
	for i, r := range d.Residual {
		if math.Abs(r) > worst {
			worst, at = math.Abs(r), i
		}
	}
	if at != 40 {
		t.Errorf("largest residual at %d, want 40", at)
	}
	if worst < amp/2 {
		t.Errorf("largest residual %.1f, want at least %.1f — the spike was "+
			"smoothed away instead of being left for the caller to see", worst, amp/2.0)
	}
}

// A shift that LASTS is a break, not an outlier, and the filter must not erase
// it. This is why the level comes from a local window: a global clip would
// remove a short step along with the spike.
func TestSustainedShiftSurvivesTheFilter(t *testing.T) {
	for _, run := range []int{4, 6, 10, 30} {
		y := series(100, 10, 0, 0, 0, 1, 3)
		for i := 60; i < 60+run; i++ {
			y[i] += 8
		}
		cps := Changepoints(y, 4)
		found := false
		for _, c := range cps {
			if abs(c-60) <= 2 {
				found = true
			}
		}
		if !found {
			t.Errorf("run of %d points: changepoints %v, want one near 60", run, cps)
		}
	}
}

// The filter must leave data with nothing wrong with it essentially alone. A
// filter that quietly rewrites clean input is worse than no filter: it lowers
// the noise estimate the break search prices splits against, and the search
// then invents breaks in pure noise. That is a defect this package shipped
// briefly, from taking the scale from the seven-point window instead of from
// the whole series — seven points underestimate σ often enough that a nominal
// 4σ test fired on about one point in 37.
//
// The bound is a rate over many seeds rather than a per-series count, because
// a per-series count is a coin flip and would make this test flake. Measured
// here: 0.06% on a noisy line, 0.05% on pure noise, and 0% on a seasonal
// series, whose own swing raises the scale well above any single point. The
// assertion allows an order of magnitude more than that.
func TestHampelLeavesCleanDataAlone(t *testing.T) {
	for _, c := range []struct {
		name string
		mk   func(uint64) []float64
	}{
		{"noisy line", func(s uint64) []float64 { return series(150, 5, 0.3, 0, 0, 1, s) }},
		{"pure noise", func(s uint64) []float64 { return series(150, 0, 0, 0, 0, 1, s) }},
		{"seasonal", func(s uint64) []float64 { return series(150, 10, 0, 6, 7, 0.5, s) }},
	} {
		moved, total, worst := 0, 0, 0
		for seed := uint64(1); seed <= 200; seed++ {
			y := c.mk(seed)
			m := 0
			for i, f := range hampel(y) {
				if f != y[i] {
					m++
				}
			}
			moved, total = moved+m, total+len(y)
			if m > worst {
				worst = m
			}
		}
		if rate := float64(moved) / float64(total); rate > 0.005 {
			t.Errorf("%s: filter moved %d of %d clean points (%.3f%%), want under 0.5%%",
				c.name, moved, total, 100*rate)
		}
		if worst > 4 {
			t.Errorf("%s: filter moved %d points in a single clean series, want at most 4",
				c.name, worst)
		}
	}
}

// An exactly piecewise-linear series has no noise to estimate a scale from.
// The filter must stand aside rather than divide by zero or shave the corner.
func TestHampelNoiselessSeries(t *testing.T) {
	y := make([]float64, 60)
	for i := range y {
		y[i] = float64(i)
		if i >= 30 {
			y[i] = float64(30 + 3*(i-30))
		}
	}
	for i, f := range hampel(y) {
		if f != y[i] {
			t.Errorf("index %d: noiseless series altered %g -> %g", i, y[i], f)
		}
	}
}

// Too few points to fill the comparison window: the filter stands aside rather
// than judging a point against a window it does not have.
func TestHampelShortSeries(t *testing.T) {
	for n := 0; n <= 2*hampelHalf+1; n++ {
		y := make([]float64, n)
		for i := range y {
			y[i] = float64(i % 3)
		}
		y = append(y[:0:0], y...)
		if n > 3 {
			y[n/2] = 500 // an obvious outlier that must NOT be touched
		}
		for i, f := range hampel(y) {
			if f != y[i] {
				t.Errorf("n=%d index %d: altered %g -> %g", n, i, y[i], f)
			}
		}
	}
}
