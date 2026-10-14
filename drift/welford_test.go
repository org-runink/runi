package drift

import (
	"math"
	"math/rand/v2"
	"testing"
)

// relErr is |got − want| / |want|, or |got| when want is 0.
func relErr(got, want float64) float64 {
	if want == 0 {
		return math.Abs(got)
	}
	return math.Abs(got-want) / math.Abs(want)
}

// twoPass is the textbook reference: the mean first, then the squared
// deviations from it. Stable, but it needs the data twice.
func twoPass(xs []float64) (mean, variance float64) {
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	for _, x := range xs {
		variance += (x - mean) * (x - mean)
	}
	return mean, variance / float64(len(xs)-1)
}

func TestWelfordKnownAnswer(t *testing.T) {
	// 2 4 4 4 5 5 7 9: Σx = 40, mean 5. Deviations −3 −1 −1 −1 0 0 2 4,
	// squares 9 1 1 1 0 0 4 16, Σ = 32. Sample variance 32/7, population
	// variance 32/8 = 4, population sd 2.
	//
	// Step by step, (n, mean, m2) after each value:
	//   2: d = 2,   mean 2,    m2 += 2·(2−2)       = 0
	//   4: d = 2,   mean 3,    m2 += 2·(4−3)       = 2
	//   4: d = 1,   mean 10/3, m2 += 1·(2/3)       = 8/3
	//   4: d = 2/3, mean 7/2,  m2 += (2/3)·(1/2)   = 3
	var w Welford
	for i, x := range []float64{2, 4, 4, 4} {
		w.Add(x)
		if i == 3 && (w.Mean() != 3.5 || math.Abs(w.m2-3) > 1e-15) {
			t.Fatalf("after 2 4 4 4: mean %v m2 %v, want 3.5 and 3", w.Mean(), w.m2)
		}
	}
	for _, x := range []float64{5, 5, 7, 9} {
		w.Add(x)
	}
	if w.N() != 8 || w.Mean() != 5 {
		t.Fatalf("N %d mean %v, want 8 and 5", w.N(), w.Mean())
	}
	if relErr(w.Variance(), 32.0/7) > 1e-15 || relErr(w.PopVariance(), 4) > 1e-15 {
		t.Fatalf("variance %v pop %v, want 32/7 and 4", w.Variance(), w.PopVariance())
	}
	if relErr(w.StdDev(), math.Sqrt(32.0/7)) > 1e-15 {
		t.Fatalf("sd %v", w.StdDev())
	}
}

func TestWelfordEmptyAndSingle(t *testing.T) {
	var w Welford
	if !math.IsNaN(w.Mean()) || !math.IsNaN(w.Variance()) || !math.IsNaN(w.PopVariance()) || !math.IsNaN(w.StdDev()) {
		t.Fatal("empty accumulator must report NaN for every statistic")
	}
	w.Add(42)
	// One value has a mean and a zero population spread, but no sample
	// variance: n−1 = 0.
	if w.Mean() != 42 || w.PopVariance() != 0 || !math.IsNaN(w.Variance()) || !math.IsNaN(w.StdDev()) {
		t.Fatalf("single value: mean %v pop %v var %v", w.Mean(), w.PopVariance(), w.Variance())
	}
}

func TestWelfordMatchesTwoPass(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	for trial := range 50 {
		n := 2 + r.IntN(5000)
		xs := make([]float64, n)
		var w Welford
		for i := range xs {
			xs[i] = 100*r.NormFloat64() + 37
			w.Add(xs[i])
		}
		m, v := twoPass(xs)
		if relErr(w.Mean(), m) > 1e-12 || relErr(w.Variance(), v) > 1e-12 {
			t.Fatalf("trial %d n=%d: welford (%v, %v) two-pass (%v, %v)", trial, n, w.Mean(), w.Variance(), m, v)
		}
	}
}

// The reason Welford exists. Readings that move by about 1 around 1e9: the
// sum of squares is near 1e21, where float64 resolves steps of about 1e5, so
// the textbook one-pass formula Σx² − (Σx)²/n returns rounding noise many
// orders of magnitude off. The naive computation is part of the test, so the
// test shows it would fail an implementation that took the shortcut.
func TestWelfordLargeOffset(t *testing.T) {
	const offset = 1e9
	r := rand.New(rand.NewPCG(13, 14))
	xs := make([]float64, 1000)
	var w Welford
	var sum, sumSq float64
	for i := range xs {
		xs[i] = offset + r.NormFloat64()
		w.Add(xs[i])
		sum += xs[i]
		sumSq += xs[i] * xs[i]
	}
	_, want := twoPass(xs)
	n := float64(len(xs))
	naive := (sumSq - sum*sum/n) / (n - 1)
	if relErr(naive, want) < 0.5 {
		t.Fatalf("naive sum of squares gave %v against %v: the data no longer exposes cancellation", naive, want)
	}
	if relErr(w.Variance(), want) > 1e-6 {
		t.Fatalf("welford variance %v, two-pass %v (naive %v)", w.Variance(), want, naive)
	}
	if relErr(w.Mean(), offset) > 1e-9 {
		t.Fatalf("mean %v", w.Mean())
	}
}

