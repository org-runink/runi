package stats

import (
	"math"
	"math/rand/v2"
	"sort"
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

// Quantiles orders its copy with a radix sort over IEEE-754 bit patterns
// rather than by comparison. That is the whole speed-up, and it is also the
// one way this could go wrong invisibly: a wrong bit mapping sorts negatives
// backwards, or puts -0 on the wrong side of 0, and every quantile is then
// quietly wrong rather than obviously broken.
//
// So the property is the direct one: the radix ordering IS the comparison
// ordering, for every input including the values a bit trick gets wrong.
func TestPropertyRadixOrderingMatchesComparisonOrdering(t *testing.T) {
	r := rand.New(rand.NewPCG(501, 502))
	awkward := []float64{
		0, math.Copysign(0, -1), 1, -1,
		math.Inf(1), math.Inf(-1),
		math.SmallestNonzeroFloat64, -math.SmallestNonzeroFloat64,
		math.MaxFloat64, -math.MaxFloat64,
		1e-308, -1e-308, 0.1, -0.1,
	}
	for i := 0; i < 3000; i++ {
		n := 1 + r.IntN(300)
		x := make([]float64, n)
		for j := range x {
			switch r.IntN(3) {
			case 0:
				x[j] = awkward[r.IntN(len(awkward))]
			case 1:
				x[j] = r.NormFloat64() * math.Pow(10, float64(r.IntN(60)-30))
			default:
				x[j] = float64(r.IntN(20) - 10)
			}
		}
		want := append([]float64(nil), x...)
		sort.Float64s(want)

		before := append([]float64(nil), x...)
		got := sortedCopy(x)
		if got == nil {
			t.Fatal("sortedCopy reported a NaN in input that has none")
		}

		// Element by element, the two orderings hold the same VALUES. Not the
		// same bits: -0 and +0 compare equal, so a comparison sort leaves
		// their relative order arbitrary while a radix sort over bit patterns
		// puts -0 first. Both orderings are correct, and the radix one has
		// the advantage of being deterministic.
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("n=%d position %d: radix gave %v, comparison gave %v", n, j, got[j], want[j])
			}
		}
		// It really is sorted, and it really is a permutation of the input:
		// equality element by element would also be satisfied by returning
		// the comparison sort's own output, which is not what is being tested.
		for j := 1; j < len(got); j++ {
			if got[j] < got[j-1] {
				t.Fatalf("n=%d: out of order at %d: %v then %v", n, j, got[j-1], got[j])
			}
		}
		if !sameMultiset(before, got) {
			t.Fatalf("n=%d: the sorted copy is not a permutation of the input", n)
		}
		// And the input is untouched.
		for j := range before {
			if math.Float64bits(x[j]) != math.Float64bits(before[j]) {
				t.Fatalf("input was modified at %d", j)
			}
		}
	}
}

// Quantiles returns exactly what the same number of Quantile calls would, in
// the order asked for. The optimisation is only allowed to be faster, not
// different.
func TestPropertyQuantilesAgreeWithQuantile(t *testing.T) {
	r := rand.New(rand.NewPCG(503, 504))
	for i := 0; i < 2000; i++ {
		n := 1 + r.IntN(200)
		x := make([]float64, n)
		for j := range x {
			x[j] = r.NormFloat64() * 100
		}
		qs := make([]float64, 1+r.IntN(6))
		for j := range qs {
			qs[j] = r.Float64()
		}
		got := Quantiles(x, qs...)
		if len(got) != len(qs) {
			t.Fatalf("asked for %d quantiles, got %d", len(qs), len(got))
		}
		for j, q := range qs {
			if want := Quantile(x, q); got[j] != want {
				t.Fatalf("q=%v: Quantiles gave %v, Quantile gave %v", q, got[j], want)
			}
		}
	}
}

// A NaN anywhere makes every quantile undefined, and an out-of-range q makes
// only its own undefined. Both are the documented rule.
func TestPropertyQuantilesNaNAndRangeRules(t *testing.T) {
	x := []float64{1, 2, 3, 4}
	for _, q := range Quantiles(append(append([]float64(nil), x...), math.NaN()), 0.5, 0.9) {
		if !math.IsNaN(q) {
			t.Fatalf("a NaN in the data gave a quantile of %v", q)
		}
	}
	got := Quantiles(x, -0.1, 0.5, 1.1, math.NaN())
	if !math.IsNaN(got[0]) || !math.IsNaN(got[2]) || !math.IsNaN(got[3]) {
		t.Fatalf("out-of-range q gave %v", got)
	}
	if math.IsNaN(got[1]) {
		t.Fatal("a valid q beside an invalid one came back NaN")
	}
	if len(Quantiles(nil, 0.5)) != 1 || !math.IsNaN(Quantiles(nil, 0.5)[0]) {
		t.Fatal("empty input should give one NaN")
	}
}

// sameMultiset reports whether a and b hold the same values with the same
// multiplicities, compared by bits so a lost -0 would show.
func sameMultiset(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	count := make(map[uint64]int, len(a))
	for _, v := range a {
		count[math.Float64bits(v)]++
	}
	for _, v := range b {
		k := math.Float64bits(v)
		count[k]--
		if count[k] < 0 {
			return false
		}
	}
	return true
}

// The sign of a zero is not meaningful in an ordering, and the radix sort puts
// -0 before +0 where a comparison sort may put either first. Pinned so the
// difference is a known property rather than a surprise, and so that a future
// change to sortableBits that moved a zero to the wrong END would be caught.
func TestZeroSignInOrdering(t *testing.T) {
	got := sortedCopy([]float64{1, math.Copysign(0, -1), 0, -1})
	if len(got) != 4 {
		t.Fatalf("got %d values", len(got))
	}
	if got[0] != -1 || got[3] != 1 {
		t.Fatalf("the extremes moved: %v", got)
	}
	if got[1] != 0 || got[2] != 0 {
		t.Fatalf("the zeroes are not in the middle: %v", got)
	}
	if !math.Signbit(got[1]) || math.Signbit(got[2]) {
		t.Fatalf("expected -0 then +0, got %v (signbits %v, %v)", got, math.Signbit(got[1]), math.Signbit(got[2]))
	}
}

