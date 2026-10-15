package throttle

import (
	"math/rand/v2"
	"testing"
	"time"
)

// The package doc promises no allocation on the hot paths; this is the test
// of it. Each closure moves time forward so the call is not optimised to
// nothing, and each crosses between its outcomes — allowed and refused,
// closed, open and half-open — so every path runs, not only the steady one.
func TestZeroAllocs(t *testing.T) {
	bk := mustBucket(t, 2, 3)
	bg := mustQuota(t, 3, 2*time.Second, t0)
	br := mustBreaker(t, 2, 2*time.Second, 2)
	full := mustBackoff(t, FullJitter, time.Millisecond, time.Second, 1)
	dec, _ := NewBackoff(DecorrelatedJitter, time.Millisecond, time.Second, rand.New(rand.NewPCG(1, 2)))
	// Eight keys, each allowed then refused; with a 2 s period and a key a
	// quarter-second apart, each comes round again just as its period ends,
	// so the table keeps expiring and re-admitting keys at a fixed size.
	cd := mustCooldown(t, 2*time.Second, 8)
	i, at := 0, t0
	var ok bool
	var n int
	var when time.Time
	var d time.Duration
	next := func() { i++; at = at.Add(250 * time.Millisecond) }
	for _, c := range []struct {
		name string
		f    func()
	}{
		{"Bucket.Allow", func() { next(); ok = bk.Allow(at); ok = bk.Allow(at) }},
		{"Bucket.AllowN", func() { next(); ok = bk.AllowN(at, i%4) }},
		{"Quota.Allow", func() { next(); ok = bg.Allow(at) }},
		{"Quota.AllowN", func() { next(); ok = bg.AllowN(at, i%3) }},
		{"Breaker.Allow+Record", func() { next(); ok = br.Allow(at); br.Record(i%5 < 2, at) }},
		{"Backoff.Next/full", func() {
			d = full.Next()
			if i++; i%20 == 0 {
				full.Reset()
			}
		}},
		{"Backoff.Next/decorrelated", func() { d = dec.Next() }},
		{"Cooldown.Allow", func() { next(); ok = cd.Allow(i%8, at); ok = cd.Allow(i%8, at) }},
		{"Cooldown.Sweep", func() { next(); n = cd.Sweep(at) }},
		{"queries", func() {
			next()
			n = bk.Tokens(at) + bg.Remaining(at)
			when = bk.Next(at)
			when, _ = bk.NextN(at, 2)
			when = bg.WindowEnd(at)
			when = cd.Next(i%8, at)
			_ = br.State(at)
		}},
	} {
		if got := testing.AllocsPerRun(1000, c.f); got != 0 {
			t.Errorf("%s: %v allocations per call, want 0", c.name, got)
		}
	}
	_, _, _, _ = ok, n, when, d
	if bk.Denied() == 0 || bk.Allowed() == 0 || bg.Denied() == 0 || br.Transitions() < 3 || cd.Allowed() < 1000 || cd.Denied() < 1000 {
		t.Fatal("a closure never changed its outcome; the test misses a path")
	}
}

// A check that cannot fail is worth nothing: the same measurement does see
// an allocation when there is one. NewCooldown makes a table, and it is
// caught.
func TestZeroAllocsCanFail(t *testing.T) {
	var sink *Cooldown[int]
	if n := testing.AllocsPerRun(100, func() { sink, _ = NewCooldown[int](time.Second, 0) }); n == 0 {
		t.Fatal("AllocsPerRun reported 0 for NewCooldown, which allocates")
	}
	_ = sink
}

func BenchmarkBucketAllow(b *testing.B) {
	b.ReportAllocs()
	bk := mustBucket(b, 1000, 10)
	at := t0
	for range b.N {
		at = at.Add(time.Millisecond)
		bk.Allow(at)
	}
}

func BenchmarkQuotaAllow(b *testing.B) {
	b.ReportAllocs()
	bg := mustQuota(b, 100, time.Second, t0)
	at := t0
	for range b.N {
		at = at.Add(time.Millisecond)
		bg.Allow(at)
	}
}

func BenchmarkCooldownAllow(b *testing.B) {
	b.ReportAllocs()
	cd := mustCooldown(b, time.Second, 0)
	at := t0
	for i := range b.N {
		at = at.Add(time.Millisecond)
		cd.Allow(i%1024, at)
	}
}

func BenchmarkBackoffNext(b *testing.B) {
	b.ReportAllocs()
	bo := mustBackoff(b, DecorrelatedJitter, time.Millisecond, time.Minute, 1)
	for range b.N {
		bo.Next()
	}
}

func BenchmarkBreaker(b *testing.B) {
	b.ReportAllocs()
	br := mustBreaker(b, 3, time.Second, 1)
	at := t0
	for i := range b.N {
		at = at.Add(100 * time.Millisecond)
		if br.Allow(at) {
			br.Record(i%4 != 0, at)
		}
	}
}
