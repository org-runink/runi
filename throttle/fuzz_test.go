package throttle

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

// fuzzCalls decodes b as 4-byte records: a signed 16-bit time step in
// milliseconds, so duplicates and out-of-order times are as easy to reach as
// ordered ones, then a byte for the size (−2 to 13) and key (0 to 15), and a
// byte for the operation and the result to record.
func fuzzCalls(b []byte) []call {
	var cs []call
	at := t0
	for i := 0; i+4 <= len(b) && len(cs) < 128; i += 4 {
		at = at.Add(time.Duration(int16(binary.LittleEndian.Uint16(b[i:]))) * time.Millisecond)
		cs = append(cs, call{at: at, n: int(b[i+2]&15) - 2, key: int(b[i+2] >> 4), op: int(b[i+3] & 3), ok: b[i+3]&4 != 0})
	}
	return cs
}

func calls(steps ...int16) []byte {
	var b []byte
	for i, s := range steps {
		b = binary.LittleEndian.AppendUint16(b, uint16(s))
		b = append(b, byte(i*37), byte(i*11))
	}
	return b
}

// Every type agrees with its naive reference for any calls and any
// parameters its constructor accepts, and every constructor accepts exactly
// the parameters its doc names.
func FuzzThrottle(f *testing.F) {
	f.Add(calls(1000, 0, 0, -500, 250, 3000, -3000, 1), 1.0, uint8(3), int64(time.Second), int64(10*time.Second), uint64(1))
	f.Add(calls(1, 1, 1, 1, -1, 32767, -32768, 0), 3.0, uint8(1), int64(1), int64(math.MaxInt64), uint64(2))
	f.Add(calls(0, 0, 0, 0), 2e9, uint8(255), int64(0), int64(0), uint64(3))
	f.Add(calls(500, 500, -1, 500), 1e-9, uint8(9), int64(-1), int64(5), uint64(4))
	f.Add(calls(), math.NaN(), uint8(0), int64(math.MaxInt64), int64(math.MaxInt64), uint64(5))
	f.Fuzz(func(t *testing.T, b []byte, rate float64, small uint8, p, q int64, seed uint64) {
		cs := fuzzCalls(b)
		burst := int(small)

		// Bucket.
		bk, err := NewBucket(rate, burst)
		iv := math.Round(1e9 / rate)
		valid := rate > 0 && !math.IsInf(rate, 1) && iv >= 1 && iv < 0x1p63 && burst >= 1 && int64(burst) <= math.MaxInt64/int64(iv)
		if (err == nil) != valid {
			t.Fatalf("NewBucket(%v, %d): %v", rate, burst, err)
		}
		if err == nil {
			ref := refBucket{T: int64(bk.Interval()), burst: int64(burst)}
			for i, c := range cs {
				if got, want := bk.AllowN(c.at, c.n), ref.allow(c.at, c.n); got != want {
					t.Fatalf("Bucket call %d: %v, want %v", i, got, want)
				}
				if c.op == 0 && burst <= 16 {
					if got, want := bk.Tokens(c.at), ref.tokens(c.at); got != want {
						t.Fatalf("Bucket call %d: Tokens %d, want %d", i, got, want)
					}
				}
			}
		}

		// Quota: limit from small, window p, origin offset q.
		w := time.Duration(p)
		bg, err := NewQuota(burst, w, t0.Add(time.Duration(q)))
		if (err == nil) != (burst >= 1 && w > 0) {
			t.Fatalf("NewQuota(%d, %v): %v", burst, w, err)
		}
		if err == nil {
			ref := refQuota{limit: burst, W: p, origin: t0.Add(time.Duration(q)), used: map[int64]int{}}
			for i, c := range cs {
				if got, want := bg.AllowN(c.at, c.n), ref.allow(c.at, c.n); got != want {
					t.Fatalf("Quota call %d: %v, want %v", i, got, want)
				}
			}
		}

		// Cooldown: period p, cap small mod 8.
		maxKeys := int(small%8) - 1
		cd, err := NewCooldown[int](w, maxKeys)
		if (err == nil) != (w > 0 && maxKeys >= 0) {
			t.Fatalf("NewCooldown(%v, %d): %v", w, maxKeys, err)
		}
		if err == nil {
			ref := refCooldown{period: w, max: maxKeys, last: map[int]time.Time{}}
			for i, c := range cs {
				got := cd.Allow(c.key, c.at)
				if want, _ := ref.allow(c.key, c.at); got != want {
					t.Fatalf("Cooldown call %d: %v, want %v", i, got, want)
				}
			}
		}

		// Breaker: threshold and probes from small, open duration q.
		th, probes := int(small&7)-1, int(small>>3&7)-1
		br, err := NewBreaker(th, time.Duration(q), probes)
		if (err == nil) != (th >= 1 && q >= 0 && probes >= 1) {
			t.Fatalf("NewBreaker(%d, %v, %d): %v", th, time.Duration(q), probes, err)
		}
		if err == nil {
			ref := refBreaker{threshold: th, openFor: time.Duration(q), probes: probes}
			for i, c := range cs {
				if c.op == 3 {
					br.Record(c.ok, c.at)
					ref.record(c.ok, c.at)
				} else if got, want := br.Allow(c.at), ref.allow(c.at); got != want {
					t.Fatalf("Breaker call %d: %v, want %v", i, got, want)
				}
				if br.state != ref.state || br.Transitions() != ref.changes {
					t.Fatalf("Breaker call %d: %v after %d, want %v after %d", i, br.state, br.Transitions(), ref.state, ref.changes)
				}
			}
		}

		// Backoff: base p, cap q, jitter from small.
		j := Jitter(small % 3)
		bo, err := NewBackoff(j, time.Duration(p), time.Duration(q), rand.NewPCG(seed, 1))
		if (err == nil) != (j <= DecorrelatedJitter && p > 0 && q >= p) {
			t.Fatalf("NewBackoff(%v, %v, %v): %v", j, p, q, err)
		}
		if err == nil {
			want := refBackoff(j, time.Duration(p), time.Duration(q), rand.NewPCG(seed, 1), 70)
			for n, w := range want {
				d := bo.Next()
				lo, hi := time.Duration(0), refCeiling(time.Duration(p), time.Duration(q), n)
				if j == DecorrelatedJitter {
					lo, hi = time.Duration(p), time.Duration(q)
				}
				if d != w || d < lo || d > hi {
					t.Fatalf("Backoff %v attempt %d: %v, want %v in [%v, %v]", j, n, d, w, lo, hi)
				}
			}
		}
	})
}
