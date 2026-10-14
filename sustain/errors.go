// SPDX-License-Identifier: BSD-3-Clause

package sustain

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrInvalid is wrapped by every constructor error: a parameter that is NaN,
// infinite where a finite value is needed, or outside the range its doc
// names. Match it with errors.Is; the message text is not part of the API.
var ErrInvalid = errors.New("sustain: invalid parameter")

func invalid(name string, v any, want string) error {
	return fmt.Errorf("%w: %s = %v, want %s", ErrInvalid, name, v, want)
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// clock is the time bookkeeping every type shares: the time of the last
// accepted reading, the time the current state was entered, and the counts.
// A reading is accepted when it is the first, or when its time is after the
// last accepted one; anything else is counted in skipped and leaves no other
// trace.
type clock struct {
	last, since time.Time
	started     bool
	changes     int64
	skipped     int64
}

// accept records at as the time of a new reading and reports whether it was
// taken. The first reading also starts the current state.
func (c *clock) accept(at time.Time) bool {
	if c.started && !at.After(c.last) {
		c.skipped++
		return false
	}
	if !c.started {
		c.started, c.since = true, at
	}
	c.last = at
	return true
}

// change records a change of state at time at.
func (c *clock) change(at time.Time) {
	c.since = at
	c.changes++
}

// settled reports whether the state may change at time at: always before the
// first change, and afterwards once dwell has passed since the last one.
func (c *clock) settled(at time.Time, dwell time.Duration) bool {
	return c.changes == 0 || at.Sub(c.since) >= dwell
}

// inState is how long the current state has lasted as of at: zero before the
// first reading and for an at before the state began.
func (c *clock) inState(at time.Time) time.Duration {
	if !c.started {
		return 0
	}
	return max(at.Sub(c.since), 0)
}
