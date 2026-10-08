package stats

import (
	"errors"
	"math"
	"testing"
)

func TestStandardiserRoundTrip(t *testing.T) {
	train := []float64{10, 12, 14, 16, 18}
	sc := FitStandardiser(train)

	z, err := sc.Transform(train)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, Mean(z), 0, 1e-12, "standardised mean")
	approx(t, StdDev(z), 1, 1e-12, "standardised sd")

	back, err := sc.Inverse(z)
	if err != nil {
		t.Fatal(err)
	}
	for i := range train {
		approx(t, back[i], train[i], 1e-9, "Inverse")
	}
}

func TestStandardiserUsesTrainingParametersOnTestData(t *testing.T) {
	// The whole point of the type. Test data must be scaled by the TRAINING
	// mean and sd; if it were re-fitted, its own mean would become 0 and the
	// distribution shift would be invisible.
	train := []float64{0, 1, 2, 3, 4}
	test := []float64{10, 11, 12}

	sc := FitStandardiser(train)
	z, err := sc.Transform(test)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(Mean(z)) < 1 {
		t.Fatalf("test data centred on itself (mean %v); the training mean must be used", Mean(z))
	}
	want := (10 - sc.Mean) / sc.StdDev
	approx(t, z[0], want, 1e-12, "test value scaled by training parameters")
}

func TestStandardiserOnAConstantColumn(t *testing.T) {
	sc := FitStandardiser([]float64{7, 7, 7, 7})
	z, err := sc.Transform([]float64{7, 7})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range z {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("z[%d] = %v; a constant column must not produce NaN/Inf", i, v)
		}
		if v != 0 {
			t.Fatalf("z[%d] = %v; want 0 for a constant column", i, v)
		}
	}
	if sc.StdDev != 1 {
		t.Fatalf("StdDev = %v; want the documented 1 for a constant column", sc.StdDev)
	}
}

func TestStandardiserNotFitted(t *testing.T) {
	var nilSc *Standardiser
	if _, err := nilSc.Transform([]float64{1}); !errors.Is(err, ErrNotFitted) {
		t.Fatalf("nil Transform err = %v; want ErrNotFitted", err)
	}
	if _, err := nilSc.Inverse([]float64{1}); !errors.Is(err, ErrNotFitted) {
		t.Fatalf("nil Inverse err = %v; want ErrNotFitted", err)
	}
	empty := FitStandardiser(nil)
	if _, err := empty.Transform([]float64{1}); !errors.Is(err, ErrNotFitted) {
		t.Fatalf("empty-fit Transform err = %v; want ErrNotFitted", err)
	}
	if _, err := empty.Inverse([]float64{1}); !errors.Is(err, ErrNotFitted) {
		t.Fatalf("empty-fit Inverse err = %v; want ErrNotFitted", err)
	}
}

func TestStandardiserSingleValue(t *testing.T) {
	// One observation has no sample standard deviation (NaN), which must be
	// handled like a constant column rather than propagating.
	sc := FitStandardiser([]float64{5})
	z, err := sc.Transform([]float64{5, 6})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range z {
		if math.IsNaN(v) {
			t.Fatalf("z[%d] is NaN after fitting on a single value", i)
		}
	}
}

func TestMinMaxScaler(t *testing.T) {
	train := []float64{10, 20, 30, 40, 50}
	sc := FitMinMax(train)

	z, err := sc.Transform(train)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, z[0], 0, 1e-12, "min maps to 0")
	approx(t, z[len(z)-1], 1, 1e-12, "max maps to 1")
	approx(t, z[2], 0.5, 1e-12, "midpoint")

	back, err := sc.Inverse(z)
	if err != nil {
		t.Fatal(err)
	}
	for i := range train {
		approx(t, back[i], train[i], 1e-9, "MinMax Inverse")
	}
}

func TestMinMaxDoesNotClipOutOfRangeValues(t *testing.T) {
	// Documented behaviour: a value beyond the fitted range maps outside [0,1]
	// rather than being silently clipped, because that is the signal that the
	// new data does not look like the training data.
	sc := FitMinMax([]float64{0, 10})
	z, err := sc.Transform([]float64{-5, 15})
	if err != nil {
		t.Fatal(err)
	}
	if z[0] >= 0 {
		t.Fatalf("below-range value mapped to %v; want < 0, not clipped", z[0])
	}
	if z[1] <= 1 {
		t.Fatalf("above-range value mapped to %v; want > 1, not clipped", z[1])
	}
}

