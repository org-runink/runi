package arimax

import (
	"math"
	"testing"
)

// lcg is a tiny deterministic generator. math/rand's stream is not guaranteed
// stable across Go releases, and a test that silently changes its data is worse
// than no test.
type lcg uint64

func (g *lcg) next() float64 {
	*g = lcg(uint64(*g)*6364136223846793005 + 1442695040888963407)
	return float64(uint64(*g)>>11)/float64(1<<53)*2 - 1
}

func closeTo(t *testing.T, got, want, tol float64, what string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %.6f, want %.6f (tol %g)", what, got, want, tol)
	}
}

func TestOLSRecoversKnownCoefficients(t *testing.T) {
	// y = 2 + 3*x1 - 1.5*x2, exactly. QR must return those.
	n, k := 40, 2
	cols := k + 1
	design := make([]float64, n*cols)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		x1 := float64(i)
		x2 := float64(i%7) * 2
		design[i*cols] = 1
		design[i*cols+1] = x1
		design[i*cols+2] = x2
		y[i] = 2 + 3*x1 - 1.5*x2
	}
	b, err := olsQR(design, y, n, cols)
	if err != nil {
		t.Fatalf("olsQR: %v", err)
	}
	closeTo(t, b[0], 2, 1e-8, "intercept")
	closeTo(t, b[1], 3, 1e-8, "beta1")
	closeTo(t, b[2], -1.5, 1e-8, "beta2")
}

func TestOLSRejectsRankDeficient(t *testing.T) {
	// Two identical columns: no unique solution.
	n, cols := 10, 2
	design := make([]float64, n*cols)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		design[i*cols] = 1
		design[i*cols+1] = 1
		y[i] = float64(i)
	}
	if _, err := olsQR(design, y, n, cols); err == nil {
		t.Fatal("expected ErrSingular for duplicated columns, got nil")
	}
}

func TestDifferenceIntegrateRoundTrip(t *testing.T) {
	// integrate is the forecasting path: given the differenced values for
	// future steps and the tail of the original series, recover the levels.
	y := []float64{3, 7, 2, 9, 14, 6, 11, 5, 13, 8}
	for d := 1; d <= 3; d++ {
		dy := Difference(y, d)
		// Treat the last 4 differenced points as if they were forecasts, with
		// the original values immediately before them as the tail.
		const future = 4
		split := len(dy) - future
		tail := y[split : split+d]
		got := integrate(dy[split:], tail, d)
		if got == nil {
			t.Fatalf("d=%d: integrate returned nil", d)
		}
		for i := range got {
			closeTo(t, got[i], y[split+d+i], 1e-9, "round trip")
		}
	}
}

func TestACFPACFOnAR1(t *testing.T) {
	// AR(1) with phi=0.7: ACF decays geometrically, PACF cuts off after lag 1.
	g := lcg(42)
	n := 4000
	y := make([]float64, n)
	for i := 1; i < n; i++ {
		y[i] = 0.7*y[i-1] + g.next()
	}
	acf := ACF(y, 3)
	closeTo(t, acf[0], 1, 1e-12, "acf[0]")
	closeTo(t, acf[1], 0.7, 0.06, "acf[1] ~ phi")
	closeTo(t, acf[2], 0.49, 0.08, "acf[2] ~ phi^2")

	pacf := PACF(y, 4)
	closeTo(t, pacf[1], 0.7, 0.06, "pacf[1] ~ phi")
	if math.Abs(pacf[2]) > 0.08 {
		t.Errorf("pacf[2] should be ~0 for AR(1), got %.4f", pacf[2])
	}
}

func TestFitRecoversExogenousBeta(t *testing.T) {
	// y = 5 + 2.5*x + AR(1) noise. Beta must come back despite the serial
	// correlation — that is the whole point of the staged fit.
	g := lcg(7)
	n := 600
	x := make([]float64, n)
	y := make([]float64, n)
	var e float64
	for i := 0; i < n; i++ {
		x[i] = math.Sin(float64(i)/9) * 10
		e = 0.6*e + g.next()
		y[i] = 5 + 2.5*x[i] + e
	}
	m, err := Fit(y, x, 1, Order{P: 1, D: 0, Q: 0})
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	closeTo(t, m.Beta[0], 2.5, 0.05, "beta")
	closeTo(t, m.Intercept, 5, 0.3, "intercept")
	closeTo(t, m.AR[0], 0.6, 0.12, "AR(1)")
}

