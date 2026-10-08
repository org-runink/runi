package arimax

import (
	"errors"
	"math"
	"testing"
)

// These cover the guards and branches the accuracy and round-trip tests never
// reach: bad arguments, degenerate input, and the tails of the normal inverse.
// Each one asserts the documented behaviour, not merely that the line ran.

func TestFitRejectsBadArguments(t *testing.T) {
	y := make([]float64, 50)
	for i := range y {
		y[i] = float64(i%7) + 1
	}

	cases := []struct {
		name string
		y    []float64
		x    []float64
		k    int
		ord  Order
		want error
	}{
		{"negative P", y, nil, 0, Order{P: -1}, errOrder},
		{"negative D", y, nil, 0, Order{D: -1}, errOrder},
		{"negative Q", y, nil, 0, Order{Q: -1}, errOrder},
		{"series shorter than the order", y[:4], nil, 0, Order{P: 1, Q: 1}, errShort},
		{"exog rows do not match", y, make([]float64, 10), 1, Order{P: 1}, errExogLen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Fit(tc.y, tc.x, tc.k, tc.ord)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Fit error = %v; want %v", err, tc.want)
			}
		})
	}
}

func TestForecastGuards(t *testing.T) {
	y := make([]float64, 60)
	x := make([]float64, 60)
	for i := range y {
		x[i] = float64(i % 5)
		y[i] = 1 + 2*x[i] + float64(i%3)*0.1
	}
	m, err := Fit(y, x, 1, Order{P: 1, D: 0, Q: 0})
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}

	t.Run("h<=0 returns nothing and no error", func(t *testing.T) {
		pt, lo, hi, err := m.Forecast(0, nil, 0.05)
		if err != nil || pt != nil || lo != nil || hi != nil {
			t.Fatalf("Forecast(0) = %v,%v,%v,%v; want all nil", pt, lo, hi, err)
		}
	})

	t.Run("missing future X is an error, not a guess", func(t *testing.T) {
		if _, _, _, err := m.Forecast(3, nil, 0.05); !errors.Is(err, errFuture) {
			t.Fatalf("Forecast without xFuture err = %v; want errFuture", err)
		}
		// Right length is required too, not merely non-nil.
		if _, _, _, err := m.Forecast(3, []float64{1, 2}, 0.05); !errors.Is(err, errFuture) {
			t.Fatalf("Forecast with short xFuture err = %v; want errFuture", err)
		}
	})
}

func TestSeasonalDifference(t *testing.T) {
	y := []float64{1, 2, 3, 10, 12, 14, 20, 23}

	got := SeasonalDifference(y, 3)
	want := []float64{9, 10, 11, 10, 11}
	if len(got) != len(want) {
		t.Fatalf("len = %d; want %d", len(got), len(want))
	}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Fatalf("SeasonalDifference[%d] = %v; want %v", i, got[i], want[i])
		}
	}

	// A seasonal difference of a purely seasonal series is flat: that is the
	// property the function exists for.
	seasonal := make([]float64, 24)
	for i := range seasonal {
		seasonal[i] = float64(i % 12)
	}
	flat := SeasonalDifference(seasonal, 12)
	for i, v := range flat {
		if math.Abs(v) > 1e-12 {
			t.Fatalf("seasonal diff[%d] = %v; want 0", i, v)
		}
	}

	for _, tc := range []struct{ m, n int }{{0, 5}, {-1, 5}, {5, 5}, {9, 5}} {
		if got := SeasonalDifference(make([]float64, tc.n), tc.m); got != nil {
			t.Fatalf("SeasonalDifference(len %d, m=%d) = %v; want nil", tc.n, tc.m, got)
		}
	}
}

func TestDifferenceRunsOut(t *testing.T) {
	// Differencing more times than there is data returns nil rather than
	// panicking on an empty slice.
	if got := Difference([]float64{1, 2, 3}, 5); got != nil {
		t.Fatalf("Difference(3 points, d=5) = %v; want nil", got)
	}
	if got := Difference(nil, 1); got != nil {
		t.Fatalf("Difference(nil, 1) = %v; want nil", got)
	}
	if got := Difference([]float64{1, 2, 3}, 0); len(got) != 3 {
		t.Fatalf("Difference(d=0) should copy the series, got %v", got)
	}
}

func TestIntegrateRejectsShortTail(t *testing.T) {
	if got := integrate([]float64{1, 2}, []float64{5}, 2); got != nil {
		t.Fatalf("integrate with a tail shorter than d = %v; want nil", got)
	}
}

func TestMetricsOnEmptyAndMismatched(t *testing.T) {
	if v := MAE(nil, nil); !math.IsNaN(v) {
		t.Fatalf("MAE(nil,nil) = %v; want NaN", v)
	}
	if v := RMSE(nil, nil); !math.IsNaN(v) {
		t.Fatalf("RMSE(nil,nil) = %v; want NaN", v)
	}
	if v, skipped := MAPE(nil, nil); !math.IsNaN(v) || skipped != 0 {
		t.Fatalf("MAPE(nil,nil) = %v,%d; want NaN,0", v, skipped)
	}

	// Mismatched lengths use the shorter of the two rather than panicking.
	if v := MAE([]float64{1, 2, 3}, []float64{1}); v != 0 {
		t.Fatalf("MAE over the common prefix = %v; want 0", v)
	}
}

