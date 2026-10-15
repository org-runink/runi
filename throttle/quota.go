// SPDX-License-Identifier: BSD-3-Clause

package throttle

import "time"

// Quota allows at most a fixed number of actions in each window of fixed
// length: "no more than 5 restarts per hour, on the hour".
//
// Windows are laid end to end from an origin the caller chooses: window k
// is [origin + k·length, origin + (k+1)·length), for every integer k, so
// windows before the origin exist too. A time on a boundary belongs to the
// window it starts. The count starts again from zero at each boundary; a
// window's unused actions do not carry over.
//
// The guarantee is per window, not per stretch of time of the window's
// length: actions taken at the end of one window and at the start of the
// next fall in different windows, so a stretch of one window's length that
// straddles a boundary can see up to twice the limit. Where that matters,
// use a Bucket, whose bound holds over every stretch, or both.
//
// Method: the jumping window of E. P. Rathgeb, "Modeling and performance
// comparison of policing mechanisms for ATM networks", IEEE Journal on
// Selected Areas in Communications 9(3), 1991, which also sets out the
// boundary effect above. It suits quotas people read off a calendar or a
// report — a count per hour, per day, per shift — that a token bucket's
// continuous refill does not match.
//
// A time before the latest one the quota has seen is taken as the latest
// (and counted in Late), so a clock stepped back cannot reopen an earlier
// window. Window boundaries are computed from time.Time.Sub of the origin,
// which saturates about 292 years away from it.
//
// The zero value is not usable (it panics); build one with NewQuota. Safe
// to copy, not safe for concurrent use.
type Quota struct {
	limit   int
	window  time.Duration
	origin  time.Time
	k       int64 // index of the window used counts
	used    int
	c       clock
	allowed int64
	denied  int64
}

// NewQuota returns a quota of limit ≥ 1 actions per window of length
// window > 0, with windows aligned to origin.
func NewQuota(limit int, window time.Duration, origin time.Time) (Quota, error) {
	if limit < 1 {
		return Quota{}, invalid("limit", limit, ">= 1")
	}
	if window <= 0 {
		return Quota{}, invalid("window", window, "> 0")
	}
	return Quota{limit: limit, window: window, origin: origin}, nil
}

// index is the number of the window that holds now: ⌊(now − origin)/window⌋.
func (b *Quota) index(now time.Time) int64 {
	e := now.Sub(b.origin)
	k := int64(e / b.window)
	if e%b.window < 0 {
		k--
	}
	return k
}

// Allow takes one action from the window that holds at and reports whether
// it was within the limit. It does not allocate.
func (b *Quota) Allow(at time.Time) bool { return b.AllowN(at, 1) }

// AllowN takes n actions from the window that holds at, all or none, and
// reports whether they were within the limit. An n ≤ 0 takes nothing,
// reports true and is not counted. It does not allocate.
func (b *Quota) AllowN(at time.Time, n int) bool {
	b.c.step(at)
	if k := b.index(b.c.last); k != b.k {
		b.k, b.used = k, 0
	}
	if n <= 0 {
		return true
	}
	if n > b.limit-b.used {
		b.denied++
		return false
	}
	b.used += n
	b.allowed++
	return true
}

// Remaining is the number of actions left in the window that holds at. It
// changes nothing.
func (b *Quota) Remaining(at time.Time) int {
	if b.index(b.c.now(at)) != b.k {
		return b.limit
	}
	return b.limit - b.used
}

// WindowEnd is the end of the window that holds at: the time the count
// starts again. It changes nothing.
func (b *Quota) WindowEnd(at time.Time) time.Time {
	now := b.c.now(at)
	r := now.Sub(b.origin) % b.window
	if r < 0 {
		r += b.window
	}
	return now.Add(b.window - r)
}

// Limit is the number of actions allowed per window.
func (b *Quota) Limit() int { return b.limit }

// Window is the length of a window.
func (b *Quota) Window() time.Duration { return b.window }

// Allowed is the number of AllowN calls that took actions.
func (b *Quota) Allowed() int64 { return b.allowed }

// Denied is the number of AllowN calls that were refused.
func (b *Quota) Denied() int64 { return b.denied }

// Late is the number of calls whose time was before the latest time the
// quota had seen, and was taken as that time.
func (b *Quota) Late() int64 { return b.c.late }

// Reset empties every window, forgets every time it has seen and zeroes
// every count, keeping the limit, window and origin.
func (b *Quota) Reset() { *b = Quota{limit: b.limit, window: b.window, origin: b.origin} }
