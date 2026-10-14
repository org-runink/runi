// SPDX-License-Identifier: BSD-3-Clause

package drift

import (
	"math"
	"time"
)

// Decayed turns a cumulative counter, read at irregular times, into an
// exponentially decayed average rate.
//
// Many system statistics are published as running totals rather than rates:
// microseconds spent stalled on memory or I/O since boot, bytes sent, requests
// served. What a reader wants is "the share of time stalled over the last
// minute" or "requests per second lately", which is what operating systems
// report alongside such totals as 10, 60 and 300 second averages. Decayed
// computes that from the totals alone. For a counter of microseconds stalled,
// the rate is microseconds per second, so dividing by 1e6 gives the share of
// time (multiply by 100 for a percentage).
//
// Each Update takes the rate over the interval since the last one,
// r = Δtotal/Δt, and blends it into the average with
//
//	e = exp(−Δt/window),   a = a·e + r·(1−e).
//
// This is exact for irregular sampling: it is the continuous-time exponential
// average with time constant window of a rate that was r throughout the
// interval, which is all a pair of counter readings can say about it. A fixed
// per-sample weight, as in EWMA, would be wrong under jitter: a reading
// delayed to 3 s would count the same as one taken after 1 s, and a burst of
// readings close together would drag the average towards whatever those
// moments happened to show. Here a reading's weight is the time it covers.
//
// After one window of a new constant rate the average has moved 1 − 1/e, about
// 63%, of the way from the old rate to the new one, whatever the spacing of the
// readings; after three windows, 95%. The first rate initialises the average
// directly rather than being blended into an implicit zero, which would read
// as a quiet system for the first window and then as a rise that never
// happened.
//
// A total lower than the previous one means the counter was reset, or wrapped
// past its maximum. With one reading the two cannot be told apart, so both are
// treated as a reset: the reading becomes a new baseline, no rate is produced,
// and Resets is incremented. A counter that wraps loses one interval, never
// yields an absurd rate.
//
// Time comes only from the at argument; nothing here reads the clock, so it
// is as testable as the caller's clock is. Intervals are taken with
// time.Time.Sub, which uses the monotonic reading that time.Now attaches, so a
// wall-clock step between two such readings does not distort the interval.
// Readings at or before the previous one (a clock stepped backwards, a
// duplicate) are skipped and counted in Skipped, and leave the baseline where
// it was: the next valid reading covers their interval.
//
// Method: the exponential smoothing of irregularly spaced data of D. J. Wright,
// "Forecasting data published at irregular time intervals using an extension
// of Holt's method", Management Science 32(4), 1986, here applied to the
// interval-average rate, which makes the step response exact rather than
// approximate.
//
// The zero value is not usable; build one with NewDecayed. Safe to copy, not
// safe for concurrent use.
type Decayed struct {
	window time.Duration

	last     uint64
	lastAt   time.Time
	baseline bool // last/lastAt hold a reading
	avg      float64
	rated    bool // avg holds at least one rate

	resets, skipped int64
}

// NewDecayed returns an averager with time constant window > 0: the time over
// which a step in the rate is 63% absorbed.
func NewDecayed(window time.Duration) (Decayed, error) {
	if window <= 0 {
		return Decayed{}, invalid("window", window.Seconds(), "> 0")
	}
	return Decayed{window: window}, nil
}

// Update reports the counter's total as read at time at, and returns the
// average rate in total-per-second with ok = true when the reading produced a
// rate. It returns the current Value (NaN if none yet) with ok = false for the
// first reading, for a reading not after the previous one, and for a counter
// reset. It does not allocate.
func (d *Decayed) Update(total uint64, at time.Time) (rate float64, ok bool) {
	if !d.baseline {
		d.last, d.lastAt, d.baseline = total, at, true
		return d.Value(), false
	}
	dt := at.Sub(d.lastAt)
	if dt <= 0 {
		d.skipped++
		return d.Value(), false
	}
	if total < d.last {
		d.resets++
		d.last, d.lastAt = total, at
		return d.Value(), false
	}
	// Integer difference first: totals near 2^64 differ by small amounts that
	// would vanish in float64.
	r := float64(total-d.last) / dt.Seconds()
	d.last, d.lastAt = total, at
	if !d.rated {
		d.avg, d.rated = r, true
		return d.avg, true
	}
	// a·e + r·(1−e) written as a + (r−a)·(1−e), with 1−e from expm1 so that
	// readings much closer together than the window keep their weight.
	d.avg += (r - d.avg) * -math.Expm1(-float64(dt)/float64(d.window))
	return d.avg, true
}

// Value is the current average rate in total-per-second, NaN before the first
// rate.
func (d *Decayed) Value() float64 {
	if !d.rated {
		return math.NaN()
	}
	return d.avg
}

// Window is the time constant.
func (d *Decayed) Window() time.Duration { return d.window }

// Resets is the number of readings below their predecessor, taken as counter
// resets.
func (d *Decayed) Resets() int64 { return d.resets }

// Skipped is the number of readings not after their predecessor.
func (d *Decayed) Skipped() int64 { return d.skipped }

// Reset forgets the baseline, the average and both counts, keeping the window.
func (d *Decayed) Reset() { *d = Decayed{window: d.window} }
