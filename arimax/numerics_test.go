package arimax

import (
	"errors"
	"math"
	"testing"
)

// The numeric internals: guards and degenerate inputs that the end-to-end fits
// never produce, but that a caller with awkward data will.

func TestOLSQRGuards(t *testing.T) {
	t.Run("no regressors", func(t *testing.T) {
		beta, err := olsQR(nil, []float64{1, 2, 3}, 3, 0)
		if err != nil || beta != nil {
			t.Fatalf("olsQR with p=0 = %v,%v; want nil,nil", beta, err)
		}
	})

	t.Run("fewer rows than columns is singular", func(t *testing.T) {
		// 2 observations, 3 unknowns: underdetermined.
		x := make([]float64, 2*3)
		_, err := olsQR(x, []float64{1, 2}, 2, 3)
		if !errors.Is(err, ErrSingular) {
			t.Fatalf("olsQR(n<p) err = %v; want ErrSingular", err)
		}
	})

	t.Run("an all-zero column is singular", func(t *testing.T) {
		// Column 0 is entirely zero, so its Householder norm is 0.
		n, p := 4, 2
		x := make([]float64, n*p)
		for i := 0; i < n; i++ {
			x[i*p+0] = 0
			x[i*p+1] = float64(i + 1)
		}
		_, err := olsQR(x, []float64{1, 2, 3, 4}, n, p)
		if !errors.Is(err, ErrSingular) {
			t.Fatalf("olsQR with a zero column err = %v; want ErrSingular", err)
		}
	})
}

func TestOLSQRRecoversKnownCoefficients(t *testing.T) {
	// y = 3 + 2*x1 - 1*x2 exactly; QR should return it to rounding.
	n, p := 20, 3
	x := make([]float64, n*p)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		x1 := float64(i)
		x2 := float64((i * 7) % 5)
		x[i*p+0] = 1 // intercept
		x[i*p+1] = x1
		x[i*p+2] = x2
		y[i] = 3 + 2*x1 - x2
	}
	beta, err := olsQR(x, y, n, p)
	if err != nil {
		t.Fatalf("olsQR: %v", err)
	}
	want := []float64{3, 2, -1}
	for i := range want {
		if math.Abs(beta[i]-want[i]) > 1e-9 {
			t.Fatalf("beta[%d] = %v; want %v", i, beta[i], want[i])
		}
	}
}

func TestOLSQRHandlesADuplicatedColumn(t *testing.T) {
	// Perfectly collinear columns: the second contributes nothing after the
	// first is eliminated, so the reflector is skipped rather than dividing by
	// ~0. The result must be finite either way — this is the case the comment
	// in linalg.go calls out as the reason for QR over normal equations.
	n, p := 10, 2
	x := make([]float64, n*p)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		v := float64(i + 1)
		x[i*p+0] = v
		x[i*p+1] = v // identical column
		y[i] = 2 * v
	}
	beta, err := olsQR(x, y, n, p)
	if err != nil {
		if !errors.Is(err, ErrSingular) {
			t.Fatalf("collinear olsQR err = %v; want nil or ErrSingular", err)
		}
		return
	}
	for i, b := range beta {
		if math.IsNaN(b) || math.IsInf(b, 0) {
			t.Fatalf("beta[%d] = %v; collinearity must not produce NaN/Inf", i, b)
		}
	}
}

func TestNelderMeadEmptyStart(t *testing.T) {
	called := false
	got, v := nelderMead(func(p []float64) float64 { called = true; return 42 }, nil, 100, coldStep)
	if got != nil {
		t.Fatalf("nelderMead(nil start) params = %v; want nil", got)
	}
	if !called || v != 42 {
		t.Fatalf("value = %v; want the objective at nil, 42", v)
	}
}

func TestNelderMeadBuildsASimplexFromZeroStart(t *testing.T) {
	// A zero component cannot be scaled by (1+step); the offset is used
	// instead, otherwise that vertex would duplicate the start point and the
	// simplex would be degenerate.
	f := func(p []float64) float64 {
		// Minimum at (1, -2).
		return (p[0]-1)*(p[0]-1) + (p[1]+2)*(p[1]+2)
	}
	got, v := nelderMead(f, []float64{0, 0}, 2000, coldStep)
	if len(got) != 2 {
		t.Fatalf("params = %v; want 2", got)
	}
	if math.Abs(got[0]-1) > 1e-3 || math.Abs(got[1]+2) > 1e-3 {
		t.Fatalf("minimum at %v; want ~[1 -2]", got)
	}
	if v > 1e-6 {
		t.Fatalf("objective at the minimum = %v; want ~0", v)
	}
}

func TestNelderMeadShrinksOnAHardSurface(t *testing.T) {
	// Rosenbrock forces contraction and shrink steps, which a smooth quadratic
	// never exercises.
	f := func(p []float64) float64 {
		a, b := 1-p[0], p[1]-p[0]*p[0]
		return a*a + 100*b*b
	}
	got, v := nelderMead(f, []float64{-1.2, 1.0}, 4000, coldStep)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		t.Fatalf("objective = %v; want finite", v)
	}
	if v > 1.0 {
		t.Fatalf("objective = %v at %v; expected real progress toward [1 1]", v, got)
	}
}