func TestMAPESkipsZeroActuals(t *testing.T) {
	// A zero actual has no defined percentage error, so it is skipped and
	// counted rather than producing Inf.
	pct, skipped := MAPE([]float64{0, 100, 0, 200}, []float64{5, 110, 7, 180})
	if skipped != 2 {
		t.Fatalf("skipped = %d; want 2", skipped)
	}
	want := 100 * (0.1 + 0.1) / 2
	if math.Abs(pct-want) > 1e-9 {
		t.Fatalf("MAPE = %v; want %v", pct, want)
	}

	// Every actual zero: nothing usable, so NaN and the full skip count.
	pct, skipped = MAPE([]float64{0, 0}, []float64{1, 2})
	if !math.IsNaN(pct) || skipped != 2 {
		t.Fatalf("all-zero MAPE = %v,%d; want NaN,2", pct, skipped)
	}
}

func TestACFGuards(t *testing.T) {
	if got := ACF(nil, 5); got != nil {
		t.Fatalf("ACF(nil) = %v; want nil", got)
	}
	if got := ACF([]float64{1, 2, 3}, -1); got != nil {
		t.Fatalf("ACF with a negative maxLag = %v; want nil", got)
	}
	// maxLag beyond the series is clamped rather than refused.
	y := []float64{1, 2, 3, 4, 5}
	if got := ACF(y, 99); len(got) != len(y) {
		t.Fatalf("ACF clamped length = %d; want %d", len(got), len(y))
	}
	// A constant series has zero variance: lag 0 is still 1 by convention.
	flat := ACF([]float64{3, 3, 3, 3}, 2)
	if len(flat) == 0 || flat[0] != 1 {
		t.Fatalf("ACF of a constant series = %v; want lag0 = 1", flat)
	}
}

func TestPACFGuards(t *testing.T) {
	if got := PACF(nil, 3); got != nil {
		t.Fatalf("PACF(nil) = %v; want nil", got)
	}
	got := PACF([]float64{1, 2, 3, 4}, 0)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("PACF(maxLag=0) = %v; want [1]", got)
	}
}

func TestLjungBoxGuards(t *testing.T) {
	if q, df := LjungBox(nil, 5, 2); q != 0 || df != 0 {
		t.Fatalf("LjungBox(nil) = %v,%d; want 0,0", q, df)
	}
	if q, df := LjungBox([]float64{1, 2, 3}, 0, 0); q != 0 || df != 0 {
		t.Fatalf("LjungBox(h=0) = %v,%d; want 0,0", q, df)
	}
	// Degrees of freedom never drop below 1, however many parameters were fitted.
	resid := make([]float64, 40)
	for i := range resid {
		resid[i] = float64(i%5) - 2
	}
	if _, df := LjungBox(resid, 2, 10); df != 1 {
		t.Fatalf("df = %d; want it floored at 1", df)
	}
}

func TestZCriticalFallsBackOnBadAlpha(t *testing.T) {
	want := zCritical(0.05)
	for _, bad := range []float64{0, 1, -0.5, 1.5, math.NaN()} {
		if got := zCritical(bad); math.Abs(got-want) > 1e-12 {
			t.Fatalf("zCritical(%v) = %v; want the 0.05 default %v", bad, got, want)
		}
	}
}

func TestZCriticalKnownValues(t *testing.T) {
	// Published two-sided normal critical values.
	for _, tc := range []struct{ alpha, want float64 }{
		{0.05, 1.959964},
		{0.01, 2.575829},
		{0.10, 1.644854},
	} {
		if got := zCritical(tc.alpha); math.Abs(got-tc.want) > 1e-5 {
			t.Fatalf("zCritical(%v) = %v; want %v", tc.alpha, got, tc.want)
		}
	}
}

func TestInvNormCDFTails(t *testing.T) {
	// Both tail branches of the Acklam approximation, either side of plow.
	if got := invNormCDF(0.001); math.Abs(got-(-3.090232)) > 1e-4 {
		t.Fatalf("invNormCDF(0.001) = %v; want ~-3.090232", got)
	}
	if got := invNormCDF(0.999); math.Abs(got-3.090232) > 1e-4 {
		t.Fatalf("invNormCDF(0.999) = %v; want ~3.090232", got)
	}
	// Symmetry across the whole range, including the central branch.
	for _, p := range []float64{0.001, 0.02, 0.2, 0.5, 0.8, 0.98, 0.999} {
		if l, r := invNormCDF(p), invNormCDF(1-p); math.Abs(l+r) > 1e-6 {
			t.Fatalf("invNormCDF not symmetric at p=%v: %v vs %v", p, l, r)
		}
	}
}

func TestFitARMARejectsNegativeOrders(t *testing.T) {
	y := make([]float64, 40)
	for i := range y {
		y[i] = float64(i % 4)
	}
	if _, err := fitARMA(y, -1, 0); !errors.Is(err, errOrder) {
		t.Fatalf("fitARMA(p=-1) err = %v; want errOrder", err)
	}
	if _, err := fitARMA(y, 0, -1); !errors.Is(err, errOrder) {
		t.Fatalf("fitARMA(q=-1) err = %v; want errOrder", err)
	}
}

func TestMeanOfEmpty(t *testing.T) {
	if got := mean(nil); got != 0 {
		t.Fatalf("mean(nil) = %v; want 0", got)
	}
}

func TestFitWhiteNoiseOrderZero(t *testing.T) {
	// p=q=0 exercises the paths that skip the ARMA stage entirely.
	y := make([]float64, 60)
	for i := range y {
		y[i] = float64(i%11) - 5
	}
	m, err := Fit(y, nil, 0, Order{})
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	if len(m.AR) != 0 || len(m.MA) != 0 {
		t.Fatalf("order (0,0,0) produced AR=%v MA=%v; want neither", m.AR, m.MA)
	}
	pt, lo, hi, err := m.Forecast(3, nil, 0.05)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	for i := range pt {
		if !(lo[i] <= pt[i] && pt[i] <= hi[i]) {
			t.Fatalf("step %d: point %v outside [%v,%v]", i, pt[i], lo[i], hi[i])
		}
	}
}
