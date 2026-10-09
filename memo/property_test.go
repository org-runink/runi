package memo

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memo's two promises are that the cache stays within the bound it was given
// and that concurrent callers on one key run the function once. Both are
// claims about every schedule and every access pattern, which is exactly what
// an example test cannot reach.

// The store never holds more than Capacity entries, under any sequence of
// reads, writes and evictions.
func TestPropertyCapacityIsNeverExceeded(t *testing.T) {
	r := rand.New(rand.NewPCG(121, 122))
	for i := 0; i < 500; i++ {
		cap := 1 + r.IntN(20)
		s := New[int, int](Options{Capacity: cap})
		ctx := context.Background()
		for op := 0; op < 200; op++ {
			k := r.IntN(cap * 3)
			switch r.IntN(4) {
			case 0:
				s.Set(k, k)
			case 1:
				_, _ = s.Do(ctx, k, func(context.Context) (int, error) { return k, nil })
			case 2:
				s.Get(k)
			default:
				s.Invalidate(k)
			}
			if got := s.Stats().Entries; got > cap {
				t.Fatalf("capacity %d, holding %d entries", cap, got)
			}
		}
	}
}

// Whatever was stored last for a key is what comes back, until it is evicted
// or invalidated. A cache that returned a stale value for a key just written
// would be worse than no cache.
func TestPropertyTheLastWriteWins(t *testing.T) {
	r := rand.New(rand.NewPCG(123, 124))
	for i := 0; i < 2000; i++ {
		s := New[int, int](Options{}) // unbounded: nothing is evicted
		want := map[int]int{}
		for op := 0; op < 60; op++ {
			k, v := r.IntN(10), r.IntN(1000)
			s.Set(k, v)
			want[k] = v
		}
		for k, v := range want {
			got, ok := s.Get(k)
			if !ok || got != v {
				t.Fatalf("key %d: got %v (%v), want %v", k, got, ok, v)
			}
		}
	}
}

// joinAll releases `callers` goroutines onto one cold key and holds the
// in-flight execution open until every other caller has been recorded as
// joining it, then lets it finish.
//
// It waits on the Coalesced counter rather than sleeping. A sleep would make
// the property a statement about the scheduler instead of about the store: on
// a loaded machine a late caller arrives after the first has already returned,
// legitimately starts a second execution, and the test fails for something
// that is not a defect. Waiting for the joins to be counted makes the overlap
// a fact rather than a hope.
func joinAll[V any](t *testing.T, s *Store[string, V], callers int,
	fn func(context.Context) (V, error)) ([]V, []error) {
	t.Helper()

	gate := make(chan struct{})
	var done sync.WaitGroup
	done.Add(callers)
	vals := make([]V, callers)
	errs := make([]error, callers)

	for c := 0; c < callers; c++ {
		go func(c int) {
			defer done.Done()
			vals[c], errs[c] = s.Do(context.Background(), "cold", func(ctx context.Context) (V, error) {
				v, err := fn(ctx)
				<-gate // hold the flight open
				return v, err
			})
		}(c)
	}

	deadline := time.Now().Add(30 * time.Second)
	for s.Stats().Coalesced < uint64(callers-1) {
		if time.Now().After(deadline) {
			close(gate)
			done.Wait()
			t.Fatalf("only %d of %d callers joined the flight", s.Stats().Coalesced+1, callers)
		}
		runtime.Gosched()
	}
	close(gate)
	done.Wait()
	return vals, errs
}

// Single flight: however many callers arrive on a cold key at once, the
// function runs ONCE and every caller gets that one answer. This is the
// property the package exists for, and the only honest way to state it is
// over many callers and many repetitions.
func TestPropertyConcurrentCallersRunTheFunctionOnce(t *testing.T) {
	r := rand.New(rand.NewPCG(125, 126))
	for i := 0; i < 200; i++ {
		callers := 2 + r.IntN(64)
		s := New[string, int](Options{Capacity: 64})
		var ran int64

		got, errs := joinAll(t, s, callers, func(context.Context) (int, error) {
			atomic.AddInt64(&ran, 1)
			return 42, nil
		})

		if n := atomic.LoadInt64(&ran); n != 1 {
			t.Fatalf("%d callers ran the function %d times", callers, n)
		}
		for c := range got {
			if errs[c] != nil || got[c] != 42 {
				t.Fatalf("caller %d got %v, %v", c, got[c], errs[c])
			}
		}
	}
}

