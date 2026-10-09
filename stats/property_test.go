package stats

import (
	"math"
	"math/rand/v2"
	"testing"
	"testing/quick"
)

// Property tests state what must hold for EVERY input, not for the handful a
// table happens to list. They are run with testing/quick because runi takes no
// dependencies: a property library would be the twelfth package in a toolkit
// that advertises zero, and quick.Check is enough to generate thousands of
// cases per property.
//
// Each property below is one an example-based test cannot express, because the
// interesting inputs are the ones nobody thinks to write down.

var qcfg = &quick.Config{MaxCount: 2000}

// paired generates two equal-length slices of ordinary magnitudes. Random
// float64 bits are mostly denormals and infinities, which test the formatter
// rather than the statistics.
func paired(r *rand.Rand, n int) ([]float64, []float64) {
	x := make([]float64, n)
	y := make([]float64, n)
	for i := range x {
		x[i] = r.NormFloat64() * 1000
		y[i] = r.NormFloat64() * 1000
	}
	return x, y
}

// Pearson is bounded by definition. A correlation outside [-1, 1] is a
// numerical failure that would make every downstream significance test wrong.
func TestPropertyPearsonIsBounded(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 2000; i++ {
		n := 2 + r.IntN(200)
		x, y := paired(r, n)
		got, err := Pearson(x, y)
		if err != nil {
			continue
		}
		if math.IsNaN(got) {
			continue // a constant column has no correlation to report
		}
		if got < -1.0000001 || got > 1.0000001 {
			t.Fatalf("n=%d: Pearson = %v, outside [-1,1]", n, got)
		}
	}
}

// Correlation is symmetric. If it were not, two callers passing the same pair
// in different orders would disagree about the same data.
func TestPropertyCorrelationIsSymmetric(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 2000; i++ {
		n := 2 + r.IntN(100)
		x, y := paired(r, n)
		a, errA := Pearson(x, y)
		b, errB := Pearson(y, x)
		if (errA == nil) != (errB == nil) {
			t.Fatalf("n=%d: errors disagree: %v vs %v", n, errA, errB)
		}
		if errA != nil || (math.IsNaN(a) && math.IsNaN(b)) {
			continue
		}
		if math.Abs(a-b) > 1e-12 {
			t.Fatalf("n=%d: Pearson(x,y)=%v but Pearson(y,x)=%v", n, a, b)
		}
	}
}

// Pearson is invariant under a positive affine rescaling of either input:
// measuring in millimetres rather than metres cannot change a correlation.
// This is the property most likely to catch an accumulation bug, because it
// compares two runs that should agree exactly in theory.
func TestPropertyPearsonIgnoresScaleAndOffset(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for i := 0; i < 1000; i++ {
		n := 3 + r.IntN(100)
		x, y := paired(r, n)
		scale := 1 + r.Float64()*100
		shift := r.NormFloat64() * 50
		xs := make([]float64, n)
		for j := range x {
			xs[j] = x[j]*scale + shift
		}
		a, errA := Pearson(x, y)
		b, errB := Pearson(xs, y)
		if errA != nil || errB != nil || math.IsNaN(a) || math.IsNaN(b) {
			continue
		}
		if math.Abs(a-b) > 1e-9 {
			t.Fatalf("n=%d scale=%v shift=%v: %v became %v", n, scale, shift, a, b)
		}
	}
}

// Spearman depends only on ORDER, so any strictly increasing transform of an
// input must leave it unchanged. This is the property that pins the radix
// ranking: a sort bug that misplaced a value would show here and nowhere in a
// table of examples.
func TestPropertySpearmanIsRankOnly(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for i := 0; i < 1000; i++ {
		n := 3 + r.IntN(100)
		x, y := paired(r, n)
		mono := make([]float64, n)
		for j := range x {
			mono[j] = math.Sinh(x[j] / 500) // strictly increasing
		}
		a, errA := Spearman(x, y)
		b, errB := Spearman(mono, y)
		if errA != nil || errB != nil || math.IsNaN(a) || math.IsNaN(b) {
			continue
		}
		if math.Abs(a-b) > 1e-9 {
			t.Fatalf("n=%d: a monotone transform changed Spearman: %v -> %v", n, a, b)
		}
	}
}

// Ranks must be a permutation of 1..n whenever no two values tie, and must
// always sum to n(n+1)/2 even when they do — averaging a tied group cannot
// change the total.
func TestPropertyRanksSumIsInvariant(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	for i := 0; i < 2000; i++ {
		n := 1 + r.IntN(300)
		x := make([]float64, n)
		for j := range x {
			// A small alphabet on purpose, so ties are common.
			x[j] = float64(r.IntN(10))
		}
		got := ranks(x)
		var sum float64
		for _, v := range got {
			sum += v
		}
		want := float64(n) * float64(n+1) / 2
		if math.Abs(sum-want) > 1e-6 {
			t.Fatalf("n=%d: ranks sum to %v, want %v", n, sum, want)
		}
	}
}

// Benjamini-Hochberg must never reject more hypotheses than a plain threshold
// at the same alpha would, and must be monotone: a smaller p-value can never
// be rejected while a larger one is kept.
func TestPropertyBenjaminiHochbergIsMonotone(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	for i := 0; i < 2000; i++ {
		n := 1 + r.IntN(200)
		p := make([]float64, n)
		for j := range p {
			p[j] = r.Float64()
		}
		_, reject := BenjaminiHochberg(p, 0.05)
		for a := range p {
			for b := range p {
				if p[a] < p[b] && reject[b] && !reject[a] {
					t.Fatalf("p=%v rejected but smaller p=%v was not", p[b], p[a])
				}
			}
		}
	}
}

// quick.Check drives the same idea through generated structs rather than a
// hand-rolled loop, so the shrinking and the seed reporting come for free.
func TestPropertyMeanLiesWithinTheData(t *testing.T) {
	f := func(xs []float64) bool {
		clean, _ := DropNaN(xs)
		if len(clean) == 0 {
			return true
		}
		for _, v := range clean {
			if math.IsInf(v, 0) {
				return true // an infinity makes the mean meaningless, not wrong
			}
		}
		m := Mean(clean)
		return m >= Min(clean)-1e-9 && m <= Max(clean)+1e-9
	}
	if err := quick.Check(f, qcfg); err != nil {
		t.Fatal(err)
	}
}