func TestMinMaxConstantColumnAndNotFitted(t *testing.T) {
	sc := FitMinMax([]float64{3, 3, 3})
	z, err := sc.Transform([]float64{3, 3})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range z {
		if v != 0 {
			t.Fatalf("z[%d] = %v; want 0 for a constant column", i, v)
		}
	}

	var nilSc *MinMaxScaler
	if _, err := nilSc.Transform([]float64{1}); !errors.Is(err, ErrNotFitted) {
		t.Fatalf("nil Transform err = %v; want ErrNotFitted", err)
	}
	if _, err := nilSc.Inverse([]float64{1}); !errors.Is(err, ErrNotFitted) {
		t.Fatalf("nil Inverse err = %v; want ErrNotFitted", err)
	}
	empty := FitMinMax(nil)
	if _, err := empty.Transform([]float64{1}); !errors.Is(err, ErrNotFitted) {
		t.Fatalf("empty Transform err = %v; want ErrNotFitted", err)
	}
	if _, err := empty.Inverse([]float64{1}); !errors.Is(err, ErrNotFitted) {
		t.Fatalf("empty Inverse err = %v; want ErrNotFitted", err)
	}
	// Inverse of a constant column returns the constant.
	cz, err := sc.Inverse([]float64{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range cz {
		approx(t, v, 3, 1e-12, "constant Inverse")
		_ = i
	}
}

func TestSplitIndexAndSplit(t *testing.T) {
	if got := SplitIndex(10, 0.8); got != 8 {
		t.Fatalf("SplitIndex(10,0.8) = %d; want 8", got)
	}
	// Always leaves at least one observation on each side.
	if got := SplitIndex(10, 0.001); got != 1 {
		t.Fatalf("SplitIndex(10,0.001) = %d; want 1", got)
	}
	if got := SplitIndex(10, 0.999); got != 9 {
		t.Fatalf("SplitIndex(10,0.999) = %d; want 9", got)
	}
	for _, tc := range []struct {
		n    int
		frac float64
	}{{1, 0.5}, {0, 0.5}, {10, 0}, {10, 1}, {10, -0.5}, {10, math.NaN()}} {
		if got := SplitIndex(tc.n, tc.frac); got != 0 {
			t.Fatalf("SplitIndex(%d,%v) = %d; want 0", tc.n, tc.frac, got)
		}
	}

	x := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	train, test := Split(x, 0.7)
	if len(train) != 7 || len(test) != 3 {
		t.Fatalf("Split sizes = %d/%d; want 7/3", len(train), len(test))
	}
	// Order must be preserved: training data comes first, always.
	if train[0] != 1 || train[6] != 7 || test[0] != 8 {
		t.Fatalf("Split did not preserve order: train=%v test=%v", train, test)
	}
	if tr, te := Split(x, 0); tr != nil || te != nil {
		t.Fatalf("Split with an invalid fraction = %v,%v; want nil,nil", tr, te)
	}
}

func TestRollingFoldsOnlyTrainOnThePast(t *testing.T) {
	folds := RollingFolds(100, 4)
	if len(folds) != 4 {
		t.Fatalf("got %d folds; want 4", len(folds))
	}
	for i, f := range folds {
		if f.TrainStart != 0 {
			t.Fatalf("fold %d starts training at %d; expanding windows start at 0", i, f.TrainStart)
		}
		if f.TestStart < f.TrainEnd {
			t.Fatalf("fold %d validates on data it trained on: train ends %d, test starts %d",
				i, f.TrainEnd, f.TestStart)
		}
		if f.TestEnd <= f.TestStart || f.TrainEnd <= f.TrainStart {
			t.Fatalf("fold %d has an empty window: %+v", i, f)
		}
	}
	// Windows expand, and the last one reaches the end of the data.
	for i := 1; i < len(folds); i++ {
		if folds[i].TrainEnd <= folds[i-1].TrainEnd {
			t.Fatalf("fold %d did not expand the training window", i)
		}
	}
	if last := folds[len(folds)-1]; last.TestEnd != 100 {
		t.Fatalf("last fold ends at %d; want the full 100", last.TestEnd)
	}
}

func TestRollingFoldsGuards(t *testing.T) {
	for _, tc := range []struct{ n, k int }{{1, 1}, {0, 1}, {10, 0}, {10, -1}, {10, 10}, {10, 20}, {2, 5}} {
		if got := RollingFolds(tc.n, tc.k); got != nil {
			t.Fatalf("RollingFolds(%d,%d) = %v; want nil", tc.n, tc.k, got)
		}
	}
	if got := RollingFolds(4, 1); len(got) != 1 {
		t.Fatalf("RollingFolds(4,1) = %v; want one fold", got)
	}
}

func TestRollingFoldsInvariantHoldsForEveryValidInput(t *testing.T) {
	// The guard in RollingFolds is relied on to make every fold valid without
	// further checks. That is an argument in a comment; this is the proof.
	for n := 2; n <= 60; n++ {
		for k := 1; k < n; k++ {
			folds := RollingFolds(n, k)
			if len(folds) != k {
				t.Fatalf("n=%d k=%d: got %d folds; want %d", n, k, len(folds), k)
			}
			for i, f := range folds {
				if f.TrainEnd <= f.TrainStart {
					t.Fatalf("n=%d k=%d fold %d: empty training window %+v", n, k, i, f)
				}
				if f.TestEnd <= f.TestStart {
					t.Fatalf("n=%d k=%d fold %d: empty test window %+v", n, k, i, f)
				}
				if f.TestStart < f.TrainEnd {
					t.Fatalf("n=%d k=%d fold %d: leaks future into training %+v", n, k, i, f)
				}
				if f.TestEnd > n {
					t.Fatalf("n=%d k=%d fold %d: runs past the data %+v", n, k, i, f)
				}
			}
			if last := folds[len(folds)-1]; last.TestEnd != n {
				t.Fatalf("n=%d k=%d: last fold ends at %d; want %d", n, k, last.TestEnd, n)
			}
		}
	}
}
