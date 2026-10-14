// SPDX-License-Identifier: BSD-3-Clause

package rate

import (
	"math/bits"
	"time"
)

// Intervals summarises the gaps between reading times: the last, the mean,
// the shortest and the longest.
//
// A rate is only as good as the readings behind it. A poller that should read
// every second but is itself starved for CPU reads every five, and the rates
// it reports are then five-second averages that hide every burst shorter than
// that; a producer that has stalled sends nothing, and the next reading
// covers the whole stall. Watching the gaps shows both: Late says the last
// gap exceeded what you expect, Max shows the worst since Reset, and a Mean
// well below the nominal period shows readings arriving in bursts.
//
// The mean is exact: the sum of the gaps is held as a 128-bit integer count
// of nanoseconds and divided by N only when Mean is called, truncating to the
// nanosecond. Each gap is at most math.MaxInt64 ns, so 2^63 gaps of the
// largest size still sum to under 2^126: the sum cannot overflow for any
// input, and nothing is lost to rounding as a running float64 mean would.
//
// The first time is a baseline. A time not after the previous one (a
// duplicate, a clock stepped backwards, readings delivered out of order) is
// skipped, counted in Skipped, and leaves the baseline where it was, so the
// next valid time's gap covers it. Gaps come from time.Time.Sub, which uses
// the monotonic reading that time.Now attaches, and like it saturate at about
// 292 years.
//
// Late can only judge a gap that has closed. A producer that stops entirely
// never calls Add, and Late keeps reporting the gap before it; to see a gap
// that is still open, compare the current time with the time of your last
// reading.
//
// The zero value is empty and ready to use. Safe to copy, not safe for
// concurrent use.
type Intervals struct {
	lastAt         time.Time
	baseline       bool
	n, skipped     int64
	last, min, max time.Duration
	sumHi, sumLo   uint64 // Σ gaps in ns, as a 128-bit integer
}

// Add records a reading taken at time at. It does not allocate.
func (iv *Intervals) Add(at time.Time) {
	if !iv.baseline {
		iv.lastAt, iv.baseline = at, true
		return
	}
	d := at.Sub(iv.lastAt)
	if d <= 0 {
		iv.skipped++
		return
	}
	iv.lastAt = at
	if iv.n == 0 || d < iv.min {
		iv.min = d
	}
	if d > iv.max {
		iv.max = d
	}
	iv.last = d
	iv.n++
	var carry uint64
	iv.sumLo, carry = bits.Add64(iv.sumLo, uint64(d), 0)
	iv.sumHi += carry
}

// N is the number of gaps recorded: one fewer than the times accepted.
func (iv *Intervals) N() int64 { return iv.n }

// Skipped is the number of times not after their predecessor.
func (iv *Intervals) Skipped() int64 { return iv.skipped }

// Last is the most recent gap, zero when none.
func (iv *Intervals) Last() time.Duration { return iv.last }

// Min is the shortest gap, zero when none.
func (iv *Intervals) Min() time.Duration { return iv.min }

// Max is the longest gap, zero when none.
func (iv *Intervals) Max() time.Duration { return iv.max }

// Mean is the mean gap, truncated to the nanosecond, zero when none.
func (iv *Intervals) Mean() time.Duration {
	if iv.n == 0 {
		return 0
	}
	// The quotient is at most the largest gap, below 2^63, so sumHi < n and
	// Div64 cannot overflow.
	q, _ := bits.Div64(iv.sumHi, iv.sumLo, uint64(iv.n))
	return time.Duration(q)
}

// Late reports whether the last gap exceeded threshold. It is false when no
// gap has been recorded.
func (iv *Intervals) Late(threshold time.Duration) bool {
	return iv.n > 0 && iv.last > threshold
}

// Reset forgets every time and gap and both counts.
func (iv *Intervals) Reset() { *iv = Intervals{} }
