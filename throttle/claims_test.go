package throttle

import (
	"math/rand/v2"
	"testing"
	"time"
)

// Bucket's doc: over any stretch of time of length t, at most
// burst + ⌊t·rate⌋ tokens are taken. A stretch that holds no allowed request
// at either end can be shrunk to one that does without losing tokens, so
// checking every pair of allowed requests checks every stretch.
func TestBucketEnvelope(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	for trial := range 300 / raceScale {
		b := mustBucket(t, []float64{0.5, 1, 3, 7}[trial%4], 1+r.IntN(6))
		T := int64(b.Interval())
		type took struct {
			at time.Time
			n  int64
		}
		var log []took
		for _, c := range randomCalls(r, 300) {
			if b.AllowN(c.at, c.n) && c.n > 0 {
				log = append(log, took{b.c.last, int64(c.n)})
			}
		}
		for i := range log {
			sum := int64(0)
			for j := i; j < len(log); j++ {
				sum += log[j].n
				if bound := int64(b.Burst()) + int64(log[j].at.Sub(log[i].at))/T; sum > bound {
					t.Fatalf("trial %d: %d tokens in [%v, %v], bound %d", trial, sum, log[i].at.Sub(t0), log[j].at.Sub(t0), bound)
				}
			}
		}
		if len(log) == 0 {
			t.Fatalf("trial %d allowed nothing", trial)
		}
	}
}

// The bound is tight: a bucket drained at once and then asked every
// nanosecond-exact interval gives burst + ⌊t/T⌋ in [0, t].
func TestBucketEnvelopeTight(t *testing.T) {
	b := mustBucket(t, 3, 4)
	n := 0
	for i := range 31 {
		at := t0.Add(time.Duration(i) * b.Interval())
		for b.Allow(at) {
			n++
		}
	}
	if n != 4+30 {
		t.Fatalf("%d tokens in 30 intervals, want 34", n)
	}
}

// Quota's doc: at most the limit in each window, and up to twice the limit
// over a stretch of one window's length that straddles a boundary.
func TestQuotaPerWindow(t *testing.T) {
	r := rand.New(rand.NewPCG(13, 14))
	for trial := range 300 / raceScale {
		limit := 1 + r.IntN(5)
		w := time.Duration(1+r.IntN(6)) * 750 * time.Millisecond
		origin := t0.Add(time.Duration(r.IntN(3000)) * time.Millisecond)
		b := mustQuota(t, limit, w, origin)
		used := map[int64]int{}
		for _, c := range randomCalls(r, 300) {
			if b.AllowN(c.at, c.n) && c.n > 0 {
				k := b.index(b.c.last)
				if used[k] += c.n; used[k] > limit {
					t.Fatalf("trial %d: %d actions in window %d, limit %d", trial, used[k], k, limit)
				}
			}
		}
	}
	b := mustQuota(t, 3, time.Minute, t0)
	if !b.AllowN(ts(59.999), 3) || !b.AllowN(ts(60), 3) {
		t.Fatal("the boundary: 2·limit within 1 ms")
	}
}

// Cooldown's doc: the same decisions as a Bucket of burst 1 and one token
// per period kept for each key.
func TestCooldownIsBucketOfOne(t *testing.T) {
	r := rand.New(rand.NewPCG(15, 16))
	for trial := range 200 / raceScale {
		period := time.Duration(1+r.IntN(8)) * 500 * time.Millisecond
		cd := mustCooldown(t, period, 0)
		buckets := map[int]*Bucket{}
		clk := latest{}
		for i, c := range randomCalls(r, 300) {
			now := clk.at(c.at)
			b := buckets[c.key]
			if b == nil {
				nb := mustBucket(t, float64(time.Second)/float64(period), 1)
				b = &nb
				buckets[c.key] = b
			}
			if got, want := cd.Allow(c.key, c.at), b.Allow(now); got != want {
				t.Fatalf("trial %d call %d: Cooldown %v, Bucket %v", trial, i, got, want)
			}
		}
	}
}