// referenceQuantile is a deliberately naive implementation: sort with the
// standard library, then interpolate. It is the oracle the fast paths are
// checked against, because checking Quantiles against Quantile only proves
// they agree with each other.
func referenceQuantile(x []float64, q float64) float64 {
	if len(x) == 0 || q < 0 || q > 1 || math.IsNaN(q) {
		return math.NaN()
	}
	s := append([]float64(nil), x...)
	for _, v := range s {
		if math.IsNaN(v) {
			return math.NaN()
		}
	}
	sort.Float64s(s)
	if len(s) == 1 {
		return s[0]
	}
	pos := q * float64(len(s)-1)
	lo, hi := int(math.Floor(pos)), int(math.Ceil(pos))
	if lo == hi {
		return s[lo]
	}
	frac := pos - float64(lo)
	return s[lo]*(1-frac) + s[hi]*frac
}

// Quantiles has two strategies — selecting a few positions, and ordering the
// whole column — and a fallback inside the select when a partition goes badly.
// All three must give the answer a plain sort gives, for every input.
func TestPropertyQuantilesMatchASortedReference(t *testing.T) {
	r := rand.New(rand.NewPCG(505, 506))
	for i := 0; i < 4000; i++ {
		n := 1 + r.IntN(400)
		x := make([]float64, n)
		switch r.IntN(4) {
		case 0: // every value the same: the case that defeats a naive pivot
			v := r.NormFloat64()
			for j := range x {
				x[j] = v
			}
		case 1: // already sorted
			for j := range x {
				x[j] = float64(j)
			}
		case 2: // sorted backwards
			for j := range x {
				x[j] = float64(n - j)
			}
		default:
			for j := range x {
				x[j] = r.NormFloat64() * 1000
			}
		}

		// Few quantiles exercises the select path; many exercises the sort.
		count := 1 + r.IntN(3)
		if r.IntN(4) == 0 {
			count = 17 + r.IntN(8)
		}
		qs := make([]float64, count)
		for j := range qs {
			qs[j] = r.Float64()
		}

		got := Quantiles(x, qs...)
		for j, q := range qs {
			want := referenceQuantile(x, q)
			if math.Abs(got[j]-want) > 1e-9*(1+math.Abs(want)) {
				t.Fatalf("case %d n=%d q=%v: got %v, a sorted reference gives %v",
					i%4, n, q, got[j], want)
			}
		}
	}
}

// A column of identical values makes every partition split off one element, so
// the select burns its budget and falls back to ordering the range. That path
// is reached here rather than left untested.
func TestQuantilesFallBackOnADegenerateColumn(t *testing.T) {
	n := 5000
	x := make([]float64, n)
	for i := range x {
		x[i] = 7
	}
	for _, q := range Quantiles(x, 0, 0.25, 0.5, 0.99, 1) {
		if q != 7 {
			t.Fatalf("every value is 7, quantile came back %v", q)
		}
	}
	// Mostly identical, with a few outliers at known positions.
	x[0], x[1] = -1, -1
	x[n-1] = 100
	if got, want := Quantiles(x, 0)[0], -1.0; got != want {
		t.Errorf("min = %v, want %v", got, want)
	}
	if got, want := Quantiles(x, 1)[0], 100.0; got != want {
		t.Errorf("max = %v, want %v", got, want)
	}
}

// Asking for more quantiles than the select cutoff takes the ordering path.
func TestManyQuantilesTakeTheSortPath(t *testing.T) {
	r := rand.New(rand.NewPCG(507, 508))
	x := make([]float64, 2000)
	for i := range x {
		x[i] = r.NormFloat64()
	}
	qs := make([]float64, 25) // > selectCutoff
	for i := range qs {
		qs[i] = float64(i) / float64(len(qs)-1)
	}
	got := Quantiles(x, qs...)
	for i, q := range qs {
		if want := referenceQuantile(x, q); math.Abs(got[i]-want) > 1e-9 {
			t.Fatalf("q=%v: got %v, want %v", q, got[i], want)
		}
	}
	// And they come back non-decreasing, since the qs do.
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("quantiles are not monotone: %v then %v", got[i-1], got[i])
		}
	}
}

// sortedCopy reports a NaN by returning nil. Describe never hits this, because
// it drops NaN first, so it is exercised directly.
func TestSortedCopyRejectsNaN(t *testing.T) {
	if got := sortedCopy([]float64{1, math.NaN(), 2}); got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if got := sortedCopy([]float64{1}); len(got) != 1 || got[0] != 1 {
		t.Errorf("single value gave %v", got)
	}
	if got := sortedCopy(nil); got != nil && len(got) != 0 {
		t.Errorf("empty gave %v", got)
	}
}

// radixSortKeys guards a slice too short to reorder. radixSortFloats checks
// the same thing before calling it, so the guard is unreachable from outside
// and is exercised here directly.
func TestRadixSortKeysShortInput(t *testing.T) {
	for _, in := range [][]uint64{nil, {}, {42}} {
		got := append([]uint64(nil), in...)
		radixSortKeys(got)
		if len(got) != len(in) {
			t.Fatalf("length changed: %d -> %d", len(in), len(got))
		}
		for i := range in {
			if got[i] != in[i] {
				t.Fatalf("a %d-element slice was reordered", len(in))
			}
		}
	}
}
