package arimax

import (
	"math"
	"math/rand/v2"
	"testing"
)

// The properties here are the ones a forecast is useless without: that the
// output has the shape it was asked for, that no value is NaN, and that the
// interval actually contains its point estimate. A model that returns the
// right numbers in the wrong shape, or a band that excludes its own forecast,
// fails silently in a caller's chart.

// Differencing d times then undoing nothing must shorten the series by
// exactly d, for every d and every series. An off-by-one here misaligns a
// forecast with its own exogenous rows.
func TestPropertyDifferenceShortensByExactlyD(t *testing.T) {
	r := rand.New(rand.NewPCG(201, 202))
	for i := 0; i < 5000; i++ {
		n := r.IntN(60)
		d := r.IntN(5)
		y := make([]float64, n)
		for j := range y {
			y[j] = r.NormFloat64()
		}
		got := Difference(y, d)
		want := n - d
		if want < 0 {
			want = 0
		}
		if len(got) != want {
			t.Fatalf("n=%d d=%d: got %d values, want %d", n, d, len(got), want)
		}
	}
}

// Differencing a polynomial of degree d exactly d times leaves a constant:
// the defining property of the operator, and the one that would break if the
// loop ran the wrong number of times or in the wrong direction.
func TestPropertyDifferencingAPolynomialLeavesAConstant(t *testing.T) {
	for d := 1; d <= 3; d++ {
		n := 40
		y := make([]float64, n)
		for j := range y {
			y[j] = math.Pow(float64(j), float64(d))
		}
		got := Difference(y, d)
		if len(got) == 0 {
			t.Fatalf("d=%d: nothing left", d)
		}
		first := got[0]
		for j, v := range got {
			if math.Abs(v-first) > 1e-6*(1+math.Abs(first)) {
				t.Fatalf("d=%d: differencing x^%d gave %v at %d, not the constant %v", d, d, v, j, first)
			}
		}
	}
}

// The autocorrelation at lag 0 is 1 by construction, every value is in
// [-1, 1], and the slice has exactly the requested length. ACF feeds model
// selection, so a value outside the range is a nonsense order choice.
func TestPropertyACFIsBoundedAndStartsAtOne(t *testing.T) {
	r := rand.New(rand.NewPCG(203, 204))
	for i := 0; i < 3000; i++ {
		n := 4 + r.IntN(200)
		y := make([]float64, n)
		for j := range y {
			y[j] = r.NormFloat64()
		}
		maxLag := 1 + r.IntN(n-2)
		got := ACF(y, maxLag)
		if len(got) != maxLag+1 {
			t.Fatalf("n=%d maxLag=%d: got %d values", n, maxLag, len(got))
		}
		if math.Abs(got[0]-1) > 1e-12 {
			t.Fatalf("ACF[0] = %v, want 1", got[0])
		}
		for lag, v := range got {
			if math.IsNaN(v) || v < -1.0000001 || v > 1.0000001 {
				t.Fatalf("n=%d: ACF[%d] = %v", n, lag, v)
			}
		}
	}
}

// The partial autocorrelation is also bounded. The Durbin-Levinson recursion
// is where a sign slip or a stale coefficient shows as a value past 1.
func TestPropertyPACFIsBounded(t *testing.T) {
	r := rand.New(rand.NewPCG(205, 206))
	for i := 0; i < 3000; i++ {
		n := 8 + r.IntN(200)
		y := make([]float64, n)
		// An AR(1), so there is real structure for the recursion to find.
		phi := -0.9 + 1.8*r.Float64()
		for j := range y {
			if j == 0 {
				y[j] = r.NormFloat64()
				continue
			}
			y[j] = phi*y[j-1] + r.NormFloat64()
		}
		maxLag := 1 + r.IntN(10)
		got := PACF(y, maxLag)
		for lag, v := range got {
			if math.IsNaN(v) || v < -1.0000001 || v > 1.0000001 {
				t.Fatalf("n=%d phi=%.2f: PACF[%d] = %v", n, phi, lag, v)
			}
		}
	}
}

