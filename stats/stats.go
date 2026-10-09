// SPDX-License-Identifier: BSD-3-Clause

// Package stats is the first ten minutes of a data problem: look at the
// numbers, split them, scale them, and check whether two columns move together.
//
// It exists because Go's standard library has none of this, and a data
// scientist arriving from pandas or scikit-learn hits that wall immediately.
// Nothing here is novel; all of it is textbook. The value is that it is
// correct, documented about its edges, and has no dependencies.
//
// # The one opinion
//
// Scalers are FITTED and then APPLIED. [Standardiser] and [MinMaxScaler] are
// computed from training data and then applied, unchanged, to validation and
// test data:
//
//	sc := stats.FitStandardiser(train)
//	trainZ := sc.Transform(train)
//	testZ := sc.Transform(test) // the TRAINING mean and sd, deliberately
//
// Re-fitting on the test set leaks its distribution into the model and makes
// every score that follows too optimistic. That mistake is easy to make and
// impossible to see in the output, so the API makes the correct thing the
// obvious thing: there is no function that scales a slice in one step.
//
// # NaN
//
// Functions here do NOT silently skip NaN. A NaN in the input produces a NaN in
// the output, because quietly dropping values changes the denominator and the
// caller is rarely told. Use [DropNaN] when that is what you mean.
//
// # Is it real?
//
// A correlation or a slope is an effect size: how strong a relationship looks,
// not how likely it is to be there at all. Two independent random series of
// four points clear |r| >= 0.5 about half the time by luck; at twenty points
// it is about 2.5%. So the effect sizes here come with their evidence:
//
//	c, ok := stats.Correlate(a, b)                  // r, n and the two-sided p-value
//	t := stats.Trend(y)                             // OLS slope, standard error, t and p
//	q, keep := stats.BenjaminiHochberg(ps, 0.05)    // across a whole family of tests
//
// [Correlate] refuses (ok=false) below [MinCorrelationSamples] aligned points,
// where a bare r misleads more than it informs. Testing many pairs at once
// compounds a per-test 5% error rate, so [Adjust] applies Benjamini–Hochberg,
// which controls the expected share of false findings among those kept. Every
// "not enough evidence" case returns p = 1, never 0 or NaN, so a caller that
// forgets to check still does the conservative thing.
//
// These are tests against zero (no correlation, no slope) under the usual
// Pearson and OLS assumptions, including independent observations. Two series
// that both trend will correlate whether or not they are related: detrend
// before you Correlate them. Nothing here tests causation. The Student-t tail
// comes from the regularised incomplete beta function (the standard library
// has no t-distribution), pinned by the tests to published critical values
// and to reference values within a relative error of 1e-9.

package stats

import (
	"errors"
	"math"
)

// ErrEmpty is returned when a computation has no data to work with.
var ErrEmpty = errors.New("stats: no data")

// ErrLengthMismatch is returned when two samples must be the same length.
var ErrLengthMismatch = errors.New("stats: samples have different lengths")

// Mean is the arithmetic mean. It returns NaN for an empty slice, since the
// mean of nothing is not zero.
func Mean(x []float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	// Compensated (Kahan–Neumaier) summation: a naive loop over a long series
	// of similar magnitudes loses low-order bits, which shows up as a mean that
	// disagrees with a reordering of the same data.
	var sum, c float64
	for _, v := range x {
		t := sum + v
		if math.Abs(sum) >= math.Abs(v) {
			c += (sum - t) + v
		} else {
			c += (v - t) + sum
		}
		sum = t
	}
	out := (sum + c) / float64(len(x))
	if !math.IsInf(out, 0) && !math.IsNaN(out) {
		return out
	}
	// The accumulator overflowed on values whose mean is perfectly
	// representable. Compensation cannot help here -- no single float64 can
	// hold the sum -- so fall back to averaging a scaled copy. The check above
	// costs one comparison and the extra pass is only ever paid by data that
	// has already overflowed.
	return meanScaled(x, out)
}

