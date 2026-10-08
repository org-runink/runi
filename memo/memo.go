package memo

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// Options configures a Store. The zero value is usable: unlimited capacity, no
// expiry, errors not cached.
type Options struct {
	// Capacity is the maximum number of entries. Zero means unlimited.
	// When full, the least recently used entry is evicted.
	Capacity int

	// TTL is how long an entry stays valid after it is stored. Zero means no
	// expiry. Expiry is checked on read, so an untouched expired entry keeps
	// occupying capacity until it is read or evicted — bound memory with
	// Capacity, not with TTL.
	TTL time.Duration

	// CacheErrors stores results whose error is non-nil. Off by default: see
	// the package documentation for why this is usually the wrong thing to
	// turn on.
	CacheErrors bool

	// Now is the clock used for expiry. Nil means time.Now. Tests inject one
	// so they can advance time instead of sleeping.
	Now func() time.Time
}

// Stats is a snapshot of a Store's counters.
type Stats struct {
	Hits      uint64 // served from a stored entry
	Misses    uint64 // ran the function
	Coalesced uint64 // waited on another caller's in-flight execution
	Evictions uint64 // removed to stay within Capacity
	Expired   uint64 // found stored but past its TTL
	Entries   int    // currently stored
}

// HitRate is hits as a fraction of all lookups, counting a coalesced wait as a
// hit — it also avoided an execution. Returns 0 when there have been no
// lookups.
func (s Stats) HitRate() float64 {
	total := s.Hits + s.Misses + s.Coalesced
	if total == 0 {
		return 0
	}
	return float64(s.Hits+s.Coalesced) / float64(total)
}

type entry[V any] struct {
	key     any
	val     V
	err     error
	expires time.Time // zero means never
	el      *list.Element
}

type call[V any] struct {
	done chan struct{}
	val  V
	err  error
}

// Store is a concurrency-safe memoization cache. The zero Store is not usable;
// build one with New.
type Store[K comparable, V any] struct {
	mu       sync.Mutex
	opts     Options
	now      func() time.Time
	entries  map[K]*entry[V]
	lru      *list.List // front = most recently used; values are K
	inflight map[K]*call[V]
	stats    Stats
}

// New returns a Store.
func New[K comparable, V any](opts Options) *Store[K, V] {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Store[K, V]{
		opts:     opts,
		now:      now,
		entries:  make(map[K]*entry[V]),
		lru:      list.New(),
		inflight: make(map[K]*call[V]),
	}
}

// Do returns the memoized value for key, running fn exactly once for
// concurrent callers that miss.
//
// On a hit the stored value is returned without calling fn. On a miss fn runs;
// other callers arriving for the same key while it runs wait for that single
// execution rather than starting their own.
//
// If fn returns an error the result is not stored unless Options.CacheErrors is
// set, but it IS still shared with the callers that coalesced onto it. ctx
// cancels the wait for an in-flight execution; it does not cancel the execution
// itself, because other callers may still be waiting on it.
func (s *Store[K, V]) Do(ctx context.Context, key K, fn func(context.Context) (V, error)) (V, error) {
	s.mu.Lock()

	if e, ok := s.entries[key]; ok {
		if e.expires.IsZero() || s.now().Before(e.expires) {
			s.lru.MoveToFront(e.el)
			s.stats.Hits++
			v, err := e.val, e.err
			s.mu.Unlock()
			return v, err
		}
		// Past its TTL: drop it and fall through to a miss.
		s.removeLocked(key)
		s.stats.Expired++
	}

	if c, ok := s.inflight[key]; ok {
		s.stats.Coalesced++
		s.mu.Unlock()
		select {
		case <-c.done:
			return c.val, c.err
		case <-ctx.Done():
			var zero V
			return zero, ctx.Err()
		}
	}

	c := &call[V]{done: make(chan struct{})}
	s.inflight[key] = c
	s.stats.Misses++
	s.mu.Unlock()

	c.val, c.err = fn(ctx)

	s.mu.Lock()
	delete(s.inflight, key)
	if c.err == nil || s.opts.CacheErrors {
		s.storeLocked(key, c.val, c.err)
	}
	s.mu.Unlock()
	close(c.done)

	return c.val, c.err
}

// Get returns a stored value without running anything. ok is false on a miss or
// an expired entry.
func (s *Store[K, V]) Get(key K) (v V, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return v, false
	}
	if !e.expires.IsZero() && !s.now().Before(e.expires) {
		s.removeLocked(key)
		s.stats.Expired++
		return v, false
	}
	s.lru.MoveToFront(e.el)
	return e.val, true
}

// Set stores a value directly, as if it had been computed now.
func (s *Store[K, V]) Set(key K, v V) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storeLocked(key, v, nil)
}

// Invalidate removes key. It reports whether an entry was present.
func (s *Store[K, V]) Invalidate(key K) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[key]
	if ok {
		s.removeLocked(key)
	}
	return ok
}

// Purge removes every entry. In-flight executions are unaffected and their
// results are still stored when they finish.
func (s *Store[K, V]) Purge() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[K]*entry[V])
	s.lru.Init()
}

// Stats returns a snapshot of the counters.
func (s *Store[K, V]) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.stats
	out.Entries = len(s.entries)
	return out
}

func (s *Store[K, V]) storeLocked(key K, v V, err error) {
	if e, ok := s.entries[key]; ok {
		e.val, e.err = v, err
		e.expires = s.expiryLocked()
		s.lru.MoveToFront(e.el)
		return
	}
	e := &entry[V]{key: key, val: v, err: err, expires: s.expiryLocked()}
	e.el = s.lru.PushFront(key)
	s.entries[key] = e
	for s.opts.Capacity > 0 && len(s.entries) > s.opts.Capacity {
		back := s.lru.Back()
		if back == nil {
			break
		}
		s.removeLocked(back.Value.(K))
		s.stats.Evictions++
	}
}

func (s *Store[K, V]) expiryLocked() time.Time {
	if s.opts.TTL <= 0 {
		return time.Time{}
	}
	return s.now().Add(s.opts.TTL)
}

func (s *Store[K, V]) removeLocked(key K) {
	e, ok := s.entries[key]
	if !ok {
		return
	}
	s.lru.Remove(e.el)
	delete(s.entries, key)
}
