package throttle

import (
	"math/rand/v2"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// ts is t0 plus s seconds.
func ts(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }

// call is one offered call: a time, a size or key, and a flag whose meaning
// depends on the type (Breaker: the result to record).
type call struct {
	at  time.Time
	n   int
	key int
	ok  bool
	op  int
}

// randomCalls draws n calls. Time steps range over −2 s to +5 s in steps of
// 250 ms, so duplicates and out-of-order times are common and boundaries
// are hit exactly; sizes range over −1 to 6 and keys over 0 to 7.
func randomCalls(r *rand.Rand, n int) []call {
	cs := make([]call, n)
	at := t0
	for i := range cs {
		at = at.Add(time.Duration(r.IntN(29)-8) * 250 * time.Millisecond)
		cs[i] = call{at: at, n: r.IntN(8) - 1, key: r.IntN(8), ok: r.IntN(3) != 0, op: r.IntN(4)}
	}
	return cs
}

// latest is the package's rule for times, written once, naively: the
// effective time of each call is the largest time offered so far.
type latest struct {
	t       time.Time
	started bool
	late    int64
}

func (l *latest) at(t time.Time) time.Time {
	if l.started && t.Before(l.t) {
		l.late++
		return l.t
	}
	l.t, l.started = t, true
	return t
}

// refBucket is the token bucket's definition, not its algorithm: it keeps
// every allowed request and allows a new one of n tokens at t only if no
// stretch of time from an allowed request to t would then hold more than
// burst + (t − s)/T tokens. GCRA is the greatest rule that never breaks that
// bound, so the two must agree on every decision. O(requests) per call.
type refBucket struct {
	T, burst int64
	clk      latest
	taken    []struct {
		t time.Time
		n int64
	}
}

func (r *refBucket) allow(at time.Time, n int) bool {
	now := r.clk.at(at)
	if n <= 0 {
		return true
	}
	if int64(n) > r.burst {
		return false
	}
	sum := int64(n)
	for i := len(r.taken) - 1; i >= 0; i-- {
		sum += r.taken[i].n
		if r.over(sum, now.Sub(r.taken[i].t)) {
			return false
		}
	}
	r.taken = append(r.taken, struct {
		t time.Time
		n int64
	}{now, int64(n)})
	return true
}

// over reports whether sum tokens in a stretch of length e exceed
// burst + e/T: (sum − burst)·T > e, written as a comparison with ⌊e/T⌋ so
// that no product can overflow.
func (r *refBucket) over(sum int64, e time.Duration) bool {
	return sum-r.burst > int64(e)/r.T
}

// tokens is the largest m ≤ burst that allow would accept now, found by
// asking: the definition of "tokens available".
func (r *refBucket) tokens(at time.Time) int {
	now := at
	if r.clk.started && at.Before(r.clk.t) {
		now = r.clk.t
	}
	best := 0
	for m := int64(1); m <= r.burst; m++ {
		sum, fits := m, true
		for i := len(r.taken) - 1; i >= 0; i-- {
			sum += r.taken[i].n
			if r.over(sum, now.Sub(r.taken[i].t)) {
				fits = false
				break
			}
		}
		if fits {
			best = int(m)
		}
	}
	return best
}

// refQuota counts per window in a map, the window found by a different
// floor than the package's.
type refQuota struct {
	limit  int
	W      int64
	origin time.Time
	clk    latest
	used   map[int64]int
}

func (r *refQuota) window(now time.Time) int64 {
	e := int64(now.Sub(r.origin))
	return (e - ((e%r.W)+r.W)%r.W) / r.W
}

func (r *refQuota) allow(at time.Time, n int) bool {
	now := r.clk.at(at)
	if n <= 0 {
		return true
	}
	k := r.window(now)
	if r.used[k]+n > r.limit {
		return false
	}
	r.used[k] += n
	return true
}

// refCooldown keeps every key's last allowed time forever and counts live
// keys by scanning them all.
type refCooldown struct {
	period time.Duration
	max    int
	clk    latest
	last   map[int]time.Time
}

func (r *refCooldown) live(now time.Time, k int) bool {
	s, ok := r.last[k]
	return ok && now.Sub(s) < r.period
}

func (r *refCooldown) allow(k int, at time.Time) (ok, full bool) {
	now := r.clk.at(at)
	if r.live(now, k) {
		return false, false
	}
	n := 0
	for key := range r.last {
		if r.live(now, key) {
			n++
		}
	}
	if r.max > 0 && n >= r.max {
		return false, true
	}
	r.last[k] = now
	return true, false
}

// refBreaker is the state machine of the Breaker doc, written as a table of
// what each event does in each state.
type refBreaker struct {
	threshold, probes int
	openFor           time.Duration
	clk               latest
	state             State
	entered           time.Time
	fails             int
	out, good, given  int
	changes           int64
}

func (r *refBreaker) enter(s State, now time.Time) {
	r.state, r.entered, r.changes = s, now, r.changes+1
	r.fails, r.out, r.good, r.given = 0, 0, 0, 0
}

func (r *refBreaker) tick(at time.Time) time.Time {
	now := r.clk.at(at)
	if r.state == Open && !now.Before(r.entered.Add(r.openFor)) {
		r.enter(HalfOpen, now)
	}
	return now
}

func (r *refBreaker) allow(at time.Time) bool {
	r.tick(at)
	if r.state == Closed {
		return true
	}
	if r.state == HalfOpen && r.given < r.probes {
		r.given++
		r.out++
		return true
	}
	return false
}

func (r *refBreaker) record(ok bool, at time.Time) {
	now := r.tick(at)
	switch {
	case r.state == Closed && ok:
		r.fails = 0
	case r.state == Closed:
		if r.fails++; r.fails == r.threshold {
			r.enter(Open, now)
		}
	case r.state == HalfOpen && r.out > 0 && !ok:
		r.enter(Open, now)
	case r.state == HalfOpen && r.out > 0:
		r.out--
		if r.good++; r.good == r.probes {
			r.enter(Closed, now)
		}
	}
}

func mustBucket(t testing.TB, rate float64, burst int) Bucket {
	t.Helper()
	b, err := NewBucket(rate, burst)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustQuota(t testing.TB, limit int, w time.Duration, origin time.Time) Quota {
	t.Helper()
	b, err := NewQuota(limit, w, origin)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustCooldown(t testing.TB, period time.Duration, maxKeys int) *Cooldown[int] {
	t.Helper()
	c, err := NewCooldown[int](period, maxKeys)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustBreaker(t testing.TB, threshold int, openFor time.Duration, probes int) Breaker {
	t.Helper()
	b, err := NewBreaker(threshold, openFor, probes)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustBackoff(t testing.TB, j Jitter, base, cap time.Duration, seed uint64) Backoff {
	t.Helper()
	b, err := NewBackoff(j, base, cap, rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Each type agrees with its naive reference on every decision, every count
// and every query, over random calls with late and duplicate times.
func TestBucketModel(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for trial := range 400 / raceScale {
		rate := []float64{0.25, 0.5, 1, 2, 3, 0.3}[trial%6]
		burst := 1 + r.IntN(5)
		b := mustBucket(t, rate, burst)
		ref := refBucket{T: int64(b.Interval()), burst: int64(burst)}
		var allowed, denied int64
		for i, c := range randomCalls(r, 200) {
			if c.op == 0 {
				if got, want := b.Tokens(c.at), ref.tokens(c.at); got != want {
					t.Fatalf("trial %d call %d: Tokens = %d, want %d", trial, i, got, want)
				}
				next := b.Next(c.at)
				probe := ref
				probe.taken = append(probe.taken[:0:0], ref.taken...)
				if !probe.allow(next, 1) {
					t.Fatalf("trial %d call %d: not allowed at Next = %v", trial, i, next.Sub(t0))
				}
				if now := ref.clk.now(c.at); next.Before(now) {
					t.Fatalf("trial %d call %d: Next %v before the effective time %v", trial, i, next.Sub(t0), now.Sub(t0))
				} else if next.After(now) {
					probe = ref
					probe.taken = append(probe.taken[:0:0], ref.taken...)
					if probe.allow(next.Add(-1), 1) {
						t.Fatalf("trial %d call %d: allowed 1 ns before Next", trial, i)
					}
				}
			}
			got, want := b.AllowN(c.at, c.n), ref.allow(c.at, c.n)
			if got != want {
				t.Fatalf("trial %d call %d (n=%d at %v): AllowN = %v, want %v", trial, i, c.n, c.at.Sub(t0), got, want)
			}
			if c.n > 0 && got {
				allowed++
			} else if c.n > 0 {
				denied++
			}
		}
		if b.Allowed() != allowed || b.Denied() != denied || b.Late() != ref.clk.late {
			t.Fatalf("trial %d: counts %d/%d/%d, want %d/%d/%d", trial, b.Allowed(), b.Denied(), b.Late(), allowed, denied, ref.clk.late)
		}
	}
}

func TestQuotaModel(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for trial := range 400 / raceScale {
		limit := 1 + r.IntN(6)
		w := time.Duration(1+r.IntN(8)) * 500 * time.Millisecond
		origin := t0.Add(time.Duration(r.IntN(4001)-2000) * time.Millisecond)
		b := mustQuota(t, limit, w, origin)
		ref := refQuota{limit: limit, W: int64(w), origin: origin, used: map[int64]int{}}
		for i, c := range randomCalls(r, 200) {
			if c.op == 0 {
				now := c.at
				if ref.clk.started && now.Before(ref.clk.t) {
					now = ref.clk.t
				}
				k := ref.window(now)
				if got, want := b.Remaining(c.at), limit-ref.used[k]; got != want {
					t.Fatalf("trial %d call %d: Remaining = %d, want %d", trial, i, got, want)
				}
				end := origin.Add(time.Duration(k+1) * w)
				if got := b.WindowEnd(c.at); !got.Equal(end) {
					t.Fatalf("trial %d call %d: WindowEnd = %v, want %v", trial, i, got.Sub(t0), end.Sub(t0))
				}
			}
			if got, want := b.AllowN(c.at, c.n), ref.allow(c.at, c.n); got != want {
				t.Fatalf("trial %d call %d: AllowN(%d) = %v, want %v", trial, i, c.n, got, want)
			}
		}
		if b.Late() != ref.clk.late {
			t.Fatalf("trial %d: Late = %d, want %d", trial, b.Late(), ref.clk.late)
		}
	}
}

func TestCooldownModel(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for trial := range 400 / raceScale {
		period := time.Duration(1+r.IntN(12)) * 250 * time.Millisecond
		maxKeys := r.IntN(6)
		cd := mustCooldown(t, period, maxKeys)
		ref := refCooldown{period: period, max: maxKeys, last: map[int]time.Time{}}
		var full int64
		for i, c := range randomCalls(r, 300) {
			switch c.op {
			case 0:
				// Next is a time the reference would allow the key, and
				// one nanosecond earlier it would not, unless Next is now.
				next := cd.Next(c.key, c.at)
				if ok, _ := ref.dry(c.key, next); !ok {
					t.Fatalf("trial %d call %d: key %d refused at Next = %v", trial, i, c.key, next.Sub(t0))
				}
				if now := ref.clk.now(c.at); next.After(now) {
					if ok, _ := ref.dry(c.key, next.Add(-1)); ok {
						t.Fatalf("trial %d call %d: key %d allowed before Next", trial, i, c.key)
					}
				}
			case 1:
				cd.Sweep(c.at)
				ref.clk.at(c.at)
				continue
			}
			got, gotFull := cd.Allow(c.key, c.at), cd.Full()
			want, wantFull := ref.allow(c.key, c.at)
			if wantFull {
				full++
			}
			if got != want || gotFull != full {
				t.Fatalf("trial %d call %d (key %d at %v): Allow = %v full %d, want %v full %d", trial, i, c.key, c.at.Sub(t0), got, gotFull, want, full)
			}
			if maxKeys > 0 && cd.Len() > maxKeys {
				t.Fatalf("trial %d: Len = %d over the cap %d", trial, cd.Len(), maxKeys)
			}
		}
		if cd.Late() != ref.clk.late {
			t.Fatalf("trial %d: Late = %d, want %d", trial, cd.Late(), ref.clk.late)
		}
	}
}

// now is the effective time of a call at t, without recording it.
func (l *latest) now(t time.Time) time.Time {
	if l.started && t.Before(l.t) {
		return l.t
	}
	return t
}

// dry asks the reference whether k would be allowed at t, changing nothing.
func (r *refCooldown) dry(k int, t time.Time) (ok, full bool) {
	saved := make(map[int]time.Time, len(r.last))
	for key, v := range r.last {
		saved[key] = v
	}
	clk := r.clk
	ok, full = r.allow(k, t)
	r.last, r.clk = saved, clk
	return ok, full
}

func TestBreakerModel(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for trial := range 400 / raceScale {
		threshold := 1 + r.IntN(4)
		openFor := time.Duration(r.IntN(12)) * 250 * time.Millisecond
		probes := 1 + r.IntN(3)
		b := mustBreaker(t, threshold, openFor, probes)
		ref := refBreaker{threshold: threshold, openFor: openFor, probes: probes}
		for i, c := range randomCalls(r, 300) {
			switch c.op {
			case 0, 1:
				if got, want := b.Allow(c.at), ref.allow(c.at); got != want {
					t.Fatalf("trial %d call %d: Allow = %v, want %v (state %v)", trial, i, got, want, ref.state)
				}
			case 2:
				b.Record(c.ok, c.at)
				ref.record(c.ok, c.at)
			default:
				ref.tick(c.at)
				if got := b.State(c.at); got != ref.state {
					t.Fatalf("trial %d call %d: State = %v, want %v", trial, i, got, ref.state)
				}
			}
			if b.state != ref.state || b.Transitions() != ref.changes || !b.Since().Equal(ref.entered) || b.Failures() != ref.fails {
				t.Fatalf("trial %d call %d: state %v/%d since %v fails %d, want %v/%d since %v fails %d",
					trial, i, b.state, b.Transitions(), b.Since().Sub(t0), b.Failures(), ref.state, ref.changes, ref.entered.Sub(t0), ref.fails)
			}
		}
		if b.Late() != ref.clk.late {
			t.Fatalf("trial %d: Late = %d, want %d", trial, b.Late(), ref.clk.late)
		}
	}
}
