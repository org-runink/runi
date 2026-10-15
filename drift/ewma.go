// SPDX-License-Identifier: BSD-3-Clause

package drift

import "math"

// EWMA is an exponentially weighted moving average: each new value moves the
// average a fixed fraction alpha of the way towards itself,
//
//	v_t = v_{t−1} + alpha·(x_t − v_{t−1}),
//
// so a value k samples old carries weight alpha·(1−alpha)^k. It describes a
// level; it does not test one. For a decision about whether the level moved,
// use EWMAChart, which adds the control limits this lacks.
//
// The first value initialises the average rather than being blended with an
// implicit zero, so a series that starts at 500 does not spend its first few
// dozen samples climbing up from nothing. The average counts samples, not
// time: for readings at irregular times, Decayed is the right tool.
//
// Method: the exponentially weighted average as used in forecasting by R. G.
// Brown, Statistical Forecasting for Inventory Control, McGraw-Hill, 1959.
//
// The zero value is not usable (its alpha is 0); build one with NewEWMA or
// NewEWMAHalfLife. Safe to copy, not safe for concurrent use. NaN and ±Inf are
// skipped and counted in Skipped.
type EWMA struct {
	alpha   float64
	value   float64
	n       int64
	skipped int64
}

// NewEWMA returns an average with smoothing factor alpha in (0, 1]. alpha = 1
// tracks the latest value exactly; small alpha averages over about 1/alpha
// samples.
func NewEWMA(alpha float64) (EWMA, error) {
	if !(alpha > 0 && alpha <= 1) {
		return EWMA{}, invalid("alpha", alpha, "in (0, 1]")
	}
	return EWMA{alpha: alpha}, nil
}

// NewEWMAHalfLife returns an average in which a value's weight halves every
// `samples` samples: alpha = 1 − 2^(−1/samples). samples must be finite and
// positive; it need not be a whole number.
func NewEWMAHalfLife(samples float64) (EWMA, error) {
	if !(samples > 0) || math.IsInf(samples, 1) {
		return EWMA{}, invalid("samples", samples, "finite and > 0")
	}
	// 1 − 2^(−1/s) computed as −expm1(−ln2/s): for long half-lives 2^(−1/s)
	// rounds to 1 and the plain subtraction would return alpha = 0.
	return NewEWMA(-math.Expm1(-math.Ln2 / samples))
}

// Add folds x into the average. It does not allocate.
func (e *EWMA) Add(x float64) {
	if !finite(x) {
		e.skipped++
		return
	}
	if e.n == 0 {
		e.value = x
	} else {
		e.value += e.alpha * (x - e.value)
	}
	e.n++
}

// Value is the current average, NaN before the first value.
func (e *EWMA) Value() float64 {
	if e.n == 0 {
		return math.NaN()
	}
	return e.value
}

// Alpha is the smoothing factor.
func (e *EWMA) Alpha() float64 { return e.alpha }

// N is the number of values folded in.
func (e *EWMA) N() int64 { return e.n }

// Skipped is the number of non-finite values Add refused.
func (e *EWMA) Skipped() int64 { return e.skipped }

// Reset forgets every value and Skipped, keeping alpha.
func (e *EWMA) Reset() { *e = EWMA{alpha: e.alpha} }