// Merging any split of a series gives the statistics of the whole series.
func TestWelfordMergeAnySplit(t *testing.T) {
	r := rand.New(rand.NewPCG(15, 16))
	for trial := range 2000 {
		n := r.IntN(300)
		scale := math.Pow(10, float64(r.IntN(12)-4))
		var whole Welford
		parts := []Welford{{}, {}, {}}
		cut1 := r.IntN(n + 1)
		cut2 := cut1 + r.IntN(n-cut1+1)
		for i := range n {
			x := scale * (r.NormFloat64() + float64(r.IntN(3)))
			whole.Add(x)
			switch {
			case i < cut1:
				parts[0].Add(x)
			case i < cut2:
				parts[1].Add(x)
			default:
				parts[2].Add(x)
			}
		}
		// Merge in two different orders: the result must not depend on it.
		a := parts[0]
		a.Merge(parts[1])
		a.Merge(parts[2])
		b := parts[2]
		b.Merge(parts[0])
		b.Merge(parts[1])
		for _, m := range []Welford{a, b} {
			if m.N() != whole.N() {
				t.Fatalf("trial %d: N %d want %d", trial, m.N(), whole.N())
			}
			if n == 0 {
				continue
			}
			if math.Abs(m.Mean()-whole.Mean()) > 1e-12*(math.Abs(whole.Mean())+scale) {
				t.Fatalf("trial %d: mean %v want %v", trial, m.Mean(), whole.Mean())
			}
			if n > 1 && relErr(m.Variance(), whole.Variance()) > 1e-10 {
				t.Fatalf("trial %d (cuts %d %d of %d): variance %v want %v", trial, cut1, cut2, n, m.Variance(), whole.Variance())
			}
		}
	}
}

func TestWelfordMergeEdges(t *testing.T) {
	var w, o Welford
	o.Add(math.NaN())
	w.Merge(o) // empty into empty carries the skip count
	if w.N() != 0 || w.Skipped() != 1 {
		t.Fatalf("N %d skipped %d", w.N(), w.Skipped())
	}
	o.Add(3)
	o.Add(5)
	w.Merge(o) // non-empty into empty is a copy
	if w.N() != 2 || w.Mean() != 4 || w.Variance() != 2 || w.Skipped() != 2 {
		t.Fatalf("N %d mean %v var %v skipped %d", w.N(), w.Mean(), w.Variance(), w.Skipped())
	}
	w.Merge(Welford{}) // empty into non-empty changes nothing
	if w.N() != 2 || w.Mean() != 4 || w.Variance() != 2 {
		t.Fatalf("after empty merge: N %d mean %v var %v", w.N(), w.Mean(), w.Variance())
	}
	// {3,5} with {6,10}: whole {3,5,6,10}, mean 6, deviations −3 −1 0 4,
	// Σ² = 26, sample variance 26/3. Chan: d = 8−4 = 4, m2 = 2 + 8 + 16·(2·2/4) = 26.
	var p Welford
	p.Add(6)
	p.Add(10)
	w.Merge(p)
	if w.Mean() != 6 || relErr(w.Variance(), 26.0/3) > 1e-15 {
		t.Fatalf("mean %v var %v", w.Mean(), w.Variance())
	}
}

func TestWelfordSkipsNonFinite(t *testing.T) {
	var w Welford
	for _, x := range []float64{1, math.NaN(), math.Inf(1), 3, math.Inf(-1)} {
		w.Add(x)
	}
	if w.N() != 2 || w.Skipped() != 3 || w.Mean() != 2 {
		t.Fatalf("N %d skipped %d mean %v", w.N(), w.Skipped(), w.Mean())
	}
	w.Reset()
	if w != (Welford{}) {
		t.Fatalf("Reset left %+v", w)
	}
}

// Values near the float64 limit with opposite signs overflow x − mean. The
// mean is still right (0 here) and the variance is +Inf, the true value
// rounded, rather than the −Inf the plain update gives.
func TestWelfordNearFloatLimit(t *testing.T) {
	var w, a, b Welford
	w.Add(math.MaxFloat64)
	w.Add(-math.MaxFloat64)
	a.Add(math.MaxFloat64)
	b.Add(-math.MaxFloat64)
	a.Merge(b)
	for name, s := range map[string]Welford{"Add": w, "Merge": a} {
		if s.Mean() != 0 || !math.IsInf(s.Variance(), 1) {
			t.Errorf("%s: mean %v variance %v, want 0 and +Inf", name, s.Mean(), s.Variance())
		}
	}
	// The unguarded update, for comparison: it is what the guard prevents.
	mean, m2 := math.MaxFloat64, 0.0
	d := -math.MaxFloat64 - mean
	mean += d / 2
	m2 += d * (-math.MaxFloat64 - mean)
	if !(m2 < 0) {
		t.Fatalf("unguarded m2 %v: the inputs no longer overflow", m2)
	}
}