// The rule every timed type shares: a time before the latest is the latest.
// A twin given max(time so far) in place of each time must agree on every
// answer and every state; only Late differs.
func TestLateIsLatest(t *testing.T) {
	r := rand.New(rand.NewPCG(17, 18))
	for trial := range 100 / raceScale {
		cs := randomCalls(r, 300)
		clk := latest{}
		mono := make([]time.Time, len(cs))
		for i, c := range cs {
			mono[i] = clk.at(c.at)
		}
		if clk.late == 0 {
			t.Fatal("no late calls drawn")
		}

		b1, b2 := mustBucket(t, 2, 3), mustBucket(t, 2, 3)
		g1, g2 := mustQuota(t, 2, 3*time.Second, t0), mustQuota(t, 2, 3*time.Second, t0)
		c1, c2 := mustCooldown(t, 2*time.Second, 3), mustCooldown(t, 2*time.Second, 3)
		k1, k2 := mustBreaker(t, 2, 2*time.Second, 1), mustBreaker(t, 2, 2*time.Second, 1)
		for i, c := range cs {
			m := mono[i]
			same := b1.Tokens(c.at) == b2.Tokens(m) && b1.Next(c.at).Equal(b2.Next(m)) &&
				g1.Remaining(c.at) == g2.Remaining(m) && g1.WindowEnd(c.at).Equal(g2.WindowEnd(m)) &&
				c1.Next(c.key, c.at).Equal(c2.Next(c.key, m)) &&
				b1.AllowN(c.at, c.n) == b2.AllowN(m, c.n) &&
				g1.AllowN(c.at, c.n) == g2.AllowN(m, c.n) &&
				c1.Allow(c.key, c.at) == c2.Allow(c.key, m)
			if c.op < 2 {
				same = same && k1.Allow(c.at) == k2.Allow(m)
			} else {
				k1.Record(c.ok, c.at)
				k2.Record(c.ok, m)
			}
			if !same || k1.State(c.at) != k2.State(m) {
				t.Fatalf("trial %d call %d: a late time was not treated as the latest", trial, i)
			}
		}
		if b1.Late() != clk.late || g1.Late() != clk.late || c1.Late() != clk.late || k1.Late() != 2*clk.late {
			t.Fatalf("trial %d: Late %d %d %d %d, want %d (Breaker: twice, for State)", trial, b1.Late(), g1.Late(), c1.Late(), k1.Late(), clk.late)
		}
		b1.c.late, g1.c.late, k1.c.late = 0, 0, 0
		if b1 != b2 || g1 != g2 || k1 != k2 || c1.Allowed() != c2.Allowed() || c1.Len() != c2.Len() {
			t.Fatalf("trial %d: states differ", trial)
		}
	}
}

// The query methods change nothing: the value is identical before and after.
func TestQueriesChangeNothing(t *testing.T) {
	r := rand.New(rand.NewPCG(19, 20))
	b := mustBucket(t, 1, 3)
	g := mustQuota(t, 2, time.Second, t0)
	cd := mustCooldown(t, time.Second, 2)
	for _, c := range randomCalls(r, 500) {
		b.AllowN(c.at, c.n)
		g.AllowN(c.at, c.n)
		cd.Allow(c.key, c.at)
		q := c.at.Add(time.Duration(r.IntN(9)-4) * time.Second)
		bb, gg, n, last, ll := b, g, cd.Len(), cd.c, cd.allowed+cd.denied
		b.Tokens(q)
		b.Next(q)
		b.NextN(q, c.n)
		g.Remaining(q)
		g.WindowEnd(q)
		cd.Next(c.key, q)
		if b != bb || g != gg || cd.Len() != n || cd.c != last || cd.allowed+cd.denied != ll {
			t.Fatal("a query changed its value")
		}
	}
}
