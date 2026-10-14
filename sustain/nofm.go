// SPDX-License-Identifier: BSD-3-Clause

package sustain

import (
	"math/bits"
	"time"
)

// MaxM is the largest window NofM keeps: one bit per reading in a uint64.
const MaxM = 64

// NofM is on while at least N of the last M readings are past a level.
//
// It sits between a plain threshold, which fires on one reading, and a
// Debounce, which needs every reading of a run: a condition that holds most
// of the time but not always — three readings over in five, with one dip
// back under between them — escalates here and would keep restarting a
// Debounce. For a High level a reading is past it when it is at or above it;
// for Low, at or below.
//
// The last M outcomes are kept as bits of one word, newest in the lowest bit,
// so an update is a shift, a mask and a population count: no buffer and no
// allocation, and the count is exact. Before M readings have been accepted the
// window holds those there are, and N of them suffice. NofM has no deadband
// of its own; the window is its smoothing.
//
// NaN and ±Inf readings are skipped and take no slot, so "the last M" means
// the last M finite readings. Readings whose time is not after the last
// accepted reading are skipped too. Both are counted in Skipped.
//
// Method: the M-of-N rule is the "binary integration" (double-threshold)
// detector of radar, M. I. Skolnik, Introduction to Radar Systems,
// McGraw-Hill; the same rule over a
// control chart's zones gives the run rules of the Western Electric
// Statistical Quality Control Handbook, 1956 ("two out of three beyond two
// sigma", "four out of five beyond one sigma"), as tabulated in D. C.
// Montgomery, Introduction to Statistical Quality Control, Wiley.
//
// The zero value is not usable; build one with NewNofM. Safe to copy, not
// safe for concurrent use.
type NofM struct {
	sign, level float64 // level multiplied by sign
	n, m        int
	full        uint64 // the low m bits set
	window      uint64 // outcome of the k-th newest reading in bit k
	len         int
	on          bool
	c           clock
}

// NewNofM returns a window over the last m readings, 1 ≤ m ≤ MaxM, that is on
// while at least n of them, 1 ≤ n ≤ m, are past level in direction dir.
// level must be finite.
func NewNofM(dir Direction, level float64, n, m int) (NofM, error) {
	switch {
	case dir != High && dir != Low:
		return NofM{}, invalid("dir", dir, "High or Low")
	case !finite(level):
		return NofM{}, invalid("level", level, "finite")
	case m < 1 || m > MaxM:
		return NofM{}, invalid("m", m, "1..64")
	case n < 1 || n > m:
		return NofM{}, invalid("n", n, "1..m")
	}
	s := dir.sign()
	return NofM{sign: s, level: s * level, n: n, m: m, full: ^uint64(0) >> (MaxM - m)}, nil
}

// Update adds reading x taken at time at to the window and returns the state
// after it: true while at least N of the last M readings are past the level.
// It does not allocate.
func (w *NofM) Update(x float64, at time.Time) bool {
	if !finite(x) {
		w.c.skipped++
		return w.on
	}
	if !w.c.accept(at) {
		return w.on
	}
	var hit uint64
	if w.sign*x >= w.level {
		hit = 1
	}
	w.window = (w.window<<1 | hit) & w.full
	if w.len < w.m {
		w.len++
	}
	if on := bits.OnesCount64(w.window) >= w.n; on != w.on {
		w.on = on
		w.c.change(at)
	}
	return w.on
}

// Active is the current state: true while at least N of the last M readings
// are past the level.
func (w *NofM) Active() bool { return w.on }

// Count is how many of the readings in the window are past the level.
func (w *NofM) Count() int { return bits.OnesCount64(w.window) }

// Len is the number of readings in the window: those accepted since Reset,
// up to M.
func (w *NofM) Len() int { return w.len }

// N is the number of readings past the level that turns the state on.
func (w *NofM) N() int { return w.n }

// M is the window's size in readings.
func (w *NofM) M() int { return w.m }

// Since is the time of the reading that entered the current state: the first
// reading's time before any change, the zero time before any reading.
func (w *NofM) Since() time.Time { return w.c.since }

// InState is how long the current state has lasted as of at, zero before the
// first reading and for an at before Since.
func (w *NofM) InState(at time.Time) time.Duration { return w.c.inState(at) }

// Transitions is the number of changes of state.
func (w *NofM) Transitions() int64 { return w.c.changes }

// Skipped is the number of readings refused: non-finite, or not after the
// last accepted reading.
func (w *NofM) Skipped() int64 { return w.c.skipped }

// Reset empties the window and zeroes every count, keeping the level, N and
// M.
func (w *NofM) Reset() {
	*w = NofM{sign: w.sign, level: w.level, n: w.n, m: w.m, full: w.full}
}
