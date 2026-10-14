// SPDX-License-Identifier: BSD-3-Clause

package rate

import "math"

// Window is the minimum and maximum of the last Size values added, exact,
// in amortised O(1) time per value and fixed memory.
//
// Rescanning the last n values on every Add costs O(n) per value; a sorted
// structure costs O(log n). Window keeps instead two monotonic queues of
// candidates. A value can only ever be the window's maximum if no later
// value is at least as large, so when x arrives every queued maximum
// candidate ≤ x is dropped from the back, and a candidate that has left the
// window is dropped from the front. What remains is decreasing from front to
// back, its front is the maximum, and each value enters and leaves each queue
// once: amortised O(1), and at most Size candidates per queue. The minimum is
// the mirror image.
//
// Method: the monotonic wedge of D. Lemire, "Streaming maximum-minimum filter
// using no more than three comparisons per element", Nordic Journal of
// Computing 13(4), 2006. The block method of J. Gil and M. Werman, "Computing
// 2-D min, median, and max filters", IEEE Transactions on Pattern Analysis
// and Machine Intelligence 15(5), 1993, reaches a similar constant cost per
// value, but it processes values in blocks of the window size and so answers
// late for a stream; the wedge answers after every value.
//
// Ties: when x equals a queued candidate, the older one is dropped and x is
// kept, because x stays in the window longer and the queue stays shorter.
// Equal values are interchangeable, so which is kept never changes Min or
// Max; +0 and −0 compare equal and either may be returned for the other.
//
// The window counts accepted values: NaN and ±Inf are skipped, counted in
// Skipped, and do not take a slot, so "the last 60 values" means the last 60
// finite ones.
//
// Both queues live in one buffer of 2·Size slots, 16 bytes each, allocated by
// NewWindow; Add and Reset never allocate. Because of that buffer, a Window
// is not a plain value: a copy shares the buffer with the original, and
// using both corrupts both. Build one per series with NewWindow.
//
// The zero value is not usable (Add panics on it); build one with
// NewWindow. Not safe for concurrent use.
type Window struct {
	size    int
	maxq    deque // decreasing values: front is the maximum
	minq    deque // increasing values: front is the minimum
	n       int64 // values accepted since Reset; the next one gets index n
	skipped int64
}

// slot is one queued candidate: its value and its index in the stream.
type slot struct {
	i int64
	x float64
}

// deque is a fixed-capacity ring of slots. Capacity is the window size and
// is never exceeded: Add drops expired candidates before pushing, so a queue
// holds at most the size − 1 others still in the window plus the new value.
type deque struct {
	buf         []slot
	head, count int
}

func (d *deque) front() slot { return d.buf[d.head] }

func (d *deque) back() slot {
	return d.buf[d.at(d.count-1)]
}

func (d *deque) at(k int) int {
	k += d.head
	if k >= len(d.buf) {
		k -= len(d.buf)
	}
	return k
}

func (d *deque) popFront() {
	d.head = d.at(1)
	d.count--
}

func (d *deque) push(s slot) {
	d.buf[d.at(d.count)] = s
	d.count++
}

// maxWindow keeps 2·size·16 bytes addressable as an int on every platform.
const maxWindow = math.MaxInt / 32

// NewWindow returns a window over the last size values, 1 ≤ size ≤
// math.MaxInt/32 (a bound on what can be addressed, not a suggestion: memory
// is 32 bytes per slot).
func NewWindow(size int) (Window, error) {
	if size < 1 || size > maxWindow {
		return Window{}, invalid("size", size, "1..MaxInt/32")
	}
	buf := make([]slot, 2*size)
	return Window{
		size: size,
		maxq: deque{buf: buf[:size:size]},
		minq: deque{buf: buf[size:]},
	}, nil
}

// Add appends x to the window, dropping the oldest value once the window is
// full. NaN and ±Inf are skipped. It does not allocate.
func (w *Window) Add(x float64) {
	if !finite(x) {
		w.skipped++
		return
	}
	i := w.n
	w.n++
	// Expire first, so that the push below never exceeds the capacity: the
	// value at index i − size has just left the window.
	old := i - int64(w.size)
	if w.maxq.count > 0 && w.maxq.front().i <= old {
		w.maxq.popFront()
	}
	if w.minq.count > 0 && w.minq.front().i <= old {
		w.minq.popFront()
	}
	for w.maxq.count > 0 && w.maxq.back().x <= x {
		w.maxq.count--
	}
	for w.minq.count > 0 && w.minq.back().x >= x {
		w.minq.count--
	}
	w.maxq.push(slot{i, x})
	w.minq.push(slot{i, x})
}

// Max is the largest of the values in the window, NaN when it is empty.
func (w *Window) Max() float64 {
	if w.maxq.count == 0 {
		return math.NaN()
	}
	return w.maxq.front().x
}

// Min is the smallest of the values in the window, NaN when it is empty.
func (w *Window) Min() float64 {
	if w.minq.count == 0 {
		return math.NaN()
	}
	return w.minq.front().x
}

// Len is the number of values in the window: those added since Reset, up to
// Size.
func (w *Window) Len() int {
	if w.n < int64(w.size) {
		return int(w.n)
	}
	return w.size
}

// Size is the window's capacity in values.
func (w *Window) Size() int { return w.size }

// Skipped is the number of non-finite values Add refused.
func (w *Window) Skipped() int64 { return w.skipped }

// Reset empties the window and zeroes Skipped, keeping its buffer.
func (w *Window) Reset() {
	w.maxq.head, w.maxq.count = 0, 0
	w.minq.head, w.minq.count = 0, 0
	w.n, w.skipped = 0, 0
}
