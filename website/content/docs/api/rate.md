---
title: "runi/rate"
weight: 14
---

```go
import "github.com/org-runink/runi/rate"
```

Package rate turns cumulative counters into per-interval rates, and keeps
bounded-memory summaries of those rates.

Many system statistics are published as running totals rather than rates:
bytes and packets sent, CPU time in nanoseconds, time stalled in
microseconds, context switches. A poller reads each total on every tick and
wants the rate over the tick, together with a short memory of what the rate
has been: the highest and lowest of the last minute, the peak since the last
report, whether the poller itself is keeping time. Nothing here sleeps or
reads the clock: the caller passes each reading with the time it was taken,
so every type is as testable as the caller's clock is. Every update does
O(1) work (amortised, for Window) in fixed memory and allocates nothing.

Which to use:

  - Counter turns successive totals into a Reading: the increase, the
    interval it covers, and the increase per second. It knows the counter's
    width, so a 32-bit counter that wraps past its maximum still yields the
    right increase, and it tells a wrap from a reset (the counter restarted
    from zero) by how large the increase would have to be.
  - Window is the minimum and maximum of the last n values, exact, in
    amortised O(1) per value: "the highest rate in the last 60 readings".
  - Peak is the maximum since you last called Reset, and when it happened:
    "the peak since the last report".
  - Intervals watches the gaps between readings rather than the readings:
    a producer that has stalled shows up as a long last interval, one that
    is bursting as a short mean.

Rates here are raw: the rate over one interval, no smoothing. For a rate
that forgets gradually — the 10, 60 and 300 second averages operating
systems publish beside such totals — use Decayed from
github.com/org-runink/runi/drift, which takes the same totals directly.
Use Counter when you need each interval's own rate (to keep a peak or a
windowed maximum, to export it, to feed a detector), or when the counter is
narrower than 64 bits and wraps: Decayed treats every decrease as a reset.

c, _ := rate.NewCounter(32)
w, _ := rate.NewWindow(60)
for tick := range ticks {
	if r, ok := c.Update(readOctets(), tick); ok {
		w.Add(r.PerSecond)
	}
}
log.Printf("busiest second of the last minute: %.0f B/s", w.Max())

Every type that takes float64 values skips NaN and ±Inf rather than folding
them in, and counts them in Skipped: a missed reading should not poison a
summary, and a series that is mostly NaN should be visible as one. Readings
whose time is not after the previous reading are skipped and counted too.
Counter, Peak and Intervals are plain values, safe to copy; NewWindow
returns a *Window, which holds its buffer by reference and is shared, not
copied. None is safe for concurrent use; give each goroutine its own, or
guard it.

# What this package does not do

It cannot see more than two readings say. A counter read once per interval
that wraps twice in that interval, or wraps once by more than half its
range, or is reset and then climbs past its old value before the next
reading, gives a wrong increase or none, and nothing in the readings can
show it. Read a counter often enough that it cannot move by half its range
between two readings; Counter's doc gives the arithmetic.

It does not smooth, forecast or alert. A Reading is the rate over one
interval, as noisy as the interval is short; for smoothing see drift.Decayed
and drift.EWMA, and for "has this rate changed?" the detectors in drift.

Window counts readings, not time. "The last 60 values" is "the last minute"
only when readings arrive once a second; under irregular polling a time
window would need to hold a variable number of values, which this package
does not do in fixed memory.

It does not interpolate. A rate is attributed to the whole interval it was
measured over; a burst inside the interval is averaged over it, and a peak
shorter than the polling interval is never seen at its full height.

It does not read counters. Where the totals come from — files, sockets,
device registers — is the caller's business.

## Index

