package throttle

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
	"time"
)

func TestNewBackoffInvalid(t *testing.T) {
	src := rand.NewPCG(1, 2)
	for _, c := range []struct {
		j         Jitter
		base, cap time.Duration
		src       rand.Source
	}{
		{NoJitter + 1, 1, 1, src}, {FullJitter, 0, 1, src}, {FullJitter, -1, 1, src},
		{DecorrelatedJitter, 2, 1, src}, {FullJitter, 1, 1, nil},
	} {
		if _, err := NewBackoff(c.j, c.base, c.cap, c.src); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewBackoff(%v, %v, %v) error = %v, want ErrInvalid", c.j, c.base, c.cap, err)
		}
	}
}

func TestJitterString(t *testing.T) {
	for j, want := range map[Jitter]string{FullJitter: "full", DecorrelatedJitter: "decorrelated", NoJitter: "none", 9: "Jitter(9)"} {
		if j.String() != want {
			t.Errorf("%d.String() = %q, want %q", j, j.String(), want)
		}
	}
}

var two64 = new(big.Int).Lsh(big.NewInt(1), 64)

// refBelow is Lemire's method written out in big integers: x·n split into
// its high and low 64-bit words, rejecting x when the low word is below
// 2⁶⁴ mod n.
func refBelow(src rand.Source, n uint64) uint64 {
	N := new(big.Int).SetUint64(n)
	thresh := new(big.Int).Mod(two64, N)
	for {
		p := new(big.Int).Mul(new(big.Int).SetUint64(src.Uint64()), N)
		lo := new(big.Int).Mod(p, two64)
		if lo.Cmp(thresh) >= 0 {
			return new(big.Int).Rsh(p, 64).Uint64()
		}
	}
}

// refCeiling is min(cap, base·2ⁿ) in big integers.
func refCeiling(base, cap time.Duration, n int) time.Duration {
	c := new(big.Int).Lsh(big.NewInt(int64(base)), uint(n))
	if c.Cmp(big.NewInt(int64(cap))) > 0 {
		return cap
	}
	return time.Duration(c.Int64())
}

// refBackoff is Brooker's two formulas, literally, drawing from a second
// source seeded the same.
func refBackoff(j Jitter, base, cap time.Duration, src rand.Source, n int) []time.Duration {
	out := make([]time.Duration, n)
	sleep := base
	for i := range out {
		if j == FullJitter {
			hi := refCeiling(base, cap, i)
			out[i] = time.Duration(refBelow(src, uint64(hi)+1))
		} else {
			hi := new(big.Int).Mul(big.NewInt(int64(sleep)), big.NewInt(3))
			if !hi.IsInt64() {
				hi.SetInt64(math.MaxInt64)
			}
			d := int64(base) + int64(refBelow(src, uint64(hi.Int64()-int64(base))+1))
			sleep = min(cap, time.Duration(d))
			out[i] = sleep
		}
	}
	return out
}

// Every delay is the reference's, drawn from the same stream, and lies in
// [0, min(cap, base·2ⁿ)] (full) or [base, cap] (decorrelated). The ranges
// include the extremes: a base of 1 ns, a cap of the largest Duration, and
// attempts far past 63.
func TestBackoffMatchesReference(t *testing.T) {
	for _, c := range []struct {
		base, cap time.Duration
	}{
		{time.Millisecond, time.Second},
		{100 * time.Millisecond, 100 * time.Millisecond},
		{1, math.MaxInt64},
		{3, 1 << 40},
		{math.MaxInt64 / 2, math.MaxInt64},
	} {
		for _, j := range []Jitter{FullJitter, DecorrelatedJitter} {
			for seed := range uint64(5) {
				b := mustBackoff(t, j, c.base, c.cap, seed)
				want := refBackoff(j, c.base, c.cap, rand.NewPCG(seed, seed^0x9e3779b97f4a7c15), 100)
				for n, w := range want {
					ceil := b.Ceiling()
					d := b.Next()
					if d != w {
						t.Fatalf("%v base %v cap %v seed %d attempt %d: %v, want %v", j, c.base, c.cap, seed, n, d, w)
					}
					lo, hi := time.Duration(0), refCeiling(c.base, c.cap, n)
					if j == DecorrelatedJitter {
						lo, hi = c.base, c.cap
					}
					if d < lo || d > hi || d > ceil {
						t.Fatalf("%v attempt %d: %v outside [%v, %v] or above Ceiling %v", j, n, d, lo, hi, ceil)
					}
					if b.Attempt() != n+1 {
						t.Fatalf("Attempt = %d, want %d", b.Attempt(), n+1)
					}
				}
			}
		}
	}
}

