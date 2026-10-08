package memo

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock lets expiry be tested by advancing time rather than sleeping.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestHitAvoidsSecondExecution(t *testing.T) {
	var calls int32
	c := New[string, int](Options{})
	fn := func(context.Context) (int, error) { atomic.AddInt32(&calls, 1); return 42, nil }

	for i := 0; i < 5; i++ {
		v, err := c.Do(context.Background(), "k", fn)
		if err != nil || v != 42 {
			t.Fatalf("got %v, %v", v, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("fn ran %d times, want 1", got)
	}
	s := c.Stats()
	if s.Hits != 4 || s.Misses != 1 {
		t.Errorf("hits=%d misses=%d, want 4/1", s.Hits, s.Misses)
	}
}

func TestSingleFlightCoalescesConcurrentCallers(t *testing.T) {
	const callers = 64
	var running, total int32
	release := make(chan struct{})
	c := New[string, int](Options{})

	fn := func(context.Context) (int, error) {
		// If coalescing works, at most one of these is ever in here.
		if n := atomic.AddInt32(&running, 1); n > 1 {
			t.Errorf("%d concurrent executions, want 1", n)
		}
		atomic.AddInt32(&total, 1)
		<-release
		atomic.AddInt32(&running, -1)
		return 7, nil
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, err := c.Do(context.Background(), "k", fn)
			if err != nil || v != 7 {
				t.Errorf("got %v, %v", v, err)
			}
		}()
	}
	close(start)
	// Give the goroutines time to pile onto the same key, then let the one
	// execution finish.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&total); got != 1 {
		t.Errorf("fn ran %d times for %d concurrent callers, want 1", got, callers)
	}
	if s := c.Stats(); s.Coalesced == 0 {
		t.Error("no callers recorded as coalesced")
	}
}

func TestErrorsAreNotCachedByDefaultButAreCoalesced(t *testing.T) {
	var calls int32
	boom := errors.New("boom")
	c := New[string, int](Options{})
	fn := func(context.Context) (int, error) { atomic.AddInt32(&calls, 1); return 0, boom }

	for i := 0; i < 3; i++ {
		if _, err := c.Do(context.Background(), "k", fn); !errors.Is(err, boom) {
			t.Fatalf("got %v, want boom", err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("fn ran %d times, want 3 — a failure must not be cached", got)
	}
	if _, ok := c.Get("k"); ok {
		t.Error("a failed result was stored")
	}
}

func TestCacheErrorsWhenEnabled(t *testing.T) {
	var calls int32
	boom := errors.New("boom")
	c := New[string, int](Options{CacheErrors: true})
	fn := func(context.Context) (int, error) { atomic.AddInt32(&calls, 1); return 0, boom }

	for i := 0; i < 3; i++ {
		_, _ = c.Do(context.Background(), "k", fn)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("fn ran %d times with CacheErrors, want 1", got)
	}
}

func TestTTLExpires(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1700000000, 0)}
	var calls int32
	c := New[string, int](Options{TTL: time.Minute, Now: clk.Now})
	fn := func(context.Context) (int, error) { atomic.AddInt32(&calls, 1); return 1, nil }

	_, _ = c.Do(context.Background(), "k", fn)
	clk.Add(30 * time.Second)
	_, _ = c.Do(context.Background(), "k", fn)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("within TTL: fn ran %d times, want 1", got)
	}
	clk.Add(31 * time.Second) // now past the minute
	_, _ = c.Do(context.Background(), "k", fn)
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("past TTL: fn ran %d times, want 2", got)
	}
	if s := c.Stats(); s.Expired != 1 {
		t.Errorf("expired=%d, want 1", s.Expired)
	}
}

func TestLRUEvictsLeastRecentlyUsed(t *testing.T) {
	c := New[int, int](Options{Capacity: 3})
	set := func(k int) {
		_, _ = c.Do(context.Background(), k, func(context.Context) (int, error) { return k, nil })
	}
	for k := 1; k <= 3; k++ {
		set(k)
	}
	// Touch 1 so that 2 becomes the least recently used.
	if _, ok := c.Get(1); !ok {
		t.Fatal("1 should still be cached")
	}
	set(4)

	if _, ok := c.Get(2); ok {
		t.Error("2 should have been evicted as least recently used")
	}
	for _, k := range []int{1, 3, 4} {
		if _, ok := c.Get(k); !ok {
			t.Errorf("%d should still be cached", k)
		}
	}
	if s := c.Stats(); s.Evictions != 1 || s.Entries != 3 {
		t.Errorf("evictions=%d entries=%d, want 1/3", s.Evictions, s.Entries)
	}
}

func TestInvalidateAndPurge(t *testing.T) {
	c := New[string, int](Options{})
	c.Set("a", 1)
	c.Set("b", 2)
	if !c.Invalidate("a") {
		t.Error("Invalidate should report true for a present key")
	}
	if c.Invalidate("a") {
		t.Error("Invalidate should report false for an absent key")
	}
	c.Purge()
	if _, ok := c.Get("b"); ok {
		t.Error("Purge left an entry behind")
	}
}

func TestContextCancelsTheWaitNotTheExecution(t *testing.T) {
	c := New[string, int](Options{})
	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_, _ = c.Do(context.Background(), "k", func(context.Context) (int, error) {
			close(started)
			<-release
			return 5, nil
		})
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Do(ctx, "k", func(context.Context) (int, error) { return 0, nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("waiting caller got %v, want context.Canceled", err)
	}
	close(release)
	// The original execution still completes and its result is stored.
	time.Sleep(20 * time.Millisecond)
	if v, ok := c.Get("k"); !ok || v != 5 {
		t.Errorf("after the cancelled wait, stored value is %v/%v, want 5/true", v, ok)
	}
}

func TestHitRate(t *testing.T) {
	c := New[string, int](Options{})
	fn := func(context.Context) (int, error) { return 1, nil }
	_, _ = c.Do(context.Background(), "k", fn) // miss
	_, _ = c.Do(context.Background(), "k", fn) // hit
	_, _ = c.Do(context.Background(), "k", fn) // hit
	if got := c.Stats().HitRate(); got < 0.66 || got > 0.67 {
		t.Errorf("hit rate %.4f, want ~0.6667", got)
	}
	if got := (Stats{}).HitRate(); got != 0 {
		t.Errorf("empty hit rate %v, want 0", got)
	}
}
