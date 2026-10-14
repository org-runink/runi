// SPDX-License-Identifier: BSD-3-Clause

package drift

import "math"

// CUSUM is the two-sided tabular cumulative-sum test for a shift in the mean
// away from a known target.
//
// Each side accumulates the evidence for a shift in its direction and is
// floored at zero, so that time spent in control is not credited against a
// later shift:
//
//	S⁺_t = max(0, S⁺_{t−1} + x_t − target − k)
//	S⁻_t = max(0, S⁻_{t−1} + target − x_t − k)
//
// and a side signals once it exceeds h. k is the slack: deviations smaller
// than k drain the sum rather than feed it. Set to half the shift you want to
// catch, the test is the fastest possible detector for a shift of exactly
// that size among those with the same false-alarm rate, in the worst-case
// sense of Lorden (G. V. Moustakides, "Optimal stopping times for detecting
// changes in distributions", Annals of Statistics 14(4), 1986). For shifts
// much smaller than 2k it is slow, and EWMAChart is the better choice when
// the size is unknown.
//
// Since estimates when the shift began: a side becomes positive at about the
// sample the shift started, so the run of samples for which it has been
// positive is the age of the shift. This is Montgomery's estimate, and it
// costs nothing to keep.
//
// Method: E. S. Page, "Continuous inspection schemes", Biometrika 41(1/2),
// 1954. Tabular form, the Since estimate and the run-length tables quoted
// below: D. C. Montgomery, Introduction to Statistical Quality Control,
// Wiley, chapter on the cumulative sum control chart.
//
// k and h are in the data's units. A signal does not reset the statistics:
// they keep accumulating, and keep signalling while the shift lasts, until
// you call Reset. Whether to reset, re-estimate the target, or simply note
// the change is the caller's decision. Like EWMAChart, it assumes independent
// samples with a known in-control mean.
//
// The zero value is not usable; build one with NewCUSUM or DefaultCUSUM. Safe
// to copy, not safe for concurrent use. NaN and ±Inf are skipped and counted
// in Skipped. Each side is capped at the largest finite float64, so no finite
// input, however large, can turn the statistics into Inf or NaN.
type CUSUM struct {
	target, k, h float64

	up, down       float64 // S⁺, S⁻
	upRun, downRun int64   // consecutive samples each side has been > 0
	n, skipped     int64
}

// NewCUSUM returns a two-sided CUSUM around target with slack k >= 0 and
// decision interval h > 0, all finite and in the data's units.
func NewCUSUM(target, k, h float64) (CUSUM, error) {
	switch {
	case !finite(target):
		return CUSUM{}, invalid("target", target, "finite")
	case !(k >= 0) || math.IsInf(k, 1):
		return CUSUM{}, invalid("k", k, "finite and >= 0")
	case !(h > 0) || math.IsInf(h, 1):
		return CUSUM{}, invalid("h", h, "finite and > 0")
	}
	return CUSUM{target: target, k: k, h: h}, nil
}

// DefaultCUSUM returns NewCUSUM(target, 0.5·sigma, 5·sigma) for a series with
// in-control standard deviation sigma > 0.
//
// Montgomery recommends k = σ/2, which tunes the test for a one-sigma shift,
// with h = 4σ or h = 5σ. Two-sided, these give an in-control average run
// length of about 168 samples at h = 4 and about 465 at h = 5, against about
// 8.4 and 10.4 samples to detect a one-sigma shift. 5 is the default because
// it roughly triples the time between false alarms for two extra samples of
// delay; on a series read every second, h = 4 would raise a false alarm about
// every three minutes per series. TestCUSUMARL measures both run lengths.
func DefaultCUSUM(target, sigma float64) (CUSUM, error) {
	if !(sigma > 0) || math.IsInf(sigma, 1) {
		return CUSUM{}, invalid("sigma", sigma, "finite and > 0")
	}
	return NewCUSUM(target, 0.5*sigma, 5*sigma)
}

// Add folds x into both sums and reports Up when S⁺ > h, Down when S⁻ > h, and
// when both are over the larger one, Up on a tie. It does not allocate.
func (c *CUSUM) Add(x float64) Signal {
	if !finite(x) {
		c.skipped++
		return None
	}
	c.n++
	c.up = side(c.up, x-c.target-c.k, &c.upRun)
	c.down = side(c.down, c.target-x-c.k, &c.downRun)
	return twoSided(c.up, c.down, c.h)
}

// side advances one half of the CUSUM. The cap at MaxFloat64 keeps the sum
// finite after inputs near the float64 limit: an infinite sum would absorb
// every later step, never drain, and signal for the rest of the series,
// while a capped one drains like any other.
func side(s, step float64, run *int64) float64 {
	s = min(max(0, s+step), math.MaxFloat64)
	if s > 0 {
		*run++
	} else {
		*run = 0
	}
	return s
}

// Upper is S⁺, the evidence for an upward shift. Never negative.
func (c *CUSUM) Upper() float64 { return c.up }

// Lower is S⁻, the evidence for a downward shift. Never negative.
func (c *CUSUM) Lower() float64 { return c.down }

// Since is the number of consecutive samples, ending with the latest, for
// which the leading side has been above zero: an estimate of how many samples
// ago the shift began, so it started at sample N() − Since() + 1.
//
// The leading side is the larger of S⁺ and S⁻, Up on a tie. That is always the
// side Add just signalled, if it signalled: a side over h is larger than one
// that is not, and when both are over, Add picks the larger too. Since is 0
// while both sides are at zero.
func (c *CUSUM) Since() int64 {
	if c.up >= c.down {
		return c.upRun
	}
	return c.downRun
}

// N is the number of samples folded in.
func (c *CUSUM) N() int64 { return c.n }

// Skipped is the number of non-finite values Add refused.
func (c *CUSUM) Skipped() int64 { return c.skipped }

// Reset sets both sums to zero, keeping target, k and h, and clears N and
// Skipped.
func (c *CUSUM) Reset() { *c = CUSUM{target: c.target, k: c.k, h: c.h} }
