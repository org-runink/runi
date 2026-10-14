package drift

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// floats decodes b as little-endian float64s, so the fuzzer can reach every
// bit pattern: NaNs, infinities, subnormals, and the largest finite values.
func floats(b []byte) []float64 {
	xs := make([]float64, len(b)/8)
	for i := range xs {
		xs[i] = math.Float64frombits(binary.LittleEndian.Uint64(b[8*i:]))
	}
	return xs
}

func bytesOf(xs ...float64) []byte {
	b := make([]byte, 8*len(xs))
	for i, x := range xs {
		binary.LittleEndian.PutUint64(b[8*i:], math.Float64bits(x))
	}
	return b
}

// Welford never reports a negative variance, and merging any split agrees
// with one pass over the whole. The seed with ±MaxFloat64 is the input that
// made an earlier version report a variance of −Inf.
func FuzzWelford(f *testing.F) {
	f.Add(bytesOf(2, 4, 4, 4, 5, 5, 7, 9), uint8(3))
	f.Add(bytesOf(1e9+1, 1e9+2, 1e9+3), uint8(1))
	f.Add(bytesOf(math.NaN(), 1, math.Inf(1), -1), uint8(2))
	f.Add(bytesOf(1e150, -1e150, 3), uint8(0))
	f.Add(bytesOf(math.MaxFloat64, -math.MaxFloat64), uint8(1))
	f.Fuzz(func(t *testing.T, b []byte, cut uint8) {
		xs := floats(b)
		k := int(cut) % (len(xs) + 1)
		var whole, a, c Welford
		bounded, scale := true, 0.0
		for i, x := range xs {
			whole.Add(x)
			if i < k {
				a.Add(x)
			} else {
				c.Add(x)
			}
			if finite(x) {
				// Past 1e150 squares overflow; the doc says so, and those
				// inputs are held only to "never negative".
				bounded = bounded && math.Abs(x) <= 1e150
				scale = max(scale, math.Abs(x))
			}
		}
		a.Merge(c)
		for _, w := range []Welford{whole, a} {
			if w.Variance() < 0 || w.PopVariance() < 0 {
				t.Fatalf("negative variance %v / %v from %v", w.Variance(), w.PopVariance(), xs)
			}
			// At any magnitude: the mean stays finite and the variance is a
			// number, +Inf at worst.
			if w.N() >= 1 && !finite(w.Mean()) || w.N() >= 2 && math.IsNaN(w.Variance()) {
				t.Fatalf("mean %v variance %v from %v", w.Mean(), w.Variance(), xs)
			}
			if bounded && w.N() >= 2 && (!finite(w.Variance()) || !finite(w.Mean())) {
				t.Fatalf("non-finite statistics %v %v from bounded input %v", w.Mean(), w.Variance(), xs)
			}
		}
		if a.N() != whole.N() || a.Skipped() != whole.Skipped() {
			t.Fatalf("merge N %d/%d skipped %d/%d", a.N(), whole.N(), a.Skipped(), whole.Skipped())
		}
		if bounded && whole.N() > 0 {
			// The absolute floor is for subnormal inputs, where float64 has
			// only absolute precision left and scale² underflows to zero.
			if math.Abs(a.Mean()-whole.Mean()) > 1e-9*scale+1e-300 {
				t.Fatalf("merged mean %v, one pass %v (%v)", a.Mean(), whole.Mean(), xs)
			}
			// The variance is bounded by scale²; compare on that scale, since
			// a relative test fails on variances that are tiny by cancellation.
			if whole.N() >= 2 && math.Abs(a.Variance()-whole.Variance()) > 1e-9*scale*scale+1e-300 {
				t.Fatalf("merged variance %v, one pass %v (%v)", a.Variance(), whole.Variance(), xs)
			}
		}
	})
}

// CUSUM statistics are never negative and never NaN, for any input and any
// valid parameters.
func FuzzCUSUM(f *testing.F) {
	f.Add(bytesOf(12, 13, 11, 12, 4, 9), 10.0, 1.0, 3.0)
	f.Add(bytesOf(math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64), 0.0, 0.0, 1.0)
	f.Add(bytesOf(-math.MaxFloat64, math.NaN(), math.Inf(-1)), math.MaxFloat64, 0.0, 1.0)
	f.Add(bytesOf(1, 2, 3), -math.MaxFloat64, math.MaxFloat64, math.SmallestNonzeroFloat64)
	f.Fuzz(func(t *testing.T, b []byte, target, k, h float64) {
		c, err := NewCUSUM(target, k, h)
		if err != nil {
			return
		}
		for _, x := range floats(b) {
			s := c.Add(x)
			up, lo := c.Upper(), c.Lower()
			if !(up >= 0) || !(lo >= 0) || math.IsInf(up, 0) || math.IsInf(lo, 0) {
				t.Fatalf("after %v: upper %v lower %v", x, up, lo)
			}
			if finite(x) && (s == Up) != (up > h && up >= lo) || s != None && !finite(x) || c.Since() < 0 || c.Since() > c.N() {
				t.Fatalf("after %v: signal %v with upper %v lower %v h %v since %d", x, s, up, lo, h, c.Since())
			}
		}
	})
}

// Decayed never returns NaN or Inf with ok = true, for any sequence of
// totals and times, including resets, wraps and clocks going backwards.
func FuzzDecayed(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}, int64(time.Second))
	f.Add(bytesOf(1, 2, 3, 4, 5, 6), int64(1))
	f.Add(bytesOf(math.MaxFloat64, 0, math.MaxFloat64, 1), int64(math.MaxInt64))
	f.Fuzz(func(t *testing.T, b []byte, window int64) {
		d, err := NewDecayed(time.Duration(window))
		if err != nil {
			return
		}
		at := t0
		for i := 0; i+16 <= len(b); i += 16 {
			total := binary.LittleEndian.Uint64(b[i:])
			// Steps of any sign and size, up to about ±146 years.
			at = at.Add(time.Duration(int64(binary.LittleEndian.Uint64(b[i+8:])) >> 2))
			v, ok := d.Update(total, at)
			if ok && !finite(v) {
				t.Fatalf("rate %v with ok at total %d", v, total)
			}
			if ok && v < 0 {
				t.Fatalf("negative rate %v from a non-decreasing counter", v)
			}
		}
	})
}
