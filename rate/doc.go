// SPDX-License-Identifier: BSD-3-Clause

// Package rate turns cumulative counters into per-interval rates, and keeps
// bounded-memory summaries of those rates.
//
// Many system statistics are published as running totals rather than rates:
// bytes and packets sent, CPU time in nanoseconds, time stalled in
// microseconds, context switches. A poller reads each total on every tick and
// wants the rate over the tick, together with a short memory of what the rate
// has been: the highest and lowest of the last minute, the peak since the last
// report, whether the poller itself is keeping time. Nothing here sleeps or
// reads the clock: the caller passes each reading with the time it was taken,
// so every type is as testable as the caller's clock is. Every update does
// O(1) work (amortised, for Window) in fixed memory and allocates nothing.
//
// Which to use:
//
//   - Counter turns successive totals into a Reading: the increase, the
//     interval it covers, and the increase per second. It knows the counter's
//     width, so a 32-bit counter that wraps past its maximum still yields the
//     right increase, and it tells a wrap from a reset (the counter restarted
//     from zero) by how large the increase would have to be.
//   - Window is the minimum and maximum of the last n values, exact, in
//     amortised O(1) per value: "the highest rate in the last 60 readings".
//   - Peak is the maximum since you last called Reset, and when it happened:
//     "the peak since the last report".
//   - Intervals watches the gaps between readings rather than the readings:
//     a producer that has stalled shows up as a long last interval, one that
//     is bursting as a short mean.
//
// Rates here are raw: the rate over one interval, no smoothing. For a rate
// that forgets gradually — the 10, 60 and 300 second averages operating
// systems publish beside such totals — use Decayed from
// github.com/org-runink/runi/drift, which takes the same totals directly.
// Use Counter when you need each interval's own rate (to keep a peak or a
// windowed maximum, to export it, to feed a detector), or when the counter is
// narrower than 64 bits and wraps: Decayed treats every decrease as a reset.
//
//	c, _ := rate.NewCounter(32)
//	w, _ := rate.NewWindow(60)
//	for tick := range ticks {
//		if r, ok := c.Update(readOctets(), tick); ok {
//			w.Add(r.PerSecond)
//		}
//	}
//	log.Printf("busiest second of the last minute: %.0f B/s", w.Max())
//
// Every type that takes float64 values skips NaN and ±Inf rather than folding
// them in, and counts them in Skipped: a missed reading should not poison a
// summary, and a series that is mostly NaN should be visible as one. Readings
// whose time is not after the previous reading are skipped and counted too.
// Counter, Peak and Intervals are plain values, safe to copy; NewWindow
// returns a *Window, which holds its buffer by reference and is shared, not
// copied. None is safe for concurrent use; give each goroutine its own, or
// guard it.
//
// # What this package does not do
//
// It cannot see more than two readings say. A counter read once per interval
// that wraps twice in that interval, or wraps once by more than half its
// range, or is reset and then climbs past its old value before the next
// reading, gives a wrong increase or none, and nothing in the readings can
// show it. Read a counter often enough that it cannot move by half its range
// between two readings; Counter's doc gives the arithmetic.
//
// It does not smooth, forecast or alert. A Reading is the rate over one
// interval, as noisy as the interval is short; for smoothing see drift.Decayed
// and drift.EWMA, and for "has this rate changed?" the detectors in drift.
//
// Window counts readings, not time. "The last 60 values" is "the last minute"
// only when readings arrive once a second; under irregular polling a time
// window would need to hold a variable number of values, which this package
// does not do in fixed memory.
//
// It does not interpolate. A rate is attributed to the whole interval it was
// measured over; a burst inside the interval is averaged over it, and a peak
// shorter than the polling interval is never seen at its full height.
//
// It does not read counters. Where the totals come from — files, sockets,
// device registers — is the caller's business.
package rate