- `type` [Counter](#counter)
- `type` [Intervals](#intervals)
- `type` [Peak](#peak)
- `type` [Reading](#reading)
- `type` [Window](#window)
- `method` [Counter.Invalid](#counterinvalid)
- `method` [Counter.Reset](#counterreset)
- `method` [Counter.Resets](#counterresets)
- `method` [Counter.Skipped](#counterskipped)
- `method` [Counter.Update](#counterupdate)
- `method` [Counter.Width](#counterwidth)
- `method` [Counter.Wraps](#counterwraps)
- `const` [ErrInvalid](#errinvalid)
- `method` [Intervals.Add](#intervalsadd)
- `method` [Intervals.Last](#intervalslast)
- `method` [Intervals.Late](#intervalslate)
- `method` [Intervals.Max](#intervalsmax)
- `method` [Intervals.Mean](#intervalsmean)
- `method` [Intervals.Min](#intervalsmin)
- `method` [Intervals.N](#intervalsn)
- `method` [Intervals.Reset](#intervalsreset)
- `method` [Intervals.Skipped](#intervalsskipped)
- `func` [NewCounter](#newcounter)
- `func` [NewWindow](#newwindow)
- `method` [Peak.Add](#peakadd)
- `method` [Peak.At](#peakat)
- `method` [Peak.Max](#peakmax)
- `method` [Peak.N](#peakn)
- `method` [Peak.Reset](#peakreset)
- `method` [Peak.Skipped](#peakskipped)
- `method` [Window.Add](#windowadd)
- `method` [Window.Len](#windowlen)
- `method` [Window.Max](#windowmax)
- `method` [Window.Min](#windowmin)
- `method` [Window.Reset](#windowreset)
- `method` [Window.Size](#windowsize)
- `method` [Window.Skipped](#windowskipped)

### Counter

```go
type Counter struct
```

Counter turns successive readings of a cumulative counter into
per-interval increases and rates, across wraps and resets.

A counter of width w bits counts modulo 2^w: after 2^w − 1 it wraps to 0
and carries on, as RFC 2578 §7.1.6 defines for SNMP's Counter32 ("when it
reaches a maximum value of 2^32-1 ... it wraps around and starts increasing
again from zero"). Many kernel and device counters behave the same way at
32 bits, and most modern ones at 64. When a reading is below the previous
one, the increase across one wrap is (2^w − previous) + total, which is
(total − previous) mod 2^w.

But a reading below the previous one is also what a reset looks like: the
device, process or kernel restarted and the counter began again from zero.
Two readings cannot tell a wrap from a reset to a small number, so Counter
uses a heuristic, and it is only that: a decrease is taken as a wrap when
the increase it implies is at most half the modulus, 2^(w−1), and as a
reset otherwise. Put the other way round, a fall of less than half the
range is a reset and a fall of half the range or more is a wrap. The
reasoning is the standard assumption that the counter is read often enough
to wrap at most once per interval: a reader that keeps to it comfortably
sees increases well below half the range, so a wrap implying more than
that is more likely a reset. The rule is wrong both ways at the margin. A
genuine wrap by more than half the range is taken as a reset, and that
interval is lost. A reset of a counter that stood in the top half of its
range is taken as a wrap when the new total is at least half the range
below the old one, and that interval's increase is overstated by up to
half the range. Read the counter often enough that it moves by well under
half its range between readings: a 32-bit octet counter on a 10 Gbit/s
link (1.25e9 bytes per second) wraps every 3.4 s and must be read at least
every 1.7 s, which is why RFC 2863 defines 64-bit "high capacity"
interface counters. Where a counter is known not to wrap in practice,
width 64 makes every decrease a reset.

A 64-bit counter is never taken to have wrapped: every decrease is a reset.
At 100 Gbit/s, 1.25e10 bytes per second, a 64-bit byte counter wraps once
in 2^64 / 1.25e10 ≈ 1.5e9 s, about 47 years; a 64-bit count of nanoseconds
of CPU time wraps once in 584 years of one CPU's time. No interval between
two readings is that long, and a decrease is a restart.

The first reading, and the first after a reset, is a baseline and yields
no rate. A reading whose time is not after the previous one (a duplicate,
a clock stepped backwards) is skipped and counted in Skipped, leaving the
baseline where it was: the next valid reading covers its interval. A total
above 2^w − 1 cannot come from a w-bit counter; it is skipped and counted
in Invalid, before any other check, and also leaves the baseline alone.
Intervals come from time.Time.Sub, which uses the monotonic reading that
time.Now attaches, so a wall-clock step between two such readings does
not distort them.

The zero value is not usable; build one with NewCounter. Safe to copy, not
safe for concurrent use.

<small>Defined in <code>rate/counter.go</code>.</small>

### Intervals

```go
type Intervals struct
```

Intervals summarises the gaps between reading times: the last, the mean,
the shortest and the longest.

A rate is only as good as the readings behind it. A poller that should read
every second but is itself starved for CPU reads every five, and the rates
it reports are then five-second averages that hide every burst shorter than
that; a producer that has stalled sends nothing, and the next reading
covers the whole stall. Watching the gaps shows both: Late says the last
gap exceeded what you expect, Max shows the worst since Reset, and a Mean
well below the nominal period shows readings arriving in bursts.

The mean is exact: the sum of the gaps is held as a 128-bit integer count
of nanoseconds and divided by N only when Mean is called, truncating to the
nanosecond. Each gap is at most math.MaxInt64 ns, so 2^63 gaps of the
largest size still sum to under 2^126: the sum cannot overflow for any
input, and nothing is lost to rounding as a running float64 mean would.

The first time is a baseline. A time not after the previous one (a
duplicate, a clock stepped backwards, readings delivered out of order) is
skipped, counted in Skipped, and leaves the baseline where it was, so the
next valid time's gap covers it. Gaps come from time.Time.Sub, which uses
the monotonic reading that time.Now attaches, and like it saturate at about
292 years.

Late can only judge a gap that has closed. A producer that stops entirely
never calls Add, and Late keeps reporting the gap before it; to see a gap
that is still open, compare the current time with the time of your last
reading.

The zero value is empty and ready to use. Safe to copy, not safe for
concurrent use.

<small>Defined in <code>rate/intervals.go</code>.</small>

### Peak

```go
type Peak struct
```

Peak is the largest value seen since the last Reset, and the time it was
seen.

It is the summary a periodic report wants: "the busiest second since the
last report, and when", after which the report calls Reset. It does not
decay and does not forget on its own; for the largest of the last n values
use Window, and for a level that forgets gradually use drift.Decayed.

Ties keep the first: a value equal to the peak does not move At, so At is
the time given with the first value to reach the peak. Times are stored as
given and need not increase; a larger value replaces the peak whatever its
time. NaN and ±Inf are skipped and counted in Skipped.

The zero value is an empty peak, ready to use. Safe to copy, not safe for
concurrent use.

<small>Defined in <code>rate/peak.go</code>.</small>

### Reading

```go
type Reading struct
```

Reading is what one pair of counter readings says: the counter rose by
Delta over Interval, which is PerSecond per second.

<small>Defined in <code>rate/counter.go</code>.</small>

### Window

```go
type Window struct
```

Window is the minimum and maximum of the last Size values added, exact,
in amortised O(1) time per value and fixed memory.

Rescanning the last n values on every Add costs O(n) per value; a sorted
structure costs O(log n). Window keeps instead two monotonic queues of
candidates. A value can only ever be the window's maximum if no later
value is at least as large, so when x arrives every queued maximum
candidate ≤ x is dropped from the back, and a candidate that has left the
window is dropped from the front. What remains is decreasing from front to
back, its front is the maximum, and each value enters and leaves each queue
once: amortised O(1), and at most Size candidates per queue. The minimum is
the mirror image.

Method: the monotonic wedge of D. Lemire, "Streaming maximum-minimum filter
using no more than three comparisons per element", Nordic Journal of
Computing 13(4), 2006. The block method of J. Gil and M. Werman, "Computing
2-D min, median, and max filters", IEEE Transactions on Pattern Analysis
and Machine Intelligence 15(5), 1993, reaches a similar constant cost per
value, but it processes values in blocks of the window size and so answers
late for a stream; the wedge answers after every value.

Ties: when x equals a queued candidate, the older one is dropped and x is
kept, because x stays in the window longer and the queue stays shorter.
Equal values are interchangeable, so which is kept never changes Min or
Max; +0 and −0 compare equal and either may be returned for the other.

The window counts accepted values: NaN and ±Inf are skipped, counted in
Skipped, and do not take a slot, so "the last 60 values" means the last 60
finite ones.

Both queues live in one buffer of 2·Size slots, 16 bytes each, allocated by
NewWindow; Add and Reset never allocate. NewWindow returns a pointer, and
a Window is used through it: a copy of the struct would share the buffer
with the original, and using both would corrupt both. Build one per series
with NewWindow.

The zero value is not usable (Add panics on it); build one with
NewWindow. Not safe to copy and not safe for concurrent use.

<small>Defined in <code>rate/window.go</code>.</small>

### Counter.Invalid

```go
func (c *Counter) Invalid() int64
```

Invalid is the number of totals above 2^Width − 1.

<small>Defined in <code>rate/counter.go</code>.</small>

### Counter.Reset

```go
func (c *Counter) Reset()
```

Reset forgets the baseline and every count, keeping the width.

<small>Defined in <code>rate/counter.go</code>.</small>

### Counter.Resets

```go
func (c *Counter) Resets() int64
```

Resets is the number of decreases taken as counter resets.

<small>Defined in <code>rate/counter.go</code>.</small>

### Counter.Skipped

```go
func (c *Counter) Skipped() int64
```

Skipped is the number of readings not after their predecessor.

<small>Defined in <code>rate/counter.go</code>.</small>

### Counter.Update

```go
func (c *Counter) Update(total uint64, at time.Time) (r Reading, ok bool)
```

Update reports the counter's total as read at time at. It returns the
increase and rate since the previous reading with ok = true, or a zero
Reading with ok = false for a baseline, a reset, a reading not after the
previous one, or a total too large for the width. It does not allocate.

<small>Defined in <code>rate/counter.go</code>.</small>

### Counter.Width

```go
func (c *Counter) Width() uint
```

Width is the counter's width in bits.

<small>Defined in <code>rate/counter.go</code>.</small>

### Counter.Wraps

```go
func (c *Counter) Wraps() int64
```

Wraps is the number of decreases taken as wraps past the maximum.

<small>Defined in <code>rate/counter.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("rate: invalid parameter")
```

ErrInvalid is wrapped by every constructor error: a parameter outside the
range its doc names. Match it with errors.Is; the message text is not part
of the API.

<small>Defined in <code>rate/errors.go</code>.</small>

### Intervals.Add

```go
func (iv *Intervals) Add(at time.Time)
```

Add records a reading taken at time at. It does not allocate.

<small>Defined in <code>rate/intervals.go</code>.</small>

### Intervals.Last

```go
func (iv *Intervals) Last() time.Duration
```

Last is the most recent gap, zero when none.

<small>Defined in <code>rate/intervals.go</code>.</small>

### Intervals.Late

```go
func (iv *Intervals) Late(threshold time.Duration) bool
```

Late reports whether the last gap exceeded threshold. It is false when no
gap has been recorded.

<small>Defined in <code>rate/intervals.go</code>.</small>

### Intervals.Max

```go
func (iv *Intervals) Max() time.Duration
```

Max is the longest gap, zero when none.

<small>Defined in <code>rate/intervals.go</code>.</small>

### Intervals.Mean

```go
func (iv *Intervals) Mean() time.Duration
```

Mean is the mean gap, truncated to the nanosecond, zero when none.

<small>Defined in <code>rate/intervals.go</code>.</small>

### Intervals.Min

```go
func (iv *Intervals) Min() time.Duration
```

Min is the shortest gap, zero when none.

<small>Defined in <code>rate/intervals.go</code>.</small>

### Intervals.N

```go
func (iv *Intervals) N() int64
```

N is the number of gaps recorded: one fewer than the times accepted.

<small>Defined in <code>rate/intervals.go</code>.</small>

### Intervals.Reset

```go
func (iv *Intervals) Reset()
```

Reset forgets every time and gap and both counts.

<small>Defined in <code>rate/intervals.go</code>.</small>

### Intervals.Skipped

```go
func (iv *Intervals) Skipped() int64
```

Skipped is the number of times not after their predecessor.

<small>Defined in <code>rate/intervals.go</code>.</small>

### NewCounter

```go
func NewCounter(width uint) (Counter, error)
```

NewCounter returns a counter of the given width in bits, 1 to 64: one that
wraps to zero after 2^width − 1.

<small>Defined in <code>rate/counter.go</code>.</small>

### NewWindow

```go
func NewWindow(size int) (*Window, error)
```

NewWindow returns a window over the last size values, 1 ≤ size ≤
math.MaxInt/32 (a bound on what can be addressed, not a suggestion: memory
is 32 bytes per slot).

<small>Defined in <code>rate/window.go</code>.</small>

### Peak.Add

```go
func (p *Peak) Add(x float64, at time.Time)
```

Add offers x, seen at time at. It does not allocate.

<small>Defined in <code>rate/peak.go</code>.</small>

### Peak.At

```go
func (p *Peak) At() time.Time
```

At is the time Max was first reached, the zero time when nothing has been
added.

<small>Defined in <code>rate/peak.go</code>.</small>

### Peak.Max

```go
func (p *Peak) Max() float64
```

Max is the largest value added since Reset, NaN when none has been.

<small>Defined in <code>rate/peak.go</code>.</small>

### Peak.N

```go
func (p *Peak) N() int64
```

N is the number of values added since Reset, skipped ones not included.

<small>Defined in <code>rate/peak.go</code>.</small>

### Peak.Reset

```go
func (p *Peak) Reset()
```

Reset forgets the peak and both counts.

<small>Defined in <code>rate/peak.go</code>.</small>

### Peak.Skipped

```go
func (p *Peak) Skipped() int64
```

Skipped is the number of non-finite values Add refused.

<small>Defined in <code>rate/peak.go</code>.</small>

### Window.Add

```go
func (w *Window) Add(x float64)
```

Add appends x to the window, dropping the oldest value once the window is
full. NaN and ±Inf are skipped. It does not allocate.

<small>Defined in <code>rate/window.go</code>.</small>

### Window.Len

```go
func (w *Window) Len() int
```

Len is the number of values in the window: those added since Reset, up to
Size.

<small>Defined in <code>rate/window.go</code>.</small>

### Window.Max

```go
func (w *Window) Max() float64
```

Max is the largest of the values in the window, NaN when it is empty.

<small>Defined in <code>rate/window.go</code>.</small>

### Window.Min

```go
func (w *Window) Min() float64
```

Min is the smallest of the values in the window, NaN when it is empty.

<small>Defined in <code>rate/window.go</code>.</small>

### Window.Reset

```go
func (w *Window) Reset()
```

Reset empties the window and zeroes Skipped, keeping its buffer.

<small>Defined in <code>rate/window.go</code>.</small>

### Window.Size

```go
func (w *Window) Size() int
```

Size is the window's capacity in values.

<small>Defined in <code>rate/window.go</code>.</small>

### Window.Skipped

```go
func (w *Window) Skipped() int64
```

Skipped is the number of non-finite values Add refused.

<small>Defined in <code>rate/window.go</code>.</small>
