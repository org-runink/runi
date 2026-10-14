package throttle

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestNewBucketInvalid(t *testing.T) {
	for _, c := range []struct {
		rate  float64
		burst int
	}{
		{0, 1}, {-1, 1}, {math.NaN(), 1}, {math.Inf(1), 1}, {math.Inf(-1), 1},
		{3e9, 1},   // 1/rate rounds to 0 ns
		{1e-11, 1}, // 1/rate is 1e20 ns, past 2^63
		{1, 0}, {1, -1},
		{1e-9, 10}, // 10 tokens of 1e18 ns each is past 2^63 ns
	} {
		if _, err := NewBucket(c.rate, c.burst); !errors.Is(err, ErrInvalid) {
			t.Errorf("NewBucket(%v, %d) error = %v, want ErrInvalid", c.rate, c.burst, err)
		}
	}
	for _, c := range []struct {
		rate  float64
		burst int
	}{{2e9, 1}, {1, 1}, {1e-9, 9}, {1.1e-10, 1}} {
		if _, err := NewBucket(c.rate, c.burst); err != nil {
			t.Errorf("NewBucket(%v, %d): %v", c.rate, c.burst, err)
		}
	}
}

// One token a second, three at once: a burst of three, then one a second.
func TestBucketScript(t *testing.T) {
	b := mustBucket(t, 1, 3)
	if b.Interval() != time.Second || b.Rate() != 1 || b.Burst() != 3 {
		t.Fatalf("Interval %v Rate %v Burst %d", b.Interval(), b.Rate(), b.Burst())
	}
	if got := b.Tokens(ts(0)); got != 3 {
		t.Fatalf("a new bucket holds %d tokens, want 3", got)
	}
	for i := range 3 {
		if !b.Allow(ts(0)) {
			t.Fatalf("token %d of the burst refused", i)
		}
	}
	if b.Allow(ts(0)) || b.Tokens(ts(0)) != 0 {
		t.Fatal("a fourth token at once")
	}
	if got := b.Next(ts(0)); !got.Equal(ts(1)) {
		t.Fatalf("Next = %v, want 1 s", got.Sub(t0))
	}
	if b.Allow(ts(1).Add(-1)) {
		t.Fatal("a token 1 ns early")
	}
	if !b.Allow(ts(1)) || b.Allow(ts(1)) {
		t.Fatal("not exactly one token after a second")
	}
	if got, ok := b.NextN(ts(1), 3); !ok || !got.Equal(ts(4)) {
		t.Fatalf("NextN(3) = %v %v, want 4 s", got.Sub(t0), ok)
	}
	if !b.AllowN(ts(2.5), 0) || !b.AllowN(ts(2.5), -4) {
		t.Fatal("AllowN of nothing refused")
	}
	if got, ok := b.NextN(ts(2.5), 0); !ok || !got.Equal(ts(2.5)) {
		t.Fatalf("NextN(0) = %v %v, want now", got.Sub(t0), ok)
	}
	if b.AllowN(ts(100), 4) {
		t.Fatal("AllowN above the burst allowed")
	}
	if got, ok := b.NextN(ts(100), 4); ok || !got.IsZero() {
		t.Fatalf("NextN above the burst = %v %v, want zero false", got, ok)
	}
	if got, ok := b.NextN(ts(100), 3); !ok || !got.Equal(ts(100)) {
		t.Fatalf("NextN(3) on a full bucket = %v %v, want now", got.Sub(t0), ok)
	}
	if !b.AllowN(ts(100), 3) || b.Tokens(ts(100)) != 0 {
		t.Fatal("a refilled bucket does not give its burst")
	}
	if b.Allowed() != 5 || b.Denied() != 4 || b.Late() != 0 {
		t.Fatalf("Allowed %d Denied %d Late %d, want 5 4 0", b.Allowed(), b.Denied(), b.Late())
	}
	b.Reset()
	if b.Allowed() != 0 || b.Denied() != 0 || b.Tokens(ts(0)) != 3 || b.Burst() != 3 || b.Interval() != time.Second {
		t.Fatal("Reset did not refill and keep the parameters")
	}
	if !b.AllowN(ts(-50), 3) {
		t.Fatal("after Reset an earlier time is not late")
	}
}

// The interval is 1/rate rounded once to the nearest nanosecond; Rate is the
// rate that interval keeps.
func TestBucketInterval(t *testing.T) {
	for _, c := range []struct {
		rate float64
		want time.Duration
	}{
		{3, 333333333}, {1.5, 666666667}, {2e9, 1}, {1.0 / 3, 3 * time.Second}, {1e-9, 1e18},
	} {
		b := mustBucket(t, c.rate, 1)
		if b.Interval() != c.want {
			t.Errorf("rate %v: Interval %d, want %d", c.rate, b.Interval(), c.want)
		}
		if got := b.Rate(); got != 1e9/float64(c.want) {
			t.Errorf("rate %v: Rate %v", c.rate, got)
		}
	}
}

// A bucket drained daily for ten years has given exactly its burst plus one
// token per interval elapsed: integer time debt does not drift. Each day
// earns fewer tokens than the burst, so none is lost to a full bucket.
func TestBucketNoDrift(t *testing.T) {
	b := mustBucket(t, 3, 1_000_000)
	day := 24*time.Hour + time.Nanosecond // an awkward step on purpose
	var total int64
	for d := range 3653 {
		at := t0.Add(time.Duration(d) * day)
		n := b.Tokens(at)
		if !b.AllowN(at, n) || b.Allow(at) {
			t.Fatalf("day %d: Tokens %d is not exactly what AllowN gives", d, n)
		}
		total += int64(n)
		want := 1_000_000 + int64(time.Duration(d)*day)/int64(b.Interval())
		if total != want {
			t.Fatalf("day %d: %d tokens taken, want %d", d, total, want)
		}
	}
}

// The zero Bucket refuses everything and says so.
func TestBucketZero(t *testing.T) {
	var b Bucket
	if b.Allow(ts(0)) || b.Tokens(ts(0)) != 0 {
		t.Fatal("the zero Bucket gave a token")
	}
	if got, ok := b.NextN(ts(0), 1); ok || !got.IsZero() {
		t.Fatal("the zero Bucket promised a token")
	}
	if !b.Next(ts(0)).IsZero() {
		t.Fatal("Next on the zero Bucket is not the zero time")
	}
}

// Large times saturate rather than overflow: a bucket used across centuries
// is full again, not in debt.
func TestBucketExtremeTimes(t *testing.T) {
	b := mustBucket(t, 1e-9, 9) // one token per ~31.7 years, the largest burst allowed
	far := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	if !b.AllowN(far, 9) || b.Allow(far) {
		t.Fatal("burst at year 1")
	}
	end := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if b.Tokens(end) != 9 || !b.AllowN(end, 9) {
		t.Fatal("not full after 9998 years")
	}
	if got := b.Next(end); !got.Equal(end.Add(time.Duration(1e18))) {
		t.Fatalf("Next = %v", got)
	}
}
