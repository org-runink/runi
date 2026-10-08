package season

import (
	"testing"
)

// A maxPeriod so small that no lag survives means there is nothing to look
// for, which is a 0, not an error.
func TestPeriodMaxPeriodTooSmall(t *testing.T) {
	y := series(40, 10, 0, 5, 7, 0.5, 1)
	if got := Period(y, 1); got != 0 {
		t.Errorf("Period(maxPeriod=1) = %d, want 0", got)
	}
}

// Asking for a season far longer than the data is the mistake behind most
// "why is my yearly seasonality nonsense" reports: a handful of daily points
// cannot identify a thousand-point cycle, because every basis column is flat
// across the window and indistinguishable from the intercept. The design is
// singular and must be refused, not solved approximately.
func TestFitFourierCannotIdentifyPeriod(t *testing.T) {
	for _, c := range []struct{ n, period, harmonics int }{
		{8, 1000, 3}, {10, 10000, 4}, {20, 1000, 3},
	} {
		y := series(c.n, 10, 0.5, 0, 0, 1, 1)
		if _, err := FitFourier(y, c.period, c.harmonics); err == nil {
			t.Errorf("n=%d period=%d: fitted a season it cannot identify", c.n, c.period)
		}
	}
}

// Enough breaks to split the series into segments shorter than a line can be
// fitted to. The search must leave those alone rather than fit a line to three
// points and call it a trend.
func TestChangepointsStopsAtUnsplittableSegments(t *testing.T) {
	y := make([]float64, 24)
	for i := range y {
		y[i] = float64((i / 4) * 20)
	}
	cps := Changepoints(y, 8)
	if len(cps) != 5 {
		t.Fatalf("changepoints = %v, want the five step boundaries", cps)
	}
	for _, c := range cps {
		if c%4 != 0 {
			t.Errorf("changepoint %d is not on a step boundary", c)
		}
	}
}

// One point carries no slope. The line through it is its own value, flat.
func TestFitLinesSinglePoint(t *testing.T) {
	got := fitLines([]float64{7}, nil)
	if len(got) != 1 {
		t.Fatalf("lines = %+v", got)
	}
	if got[0].a != 7 || got[0].b != 0 {
		t.Errorf("line = %+v, want intercept 7 and slope 0", got[0])
	}
}

func TestMedianEvenLength(t *testing.T) {
	if got := median([]float64{1, 2, 3, 4}); got != 2.5 {
		t.Errorf("median = %v, want 2.5", got)
	}
	if got := median([]float64{5}); got != 5 {
		t.Errorf("median = %v, want 5", got)
	}
}

// A two-point cycle is the shortest there is, so the refinement has no shorter
// neighbour to compare against and must skip it rather than ask for a period
// of one, which is not a season at all.
func TestPeriodTwoHasNoShorterNeighbour(t *testing.T) {
	y := make([]float64, 60)
	for i := range y {
		y[i] = float64(i%2) * 10
	}
	if got := Period(y, 0); got != 2 {
		t.Errorf("Period of an alternating series = %d, want 2", got)
	}
}

// The refinement also has no longer neighbour to try when the peak sits at the
// longest lag it was allowed to consider.
func TestPeriodAtMaxLagHasNoLongerNeighbour(t *testing.T) {
	y := series(60, 10, 0, 6, 7, 0.4, 3)
	if got := Period(y, 7); got != 7 {
		t.Errorf("Period(maxPeriod=7) = %d, want 7", got)
	}
}

func TestMedianOfEvenLength(t *testing.T) {
	v := []float64{4, 1, 3, 2}
	if got := medianOf(v); got != 2.5 {
		t.Errorf("medianOf = %v, want 2.5", got)
	}
	if v[0] != 4 {
		t.Error("medianOf reordered the caller's slice")
	}
	if got := medianOf([]float64{9, 1, 5}); got != 5 {
		t.Errorf("medianOf odd = %v, want 5", got)
	}
}
