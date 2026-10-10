// SPDX-License-Identifier: BSD-3-Clause

package stats

import (
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"testing/quick"
)

// ---------------------------------------------------------------------------
// References. ranksOldSPRK / pearsonOldSPRK / spearmanOldSPRK are the
// implementations this branch replaced, kept verbatim so the new ones can be
// held against them. ranksRefSPRK is a deliberately naive comparison sort,
// which is the real authority: it is what "average the ranks of a tied group"
// means, written so plainly there is nowhere for a bug to hide.
// ---------------------------------------------------------------------------

// sameValueSPRK is the equality that ties: numeric equality, with all NaNs
// counted as one value. It is what makes -0 tie with +0.
func sameValueSPRK(a, b float64) bool {
	return a == b || (a != a && b != b)
}

// lessValueSPRK orders values the way the ranking must: numerically, with
// every NaN above every number.
func lessValueSPRK(a, b float64) bool {
	an, bn := a != a, b != b
	if an || bn {
		return !an && bn
	}
	return a < b
}

// ranksRefSPRK returns 1-based ranks, tied groups taking the average, using a
// comparison sort and nothing clever.
func ranksRefSPRK(x []float64) []float64 {
	n := len(x)
	ord := make([]int, n)
	for i := range ord {
		ord[i] = i
	}
	sort.SliceStable(ord, func(a, b int) bool { return lessValueSPRK(x[ord[a]], x[ord[b]]) })
	out := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j+1 < n && sameValueSPRK(x[ord[j+1]], x[ord[i]]) {
			j++
		}
		avg := (float64(i+1) + float64(j+1)) / 2
		for k := i; k <= j; k++ {
			out[ord[k]] = avg
		}
		i = j + 1
	}
	return out
}

// radixSortOldSPRK is the sort that was in stats.go before this change.
func radixSortOldSPRK(keys []uint64, idx []int32) {
	n := len(keys)
	if n < 2 {
		return
	}
	tmpK := make([]uint64, n)
	tmpI := make([]int32, n)
	var count [256]int
	for shift := uint(0); shift < 64; shift += 8 {
		for i := range count {
			count[i] = 0
		}
		for _, k := range keys {
			count[(k>>shift)&0xff]++
		}
		if count[(keys[0]>>shift)&0xff] == n {
			continue
		}
		sum := 0
		for i := range count {
			c := count[i]
			count[i] = sum
			sum += c
		}
		for i, k := range keys {
			p := count[(k>>shift)&0xff]
			count[(k>>shift)&0xff] = p + 1
			tmpK[p], tmpI[p] = k, idx[i]
		}
		copy(keys, tmpK)
		copy(idx, tmpI)
	}
}

// ranksOldSPRK is the ranking that was in stats.go before this change.
func ranksOldSPRK(x []float64) []float64 {
	n := len(x)
	keys := make([]uint64, n)
	idx := make([]int32, n)
	for i, v := range x {
		keys[i] = sortableBits(v)
		idx[i] = int32(i)
	}
	radixSortOldSPRK(keys, idx)
	out := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j+1 < n && keys[j+1] == keys[i] {
			j++
		}
		avg := (float64(i+1) + float64(j+1)) / 2
		for k := i; k <= j; k++ {
			out[idx[k]] = avg
		}
		i = j + 1
	}
	return out
}

