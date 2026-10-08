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
	"sort"
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
	return (sum + c) / float64(len(x))
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
func Quantile(x []float64, q float64) float64 {
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
	s.Min = Min(clean)
	s.Q1 = Quantile(clean, 0.25)
	s.Median = Median(clean)
	s.Q3 = Quantile(clean, 0.75)
	s.Max = Max(clean)
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
func ranks(x []float64) []float64 {
	type pair struct {
		v float64
		i int
	}
	ps := make([]pair, len(x))
	for i, v := range x {
		ps[i] = pair{v, i}
	}
	sort.SliceStable(ps, func(a, b int) bool { return ps[a].v < ps[b].v })

	out := make([]float64, len(x))
	for i := 0; i < len(ps); {
		j := i
		for j+1 < len(ps) && ps[j+1].v == ps[i].v {
			j++
		}
		// Ranks i+1 .. j+1 are tied; they all take the average.
		avg := (float64(i+1) + float64(j+1)) / 2
		for k := i; k <= j; k++ {
			out[ps[k].i] = avg
		}
		i = j + 1
	}
	return out
}