// meanScaled averages x after dividing through by the largest magnitude in it,
// so the running sum cannot overflow. fast is what the single-accumulator pass
// produced; it is returned unchanged when x holds a NaN or an infinity, since
// those make the mean genuinely undefined rather than merely unrepresentable.
//
// Its two guards cannot be reached through Mean, which is why it is a separate
// function rather than an inline branch: Mean only calls it after the fast
// path produced a non-finite result, and neither a series holding a NaN or an
// infinity nor an all-zero series can be rescued by rescaling -- the first is
// undefined and the second never overflows in the first place. Splitting them
// out is what lets them be tested with the input they exist for.
func meanScaled(x []float64, fast float64) float64 {
	var scale float64
	for _, v := range x {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fast
		}
		if a := math.Abs(v); a > scale {
			scale = a
		}
	}
	if scale == 0 {
		return fast
	}
	var sum, c float64
	for _, v := range x {
		v /= scale
		t := sum + v
		if math.Abs(sum) >= math.Abs(v) {
			c += (sum - t) + v
		} else {
			c += (v - t) + sum
		}
		sum = t
	}
	return (sum + c) / float64(len(x)) * scale
}

// Variance is the SAMPLE variance, dividing by n-1. Use [PopVariance] when the
// slice is the whole population rather than a sample from it. Fewer than two
// values gives NaN.
func Variance(x []float64) float64 {
	if len(x) < 2 {
		return math.NaN()
	}
	m := Mean(x)
	var s, c float64
	for _, v := range x {
		d := v - m
		y := d*d - c
		t := s + y
		c = (t - s) - y
		s = t
	}
	return s / float64(len(x)-1)
}

// PopVariance is the population variance, dividing by n.
func PopVariance(x []float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	m := Mean(x)
	var s float64
	for _, v := range x {
		d := v - m
		s += d * d
	}
	return s / float64(len(x))
}

// StdDev is the sample standard deviation: the square root of [Variance].
func StdDev(x []float64) float64 { return math.Sqrt(Variance(x)) }

// PopStdDev is the population standard deviation.
func PopStdDev(x []float64) float64 { return math.Sqrt(PopVariance(x)) }

// Min and Max return the smallest and largest value. Both return NaN for an
// empty slice, and propagate a NaN present in the input.
func Min(x []float64) float64 { return extreme(x, true) }

// Max returns the largest value, or NaN for an empty slice.
func Max(x []float64) float64 { return extreme(x, false) }

func extreme(x []float64, wantMin bool) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	out := x[0]
	for _, v := range x[1:] {
		if math.IsNaN(v) || math.IsNaN(out) {
			return math.NaN()
		}
		if (wantMin && v < out) || (!wantMin && v > out) {
			out = v
		}
	}
	return out
}

// Quantile returns the q-th quantile of x for q in [0,1], using linear
// interpolation between order statistics — the same definition as NumPy's
// default and R's type 7, so a number computed here matches one computed there.
//
// It does not modify x: the data is copied before sorting.
//
// Asking for several quantiles of the same column calls [Quantiles] instead:
// each call here sorts its own copy, so three quantiles of a 200,000-point
// column is three sorts of 200,000 points.
func Quantile(x []float64, q float64) float64 { return Quantiles(x, q)[0] }

