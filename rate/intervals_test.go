package rate

import (
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
	"time"
)

func TestIntervalsKnownAnswer(t *testing.T) {
	var iv Intervals
	if iv.N() != 0 || iv.Mean() != 0 || iv.Min() != 0 || iv.Max() != 0 || iv.Last() != 0 || iv.Late(0) {
		t.Fatalf("zero value: %+v", iv)
	}
	iv.Add(t0) // a baseline only
	if iv.N() != 0 || iv.Late(-time.Hour) {
		t.Fatalf("after one time: n %d", iv.N())
	}
	at := t0
	for _, d := range []time.Duration{time.Second, 3 * time.Second, 500 * time.Millisecond, 1001 * time.Millisecond} {
		at = at.Add(d)
		iv.Add(at)
	}
	// Σ = 5.501 s over 4 gaps: 1.37525 s.
	if iv.N() != 4 || iv.Mean() != 1375250*time.Microsecond || iv.Min() != 500*time.Millisecond ||
		iv.Max() != 3*time.Second || iv.Last() != 1001*time.Millisecond {
		t.Fatalf("n %d mean %v min %v max %v last %v", iv.N(), iv.Mean(), iv.Min(), iv.Max(), iv.Last())
	}
	if !iv.Late(time.Second) || iv.Late(1001*time.Millisecond) {
		t.Fatal("Late is strictly greater than the threshold")
	}
}

// The mean truncates to the nanosecond: 1, 1 and 2 ns average 1.33 ns → 1.
func TestIntervalsMeanTruncates(t *testing.T) {
	var iv Intervals
	at := t0
	iv.Add(at)
	for _, d := range []time.Duration{1, 1, 2} {
		at = at.Add(d)
		iv.Add(at)
	}
	if iv.Mean() != 1 {
		t.Fatalf("mean %v, want 1ns", iv.Mean())
	}
}

// Duplicate and out-of-order times are skipped and keep the baseline: the next
// valid gap is measured from the last accepted time.
func TestIntervalsOutOfOrder(t *testing.T) {
	var iv Intervals
	iv.Add(t0)
	iv.Add(t0.Add(time.Second))
	iv.Add(t0.Add(time.Second))     // duplicate
	iv.Add(t0.Add(-time.Second))    // out of order
	iv.Add(t0.Add(time.Second / 2)) // before the baseline still
	iv.Add(t0.Add(4 * time.Second)) // 3 s after the baseline
	if iv.Skipped() != 3 || iv.N() != 2 || iv.Last() != 3*time.Second || iv.Mean() != 2*time.Second {
		t.Fatalf("skipped %d n %d last %v mean %v", iv.Skipped(), iv.N(), iv.Last(), iv.Mean())
	}
	iv.Reset()
	if iv.N() != 0 || iv.Skipped() != 0 || iv.Max() != 0 {
		t.Fatalf("after Reset: %+v", iv)
	}
	iv.Add(t0.Add(-time.Hour)) // a baseline again, whatever came before
	iv.Add(t0)
	if iv.N() != 1 || iv.Last() != time.Hour {
		t.Fatalf("n %d last %v", iv.N(), iv.Last())
	}
}

// The 128-bit sum: gaps of the largest Duration, which a 64-bit sum would
// overflow on the second, still average exactly.
func TestIntervalsMeanNoOverflow(t *testing.T) {
	var iv Intervals
	iv.Add(time.Time{})
	at := time.Time{}
	for range 4 {
		next := at.Add(math.MaxInt64)
		iv.Add(next)
		at = next
	}
	if iv.N() != 4 || iv.Mean() != math.MaxInt64 || iv.Max() != math.MaxInt64 {
		t.Fatalf("n %d mean %v max %v", iv.N(), iv.Mean(), iv.Max())
	}
	// And the overflow is real: the naive int64 sum wraps negative.
	var naive time.Duration
	for range 4 {
		naive += math.MaxInt64
	}
	if naive >= 0 {
		t.Fatal("an int64 sum of four MaxInt64 did not overflow")
	}
}

// Against an exact big-integer mean on random gaps.
func TestIntervalsMatchesExact(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	var iv Intervals
	at := t0
	iv.Add(at)
	sum := new(big.Int)
	lo, hi := time.Duration(math.MaxInt64), time.Duration(0)
	for i := range 5000 {
		d := time.Duration(1 + r.Int64N(int64(10*time.Second)))
		if i%100 == 0 {
			d = time.Duration(1 + r.Int64N(math.MaxInt64/8))
		}
		at = at.Add(d)
		iv.Add(at)
		sum.Add(sum, big.NewInt(int64(d)))
		lo, hi = min(lo, d), max(hi, d)
		want := new(big.Int).Quo(sum, big.NewInt(int64(i+1)))
		if iv.Mean() != time.Duration(want.Int64()) || iv.Min() != lo || iv.Max() != hi || iv.Last() != d {
			t.Fatalf("step %d: mean %v want %v; min %v/%v max %v/%v", i, iv.Mean(), want, iv.Min(), lo, iv.Max(), hi)
		}
	}
}
