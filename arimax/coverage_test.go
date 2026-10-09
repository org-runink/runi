package arimax

import (
	"errors"
	"math"
	"testing"
)

// A perfectly correlated autocorrelation sequence leaves no prediction variance
// after the first lag. The recursion must stop rather than divide by it. This
// cannot be produced through PACF — a sample autocorrelation from real data
// keeps every reflection coefficient strictly inside (-1, 1) — which is why
// levinson is reachable on its own.
func TestLevinsonVarianceCollapse(t *testing.T) {
	r := []float64{1, 1, 1, 1, 1}
	got := levinson(r, 4)
	if got[0] != 1 || got[1] != 1 {
		t.Fatalf("lags 0 and 1 = %v, want 1 and 1", got[:2])
	}
	for k := 2; k < len(got); k++ {
		if got[k] != 0 {
			t.Errorf("lag %d = %v, want 0: the recursion should have stopped", k, got[k])
		}
	}
	// A negative perfect correlation collapses identically.
	if got := levinson([]float64{1, -1, 1, -1}, 3); got[2] != 0 || got[3] != 0 {
		t.Errorf("negative collapse: %v", got)
	}
}

func TestLevinsonMaxLagZero(t *testing.T) {
	if got := levinson([]float64{1}, 0); len(got) != 1 || got[0] != 1 {
		t.Errorf("maxLag 0 = %v, want [1]", got)
	}
}

// Differencing shortens the series, so an order that looks affordable against
// the input length can still leave the ARMA stage with too little to fit. The
// error has to come back out of Fit rather than being fitted to noise.
func TestFitARMAStageTooShort(t *testing.T) {
	y := make([]float64, 14)
	for i := range y {
		y[i] = math.Sin(float64(i)) + float64(i)
	}
	_, err := Fit(y, nil, 0, Order{P: 5, D: 3, Q: 5})
	if !errors.Is(err, errShort) {
		t.Fatalf("err = %v, want errShort", err)
	}
}

// A design matrix with a column of zeros is rank deficient. The decomposition
// must say so rather than returning coefficients derived from a zero pivot.
func TestOLSQRZeroColumn(t *testing.T) {
	const n, p = 6, 3
	x := make([]float64, n*p)
	b := make([]float64, n)
	for i := 0; i < n; i++ {
		x[i*p+0] = 1
		x[i*p+1] = 0
		x[i*p+2] = float64(i)
		b[i] = float64(i) * 2
	}
	if _, err := olsQR(x, b, n, p); !errors.Is(err, ErrSingular) {
		t.Fatalf("err = %v, want ErrSingular", err)
	}
}

// A column whose values are near the floating-point floor has a norm that is
// not zero but a Householder vector whose squared norm underflows. Dividing by
// it would produce infinities, so the step is skipped.
func TestOLSQRUnderflowingColumn(t *testing.T) {
	const n, p = 6, 3
	x := make([]float64, n*p)
	b := make([]float64, n)
	for i := 0; i < n; i++ {
		x[i*p+0] = 1e-160
		x[i*p+1] = 1
		x[i*p+2] = float64(i)
		b[i] = float64(i)
	}
	// Whatever it returns, it must not be infinities or NaN from a division by
	// an underflowed norm.
	sol, err := olsQR(x, b, n, p)
	if err == nil {
		for i, v := range sol {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Errorf("coefficient %d = %v", i, v)
			}
		}
	}
}

// Differencing consumes a point per order, so a series can be long enough to
// regress on and still have nothing left to fit. Fit refuses before it
// differences away the whole series.
func TestFitSeriesTooShortForDifferencing(t *testing.T) {
	y := []float64{1, 2, 3, 4}
	if _, err := Fit(y, nil, 0, Order{D: 5}); !errors.Is(err, errShort) {
		t.Fatalf("err = %v, want errShort", err)
	}
}

// Prewhitening consumes observations: d of them to difference and p more to
// condition on. A design that ordinary least squares can solve in levels can
// therefore be over-determined once filtered, and the generalised-least-squares
// step has to refuse rather than solve a system with fewer rows than columns.
//
// What must come back is the STAGED estimate — a worse estimate of beta, but a
// real one — not an error and not a fabricated refinement. Here n=6 with four
// regressors leaves 5 filtered rows for 5 columns, one short.
func TestGLSStepRefusesAnOverDeterminedWhitenedDesign(t *testing.T) {
	const n, k = 6, 4
	cols := k + 1
	y := make([]float64, n)
	x := make([]float64, n*k)
	g := lcg(4242)
	for i := 0; i < n; i++ {
		for j := 0; j < k; j++ {
			x[i*k+j] = g.next()
		}
		y[i] = 1 + float64(i) + g.next()
	}

	// The step itself refuses.
	design := make([]float64, n*cols)
	for i := 0; i < n; i++ {
		design[i*cols] = 1
		for j := 0; j < k; j++ {
			design[i*cols+1+j] = x[i*k+j]
		}
	}
	if _, err := glsStep(y, design, n, cols, 0, 0, []float64{0.5}, []float64{0.2}); !errors.Is(err, errShort) {
		t.Fatalf("glsStep err = %v, want errShort", err)
	}

	// And Fit still returns the staged fit, to the digit.
	m, err := Fit(y, x, k, Order{P: 1, Q: 1})
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	scratch := make([]float64, len(design))
	copy(scratch, design)
	want, err := olsQR(scratch, y, n, cols)
	if err != nil {
		t.Fatalf("olsQR: %v", err)
	}
	got := append([]float64{m.Intercept}, m.Beta...)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("coefficient %d = %v, want the OLS value %v", i, got[i], want[i])
		}
	}
}