// pearsonOldSPRK is the three-pass Pearson that was in stats.go before this
// change: a compensated mean of each column, then a covariance loop.
func pearsonOldSPRK(x, y []float64) (float64, error) {
	if len(x) != len(y) {
		return math.NaN(), ErrLengthMismatch
	}
	if len(x) < 2 {
		return math.NaN(), ErrEmpty
	}
	mx, my := Mean(x), Mean(y)
	var sxy, sxx, syy float64
	for i := range x {
		dx, dy := x[i]-mx, y[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	den := math.Sqrt(sxx * syy)
	if den == 0 {
		return math.NaN(), nil
	}
	return sxy / den, nil
}

// spearmanOldSPRK is the whole of the old Spearman: old ranks, old Pearson.
func spearmanOldSPRK(x, y []float64) (float64, error) {
	if len(x) != len(y) {
		return math.NaN(), ErrLengthMismatch
	}
	if len(x) < 2 {
		return math.NaN(), ErrEmpty
	}
	return pearsonOldSPRK(ranksOldSPRK(x), ranksOldSPRK(y))
}

// spearmanRefSPRK is the authority: naive ranks, then the old two-pass Pearson
// over them.
func spearmanRefSPRK(x, y []float64) (float64, error) {
	if len(x) != len(y) {
		return math.NaN(), ErrLengthMismatch
	}
	if len(x) < 2 {
		return math.NaN(), ErrEmpty
	}
	return pearsonOldSPRK(ranksRefSPRK(x), ranksRefSPRK(y))
}

// ranksSPRK recovers 1-based average ranks from the doubled, centred
// deviations the implementation actually produces: d = 2*rank - (n+1).
func ranksSPRK(x []float64) []float64 {
	n := len(x)
	w := getRankWork(n)
	dev, _ := w.rankDevs(x)
	out := make([]float64, n)
	for i, d := range dev {
		out[i] = (float64(d) + float64(n) + 1) / 2
	}
	putRankWork(w)
	return out
}

// ---------------------------------------------------------------------------
// Properties
// ---------------------------------------------------------------------------

// specialsSPRK is every float64 shape that has ever broken a radix ranking:
// both zeroes, both infinities, both NaN signs, denormals, and the extremes.
var specialsSPRK = []float64{
	0,
	math.Copysign(0, -1),
	1, -1, 2, -2, 0.5,
	math.Inf(1), math.Inf(-1),
	math.NaN(),
	math.Float64frombits(0xFFF8000000000000), // a NEGATIVE NaN
	math.Float64frombits(0x7FF8000000000001), // a NaN with a payload
	math.SmallestNonzeroFloat64,
	-math.SmallestNonzeroFloat64,
	math.Float64frombits(0x000FFFFFFFFFFFFF), // the largest denormal
	math.MaxFloat64, -math.MaxFloat64,
	1e-300, -1e-300, 1e300, -1e300,
}

// columnSPRK is a generated pair of columns drawn from a small alphabet, so
// ties are the rule rather than the exception, and salted with the specials.
type columnSPRK struct {
	X, Y []float64
}

func (columnSPRK) Generate(r *rand.Rand, size int) reflect.Value {
	n := r.Intn(size + 1)
	// A small alphabet makes ties common; a large one makes them rare. Draw
	// both, and sometimes an alphabet of one, which is a constant column.
	alpha := 1 + r.Intn(6)
	pool := make([]float64, alpha)
	for i := range pool {
		switch r.Intn(3) {
		case 0:
			pool[i] = specialsSPRK[r.Intn(len(specialsSPRK))]
		case 1:
			pool[i] = float64(r.Intn(7) - 3)
		default:
			pool[i] = r.NormFloat64()
		}
	}
	draw := func() []float64 {
		out := make([]float64, n)
		for i := range out {
			out[i] = pool[r.Intn(alpha)]
		}
		return out
	}
	return reflect.ValueOf(columnSPRK{X: draw(), Y: draw()})
}

// The ranks must equal the naive comparison-sorted ones EXACTLY, not nearly.
// An average rank is a whole number of halves no larger than n, so every one
// of them is representable in a float64 without rounding; there is no
// arithmetic here for a tolerance to absorb. Anything looser than exact
// equality would hide a misplaced value, and at n = 200,000 one misplaced
// value moves Spearman by about 1e-15 -- far below any tolerance a
// correlation test would dare assert, which is why the ranks are checked
// here rather than only the coefficient.
func TestSpearmanRanksMatchComparisonSortSPRK(t *testing.T) {
	f := func(c columnSPRK) bool {
		got, want := ranksSPRK(c.X), ranksRefSPRK(c.X)
		if len(got) != len(want) {
			return false
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("x=%v\n rank[%d] = %v, want %v\n got  %v\n want %v",
					c.X, i, got[i], want[i], got, want)
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 4000}); err != nil {
		t.Fatal(err)
	}
}

// Spearman itself must agree with ranks-then-Pearson computed the slow way.
// 1e-12 absolute: the two sides sum the same integers in different orders and
// different units (the implementation works in doubled, centred ranks, the
// reference in raw ones), so they differ only by float rounding, which at
// these sizes is ~1e-15. One misassigned rank at n = 50 moves the result by
// ~1e-4, four orders of magnitude above the tolerance.
func TestSpearmanAgreesWithReferenceSPRK(t *testing.T) {
	f := func(c columnSPRK) bool {
		trimPairSPRK(&c)
		got, errGot := Spearman(c.X, c.Y)
		want, errWant := spearmanRefSPRK(c.X, c.Y)
		if (errGot == nil) != (errWant == nil) {
			t.Errorf("errors disagree: %v vs %v", errGot, errWant)
			return false
		}
		if errGot != nil {
			return true
		}
		if math.IsNaN(got) != math.IsNaN(want) {
			t.Errorf("x=%v y=%v: got %v, want %v", c.X, c.Y, got, want)
			return false
		}
		if math.IsNaN(got) {
			return true
		}
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("x=%v y=%v: got %v, want %v", c.X, c.Y, got, want)
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 4000}); err != nil {
		t.Fatal(err)
	}
}

// trimPairSPRK cuts the generated columns to a common length, since Spearman
// rejects a mismatch before it ranks anything.
func trimPairSPRK(c *columnSPRK) {
	if len(c.X) > len(c.Y) {
		c.X = c.X[:len(c.Y)]
	} else {
		c.Y = c.Y[:len(c.X)]
	}
}

// And it must agree with the implementation it replaced, on every input where
// the two are meant to agree -- which is every input holding neither a NaN nor
// a negative zero, the two cases the old one got wrong and this one fixes.
func TestSpearmanMatchesOldImplementationSPRK(t *testing.T) {
	r := rand.New(rand.NewSource(1234))
	for trial := 0; trial < 2000; trial++ {
		n := 2 + r.Intn(300)
		x, y := make([]float64, n), make([]float64, n)
		for i := range x {
			// A small, randomly sized alphabet: ties everywhere, which is
			// where a ranking goes wrong.
			x[i] = float64(r.Intn(1 + r.Intn(12)))
			y[i] = float64(r.Intn(1+r.Intn(12))) + 0.5*x[i]
		}
		got, errGot := Spearman(x, y)
		want, errWant := spearmanOldSPRK(x, y)
		if (errGot == nil) != (errWant == nil) {
			t.Fatalf("n=%d: errors disagree: %v vs %v", n, errGot, errWant)
		}
		if math.IsNaN(got) != math.IsNaN(want) {
			t.Fatalf("n=%d: got %v, old gave %v", n, got, want)
		}
		if !math.IsNaN(got) && math.Abs(got-want) > 1e-12 {
			t.Fatalf("n=%d: got %v, old gave %v", n, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// The named edge cases
// ---------------------------------------------------------------------------

func TestSpearmanRankShapesSPRK(t *testing.T) {
	negNaN := math.Float64frombits(0xFFF8000000000000)
	denorm := math.SmallestNonzeroFloat64
	asc := make([]float64, 64)
	desc := make([]float64, 64)
	for i := range asc {
		asc[i] = float64(i)
		desc[i] = float64(len(desc) - i)
	}
	allButOne := make([]float64, 50)
	for i := range allButOne {
		allButOne[i] = 4
	}
	allButOne[17] = -1

	cases := []struct {
		name string
		in   []float64
	}{
		{"empty", nil},
		{"single", []float64{3}},
		{"pair", []float64{2, 1}},
		{"all identical", []float64{7, 7, 7, 7, 7}},
		{"two values repeated", []float64{1, 2, 1, 2, 1, 2, 2, 1, 1, 2}},
		{"all tied but one", allButOne},
		{"already sorted", asc},
		{"reverse sorted", desc},
		{"both zeroes", []float64{0, math.Copysign(0, -1), 0, math.Copysign(0, -1), 1}},
		{"infinities", []float64{math.Inf(1), math.Inf(-1), 0, math.Inf(1), -1}},
		{"nans", []float64{math.NaN(), negNaN, 1, math.NaN(), math.Inf(1)}},
		{"denormals", []float64{denorm, -denorm, 0, denorm, 2 * denorm}},
		{"huge and tiny", []float64{math.MaxFloat64, -math.MaxFloat64, 1e-300, -1e-300, 0}},
		{"everything", specialsSPRK},
	}
	for _, c := range cases {
		got, want := ranksSPRK(c.in), ranksRefSPRK(c.in)
		if len(got) != len(want) {
			t.Fatalf("%s: got %d ranks, want %d", c.name, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: rank[%d] = %v, want %v\n got  %v\n want %v",
					c.name, i, got[i], want[i], got, want)
				break
			}
		}
	}
}

// -0 and +0 are the SAME NUMBER, so they must share a rank. They are also the
// two bit patterns a radix sort over raw bits puts furthest apart, which is
// why the old implementation ranked them one after the other. This pins both
// the fix and the bug it fixes.
func TestSpearmanZeroSignTiesSPRK(t *testing.T) {
	in := []float64{0, math.Copysign(0, -1)}
	got := ranksSPRK(in)
	if got[0] != 1.5 || got[1] != 1.5 {
		t.Fatalf("+0 and -0 got ranks %v; they are equal and must share rank 1.5", got)
	}
	if old := ranksOldSPRK(in); old[0] == old[1] {
		t.Fatal("the old ranking is expected to split +0 from -0; it no longer does, so this test has lost its point")
	}
	// A column of zeroes of mixed sign is a CONSTANT column, so there is no
	// correlation to report. The old one saw two distinct values and reported
	// a number.
	zeroes := []float64{0, math.Copysign(0, -1), 0, math.Copysign(0, -1)}
	s, err := Spearman(zeroes, []float64{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(s) {
		t.Fatalf("Spearman against a column of signed zeroes = %v; want NaN, the constant-column answer", s)
	}
	if old, _ := spearmanOldSPRK(zeroes, []float64{1, 2, 3, 4}); math.IsNaN(old) {
		t.Fatal("the old Spearman is expected to report a number here; it no longer does")
	}
}

// Every NaN is one value: tied with the other NaNs and above every number,
// whatever its sign or payload. The old mapping sent a NEGATIVE NaN below
// negative infinity, which no reading of "NaN sorts high" supports.
func TestSpearmanNaNTiesAndRanksHighSPRK(t *testing.T) {
	negNaN := math.Float64frombits(0xFFF8000000000000)
	payload := math.Float64frombits(0x7FF8000000000001)
	in := []float64{math.Inf(-1), math.NaN(), negNaN, payload, math.Inf(1)}
	got := ranksSPRK(in)
	// -Inf rank 1, +Inf rank 2, the three NaNs tie across ranks 3, 4 and 5.
	want := []float64{1, 4, 4, 4, 2}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranks = %v, want %v", got, want)
		}
	}
	if old := ranksOldSPRK(in); old[2] != 1 {
		t.Fatalf("the old ranking is expected to put a negative NaN at the bottom (rank 1); it gave %v", old)
	}
}

// Ranks sum to n(n+1)/2 whatever the ties, which is the invariant the whole
// doubled-and-centred representation rests on: it is exactly the statement
// that the deviations sum to zero.
func TestSpearmanDeviationsSumToZeroSPRK(t *testing.T) {
	r := rand.New(rand.NewSource(99))
	for trial := 0; trial < 500; trial++ {
		n := r.Intn(400)
		x := make([]float64, n)
		for i := range x {
			x[i] = specialsSPRK[r.Intn(len(specialsSPRK))]
		}
		w := getRankWork(n)
		dev, sxx := w.rankDevs(x)
		var sum int64
		for _, d := range dev {
			sum += int64(d)
		}
		putRankWork(w)
		if sum != 0 {
			t.Fatalf("n=%d: deviations sum to %d, not 0", n, sum)
		}
		if sxx < 0 {
			t.Fatalf("n=%d: sum of squares is %v", n, sxx)
		}
	}
}

// The scratch is pooled and reused, growing when a bigger column arrives and
// being handed straight back when a smaller one does. The answer cannot
// depend on what the previous caller left in the buffers.
func TestSpearmanScratchPoolReuseSPRK(t *testing.T) {
	for _, n := range []int{1000, 10, 5000, 3, 2} {
		x := make([]float64, n)
		y := make([]float64, n)
		for i := range x {
			x[i] = float64((i * 7919) % 101)
			y[i] = float64(i)
		}
		got, err := Spearman(x, y)
		if err != nil {
			t.Fatal(err)
		}
		want, err := spearmanRefSPRK(x, y)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(got-want) > 1e-12 {
			t.Fatalf("n=%d: got %v, want %v", n, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Pearson
// ---------------------------------------------------------------------------

// The one-pass shifted form must agree with the three-pass form it replaced.
// 1e-12 absolute on a quantity bounded by 1: both sides are sums of n products
// in float64, so each carries roughly n*eps ~ 1e-14 of relative error at these
// sizes. A tolerance tighter than that would be testing the rounding; one
// looser would stop catching a wrong formula, which moves the answer by whole
// percent.
func TestPearsonOnePassMatchesThreePassSPRK(t *testing.T) {
	f := func(c columnSPRK) bool {
		trimPairSPRK(&c)
		got, errGot := Pearson(c.X, c.Y)
		want, errWant := pearsonOldSPRK(c.X, c.Y)
		if (errGot == nil) != (errWant == nil) {
			t.Errorf("errors disagree: %v vs %v", errGot, errWant)
			return false
		}
		if errGot != nil {
			return true
		}
		if constantSPRK(c.X) || constantSPRK(c.Y) {
			// A constant column is undefined, and the one-pass form says so
			// every time. The three-pass form does not: its centre is a
			// ROUNDED mean, so subtracting it from a constant that the mean
			// cannot represent exactly leaves a residue of about one ulp, and
			// the correlation of two such residues comes back as a confident
			// 1. See TestPearsonConstantColumnIsUndefinedSPRK.
			if !math.IsNaN(got) {
				t.Errorf("x=%v y=%v: constant column gave %v, want NaN", c.X, c.Y, got)
				return false
			}
			return true
		}
		if math.IsNaN(got) || math.IsNaN(want) {
			// Both forms report NaN for a constant column. The generated
			// specials also produce columns whose own arithmetic overflows --
			// an infinity in the data, or a value so large that the square of
			// a deviation is not representable -- where the answer is
			// undefined and the two forms are not obliged to agree on which
			// flavour of nonsense to return. On data the computation fits in,
			// they are.
			if math.IsNaN(got) != math.IsNaN(want) && representableSPRK(c.X) && representableSPRK(c.Y) {
				t.Errorf("x=%v y=%v: got %v, old gave %v", c.X, c.Y, got, want)
				return false
			}
			return true
		}
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("x=%v y=%v: got %v, old gave %v", c.X, c.Y, got, want)
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 4000}); err != nil {
		t.Fatal(err)
	}
}

// representableSPRK reports whether a correlation over x can be computed at
// all: every value finite, and small enough that the square of a deviation
// from another value in the same column still fits in a float64. A column
// holding both -1.8e308 and 0 does not qualify -- their difference squared is
// +Inf, so the sum of squares is +Inf whichever centre is subtracted, and
// every form of the calculation is reduced to guessing.
// constantSPRK reports whether every value in x is the same value, which is
// the case that has no correlation to report.
func constantSPRK(x []float64) bool {
	for _, v := range x {
		if !sameValueSPRK(v, x[0]) {
			return false
		}
	}
	return true
}

func representableSPRK(x []float64) bool {
	const limit = 1.3e154 // just under sqrt(MaxFloat64)
	for _, v := range x {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > limit {
			return false
		}
	}
	return true
}

// A large mean with a tiny spread is where the textbook one-pass formula
// (sum(xy) - n*xbar*ybar) loses every digit it has. The data here is built so
// the answer is EXACTLY 1: y - 2e9 is exactly twice x - 1e9, and every value
// is exactly representable, so any departure from 1 is the formula's own.
func TestPearsonLargeMeanTinyVarianceSPRK(t *testing.T) {
	const n = 2000
	x, y := make([]float64, n), make([]float64, n)
	for i := range x {
		x[i] = 1e9 + float64(i)/1024
		y[i] = 2e9 + float64(i)/512
	}
	got, err := Pearson(x, y)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-1) > 1e-12 {
		t.Fatalf("Pearson of an exactly affine pair at 1e9 = %v; want 1", got)
	}
	old, err := pearsonOldSPRK(x, y)
	if err != nil {
		t.Fatal(err)
	}
	// The naive one-pass form, for contrast: this is what must NOT be shipped.
	var sxy, sxx, syy, sx, sy float64
	for i := range x {
		sx += x[i]
		sy += y[i]
		sxy += x[i] * y[i]
		sxx += x[i] * x[i]
		syy += y[i] * y[i]
	}
	naive := (sxy - sx*sy/n) / math.Sqrt((sxx-sx*sx/n)*(syy-sy*sy/n))
	if math.Abs(naive-1) <= 1e-12 {
		t.Fatalf("the naive form gave %v, within tolerance -- this test no longer demonstrates anything", naive)
	}
	t.Logf("shifted %.17g (err %.3g); three-pass %.17g (err %.3g); naive %.17g (err %.3g)",
		got, math.Abs(got-1), old, math.Abs(old-1), naive, math.Abs(naive-1))
}

// "Undefined is not unrelated": a constant column has no direction to
// correlate with and must report NaN. The three-pass form only managed that
// when the column's mean happened to be exactly representable. When it was
// not -- which is most real numbers -- subtracting the rounded mean left
// about an ulp of residue in every row, and correlating one column of residue
// against another returned a confident 1 for data that is pure rounding.
func TestPearsonConstantColumnIsUndefinedSPRK(t *testing.T) {
	for _, v := range []float64{-1.5169628122204337, 0.1, 1.0 / 3.0, 7, 1e-300, 1e300} {
		col := make([]float64, 13)
		for i := range col {
			col[i] = v
		}
		got, err := Pearson(col, col)
		if err != nil {
			t.Fatal(err)
		}
		if !math.IsNaN(got) {
			t.Fatalf("Pearson of a constant column of %v = %v; want NaN", v, got)
		}
		other := make([]float64, len(col))
		for i := range other {
			other[i] = float64(i)
		}
		if got, err = Pearson(col, other); err != nil || !math.IsNaN(got) {
			t.Fatalf("Pearson of a constant column of %v against a ramp = %v (%v); want NaN", v, got, err)
		}
	}
	// And the case the old form got wrong, pinned: a constant whose mean does
	// not round to itself.
	col := make([]float64, 13)
	for i := range col {
		col[i] = -1.5169628122204337
	}
	if old, _ := pearsonOldSPRK(col, col); math.IsNaN(old) {
		t.Fatal("the three-pass form is expected to report a number for this constant column; it no longer does, so this test has lost its point")
	}
}

// Columns whose squares sit near the ends of the float64 range still have a
// correlation. sum*sum/n would overflow the correction and sqrt(cxx*cyy) would
// overflow -- or underflow -- the denominator; the forms used avoid both.
func TestPearsonExtremeMagnitudesSPRK(t *testing.T) {
	const n = 1000
	x, y := make([]float64, n), make([]float64, n)
	for i := range x {
		x[i] = float64(i) * 1e149
		y[i] = float64(i) * 2e149
	}
	got, err := Pearson(x, y)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-1) > 1e-9 {
		t.Fatalf("Pearson of two columns around 1e152 = %v; want 1", got)
	}
	// And the other end: columns so small that cxx*cyy underflows to zero.
	for i := range x {
		x[i] = float64(i) * 1e-160
		y[i] = float64(i) * 2e-160
	}
	got, err = Pearson(x, y)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-1) > 1e-9 {
		t.Fatalf("Pearson of two columns around 1e-157 = %v; want 1", got)
	}
}

func TestPearsonEdgeCasesSPRK(t *testing.T) {
	if _, err := Pearson([]float64{1, 2, 3}, []float64{1, 2}); err != ErrLengthMismatch {
		t.Fatal("length mismatch not reported")
	}
	if _, err := Pearson([]float64{1}, []float64{1}); err != ErrEmpty {
		t.Fatal("a single point should be ErrEmpty")
	}
	if _, err := Pearson(nil, nil); err != ErrEmpty {
		t.Fatal("no points should be ErrEmpty")
	}
	// n = 2 is always perfectly correlated, in one direction or the other.
	up, err := Pearson([]float64{1, 2}, []float64{5, 9})
	if err != nil || math.Abs(up-1) > 1e-15 {
		t.Fatalf("n=2 rising: %v, %v", up, err)
	}
	down, err := Pearson([]float64{1, 2}, []float64{9, 5})
	if err != nil || math.Abs(down+1) > 1e-15 {
		t.Fatalf("n=2 falling: %v, %v", down, err)
	}
	// A constant column, either side, is undefined and must say so.
	for _, pair := range [][2][]float64{
		{{1, 1, 1}, {1, 2, 3}},
		{{1, 2, 3}, {4, 4, 4}},
		{{2, 2}, {2, 2}},
	} {
		got, err := Pearson(pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if !math.IsNaN(got) {
			t.Fatalf("Pearson(%v, %v) = %v; want NaN, not a number", pair[0], pair[1], got)
		}
	}
	// A NaN or an infinity anywhere makes the answer undefined.
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		got, err := Pearson([]float64{1, 2, bad}, []float64{1, 2, 3})
		if err != nil {
			t.Fatal(err)
		}
		if !math.IsNaN(got) {
			t.Fatalf("Pearson with %v in the data = %v; want NaN", bad, got)
		}
		got, err = Pearson([]float64{1, 2, 3}, []float64{1, 2, bad})
		if err != nil {
			t.Fatal(err)
		}
		if !math.IsNaN(got) {
			t.Fatalf("Pearson with %v in the second column = %v; want NaN", bad, got)
		}
	}
	// Perfect, and perfectly inverted, at a size where the sums are long.
	n := 10000
	a, b, c := make([]float64, n), make([]float64, n), make([]float64, n)
	for i := range a {
		a[i] = float64(i)
		b[i] = 3*float64(i) + 11
		c[i] = -2*float64(i) + 7
	}
	if got, _ := Pearson(a, b); math.Abs(got-1) > 1e-12 {
		t.Fatalf("perfect positive = %v", got)
	}
	if got, _ := Pearson(a, c); math.Abs(got+1) > 1e-12 {
		t.Fatalf("perfect negative = %v", got)
	}
}

// Spearman's own error and degenerate paths, including the constant column
// that has no order to correlate with.
func TestSpearmanEdgeCasesSPRK(t *testing.T) {
	if _, err := Spearman([]float64{1, 2, 3}, []float64{1, 2}); err != ErrLengthMismatch {
		t.Fatal("length mismatch not reported")
	}
	if _, err := Spearman([]float64{1}, []float64{1}); err != ErrEmpty {
		t.Fatal("a single point should be ErrEmpty")
	}
	if _, err := Spearman(nil, nil); err != ErrEmpty {
		t.Fatal("no points should be ErrEmpty")
	}
	got, err := Spearman([]float64{5, 5, 5, 5}, []float64{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(got) {
		t.Fatalf("Spearman against a constant column = %v; want NaN", got)
	}
	// Monotone but wildly non-linear, with the special values mixed in: the
	// rank correlation is exactly 1 because the ORDER is identical. Exactly,
	// not approximately -- the sums are whole numbers and float64 carries
	// them without rounding at this size.
	x := []float64{
		-math.MaxFloat64, -1e300, -1, -math.SmallestNonzeroFloat64, 0,
		math.SmallestNonzeroFloat64, 1, 1e300, math.MaxFloat64, math.Inf(1),
	}
	y := make([]float64, len(x))
	for i := range y {
		y[i] = float64(i) * 3
	}
	if got, err := Spearman(x, y); err != nil || got != 1 {
		t.Fatalf("Spearman of an order-identical pair = %v (%v); want exactly 1", got, err)
	}
	for i := range y {
		y[i] = -y[i]
	}
	if got, err := Spearman(x, y); err != nil || got != -1 {
		t.Fatalf("Spearman of an order-reversed pair = %v (%v); want exactly -1", got, err)
	}
}

// rankKey must order exactly as the values do, must give equal values equal
// keys, and must put every NaN at the top.
func TestRankKeyOrdersValuesSPRK(t *testing.T) {
	ordered := []float64{
		math.Inf(-1), -math.MaxFloat64, -1e300, -1, -0.5,
		-math.SmallestNonzeroFloat64, 0, math.SmallestNonzeroFloat64,
		0.5, 1, 1e300, math.MaxFloat64, math.Inf(1),
	}
	for i := 1; i < len(ordered); i++ {
		if a, b := rankKey(ordered[i-1]), rankKey(ordered[i]); a >= b {
			t.Errorf("%v (%#x) should key below %v (%#x)", ordered[i-1], a, ordered[i], b)
		}
	}
	if rankKey(0) != rankKey(math.Copysign(0, -1)) {
		t.Error("-0 and +0 must share a key")
	}
	top := rankKey(math.Inf(1))
	for _, nan := range []float64{
		math.NaN(),
		math.Float64frombits(0xFFF8000000000000),
		math.Float64frombits(0x7FF8000000000001),
		math.Float64frombits(0xFFFFFFFFFFFFFFFF),
	} {
		if k := rankKey(nan); k <= top {
			t.Errorf("NaN %#x keyed to %#x, not above +Inf (%#x)", math.Float64bits(nan), k, top)
		}
		if k := rankKey(nan); k != rankKey(math.NaN()) {
			t.Errorf("NaN %#x keyed to %#x; every NaN must share one key", math.Float64bits(nan), k)
		}
	}
}