// An error is not cached by default, so a later caller gets a fresh attempt --
// but concurrent callers still share the one in-flight execution. Both halves
// matter: caching a transient failure turns a blip into an outage, and not
// coalescing turns one into a stampede.
func TestPropertyErrorsAreCoalescedButNotCached(t *testing.T) {
	r := rand.New(rand.NewPCG(127, 128))
	for i := 0; i < 200; i++ {
		callers := 2 + r.IntN(32)
		s := New[string, int](Options{Capacity: 8})
		var ran int64
		boom := errors.New("boom")

		_, errs := joinAll(t, s, callers, func(context.Context) (int, error) {
			atomic.AddInt64(&ran, 1)
			return 0, boom
		})
		if n := atomic.LoadInt64(&ran); n != 1 {
			t.Fatalf("%d callers ran the failing function %d times", callers, n)
		}
		for c := range errs {
			if !errors.Is(errs[c], boom) {
				t.Fatalf("caller %d got %v, want the shared error", c, errs[c])
			}
		}
		if _, ok := s.Get("cold"); ok {
			t.Fatal("the error was cached")
		}
		// A later caller gets a real attempt.
		v, err := s.Do(context.Background(), "cold", func(context.Context) (int, error) { return 7, nil })
		if err != nil || v != 7 {
			t.Fatalf("after a failure: %v, %v", v, err)
		}
	}
}

// Hash must be a function: the same value always hashes the same, whatever
// order the maps inside it iterate in. A cache key that varied between calls
// would silently never hit.
func TestPropertyHashIsStableForEqualValues(t *testing.T) {
	r := rand.New(rand.NewPCG(129, 130))
	for i := 0; i < 2000; i++ {
		m := map[string]string{}
		for j := 0; j < r.IntN(8); j++ {
			m[fmt.Sprint(r.IntN(50))] = fmt.Sprint(r.IntN(50))
		}
		parts := []any{r.IntN(100), fmt.Sprint(r.IntN(100)), m, r.NormFloat64()}
		first := Hash(parts...)
		for rep := 0; rep < 8; rep++ {
			if got := Hash(parts...); got != first {
				t.Fatalf("Hash is not stable: %q then %q", first, got)
			}
		}
	}
}

// Distinct values hash distinctly. Not a cryptographic claim -- a claim that
// the length prefixes and type tags do their job, so that two different keys
// cannot be served each other's value.
func TestPropertyDistinctValuesHashDistinctly(t *testing.T) {
	r := rand.New(rand.NewPCG(131, 132))
	seen := map[string]string{}
	for i := 0; i < 20000; i++ {
		a, b := fmt.Sprint(r.IntN(300)), fmt.Sprint(r.IntN(300))
		key := fmt.Sprintf("%s|%s", a, b)
		h := Hash(a, b)
		if prev, ok := seen[h]; ok && prev != key {
			t.Fatalf("%q and %q hash alike: %q", prev, key, h)
		}
		seen[h] = key
	}
}

// Purge empties the store and leaves it usable; Invalidate removes exactly
// one key and nothing else.
func TestPropertyInvalidateRemovesExactlyOneKey(t *testing.T) {
	r := rand.New(rand.NewPCG(133, 134))
	for i := 0; i < 2000; i++ {
		s := New[int, int](Options{})
		keys := map[int]bool{}
		for j := 0; j < 1+r.IntN(20); j++ {
			k := r.IntN(40)
			s.Set(k, k)
			keys[k] = true
		}
		victim := r.IntN(40)
		had := keys[victim]
		if got := s.Invalidate(victim); got != had {
			t.Fatalf("Invalidate(%d) = %v, key present = %v", victim, got, had)
		}
		delete(keys, victim)
		for k := range keys {
			if _, ok := s.Get(k); !ok {
				t.Fatalf("invalidating %d also removed %d", victim, k)
			}
		}
		if _, ok := s.Get(victim); ok {
			t.Fatalf("%d survived Invalidate", victim)
		}
		s.Purge()
		if got := s.Stats().Entries; got != 0 {
			t.Fatalf("Purge left %d entries", got)
		}
		s.Set(1, 1)
		if _, ok := s.Get(1); !ok {
			t.Fatal("the store is unusable after Purge")
		}
	}
}