// Ceiling is the exact bound: full jitter reaches it, and the decorrelated
// ceiling is min(cap, 3·prev).
func TestBackoffCeiling(t *testing.T) {
	b := mustBackoff(t, FullJitter, 10, 1000, 1)
	for n, want := range []time.Duration{10, 20, 40, 80, 160, 320, 640, 1000, 1000} {
		if b.Ceiling() != want {
			t.Fatalf("attempt %d: Ceiling %v, want %v", n, b.Ceiling(), want)
		}
		b.Next()
	}
	b.attempt = math.MaxInt
	if b.Ceiling() != 1000 {
		t.Fatal("Ceiling at the last attempt")
	}
	b.Next()
	if b.Attempt() != math.MaxInt {
		t.Fatal("Attempt does not stop at MaxInt")
	}
	d := mustBackoff(t, DecorrelatedJitter, 10, 1000, 1)
	if d.Ceiling() != 30 {
		t.Fatalf("first decorrelated Ceiling %v, want 30", d.Ceiling())
	}
	d.prev = 333
	if d.Ceiling() != 999 {
		t.Fatal("Ceiling below the cap")
	}
	d.prev = 334
	if d.Ceiling() != 1000 {
		t.Fatal("Ceiling at the cap")
	}
	if d.Base() != 10 || d.Cap() != 1000 {
		t.Fatal("Base, Cap")
	}
}

// Every value of a full-jitter range, both ends included, is equally likely:
// a chi-squared test on [0, 7] ns over 80 000 draws, at the 0.1% level for
// 7 degrees of freedom (24.32).
func TestBackoffUniform(t *testing.T) {
	b := mustBackoff(t, FullJitter, 7, 7, 42)
	var count [8]int
	const draws = 80000
	for range draws {
		count[b.Next()]++
	}
	chi := 0.0
	for _, c := range count {
		e := float64(draws) / 8
		chi += (float64(c) - e) * (float64(c) - e) / e
	}
	if chi > 24.32 {
		t.Fatalf("chi-squared %.1f over 7 degrees of freedom: %v", chi, count)
	}
}

// The same seed draws the same delays; Reset restarts the attempts but not
// the stream.
func TestBackoffDeterministic(t *testing.T) {
	a := mustBackoff(t, DecorrelatedJitter, time.Millisecond, time.Minute, 7)
	b := mustBackoff(t, DecorrelatedJitter, time.Millisecond, time.Minute, 7)
	var first []time.Duration
	for range 20 {
		x, y := a.Next(), b.Next()
		if x != y {
			t.Fatal("same seed, different delays")
		}
		first = append(first, x)
	}
	a.Reset()
	if a.Attempt() != 0 || a.prev != time.Millisecond {
		t.Fatal("Reset")
	}
	same := 0
	for i := range 20 {
		if a.Next() == first[i] {
			same++
		}
	}
	if same == 20 {
		t.Fatal("Reset rewound the source")
	}
}

// fixed returns its values in order, then repeats the last.
type fixed struct {
	v []uint64
	i int
}

func (f *fixed) Uint64() uint64 {
	x := f.v[min(f.i, len(f.v)-1)]
	f.i++
	return x
}

// The rejection step of below runs: for n = 3, 2⁶⁴ mod 3 = 1, so x = 0
// (low word 0) is rejected and the next x is used. An x whose low word is
// below n but not below the threshold is kept.
func TestBelowRejects(t *testing.T) {
	src := &fixed{v: []uint64{0, math.MaxUint64}}
	if got := below(src, 3); got != 2 || src.i != 2 {
		t.Fatalf("below = %d after %d draws, want 2 after 2", got, src.i)
	}
	// A low word of n or more is kept at once; one of 1 or 2 is below n but
	// not below the threshold of 1, and is kept too.
	src = &fixed{v: []uint64{(1<<64 - 1) / 3 * 2}} // x·3 = 2·(2⁶⁴−1): low word 2⁶⁴−2
	if got := below(src, 3); got != 1 || src.i != 1 {
		t.Fatalf("below = %d after %d draws", got, src.i)
	}
	src = &fixed{v: []uint64{(1<<64-1)/3 + 1}} // x·3 = 2⁶⁴ + 2: low word 2, in [1, 3)
	if got := below(src, 3); got != 1 || src.i != 1 {
		t.Fatalf("below = %d after %d draws, want 1 after 1", got, src.i)
	}
	// A power of two never rejects: 2⁶⁴ mod n = 0.
	for range 100 {
		src := rand.NewPCG(1, 1)
		if below(src, 1<<10) >= 1<<10 {
			t.Fatal("out of range")
		}
	}
	// A *rand.Rand is a source too.
	if _, err := NewBackoff(FullJitter, 1, 2, rand.New(rand.NewPCG(1, 2))); err != nil {
		t.Fatal(err)
	}
}
