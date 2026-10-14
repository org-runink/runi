package rate

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func bytesOf(xs ...float64) []byte {
	b := make([]byte, 8*len(xs))
	for i, x := range xs {
		binary.LittleEndian.PutUint64(b[8*i:], math.Float64bits(x))
	}
	return b
}

// pairs encodes (total, step) pairs as the counter fuzzer reads them.
func pairs(ps ...int64) []byte {
	b := make([]byte, 8*len(ps))
	for i, p := range ps {
		binary.LittleEndian.PutUint64(b[8*i:], uint64(p))
	}
	return b
}

// Counter never returns ok = true with an increase that disagrees with
// modular arithmetic from the baseline it kept, across a fall of more than
// half the range, or with a rate that is negative, NaN or infinite, for any
// width, totals and times. The model below tracks the baseline independently:
// it moves on a first reading and on every reading that produced a rate or a
// reset, and on nothing else.
func FuzzCounter(f *testing.F) {
	f.Add(pairs(250, 0, 4, int64(time.Second), 300, 1), uint8(8))
	f.Add(pairs(-1, 0, 0, 1, 5, -1, 6, 0), uint8(64))
	f.Add(pairs(1, 0, 0, 1, 1, 1, 0, 1), uint8(1))
	f.Add(pairs(1<<31, 0, 0, 1, 1<<31+1, math.MaxInt64, 0, math.MaxInt64), uint8(32))
	f.Add(pairs(0, 0, 0, 0), uint8(0))
	f.Fuzz(func(t *testing.T, b []byte, width uint8) {
		c, err := NewCounter(uint(width))
		if err != nil {
			if width >= 1 && width <= 64 {
				t.Fatalf("width %d rejected: %v", width, err)
			}
			return
		}
		mask := ^uint64(0) >> (64 - uint(width))
		var prev uint64
		var prevAt time.Time
		have := false
		at := t0
		for i := 0; i+16 <= len(b); i += 16 {
			total := binary.LittleEndian.Uint64(b[i:])
			if i%32 == 16 {
				total &= mask // keep most totals valid so the interesting paths run
			}
			// Steps of any sign and size, up to about ±146 years.
			at = at.Add(time.Duration(int64(binary.LittleEndian.Uint64(b[i+8:])) >> 2))
			resets := c.Resets()
			r, ok := c.Update(total, at)
			if ok {
				if !have || total > mask || !at.After(prevAt) {
					t.Fatalf("ok without a valid baseline: total %d have %v", total, have)
				}
				if r.Delta != (total-prev)&mask || r.Interval != at.Sub(prevAt) {
					t.Fatalf("w=%d %d→%d: %+v, want delta %d over %v", width, prev, total, r, (total-prev)&mask, at.Sub(prevAt))
				}
				if total < prev && (width == 64 || r.Delta > uint64(1)<<(width-1)) {
					t.Fatalf("w=%d %d→%d taken as a wrap of %d", width, prev, total, r.Delta)
				}
				if !finite(r.PerSecond) || r.PerSecond < 0 || r.PerSecond != float64(r.Delta)/r.Interval.Seconds() {
					t.Fatalf("PerSecond %v for %+v", r.PerSecond, r)
				}
			} else if r != (Reading{}) {
				t.Fatalf("not ok but %+v", r)
			}
			if ok || c.Resets() > resets || !have && total <= mask {
				prev, prevAt, have = total, at, true
			}
		}
	})
}

// Window agrees with brute force for any values and any size, including
// NaN, infinities, signed zeros and subnormals.
func FuzzWindow(f *testing.F) {
	f.Add(bytesOf(3, 1, 4, 1, 5, 9, 2, 6), uint8(3))
	f.Add(bytesOf(1, 1, 1, 1), uint8(1))
	f.Add(bytesOf(math.NaN(), 0, math.Copysign(0, -1), math.Inf(1), math.SmallestNonzeroFloat64), uint8(2))
	f.Add(bytesOf(-math.MaxFloat64, math.MaxFloat64), uint8(0))
	f.Fuzz(func(t *testing.T, b []byte, size uint8) {
		w, err := NewWindow(int(size))
		if err != nil {
			if size != 0 {
				t.Fatalf("size %d rejected: %v", size, err)
			}
			return
		}
		xs := make([]float64, len(b)/8)
		for i := range xs {
			xs[i] = math.Float64frombits(binary.LittleEndian.Uint64(b[8*i:]))
			w.Add(xs[i])
			lo, hi, n := brute(xs[:i+1], int(size))
			if !same(w.Min(), lo) || !same(w.Max(), hi) || w.Len() != n {
				t.Fatalf("size %d after %v: [%v, %v] len %d, want [%v, %v] len %d", size, xs[:i+1], w.Min(), w.Max(), w.Len(), lo, hi, n)
			}
		}
	})
}

// Intervals: Min ≤ Mean ≤ Max, Last within them, and Skipped plus N plus the
// baseline account for every time, for any sequence of times.
func FuzzIntervals(f *testing.F) {
	f.Add(pairs(int64(time.Second), 0, -1, int64(time.Hour)))
	f.Add(pairs(math.MaxInt64, math.MaxInt64, math.MinInt64, 1))
	f.Fuzz(func(t *testing.T, b []byte) {
		var iv Intervals
		at := t0
		k := int64(0)
		for i := 0; i+8 <= len(b); i += 8 {
			at = at.Add(time.Duration(int64(binary.LittleEndian.Uint64(b[i:])) >> 2))
			iv.Add(at)
			k++
			if iv.N()+iv.Skipped()+1 != k {
				t.Fatalf("n %d skipped %d after %d times", iv.N(), iv.Skipped(), k)
			}
			if iv.N() > 0 && !(iv.Min() > 0 && iv.Min() <= iv.Mean() && iv.Mean() <= iv.Max() &&
				iv.Min() <= iv.Last() && iv.Last() <= iv.Max()) {
				t.Fatalf("min %v mean %v max %v last %v", iv.Min(), iv.Mean(), iv.Max(), iv.Last())
			}
		}
	})
}
