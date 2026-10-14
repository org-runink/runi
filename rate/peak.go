// SPDX-License-Identifier: BSD-3-Clause

package rate

import (
	"math"
	"time"
)

// Peak is the largest value seen since the last Reset, and the time it was
// seen.
//
// It is the summary a periodic report wants: "the busiest second since the
// last report, and when", after which the report calls Reset. It does not
// decay and does not forget on its own; for the largest of the last n values
// use Window, and for a level that forgets gradually use drift.Decayed.
//
// Ties keep the first: a value equal to the peak does not move At, so At is
// the time given with the first value to reach the peak. Times are stored as
// given and need not increase; a larger value replaces the peak whatever its
// time. NaN and ±Inf are skipped and counted in Skipped.
//
// The zero value is an empty peak, ready to use. Safe to copy, not safe for
// concurrent use.
type Peak struct {
	max        float64
	at         time.Time
	n, skipped int64
}

// Add offers x, seen at time at. It does not allocate.
func (p *Peak) Add(x float64, at time.Time) {
	if !finite(x) {
		p.skipped++
		return
	}
	if p.n == 0 || x > p.max {
		p.max, p.at = x, at
	}
	p.n++
}

// Max is the largest value added since Reset, NaN when none has been.
func (p *Peak) Max() float64 {
	if p.n == 0 {
		return math.NaN()
	}
	return p.max
}

// At is the time Max was first reached, the zero time when nothing has been
// added.
func (p *Peak) At() time.Time { return p.at }

// N is the number of values added since Reset, skipped ones not included.
func (p *Peak) N() int64 { return p.n }

// Skipped is the number of non-finite values Add refused.
func (p *Peak) Skipped() int64 { return p.skipped }

// Reset forgets the peak and both counts.
func (p *Peak) Reset() { *p = Peak{} }