func TestForecastRequiresFutureExogenous(t *testing.T) {
	g := lcg(11)
	n := 200
	x := make([]float64, n)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		x[i] = float64(i % 13)
		y[i] = 1 + 0.5*x[i] + g.next()
	}
	m, err := Fit(y, x, 1, Order{P: 1})
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	if _, _, _, err := m.Forecast(5, nil, 0.05); err == nil {
		t.Fatal("forecasting with regressors and no future X must fail, got nil error")
	}
	if _, _, _, err := m.Forecast(5, make([]float64, 3), 0.05); err == nil {
		t.Fatal("wrong-length future X must fail, got nil error")
	}
	pt, lo, hi, err := m.Forecast(5, make([]float64, 5), 0.05)
	if err != nil {
		t.Fatalf("Forecast with correct future X: %v", err)
	}
	if len(pt) != 5 || len(lo) != 5 || len(hi) != 5 {
		t.Fatalf("lengths: %d %d %d, want 5", len(pt), len(lo), len(hi))
	}
}

func TestPredictionIntervalsWidenWithHorizon(t *testing.T) {
	g := lcg(3)
	n := 400
	y := make([]float64, n)
	for i := 1; i < n; i++ {
		y[i] = 0.5*y[i-1] + g.next()
	}
	m, err := Fit(y, nil, 0, Order{P: 1})
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	_, lo, hi, err := m.Forecast(10, nil, 0.05)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	prev := hi[0] - lo[0]
	if prev <= 0 {
		t.Fatalf("interval at h=1 is not positive: %g", prev)
	}
	for i := 1; i < 10; i++ {
		w := hi[i] - lo[i]
		if w < prev-1e-12 {
			t.Errorf("interval narrowed at h=%d: %.6f then %.6f", i+1, prev, w)
		}
		prev = w
	}
}

func TestMAPESkipsZerosAndReportsThem(t *testing.T) {
	actual := []float64{10, 0, 20, 0}
	pred := []float64{11, 5, 18, 1}
	pct, skipped := MAPE(actual, pred)
	if skipped != 2 {
		t.Errorf("skipped: got %d, want 2", skipped)
	}
	// |1/10| and |2/20| -> 10% and 10% -> mean 10%.
	closeTo(t, pct, 10, 1e-9, "mape")
}

func TestMetrics(t *testing.T) {
	a := []float64{1, 2, 3}
	p := []float64{2, 2, 5}
	closeTo(t, MAE(a, p), 1.0, 1e-12, "mae")
	closeTo(t, RMSE(a, p), math.Sqrt(5.0/3.0), 1e-12, "rmse")
}

func TestZCritical(t *testing.T) {
	closeTo(t, zCritical(0.05), 1.959964, 1e-5, "z 95%")
	closeTo(t, zCritical(0.01), 2.575829, 1e-5, "z 99%")
}

func TestLjungBoxFlagsStructure(t *testing.T) {
	g := lcg(5)
	n := 500
	white := make([]float64, n)
	for i := range white {
		white[i] = g.next()
	}
	qw, _ := LjungBox(white, 10, 0)

	corr := make([]float64, n)
	for i := 1; i < n; i++ {
		corr[i] = 0.8*corr[i-1] + g.next()
	}
	qc, _ := LjungBox(corr, 10, 0)

	if qc <= qw {
		t.Errorf("Ljung-Box should be far larger for correlated residuals: white %.2f, correlated %.2f", qw, qc)
	}
}

func TestFitRejectsShortSeries(t *testing.T) {
	if _, err := Fit([]float64{1, 2, 3}, nil, 0, Order{P: 2, Q: 2}); err == nil {
		t.Fatal("expected errShort, got nil")
	}
}
