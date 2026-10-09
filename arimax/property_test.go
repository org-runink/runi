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

// stagedFitCSS reproduces the estimator refine replaced — one ordinary least
// squares solve, then one ARMA fit to whatever is left — and returns its
// conditional sum of squares. It is the baseline the refinement must never do
// worse than, and it is written out here rather than reached through a flag on
// Fit so that it cannot drift into agreeing with whatever Fit now does.
func stagedFitCSS(y, x []float64, n, k int, ord Order) (float64, bool) {
	cols := k + 1
	design := make([]float64, n*cols)
	for i := 0; i < n; i++ {
		design[i*cols] = 1
		for j := 0; j < k; j++ {
			design[i*cols+1+j] = x[i*k+j]
		}
	}
	coef, err := olsQR(design, y, n, cols)
	if err != nil {
		return 0, false
	}
	a, err := fitARMA(Difference(regressErrors(y, x, n, k, coef), ord.D), ord.P, ord.Q)
	if err != nil {
		return 0, false
	}
	return sumSquares(a.resid), true
}

// levelDesign builds the row-major [1, X] design Fit builds internally.
func levelDesign(x []float64, n, k int) []float64 {
	cols := k + 1
	design := make([]float64, n*cols)
	for i := 0; i < n; i++ {
		design[i*cols] = 1
		for j := 0; j < k; j++ {
			design[i*cols+1+j] = x[i*k+j]
		}
	}
	return design
}

// randomExogSeries draws a series with autocorrelated errors, a mild trend and
// k exogenous regressors -- the shape of input Fit is given in practice.
func randomExogSeries(r *rand.Rand) (y, x []float64, n, k int) {
	n = 40 + r.IntN(200)
	k = r.IntN(3)
	x = make([]float64, n*k)
	for j := range x {
		x[j] = r.NormFloat64()
	}
	y = make([]float64, n)
	var nt float64
	for j := range y {
		nt = 0.7*nt + r.NormFloat64()
		y[j] = 0.5 + nt + 0.03*float64(j)
		for c := 0; c < k; c++ {
			y[j] += float64(c+1) * x[j*k+c]
		}
	}
	return y, x, n, k
}

// The conditional sum of squares is ONE objective, and refine descends on it
// in two blocks rather than fitting each block once. So the fit it returns can
// never score worse on that objective than the staged fit it starts from:
// the generalised-least-squares half-step is the exact minimiser over the
// coefficients at fixed phi and theta, and the ARMA half-step is warm started,
// which Nelder-Mead keeps as a simplex vertex.
//
// This is the property that justifies the whole change, and it is measured
// here rather than asserted at runtime. A guard inside refine could not be
// entered by any test, so it would report a property it never verified.
func TestPropertyRefineNeverRaisesTheConditionalSumOfSquares(t *testing.T) {
	r := rand.New(rand.NewPCG(901, 902))
	cases, worst := 0, 0.0
	for i := 0; i < 400; i++ {
		y, x, n, k := randomExogSeries(r)
		ord := Order{P: r.IntN(3), D: r.IntN(2), Q: r.IntN(3)}
		m, err := Fit(y, x, k, ord)
		if err != nil {
			continue // refusing an order it cannot fit is correct
		}
		staged, ok := stagedFitCSS(y, x, n, k, ord)
		if !ok {
			continue
		}
		got := sumSquares(m.Residuals)
		cases++
		rise := (got - staged) / (staged + 1e-300)
		if rise > worst {
			worst = rise
		}
		// The tolerance is for floating-point summation only. Anything the
		// refinement actually does to the objective shows up far above it.
		if rise > 1e-9 {
			t.Fatalf("%v n=%d k=%d: conditional sum of squares rose from %.10g to %.10g (%+.3g relative)",
				ord, n, k, staged, got, rise)
		}
	}
	if cases < 200 {
		t.Fatalf("only %d cases fitted; the property was barely exercised", cases)
	}
	t.Logf("conditional sum of squares over %d fits: worst relative rise %.3g", cases, worst)
}

