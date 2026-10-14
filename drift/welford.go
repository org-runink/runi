// SPDX-License-Identifier: BSD-3-Clause

package drift

import "math"

// Welford is a running mean and variance in one pass and fixed memory.
//
// The obvious one-pass method keeps Σx and Σx² and takes
// (Σx² − (Σx)²/n)/(n−1) at the end. On data far from zero that subtracts two
// nearly equal huge numbers, and the variance it returns is mostly rounding:
// readings that move by a few units around 1e9 lose every significant digit
// of their spread, and the result can even come out negative. Welford's
// update keeps the mean and the sum of squared deviations from it instead,
// so nothing large is ever subtracted from anything large.
//
// Method: B. P. Welford, "Note on a method for calculating corrected sums of
// squares and products", Technometrics 4(3), 1962. Merge is the pairwise
// combination of T. F. Chan, G. H. Golub and R. J. LeVeque, "Updating
// formulae and a pairwise algorithm for computing sample variances", Stanford
// report STAN-CS-79-773, 1979, published as "Algorithms for computing the
// sample variance: analysis and recommendations", The American Statistician
// 37(3), 1983.
//
// The zero value is an empty accumulator, ready to use. A Welford is a plain
// value and safe to copy, which is how a snapshot is taken; it is not safe
// for concurrent use. NaN and ±Inf are skipped and counted in Skipped, never
// folded in: one NaN would otherwise poison the mean for the rest of the
// series. Finite values whose squared spread overflows float64 (beyond about
// 1e154) give a variance of +Inf, never NaN or a negative number; the mean
// stays finite. Rescale such data first if the variance matters.
type Welford struct {
	n       int64
	mean    float64
	m2      float64 // Σ (x − mean)², the corrected sum of squares
	skipped int64
}

// Add folds x into the running statistics. It does not allocate.
func (w *Welford) Add(x float64) {
	if !finite(x) {
		w.skipped++
		return
	}
	w.n++
	d := x - w.mean
	if math.IsInf(d, 0) {
		// x and the mean are both near the float64 limit with opposite signs.
		// Weighting each before adding keeps the mean finite and correct;
		// m2 then becomes +Inf, which is the true variance rounded.
		w.mean += x/float64(w.n) - w.mean/float64(w.n)
	} else {
		w.mean += d / float64(w.n)
	}
	// d·(x − new mean) is d²·(n−1)/n written so that both factors are
	// differences of nearby numbers. The new mean lies between the old mean
	// and x, so the two factors share a sign and m2 never decreases.
	w.m2 += d * (x - w.mean)
}

// N is the number of values folded in.
func (w *Welford) N() int64 { return w.n }

// Skipped is the number of non-finite values Add refused.
func (w *Welford) Skipped() int64 { return w.skipped }

// Mean is the arithmetic mean, NaN when nothing has been added.
func (w *Welford) Mean() float64 {
	if w.n == 0 {
		return math.NaN()
	}
	return w.mean
}

// Variance is the sample variance, with the n−1 divisor, NaN below two values.
func (w *Welford) Variance() float64 {
	if w.n < 2 {
		return math.NaN()
	}
	return w.m2 / float64(w.n-1)
}

// PopVariance is the population variance, with the n divisor, NaN when
// nothing has been added. Use it when the values are the whole population
// rather than a sample of one.
func (w *Welford) PopVariance() float64 {
	if w.n == 0 {
		return math.NaN()
	}
	return w.m2 / float64(w.n)
}

// StdDev is the square root of Variance, NaN below two values.
func (w *Welford) StdDev() float64 { return math.Sqrt(w.Variance()) }

// Merge folds o into w, as if every value added to o had been added to w,
// skipped ones included. The order the values arrived in is lost, which is
// fine for a mean and a variance and is why per-shard accumulators can be
// combined at the end. The result equals one pass over all the values up to
// rounding, not bit for bit.
func (w *Welford) Merge(o Welford) {
	w.skipped += o.skipped
	switch {
	case o.n == 0:
		return
	case w.n == 0:
		w.n, w.mean, w.m2 = o.n, o.mean, o.m2
		return
	}
	n := w.n + o.n
	d := o.mean - w.mean
	// Chan et al.: the combined mean moves towards o by o's share of the
	// count, and the corrected sums add plus a between-groups term.
	share := float64(o.n) / float64(n)
	if math.IsInf(d, 0) {
		w.mean = w.mean*(1-share) + o.mean*share // as in Add: no overflow
	} else {
		w.mean += d * share
	}
	w.m2 += o.m2 + d*d*(float64(w.n)*float64(o.n)/float64(n))
	w.n = n
}

// Reset empties the accumulator, Skipped included.
func (w *Welford) Reset() { *w = Welford{} }
