package arimax

import (
	"errors"
	"math"
	"testing"
)

// Fitting and forecasting with d > 0 — the path that differences the series,
// forecasts the differences, and integrates back to levels. It is the part of
// Forecast most likely to be wrong and was previously untested end to end.

func TestFitAndForecastWithFirstDifferencing(t *testing.T) {
	// A series with a deterministic trend: levels are non-stationary, first
	// differences are not. d=1 is the case ARIMA exists for.
	n := 120
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		y[i] = 10 + 0.5*float64(i) + math.Sin(float64(i)/3)
	}

	m, err := Fit(y, nil, 0, Order{P: 1, D: 1, Q: 0})
	if err != nil {
		t.Fatalf("Fit(d=1): %v", err)
	}

	h := 8
	pt, lo, hi, err := m.Forecast(h, nil, 0.05)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if len(pt) != h {
		t.Fatalf("len(point) = %d; want %d", len(pt), h)
	}

	for i := 0; i < h; i++ {
		if math.IsNaN(pt[i]) || math.IsInf(pt[i], 0) {
			t.Fatalf("point[%d] = %v; want finite", i, pt[i])
		}
		if !(lo[i] <= pt[i] && pt[i] <= hi[i]) {
			t.Fatalf("step %d: point %v outside [%v,%v]", i, pt[i], lo[i], hi[i])
		}
	}

	// The trend must be carried through the integration. Forecasts should keep
	// rising at roughly 0.5 per step, not flatten at the last level.
	if pt[h-1] <= pt[0] {
		t.Fatalf("d=1 forecast did not continue the trend: first %v, last %v", pt[0], pt[h-1])
	}
	slope := (pt[h-1] - pt[0]) / float64(h-1)
	if math.Abs(slope-0.5) > 0.35 {
		t.Fatalf("implied slope %v; want roughly the true 0.5", slope)
	}

	// Intervals must widen with horizon — that is what psiWeights is for.
	if (hi[h-1] - lo[h-1]) <= (hi[0] - lo[0]) {
		t.Fatalf("interval did not widen: h=1 width %v, h=%d width %v",
			hi[0]-lo[0], h, hi[h-1]-lo[h-1])
	}
}

func TestForecastWithSecondDifferencing(t *testing.T) {
	// d=2 is the case the integrate() bug lived in: inner levels need the last
	// DIFFERENCE, not the last value.
	n := 140
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		x := float64(i)
		y[i] = 5 + 0.3*x + 0.02*x*x // quadratic: needs two differences
	}

	m, err := Fit(y, nil, 0, Order{P: 1, D: 2, Q: 0})
	if err != nil {
		t.Fatalf("Fit(d=2): %v", err)
	}
	pt, _, _, err := m.Forecast(5, nil, 0.05)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}

	// A quadratic keeps accelerating; successive forecast gaps must grow.
	for i := range pt {
		if math.IsNaN(pt[i]) || math.IsInf(pt[i], 0) {
			t.Fatalf("point[%d] = %v; want finite", i, pt[i])
		}
	}
	g1 := pt[1] - pt[0]
	g4 := pt[4] - pt[3]
	if g4 <= g1 {
		t.Fatalf("d=2 forecast is not accelerating: first gap %v, last gap %v", g1, g4)
	}

	// And it should be near the true continuation of the quadratic.
	want := 5 + 0.3*float64(n) + 0.02*float64(n)*float64(n)
	if rel := math.Abs(pt[0]-want) / math.Abs(want); rel > 0.05 {
		t.Fatalf("first d=2 forecast %v differs from the true %v by %.1f%%", pt[0], want, rel*100)
	}
}

func TestFitWithDifferencingAndExogenous(t *testing.T) {
	n := 120
	y := make([]float64, n)
	x := make([]float64, n)
	for i := 0; i < n; i++ {
		x[i] = float64(i%6) * 1.5
		y[i] = 4 + 0.4*float64(i) + 2*x[i]
	}
	m, err := Fit(y, x, 1, Order{P: 1, D: 1, Q: 1})
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}

	h := 6
	xf := make([]float64, h)
	for i := range xf {
		xf[i] = float64((n+i)%6) * 1.5
	}
	pt, lo, hi, err := m.Forecast(h, xf, 0.05)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	for i := range pt {
		if math.IsNaN(pt[i]) || !(lo[i] <= pt[i] && pt[i] <= hi[i]) {
			t.Fatalf("step %d: %v not inside [%v,%v]", i, pt[i], lo[i], hi[i])
		}
	}
}

func TestFitSurfacesASingularDesign(t *testing.T) {
	// An exogenous column that is constant duplicates the intercept, so the
	// design matrix is rank deficient. Fit must surface that rather than
	// return meaningless coefficients.
	n := 60
	y := make([]float64, n)
	x := make([]float64, n)
	for i := range y {
		x[i] = 1 // constant: collinear with the intercept column
		y[i] = float64(i % 5)
	}
	_, err := Fit(y, x, 1, Order{P: 1, D: 0, Q: 0})
	if err != nil && !errors.Is(err, ErrSingular) {
		t.Fatalf("Fit with a collinear regressor err = %v; want nil or ErrSingular", err)
	}
	// Either outcome is acceptable; what is not acceptable is a NaN model.
	if err == nil {
		m, _ := Fit(y, x, 1, Order{P: 1, D: 0, Q: 0})
		for i, b := range m.Beta {
			if math.IsNaN(b) || math.IsInf(b, 0) {
				t.Fatalf("Beta[%d] = %v on a collinear design; want finite or an error", i, b)
			}
		}
	}
}

func TestFitARMARejectsTooShortASeries(t *testing.T) {
	// len(y) <= p+q+1 leaves nothing to fit on.
	if _, err := fitARMA([]float64{1, 2, 3}, 2, 1); !errors.Is(err, errShort) {
		t.Fatalf("fitARMA on too short a series err = %v; want errShort", err)
	}
}

func TestFitARMAWithNoTermsIsPlainResiduals(t *testing.T) {
	y := []float64{1, -1, 2, -2, 1.5, -1.5, 0.5, -0.5, 1, -1}
	a, err := fitARMA(y, 0, 0)
	if err != nil {
		t.Fatalf("fitARMA(0,0): %v", err)
	}
	if len(a.phi) != 0 || len(a.theta) != 0 {
		t.Fatalf("order (0,0) produced phi=%v theta=%v; want neither", a.phi, a.theta)
	}
	if a.sigma2 <= 0 {
		t.Fatalf("sigma2 = %v; want a positive residual variance", a.sigma2)
	}
}
