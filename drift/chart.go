// SPDX-License-Identifier: BSD-3-Clause

package drift

import "math"

// EWMAChart is an exponentially weighted moving average control chart: it
// signals when a smoothed series leaves a band around a target level.
//
// The statistic is
//
//	z_t = λ·x_t + (1−λ)·z_{t−1},   z_0 = target,
//
// and the band at sample t is
//
//	target ± L·σ·sqrt( λ/(2−λ) · (1 − (1−λ)^(2t)) ).
//
// σ times the square root is the exact standard deviation of z_t when the x_t
// are independent with standard deviation σ. It starts at λσ and widens towards
// the asymptotic σ·sqrt(λ/(2−λ)); using the asymptotic band from the first
// sample would leave the chart nearly blind to a shift present from the start.
//
// Use it for small shifts that persist. Averaging over about 2/λ samples lets
// it see a shift of half a standard deviation that a chart of raw values
// would never flag, at the price of reacting more slowly than a raw-value
// check to a single large spike. For a shift of one known size CUSUM is the
// faster detector; the two have similar performance across a range of sizes.
//
// Method: S. W. Roberts, "Control chart tests based on geometric moving
// averages", Technometrics 1(3), 1959. Exact-variance limits and run-length
// tables: J. M. Lucas and M. S. Saccucci, "Exponentially weighted moving
// average control schemes: properties and enhancements", Technometrics 32(1),
// 1990. Presentation follows D. C. Montgomery, Introduction to Statistical
// Quality Control, Wiley, chapter on CUSUM and EWMA charts.
//
// target and σ are the in-control mean and standard deviation of the series,
// and they are yours to supply: estimate them from a stretch you know to be
// normal (Welford does that), not from the stretch under test. The chart
// assumes independent samples; a strongly autocorrelated series inflates the
// spread of z_t beyond what the limits allow for and signals too often.
//
// The zero value is not usable; build one with NewEWMAChart or
// DefaultEWMAChart. Safe to copy, not safe for concurrent use. NaN and ±Inf
// are skipped and counted in Skipped, and do not advance t. Finite inputs
// within a factor of a few of the float64 limit can overflow z_t; rescale
// such a series first.
type EWMAChart struct {
	target, lambda float64
	q2             float64 // (1−λ)², the factor decay shrinks by per sample
	wide           float64 // L·σ·sqrt(λ/(2−λ)), the asymptotic half-width

	z       float64 // the statistic
	decay   float64 // (1−λ)^(2t), kept as a running product so Add needs no Pow
	n       int64
	skipped int64
}

// NewEWMAChart returns a chart around target for a series with in-control
// standard deviation sigma > 0, smoothing lambda in (0, 1] and limit width L
// > 0 in units of the statistic's own standard deviation. All must be finite.
//
// lambda = 1 makes z_t the latest sample, and the chart becomes a plain
// Shewhart chart with limits target ± Lσ.
func NewEWMAChart(target, sigma, lambda, L float64) (EWMAChart, error) {
	switch {
	case !finite(target):
		return EWMAChart{}, invalid("target", target, "finite")
	case !(sigma > 0) || math.IsInf(sigma, 1):
		return EWMAChart{}, invalid("sigma", sigma, "finite and > 0")
	case !(lambda > 0 && lambda <= 1):
		return EWMAChart{}, invalid("lambda", lambda, "in (0, 1]")
	case !(L > 0) || math.IsInf(L, 1):
		return EWMAChart{}, invalid("L", L, "finite and > 0")
	}
	q := 1 - lambda
	c := EWMAChart{target: target, lambda: lambda, q2: q * q, wide: L * sigma * math.Sqrt(lambda/(2-lambda))}
	c.Reset()
	return c, nil
}

// DefaultEWMAChart returns NewEWMAChart(target, sigma, 0.2, 3).
//
// These are the literature's working values, not a tuning for your data.
// Montgomery recommends λ between 0.05 and 0.25, and reports that L = 3, the
// usual three-sigma width, works well, particularly with the larger λ. Lucas
// and Saccucci's tables put the limit for an in-control average run length of
// 500 at λ = 0.2 slightly inside 3 (L ≈ 2.96 for the asymptotic band); 3 is
// kept because it is the convention readers recognise and it errs towards
// fewer false alarms. Measured here with the exact, time-varying band, the
// in-control average run length at these values is about 560 samples, and a
// one-sigma shift is detected in about 10 (TestEWMAChartARL).
func DefaultEWMAChart(target, sigma float64) (EWMAChart, error) {
	return NewEWMAChart(target, sigma, 0.2, 3)
}

// Add folds x into the statistic and reports whether it is now outside the
// limits for this sample: Up above, Down below, None inside. A signal does
// not reset anything; the chart keeps running, and will usually keep
// signalling while the shift lasts, until you call Reset. It does not
// allocate.
func (c *EWMAChart) Add(x float64) Signal {
	if !finite(x) {
		c.skipped++
		return None
	}
	c.z = c.lambda*x + (1-c.lambda)*c.z
	c.n++
	if c.decay != 0 {
		c.decay *= c.q2
		// Below 2⁻⁵⁴, 1 − decay rounds to exactly 1, so zeroing it changes no
		// limit. Left alone it would never reach zero: 0.64 times the smallest
		// subnormal rounds back up to it, and subnormal arithmetic is slow
		// enough to make every later Add several times dearer.
		if c.decay <= 0x1p-54 {
			c.decay = 0
		}
	}
	lo, hi := c.Limits()
	switch {
	case c.z > hi:
		return Up
	case c.z < lo:
		return Down
	}
	return None
}

// Statistic is z_t, the smoothed series; target before the first sample.
func (c *EWMAChart) Statistic() float64 { return c.z }

// Limits is the band for the current sample count t. At t = 0 it is the
// single point target, since no sample has been judged yet.
func (c *EWMAChart) Limits() (lo, hi float64) {
	w := c.wide
	if c.decay != 0 {
		w *= math.Sqrt(1 - c.decay)
	}
	return c.target - w, c.target + w
}

// N is the number of samples judged.
func (c *EWMAChart) N() int64 { return c.n }

// Skipped is the number of non-finite values Add refused.
func (c *EWMAChart) Skipped() int64 { return c.skipped }

// Reset restarts the chart at z = target and t = 0, keeping its parameters
// and clearing Skipped.
func (c *EWMAChart) Reset() {
	c.z, c.decay, c.n, c.skipped = c.target, 1, 0, 0
}