// Quantiles returns several quantiles of x from ONE sorted copy, in the order
// asked for. It is the function to reach for when summarising a column, which
// is the common case: a median and a pair of tails is three calls to
// [Quantile] and therefore three copies and three sorts of the same data, for
// an answer that needs one.
//
// The copy is ordered by a radix sort over the IEEE-754 bit patterns rather
// than by comparison, which is linear in the length of the column instead of
// n·log n and does not call a comparator through an interface.
//
// Every quantile is NaN if x is empty or holds a NaN, and an individual
// quantile is NaN if its q is outside [0,1] — the same rule [Quantile]
// follows, applied element by element. Use [DropNaN] first to summarise a
// column that has gaps in it.
func Quantiles(x []float64, qs ...float64) []float64 {
	out := make([]float64, len(qs))
	if len(x) == 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	// A copy either way: the caller's data is never reordered.
	s := make([]float64, len(x))
	for i, v := range x {
		if math.IsNaN(v) { // a NaN anywhere makes every quantile undefined
			for j := range out {
				out[j] = math.NaN()
			}
			return out
		}
		s[i] = v
	}

	// A quantile needs at most two order statistics, so a handful of them
	// needs a handful of positions out of n. Putting just those positions in
	// place is linear in n; ordering the whole column to read five values out
	// of it is n·log n, and for a 200,000-point column that is most of the
	// work thrown away. Beyond a certain number of quantiles the selects stop
	// being cheaper than one ordering, and the sort wins.
	need := neededIndices(len(s), qs)
	if len(need) <= selectCutoff {
		multiSelect(s, need)
	} else {
		radixSortFloats(s)
	}
	for i, q := range qs {
		out[i] = quantileOfSorted(s, q)
	}
	return out
}

// selectCutoff is where selecting individual positions stops paying. Each
// select is a partition pass over what is left, so a few are much cheaper than
// an ordering and many are not.
const selectCutoff = 16