func TestVarianceOfTooFewPoints(t *testing.T) {
	if got := variance(nil); got != 0 {
		t.Fatalf("variance(nil) = %v; want 0", got)
	}
	if got := variance([]float64{5}); got != 0 {
		t.Fatalf("variance of one point = %v; want 0", got)
	}
	// Sample variance (n-1) of a known set.
	if got := variance([]float64{2, 4, 4, 4, 5, 5, 7, 9}); math.Abs(got-4.571428571) > 1e-6 {
		t.Fatalf("variance = %v; want the sample variance ~4.5714", got)
	}
}

func TestPsiWeights(t *testing.T) {
	t.Run("h=0", func(t *testing.T) {
		if got := psiWeights(&arma{}, 0); len(got) != 0 {
			t.Fatalf("psiWeights(h=0) = %v; want empty", got)
		}
	})

	t.Run("pure AR(1) decays geometrically", func(t *testing.T) {
		a := &arma{phi: []float64{0.5}}
		psi := psiWeights(a, 4)
		want := []float64{1, 0.5, 0.25, 0.125}
		for i := range want {
			if math.Abs(psi[i]-want[i]) > 1e-12 {
				t.Fatalf("psi[%d] = %v; want %v", i, psi[i], want[i])
			}
		}
	})

	t.Run("pure MA(1) cuts off", func(t *testing.T) {
		a := &arma{theta: []float64{0.3}}
		psi := psiWeights(a, 4)
		want := []float64{1, 0.3, 0, 0}
		for i := range want {
			if math.Abs(psi[i]-want[i]) > 1e-12 {
				t.Fatalf("psi[%d] = %v; want %v", i, psi[i], want[i])
			}
		}
	})
}

func TestCSSResidualsWithMATerms(t *testing.T) {
	// q > 0 with t-1-j < 0 early in the series exercises the index guard.
	y := []float64{1, 2, 1.5, 2.5, 2, 3, 2.5, 3.5, 3, 4}
	e := cssResiduals(y, []float64{0.5}, []float64{0.4})
	if len(e) != len(y)-1 {
		t.Fatalf("len(residuals) = %d; want %d", len(e), len(y)-1)
	}
	for i, v := range e {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("residual[%d] = %v; want finite", i, v)
		}
	}
}

func TestArmaForecastWithShortHistory(t *testing.T) {
	// p and q larger than the available history drive the idx < 0 branches.
	a := &arma{phi: []float64{0.4, 0.2, 0.1}, theta: []float64{0.3, 0.2}, sigma2: 1}
	out := armaForecast([]float64{1.0}, a, 4)
	if len(out) != 4 {
		t.Fatalf("len = %d; want 4", len(out))
	}
	for i, v := range out {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("forecast[%d] = %v; want finite", i, v)
		}
	}
}

func TestFitARMARejectsNonFiniteInput(t *testing.T) {
	// A NaN anywhere in the series makes the sum-of-squares objective NaN.
	// The optimiser must see +Inf — a worst-possible score it will move away
	// from — rather than NaN, which compares false against everything and
	// would silently freeze the search.
	y := make([]float64, 40)
	for i := range y {
		y[i] = float64(i % 5)
	}
	y[17] = math.NaN()

	a, err := fitARMA(y, 1, 1)
	if err != nil {
		return // refusing outright is also acceptable
	}
	for i, v := range a.phi {
		if math.IsNaN(v) {
			t.Fatalf("phi[%d] is NaN; a non-finite input must not yield NaN parameters", i)
		}
	}
	for i, v := range a.theta {
		if math.IsNaN(v) {
			t.Fatalf("theta[%d] is NaN", i)
		}
	}
}

func TestNelderMeadOnADiscontinuousSurface(t *testing.T) {
	// A step surface defeats reflection, expansion and contraction in turn,
	// which is what forces the shrink step. The guarantee under test is not
	// that it finds the optimum — it may not — but that it terminates and
	// returns finite parameters rather than diverging.
	f := func(p []float64) float64 {
		s := 0.0
		for _, v := range p {
			s += math.Floor(math.Abs(v)*4) / 4
		}
		return s
	}
	got, v := nelderMead(f, []float64{3.7, -2.9}, 3000, coldStep)
	if len(got) != 2 {
		t.Fatalf("params = %v; want 2", got)
	}
	for i, p := range got {
		if math.IsNaN(p) || math.IsInf(p, 0) {
			t.Fatalf("param[%d] = %v; want finite", i, p)
		}
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		t.Fatalf("objective = %v; want finite", v)
	}
	if start := f([]float64{3.7, -2.9}); v > start {
		t.Fatalf("objective got worse: %v from a start of %v", v, start)
	}
}

func TestNelderMeadOnAFlatSurface(t *testing.T) {
	// Every vertex scores identically, so no move ever improves. The simplex
	// must collapse and the loop must exit on its iteration bound.
	f := func(p []float64) float64 { return 1.0 }
	got, v := nelderMead(f, []float64{1, 2, 3}, 500, coldStep)
	if len(got) != 3 || v != 1.0 {
		t.Fatalf("flat surface = %v,%v; want 3 finite params and 1.0", got, v)
	}
	for i, p := range got {
		if math.IsNaN(p) {
			t.Fatalf("param[%d] is NaN on a flat surface", i)
		}
	}
}
