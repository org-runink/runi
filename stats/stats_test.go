package stats

import (
	"errors"
	"math"
	"testing"
)

func approx(t *testing.T, got, want float64, tol float64, what string) {
	t.Helper()
	if math.IsNaN(want) {
		if !math.IsNaN(got) {
			t.Fatalf("%s = %v; want NaN", what, got)
		}
		return
	}
	if math.Abs(got-want) > tol {
		t.Fatalf("%s = %v; want %v", what, got, want)
	}
}

func TestMean(t *testing.T) {
	approx(t, Mean([]float64{1, 2, 3, 4}), 2.5, 1e-12, "Mean")
	approx(t, Mean([]float64{5}), 5, 1e-12, "Mean of one")
	approx(t, Mean(nil), math.NaN(), 0, "Mean of nothing")
	approx(t, Mean([]float64{1, math.NaN(), 3}), math.NaN(), 0, "Mean with NaN")
}

func TestMeanIsCompensated(t *testing.T) {
	// A large value followed by many small ones: a naive running sum loses the
	// small ones entirely. Compensated summation keeps them.
	x := make([]float64, 10001)
	x[0] = 1e16
	for i := 1; i < len(x); i++ {
		x[i] = 1
	}
	want := (1e16 + 10000) / float64(len(x))
	got := Mean(x)
	if math.Abs(got-want) > 1e-3 {
		t.Fatalf("Mean = %.6f; want %.6f — the small values were lost", got, want)
	}
}

func TestVarianceAndStdDev(t *testing.T) {
	x := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	approx(t, Variance(x), 4.571428571428571, 1e-9, "sample Variance")
	approx(t, PopVariance(x), 4, 1e-9, "PopVariance")
	approx(t, StdDev(x), 2.138089935299395, 1e-9, "StdDev")
	approx(t, PopStdDev(x), 2, 1e-9, "PopStdDev")

	approx(t, Variance([]float64{7}), math.NaN(), 0, "Variance of one")
	approx(t, Variance(nil), math.NaN(), 0, "Variance of nothing")
	approx(t, PopVariance(nil), math.NaN(), 0, "PopVariance of nothing")
}

func TestMinMax(t *testing.T) {
	x := []float64{3, -1, 7, 0}
	approx(t, Min(x), -1, 0, "Min")
	approx(t, Max(x), 7, 0, "Max")
	approx(t, Min(nil), math.NaN(), 0, "Min of nothing")
	approx(t, Max(nil), math.NaN(), 0, "Max of nothing")
	approx(t, Min([]float64{1, math.NaN()}), math.NaN(), 0, "Min with NaN")
	approx(t, Max([]float64{1, math.NaN()}), math.NaN(), 0, "Max with NaN")
	approx(t, Min([]float64{4}), 4, 0, "Min of one")
}

func TestQuantileMatchesNumPyDefault(t *testing.T) {
	// Values checked against numpy.quantile(x, q) with its default linear
	// interpolation, so a reader can verify them outside Go.
	x := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for _, tc := range []struct{ q, want float64 }{
		{0, 1}, {0.25, 3.25}, {0.5, 5.5}, {0.75, 7.75}, {1, 10},
		{0.1, 1.9}, {0.9, 9.1},
	} {
		approx(t, Quantile(x, tc.q), tc.want, 1e-12, "Quantile")
	}
	approx(t, Median(x), 5.5, 1e-12, "Median")
	approx(t, IQR(x), 4.5, 1e-12, "IQR")
}

func TestQuantileGuards(t *testing.T) {
	x := []float64{1, 2, 3}
	for _, q := range []float64{-0.1, 1.1, math.NaN()} {
		approx(t, Quantile(x, q), math.NaN(), 0, "Quantile out of range")
	}
	approx(t, Quantile(nil, 0.5), math.NaN(), 0, "Quantile of nothing")
	approx(t, Quantile([]float64{9}, 0.3), 9, 0, "Quantile of one value")
	approx(t, Quantile([]float64{1, math.NaN()}, 0.5), math.NaN(), 0, "Quantile with NaN")
}

func TestQuantileDoesNotModifyInput(t *testing.T) {
	x := []float64{5, 1, 4, 2, 3}
	orig := append([]float64(nil), x...)
	Quantile(x, 0.5)
	for i := range orig {
		if x[i] != orig[i] {
			t.Fatalf("Quantile sorted the caller's slice in place: %v", x)
		}
	}
}

func TestDescribe(t *testing.T) {
	x := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	s := Describe(x)
	if s.N != 10 || s.NaN != 0 {
		t.Fatalf("N/NaN = %d/%d; want 10/0", s.N, s.NaN)
	}
	approx(t, s.Mean, 5.5, 1e-12, "Describe.Mean")
	approx(t, s.Median, 5.5, 1e-12, "Describe.Median")
	approx(t, s.Min, 1, 0, "Describe.Min")
	approx(t, s.Max, 10, 0, "Describe.Max")
	approx(t, s.Q1, 3.25, 1e-12, "Describe.Q1")
	approx(t, s.Q3, 7.75, 1e-12, "Describe.Q3")
}

func TestDescribeReportsNaNCountRatherThanHidingIt(t *testing.T) {
	s := Describe([]float64{1, math.NaN(), 3, math.NaN(), 5})
	if s.NaN != 2 {
		t.Fatalf("Describe.NaN = %d; want 2", s.NaN)
	}
	if s.N != 3 {
		t.Fatalf("Describe.N = %d; want 3 — N counts values actually used", s.N)
	}
	approx(t, s.Mean, 3, 1e-12, "Describe.Mean over the non-NaN values")
}