// neededIndices returns the sorted, deduplicated order statistics the given
// quantiles read, for a column of length n.
func neededIndices(n int, qs []float64) []int {
	seen := make([]int, 0, 2*len(qs))
	for _, q := range qs {
		if q < 0 || q > 1 || math.IsNaN(q) || n < 2 {
			continue
		}
		pos := q * float64(n-1)
		lo := int(math.Floor(pos))
		hi := int(math.Ceil(pos))
		seen = append(seen, lo, hi)
	}
	sortInts(seen)
	out := seen[:0]
	for i, k := range seen {
		if i == 0 || k != seen[i-1] {
			out = append(out, k)
		}
	}
	return out
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// multiSelect puts each index in ks holding the value it would hold if s were
// fully sorted. Everything else is left in an arbitrary order, which is all a
// quantile needs.
//
// ks comes from neededIndices, which returns it ascending and deduplicated, so
// each index is strictly greater than the one before and the window only ever
// moves forward. There is no guard here for an out-of-order ks because there
// is no way to reach one.
func multiSelect(s []float64, ks []int) {
	left := 0
	for _, k := range ks {
		quickSelect(s, left, len(s)-1, k)
		// Everything at or below k is now no greater than s[k], so the next
		// position can only be above it.
		left = k
	}
}

// quickSelect puts s[k] where it belongs within s[lo:hi+1].
//
// It falls back to ordering the range once it has partitioned more times than
// a well-behaved input ever needs. Median-of-three makes the quadratic case
// rare rather than impossible, and "rare" is not a guarantee a caller can rely
// on when the data is someone else's.
func quickSelect(s []float64, lo, hi, k int) {
	budget := 2 * bitLen(uint(hi-lo+1))
	for lo < hi {
		if budget <= 0 {
			radixSortFloats(s[lo : hi+1])
			return
		}
		budget--
		p := partition(s, lo, hi)
		switch {
		case k == p:
			return
		case k < p:
			hi = p - 1
		default:
			lo = p + 1
		}
	}
}

func bitLen(v uint) int {
	n := 0
	for v > 0 {
		n++
		v >>= 1
	}
	return n
}

// partition is Hoare's scheme around a median-of-three pivot, returning the
// pivot's final position.
func partition(s []float64, lo, hi int) int {
	mid := lo + (hi-lo)/2
	if s[mid] < s[lo] {
		s[mid], s[lo] = s[lo], s[mid]
	}
	if s[hi] < s[lo] {
		s[hi], s[lo] = s[lo], s[hi]
	}
	if s[hi] < s[mid] {
		s[hi], s[mid] = s[mid], s[hi]
	}
	// s[lo] <= s[mid] <= s[hi] now, so the median is the pivot. Park it at hi
	// and run Lomuto against it; parking it anywhere else while comparing
	// against s[hi] partitions around a value that is not the pivot.
	s[mid], s[hi] = s[hi], s[mid]
	pivot := s[hi]

	i := lo
	for j := lo; j < hi; j++ {
		if s[j] < pivot {
			s[i], s[j] = s[j], s[i]
			i++
		}
	}
	s[i], s[hi] = s[hi], s[i]
	return i
}

// sortedCopy returns x sorted ascending, or nil if x holds a NaN. The caller
// owns the copy; x is untouched.
func sortedCopy(x []float64) []float64 {
	out := make([]float64, len(x))
	for i, v := range x {
		if math.IsNaN(v) {
			return nil
		}
		out[i] = v
	}
	radixSortFloats(out)
	return out
}

// radixSortFloats orders a slice of non-NaN floats in place, by radix over
// their IEEE-754 bit patterns rather than by comparison.
func radixSortFloats(s []float64) {
	if len(s) < 2 {
		return
	}
	keys := make([]uint64, len(s))
	for i, v := range s {
		keys[i] = sortableBits(v)
	}
	radixSortKeys(keys)
	for i, k := range keys {
		s[i] = unsortableBits(k)
	}
}

// quantileOfSorted is the interpolation, given data already in order.
func quantileOfSorted(s []float64, q float64) float64 {
	if q < 0 || q > 1 || math.IsNaN(q) {
		return math.NaN()
	}
	if len(s) == 1 {
		return s[0]
	}
	pos := q * float64(len(s)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return s[lo]
	}
	frac := pos - float64(lo)
	return s[lo]*(1-frac) + s[hi]*frac
}

// Median is the 0.5 quantile.
func Median(x []float64) float64 { return Quantile(x, 0.5) }

// IQR is the interquartile range, Q3 − Q1: a spread measure that a few extreme
// values cannot move, unlike the standard deviation.
func IQR(x []float64) float64 { return Quantile(x, 0.75) - Quantile(x, 0.25) }

// Summary is what [Describe] reports: the shape of a column at a glance.
type Summary struct {
	N      int
	NaN    int // values that were NaN, excluded from every figure below
	Mean   float64
	StdDev float64 // sample standard deviation
	Min    float64
	Q1     float64
	Median float64
	Q3     float64
	Max    float64
}

// Describe summarises a column, the way you would look at it first.
//
// Unlike the rest of this package, Describe DOES exclude NaN — and tells you
// how many it excluded in [Summary.NaN], so the omission is visible rather than
// silent. N counts the values actually used.
func Describe(x []float64) Summary {
	clean, nans := DropNaN(x)
	s := Summary{N: len(clean), NaN: nans}
	if len(clean) == 0 {
		s.Mean, s.StdDev = math.NaN(), math.NaN()
		s.Min, s.Q1, s.Median, s.Q3, s.Max = math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN()
		return s
	}
	s.Mean = Mean(clean)
	s.StdDev = StdDev(clean)
	// One ordering for all five order statistics. DropNaN has already removed
	// the NaNs, so sortedCopy cannot fail here, and the extremes are the ends
	// of the sorted copy rather than two more passes over the column.
	sorted := sortedCopy(clean)
	s.Min = sorted[0]
	s.Max = sorted[len(sorted)-1]
	s.Q1 = quantileOfSorted(sorted, 0.25)
	s.Median = quantileOfSorted(sorted, 0.5)
	s.Q3 = quantileOfSorted(sorted, 0.75)
	return s
}

// DropNaN returns x without its NaN values, and how many were removed. The
// input is not modified.
func DropNaN(x []float64) (clean []float64, removed int) {
	clean = make([]float64, 0, len(x))
	for _, v := range x {
		if math.IsNaN(v) {
			removed++
			continue
		}
		clean = append(clean, v)
	}
	return clean, removed
}

// Pearson is the linear correlation coefficient between x and y, in [-1,1].
//
// It measures LINEAR association only: a perfect parabola through the origin
// has a Pearson correlation near zero. Reach for [Spearman] when the
// relationship may be monotonic but not straight.
func Pearson(x, y []float64) (float64, error) {
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
		// One of the columns is constant, so it has no direction to correlate
		// with. NaN rather than 0: "undefined" is not "unrelated".
		return math.NaN(), nil
	}
	return sxy / den, nil
}

