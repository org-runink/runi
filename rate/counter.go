// SPDX-License-Identifier: BSD-3-Clause

package rate

import "time"

// Reading is what one pair of counter readings says: the counter rose by
// Delta over Interval, which is PerSecond per second.
type Reading struct {
	// Delta is the increase since the previous reading, wraps included.
	Delta uint64
	// Interval is the time between the two readings, always > 0.
	Interval time.Duration
	// PerSecond is Delta divided by Interval in seconds: finite and >= 0.
	PerSecond float64
}

// Counter turns successive readings of a cumulative counter into
// per-interval increases and rates, across wraps and resets.
//
// A counter of width w bits counts modulo 2^w: after 2^w − 1 it wraps to 0
// and carries on, as RFC 2578 §7.1.6 defines for SNMP's Counter32 ("when it
// reaches a maximum value of 2^32-1 ... it wraps around and starts increasing
// again from zero"). Many kernel and device counters behave the same way at
// 32 bits, and most modern ones at 64. When a reading is below the previous
// one, the increase across one wrap is (2^w − previous) + total, which is
// (total − previous) mod 2^w.
//
// But a reading below the previous one is also what a reset looks like: the
// device, process or kernel restarted and the counter began again from zero.
// Two readings cannot tell a wrap from a reset to a small number, so Counter
// uses a heuristic, and it is only that: a decrease is taken as a wrap when
// the increase it implies is at most half the modulus, 2^(w−1), and as a
// reset otherwise. Put the other way round, a fall of less than half the
// range is a reset and a fall of half the range or more is a wrap. The
// reasoning is the standard assumption that the counter is read often enough
// to wrap at most once per interval: a reader that keeps to it comfortably
// sees increases well below half the range, so a wrap implying more than
// that is more likely a reset. The rule is wrong both ways at the margin. A
// genuine wrap by more than half the range is taken as a reset, and that
// interval is lost. A reset of a counter that stood in the top half of its
// range is taken as a wrap when the new total is at least half the range
// below the old one, and that interval's increase is overstated by up to
// half the range. Read the counter often enough that it moves by well under
// half its range between readings: a 32-bit octet counter on a 10 Gbit/s
// link (1.25e9 bytes per second) wraps every 3.4 s and must be read at least
// every 1.7 s, which is why RFC 2863 defines 64-bit "high capacity"
// interface counters. Where a counter is known not to wrap in practice,
// width 64 makes every decrease a reset.
//
// A 64-bit counter is never taken to have wrapped: every decrease is a reset.
// At 100 Gbit/s, 1.25e10 bytes per second, a 64-bit byte counter wraps once
// in 2^64 / 1.25e10 ≈ 1.5e9 s, about 47 years; a 64-bit count of nanoseconds
// of CPU time wraps once in 584 years of one CPU's time. No interval between
// two readings is that long, and a decrease is a restart.
//
// The first reading, and the first after a reset, is a baseline and yields
// no rate. A reading whose time is not after the previous one (a duplicate,
// a clock stepped backwards) is skipped and counted in Skipped, leaving the
// baseline where it was: the next valid reading covers its interval. A total
// above 2^w − 1 cannot come from a w-bit counter; it is skipped and counted
// in Invalid, before any other check, and also leaves the baseline alone.
// Intervals come from time.Time.Sub, which uses the monotonic reading that
// time.Now attaches, so a wall-clock step between two such readings does
// not distort them.
//
// The zero value is not usable; build one with NewCounter. Safe to copy, not
// safe for concurrent use.
type Counter struct {
	width uint
	mask  uint64 // 2^width − 1, the largest valid total
	half  uint64 // 2^(width−1), the largest increase accepted across a wrap

	last     uint64
	lastAt   time.Time
	baseline bool // last/lastAt hold a reading

	resets, wraps, skipped, invalid int64
}

// NewCounter returns a counter of the given width in bits, 1 to 64: one that
// wraps to zero after 2^width − 1.
func NewCounter(width uint) (Counter, error) {
	if width < 1 || width > 64 {
		return Counter{}, invalid("width", width, "1..64")
	}
	return Counter{
		width: width,
		mask:  ^uint64(0) >> (64 - width),
		half:  uint64(1) << (width - 1),
	}, nil
}

// Update reports the counter's total as read at time at. It returns the
// increase and rate since the previous reading with ok = true, or a zero
// Reading with ok = false for a baseline, a reset, a reading not after the
// previous one, or a total too large for the width. It does not allocate.
func (c *Counter) Update(total uint64, at time.Time) (r Reading, ok bool) {
	if total > c.mask {
		c.invalid++
		return Reading{}, false
	}
	if !c.baseline {
		c.last, c.lastAt, c.baseline = total, at, true
		return Reading{}, false
	}
	dt := at.Sub(c.lastAt)
	if dt <= 0 {
		c.skipped++
		return Reading{}, false
	}
	// Modular difference: exact for an increase, and for a decrease the
	// increase across one wrap. For width 64 the mask is all ones and this
	// is plain unsigned subtraction.
	delta := (total - c.last) & c.mask
	if total < c.last {
		if c.width == 64 || delta > c.half {
			c.resets++
			c.last, c.lastAt = total, at
			return Reading{}, false
		}
		c.wraps++
	}
	c.last, c.lastAt = total, at
	// Integer difference first: totals near 2^64 differ by small amounts that
	// would vanish in float64. The quotient is at most 2^64 per nanosecond,
	// about 1.8e28, so it is always finite.
	return Reading{Delta: delta, Interval: dt, PerSecond: float64(delta) / dt.Seconds()}, true
}

// Width is the counter's width in bits.
func (c *Counter) Width() uint { return c.width }

// Resets is the number of decreases taken as counter resets.
func (c *Counter) Resets() int64 { return c.resets }

// Wraps is the number of decreases taken as wraps past the maximum.
func (c *Counter) Wraps() int64 { return c.wraps }

// Skipped is the number of readings not after their predecessor.
func (c *Counter) Skipped() int64 { return c.skipped }

// Invalid is the number of totals above 2^Width − 1.
func (c *Counter) Invalid() int64 { return c.invalid }

// Reset forgets the baseline and every count, keeping the width.
func (c *Counter) Reset() { *c = Counter{width: c.width, mask: c.mask, half: c.half} }