// A forecast has the shape it was asked for, holds no NaN, and its interval
// brackets its point estimate -- lo <= point <= hi at every horizon. A band
// that does not contain its own forecast is drawn straight into a chart and
// believed.
func TestPropertyForecastsAreWellFormed(t *testing.T) {
	r := rand.New(rand.NewPCG(207, 208))
	for i := 0; i < 1000; i++ {
		n := 40 + r.IntN(200)
		y := make([]float64, n)
		for j := range y {
			if j == 0 {
				y[j] = r.NormFloat64()
				continue
			}
			y[j] = 0.6*y[j-1] + r.NormFloat64() + 0.02*float64(j)
		}
		ord := Order{P: r.IntN(3), D: r.IntN(2), Q: r.IntN(3)}
		m, err := Fit(y, nil, 0, ord)
		if err != nil {
			continue // refusing an order it cannot fit is correct
		}
		h := 1 + r.IntN(20)
		point, lo, hi, err := m.Forecast(h, nil, 0.05)
		if err != nil {
			continue
		}
		if len(point) != h || len(lo) != h || len(hi) != h {
			t.Fatalf("h=%d: got %d/%d/%d values", h, len(point), len(lo), len(hi))
		}
		for j := 0; j < h; j++ {
			if math.IsNaN(point[j]) || math.IsNaN(lo[j]) || math.IsNaN(hi[j]) {
				t.Fatalf("%v: NaN at horizon %d (%v, %v, %v)", ord, j, lo[j], point[j], hi[j])
			}
			if lo[j] > point[j] || point[j] > hi[j] {
				t.Fatalf("%v horizon %d: interval [%v, %v] does not contain %v",
					ord, j, lo[j], hi[j], point[j])
			}
		}
		// A wider interval must never be narrower: 99% contains 95%.
		_, lo99, hi99, err := m.Forecast(h, nil, 0.01)
		if err != nil {
			continue
		}
		for j := 0; j < h; j++ {
			if lo99[j] > lo[j]+1e-9 || hi99[j] < hi[j]-1e-9 {
				t.Fatalf("%v horizon %d: the 99%% band [%v,%v] is inside the 95%% band [%v,%v]",
					ord, j, lo99[j], hi99[j], lo[j], hi[j])
			}
		}
	}
}

// Fitting an exactly linear response to one regressor recovers its slope.
// This is the property that pins the regression stage: a bias in Beta would
// show as a slope that is close but never right.
func TestPropertyFitRecoversAnExactSlope(t *testing.T) {
	r := rand.New(rand.NewPCG(209, 210))
	for i := 0; i < 500; i++ {
		n := 60 + r.IntN(100)
		beta := -5 + 10*r.Float64()
		intercept := -3 + 6*r.Float64()
		x := make([]float64, n)
		y := make([]float64, n)
		for j := range y {
			x[j] = r.NormFloat64() * 3
			y[j] = intercept + beta*x[j] // no noise: the fit is exact
		}
		m, err := Fit(y, x, 1, Order{})
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if len(m.Beta) != 1 {
			t.Fatalf("got %d coefficients", len(m.Beta))
		}
		if math.Abs(m.Beta[0]-beta) > 1e-6*(1+math.Abs(beta)) {
			t.Fatalf("n=%d: beta %v recovered as %v", n, beta, m.Beta[0])
		}
		if math.Abs(m.Intercept-intercept) > 1e-6*(1+math.Abs(intercept)) {
			t.Fatalf("n=%d: intercept %v recovered as %v", n, intercept, m.Intercept)
		}
	}
}

// The error metrics agree with their own definitions, and MAPE says how many
// points it had to skip rather than quietly dividing by zero.
func TestPropertyMetricsMatchTheirDefinitions(t *testing.T) {
	r := rand.New(rand.NewPCG(211, 212))
	for i := 0; i < 3000; i++ {
		n := 1 + r.IntN(50)
		a := make([]float64, n)
		p := make([]float64, n)
		zeros := 0
		for j := range a {
			if r.IntN(6) == 0 {
				a[j] = 0
				zeros++
			} else {
				a[j] = r.NormFloat64() * 10
			}
			p[j] = a[j] + r.NormFloat64()
		}
		var sumAbs, sumSq float64
		for j := range a {
			d := a[j] - p[j]
			sumAbs += math.Abs(d)
			sumSq += d * d
		}
		if got, want := MAE(a, p), sumAbs/float64(n); math.Abs(got-want) > 1e-9 {
			t.Fatalf("MAE = %v, want %v", got, want)
		}
		if got, want := RMSE(a, p), math.Sqrt(sumSq/float64(n)); math.Abs(got-want) > 1e-9 {
			t.Fatalf("RMSE = %v, want %v", got, want)
		}
		pct, skipped := MAPE(a, p)
		if skipped != zeros {
			t.Fatalf("MAPE skipped %d points, %d were zero", skipped, zeros)
		}
		if skipped < n && math.IsNaN(pct) {
			t.Fatalf("MAPE is NaN with %d of %d usable points", n-skipped, n)
		}
		// RMSE penalises a large error more than MAE, never less.
		if RMSE(a, p) < MAE(a, p)-1e-9 {
			t.Fatalf("RMSE %v is below MAE %v", RMSE(a, p), MAE(a, p))
		}
	}
}
