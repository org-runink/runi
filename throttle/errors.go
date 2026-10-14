// SPDX-License-Identifier: BSD-3-Clause

package throttle

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalid is wrapped by every constructor error: a parameter that is NaN,
// infinite where a finite value is needed, or outside the range its doc
// names. Match it with errors.Is; the message text is not part of the API.
var ErrInvalid = errors.New("throttle: invalid parameter")

func invalid(name string, v any, want string) error {
	return fmt.Errorf("%w: %s = %v, want %s", ErrInvalid, name, v, want)
}

// clock is the time bookkeeping every timed type shares: the latest time it
// has been given and how many calls came with an earlier one. A time before
// the latest is taken as the latest, so the clock never runs backwards.
type clock struct {
	last    time.Time
	started bool
	late    int64
}

// step moves the clock to at and returns how far it moved: zero for the first
// call, and zero for an at before the latest, which is counted in late and
// leaves the clock where it was. Afterwards last is the call's effective time.
func (c *clock) step(at time.Time) time.Duration {
	if !c.started {
		c.started, c.last = true, at
		return 0
	}
	if at.Before(c.last) {
		c.late++
		return 0
	}
	d := at.Sub(c.last)
	c.last = at
	return d
}

// now is the effective time of a call at at, max(at, last), without moving
// the clock: what a query uses.
func (c *clock) now(at time.Time) time.Time {
	if c.started && at.Before(c.last) {
		return c.last
	}
	return at
}

// ahead is how far at is past the latest time, zero when it is not past it or
// before the first call; it does not move the clock.
func (c *clock) ahead(at time.Time) time.Duration {
	if !c.started {
		return 0
	}
	return max(at.Sub(c.last), 0)
}