func TestDescribeOfNothing(t *testing.T) {
	s := Describe(nil)
	if s.N != 0 || s.NaN != 0 {
		t.Fatalf("N/NaN = %d/%d; want 0/0", s.N, s.NaN)
	}
	for name, v := range map[string]float64{
		"Mean": s.Mean, "StdDev": s.StdDev, "Min": s.Min,
		"Q1": s.Q1, "Median": s.Median, "Q3": s.Q3, "Max": s.Max,
	} {
		if !math.IsNaN(v) {
			t.Fatalf("Describe(nil).%s = %v; want NaN", name, v)
		}
	}

	// All-NaN input: nothing usable, and the count says why.
	s = Describe([]float64{math.NaN(), math.NaN()})
	if s.N != 0 || s.NaN != 2 || !math.IsNaN(s.Mean) {
		t.Fatalf("all-NaN Describe = %+v; want N=0 NaN=2 Mean=NaN", s)
	}
}

func TestDropNaN(t *testing.T) {
	clean, removed := DropNaN([]float64{1, math.NaN(), 2, math.NaN(), 3})
	if removed != 2 {
		t.Fatalf("removed = %d; want 2", removed)
	}
	if len(clean) != 3 || clean[0] != 1 || clean[1] != 2 || clean[2] != 3 {
		t.Fatalf("clean = %v; want [1 2 3]", clean)
	}
	clean, removed = DropNaN(nil)
	if len(clean) != 0 || removed != 0 {
		t.Fatalf("DropNaN(nil) = %v,%d; want empty,0", clean, removed)
	}
}

func TestPearson(t *testing.T) {
	x := []float64{1, 2, 3, 4, 5}

	perfect, err := Pearson(x, []float64{2, 4, 6, 8, 10})
	if err != nil {
		t.Fatal(err)
	}
	approx(t, perfect, 1, 1e-12, "Pearson of a perfect positive")

	inverse, err := Pearson(x, []float64{10, 8, 6, 4, 2})
	if err != nil {
		t.Fatal(err)
	}
	approx(t, inverse, -1, 1e-12, "Pearson of a perfect negative")

	// Checked against numpy.corrcoef.
	mixed, err := Pearson(x, []float64{2, 1, 4, 3, 5})
	if err != nil {
		t.Fatal(err)
	}
	approx(t, mixed, 0.8, 1e-9, "Pearson")
}

func TestPearsonErrorsAndUndefinedCases(t *testing.T) {
	if _, err := Pearson([]float64{1, 2}, []float64{1}); !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("err = %v; want ErrLengthMismatch", err)
	}
	if _, err := Pearson([]float64{1}, []float64{1}); !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v; want ErrEmpty", err)
	}
	// A constant column has no direction: undefined, not uncorrelated.
	got, err := Pearson([]float64{1, 1, 1}, []float64{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(got) {
		t.Fatalf("Pearson against a constant column = %v; want NaN, not 0", got)
	}
}

func TestPearsonMissesANonLinearRelationship(t *testing.T) {
	// The documented limitation, asserted so it stays true: y = x^2 over a
	// symmetric range is perfectly determined by x and has ~zero Pearson.
	x := []float64{-3, -2, -1, 0, 1, 2, 3}
	y := make([]float64, len(x))
	for i, v := range x {
		y[i] = v * v
	}
	p, err := Pearson(x, y)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(p) > 1e-9 {
		t.Fatalf("Pearson on a parabola = %v; want ~0", p)
	}
	// Spearman does not rescue this one either — the relationship is not
	// monotonic — which is why the docs say "monotonic", not "non-linear".
	s, err := Spearman(x, y)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(s) > 1e-9 {
		t.Fatalf("Spearman on a parabola = %v; want ~0", s)
	}
}

func TestSpearmanFindsMonotonicNonLinear(t *testing.T) {
	// y = x^3 is monotonic but not linear: Spearman is exactly 1, Pearson less.
	x := []float64{1, 2, 3, 4, 5, 6}
	y := make([]float64, len(x))
	for i, v := range x {
		y[i] = v * v * v
	}
	s, err := Spearman(x, y)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, s, 1, 1e-12, "Spearman of a monotonic relationship")

	p, _ := Pearson(x, y)
	if p >= 0.99 {
		t.Fatalf("Pearson = %v; expected it to be visibly below Spearman's 1", p)
	}
}

func TestSpearmanAveragesTiedRanks(t *testing.T) {
	// Ties must take the average rank, or the coefficient is wrong.
	x := []float64{1, 2, 2, 3}
	r := ranks(x)
	want := []float64{1, 2.5, 2.5, 4}
	for i := range want {
		approx(t, r[i], want[i], 1e-12, "rank")
	}

	s, err := Spearman([]float64{1, 2, 2, 3}, []float64{1, 2, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	approx(t, s, 1, 1e-12, "Spearman of identical tied series")
}

func TestSpearmanErrors(t *testing.T) {
	if _, err := Spearman([]float64{1, 2}, []float64{1}); !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("err = %v; want ErrLengthMismatch", err)
	}
	if _, err := Spearman([]float64{1}, []float64{1}); !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v; want ErrEmpty", err)
	}
}