// Spearman is the rank correlation: Pearson applied to the ranks, with ties
// given their average rank. It detects any monotonic relationship, straight
// or not.
func Spearman(x, y []float64) (float64, error) {
	if len(x) != len(y) {
		return math.NaN(), ErrLengthMismatch
	}
	if len(x) < 2 {
		return math.NaN(), ErrEmpty
	}
	return Pearson(ranks(x), ranks(y))
}

// ranks returns the 1-based ranks of x, averaging ranks within tied groups.
//
// It sorts by RADIX on the float's bit pattern rather than by comparison.
// Ranking dominates Spearman, and a comparison sort of n float64s costs
// n log n comparisons each reached through a function value; a radix sort is
// a fixed number of linear passes with no comparisons at all. At the sizes
// correlation is run on — hundreds of thousands of points — that is the
// difference between being slower than scipy and being faster than it.
//
// Stability is not needed: every member of a tied group receives the same
// averaged rank, so their order among themselves cannot change the result.
func ranks(x []float64) []float64 {
	n := len(x)
	keys := make([]uint64, n)
	idx := make([]int32, n)
	for i, v := range x {
		keys[i] = sortableBits(v)
		idx[i] = int32(i)
	}
	radixSort(keys, idx)

	out := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j+1 < n && keys[j+1] == keys[i] {
			j++
		}
		// Ranks i+1 .. j+1 are tied; they all take the average.
		avg := (float64(i+1) + float64(j+1)) / 2
		for k := i; k <= j; k++ {
			out[idx[k]] = avg
		}
		i = j + 1
	}
	return out
}

// sortableBits maps a float64 onto a uint64 whose unsigned order is the
// float's numeric order: flip every bit of a negative, set the sign bit of a
// positive. NaN sorts above every number, which is where the comparison
// version left it too.
func sortableBits(v float64) uint64 {
	b := math.Float64bits(v)
	if b&(1<<63) != 0 {
		return ^b
	}
	return b | (1 << 63)
}

// radixSort orders keys (and moves idx with them) by eight 8-bit passes,
// least significant first. Passes whose byte is identical across every key are
// skipped, which on real data — timestamps, prices, scores that share a high
// byte — usually removes two or three of the eight.
// unsortableBits is the inverse of sortableBits.
func unsortableBits(b uint64) float64 {
	if b&(1<<63) != 0 {
		return math.Float64frombits(b &^ (1 << 63))
	}
	return math.Float64frombits(^b)
}

// radixSortKeys orders bit patterns with no payload to carry alongside. It is
// the same eight-pass LSD radix as radixSort, without the index permutation,
// because sorting a column does not need to know where each value came from.
func radixSortKeys(keys []uint64) {
	n := len(keys)
	if n < 2 {
		return
	}
	tmp := make([]uint64, n)
	var count [256]int
	for shift := uint(0); shift < 64; shift += 8 {
		for i := range count {
			count[i] = 0
		}
		for _, k := range keys {
			count[(k>>shift)&0xFF]++
		}
		// A byte that is the same in every key cannot reorder anything, so the
		// pass is skipped. For a column of ordinary magnitudes that is most of
		// the high bytes, and skipping them is most of the speed.
		if count[(keys[0]>>shift)&0xFF] == n {
			continue
		}
		sum := 0
		for i := range count {
			c := count[i]
			count[i] = sum
			sum += c
		}
		for _, k := range keys {
			b := (k >> shift) & 0xFF
			tmp[count[b]] = k
			count[b]++
		}
		// Copied back rather than swapped, as radixSort does: skipping a pass
		// makes the number of passes odd, and a swap would then leave the
		// result in the scratch slice instead of the caller's.
		copy(keys, tmp)
	}
}

func radixSort(keys []uint64, idx []int32) {
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
			continue // every key shares this byte; the pass would be a copy
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