// Prewhitening is a LINEAR filter with fixed zero pre-sample values, which is
// the reason the generalised-least-squares step is exact rather than
// approximate: filtering y - X*beta gives filter(y) - filter(X)*beta exactly.
// The visible consequence is that a noise-free linear response is still
// recovered exactly after the filter is applied, whatever ARMA the refinement
// happens to have fitted to the floating-point dust left over.
//
// It is the companion to TestPropertyFitRecoversAnExactSlope, which covers the
// orders that skip the refinement entirely.
func TestPropertyRefinePreservesAnExactFit(t *testing.T) {
	r := rand.New(rand.NewPCG(213, 214))
	orders := []Order{{P: 1}, {Q: 1}, {P: 1, Q: 1}, {P: 2, Q: 1}}
	worst := 0.0
	for i := 0; i < 200; i++ {
		n := 60 + r.IntN(100)
		beta := -5 + 10*r.Float64()
		intercept := -3 + 6*r.Float64()
		x := make([]float64, n)
		y := make([]float64, n)
		for j := range y {
			x[j] = r.NormFloat64() * 3
			y[j] = intercept + beta*x[j] // no noise: the fit is exact
		}
		for _, ord := range orders {
			m, err := Fit(y, x, 1, ord)
			if err != nil {
				t.Fatalf("n=%d %v: %v", n, ord, err)
			}
			eb := math.Abs(m.Beta[0]-beta) / (1 + math.Abs(beta))
			ec := math.Abs(m.Intercept-intercept) / (1 + math.Abs(intercept))
			if eb > worst {
				worst = eb
			}
			if ec > worst {
				worst = ec
			}
			if eb > 1e-9 || ec > 1e-9 {
				t.Fatalf("n=%d %v: beta %v -> %v, intercept %v -> %v",
					n, ord, beta, m.Beta[0], intercept, m.Intercept)
			}
		}
	}
	t.Logf("exact fit preserved through prewhitening to %.3g relative over %d fits", worst, 200*len(orders))
}

// The alternation has to settle, not oscillate. On a WELL-SPECIFIED stationary
// series -- the case the estimator is for -- one further pass past what Fit
// does must barely move the coefficients.
//
// This is a divergence guard, not a proof of convergence: refine is capped at
// maxRefine passes and on a misspecified or near-unidentified series (phi and
// theta nearly cancelling, or an AR root driven to the stationarity bound) it
// can still be moving when the cap is reached. That is stated in refine's
// documentation rather than hidden behind a tighter bound here that only holds
// for the easy cases.
func TestPropertyRefineSettlesOnAWellSpecifiedSeries(t *testing.T) {
	const bound = 0.05 // measured worst: 0.0092
	r := rand.New(rand.NewPCG(777, 778))
	cases, worst := 0, 0.0
	for i := 0; i < 300; i++ {
		n := 120 + r.IntN(300)
		k := 1 + r.IntN(2)
		phi := -0.8 + 1.6*r.Float64()
		theta := -0.8 + 1.6*r.Float64()
		x := make([]float64, n*k)
		for j := range x {
			x[j] = r.NormFloat64()
		}
		y := make([]float64, n)
		var nt, prev float64
		for j := 0; j < n; j++ {
			e := r.NormFloat64()
			nt = phi*nt + e + theta*prev
			prev = e
			y[j] = 2 + nt
			for c := 0; c < k; c++ {
				y[j] += float64(c+1) * x[j*k+c]
			}
		}
		ord := Order{P: 1, Q: 1}
		m, err := Fit(y, x, k, ord)
		if err != nil {
			continue
		}
		cases++

		// One more pass, started from exactly where Fit stopped.
		coef := append([]float64{m.Intercept}, m.Beta...)
		st := &fitState{
			y: y, x: x, design: levelDesign(x, n, k),
			n: n, k: k, cols: k + 1, ord: ord,
			coef: append([]float64(nil), coef...), errs: m.errs,
			arma: &arma{phi: m.AR, theta: m.MA},
		}
		st.refine()

		var move, scale float64
		for j := range coef {
			if d := math.Abs(st.coef[j] - coef[j]); d > move {
				move = d
			}
			if a := math.Abs(coef[j]); a > scale {
				scale = a
			}
		}
		rel := move / (1 + scale)
		if rel > worst {
			worst = rel
		}
		if rel > bound {
			t.Fatalf("n=%d k=%d phi=%.2f theta=%.2f: a further pass moved the coefficients by %.3g relative",
				n, k, phi, theta, rel)
		}
	}
	if cases < 200 {
		t.Fatalf("only %d cases fitted", cases)
	}
	t.Logf("a further pass moves the coefficients by at most %.3g relative over %d fits", worst, cases)
}
