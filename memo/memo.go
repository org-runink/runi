package memo

import (
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

// entry is one stored result, and its place in the recency order. The order is
// a doubly linked list threaded through the entries themselves: container/list
// would cost an element and an interface box for the key on every store, two
// allocations for bookkeeping the entry can carry in two pointers.
type entry[K comparable, V any] struct {
	key        K
	val        V
	err        error
	expires    time.Time    // zero means never
	prev, next *entry[K, V] // towards the most and the least recently used
}

// call is one in-flight execution. done is made only when a second caller
// arrives to wait on it, under the store's lock, so the common uncontended miss
// does not allocate a channel nobody will ever receive from.
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
	entries  map[K]*entry[K, V]
	head     *entry[K, V] // most recently used
	tail     *entry[K, V] // least recently used, the next to be evicted
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
		entries:  make(map[K]*entry[K, V]),
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
			s.toFront(e)
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
		if c.done == nil {
			c.done = make(chan struct{})
		}
		done := c.done
		s.mu.Unlock()
		select {
		case <-done:
			return c.val, c.err
		case <-ctx.Done():
			var zero V
			return zero, ctx.Err()
		}
	}

	c := &call[V]{}
	s.inflight[key] = c
	s.stats.Misses++
	s.mu.Unlock()

	c.val, c.err = fn(ctx)

	s.mu.Lock()
	delete(s.inflight, key)
	if c.err == nil || s.opts.CacheErrors {
		s.storeLocked(key, c.val, c.err)
	}
	// Read under the lock: a waiter creates done under the same lock, and once
	// the call is out of inflight no new waiter can find it.
	done := c.done
	s.mu.Unlock()
	if done != nil {
		close(done)
	}

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
	s.toFront(e)
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
	s.entries = make(map[K]*entry[K, V])
	s.head, s.tail = nil, nil
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
		s.toFront(e)
		return
	}
	// At capacity the least recently used entry makes room, and its struct is
	// reused for the new one: a full store then takes no allocation for the
	// entry at all. The tail exists, because a store at capacity is not empty.
	// Evicting before inserting rather than after leaves the same entries,
	// since the new one would go to the front and never be the one evicted.
	var e *entry[K, V]
	if s.opts.Capacity > 0 && len(s.entries) >= s.opts.Capacity {
		e = s.tail
		s.unlink(e)
		delete(s.entries, e.key)
		s.stats.Evictions++
		*e = entry[K, V]{}
	} else {
		e = new(entry[K, V])
	}
	e.key, e.val, e.err, e.expires = key, v, err, s.expiryLocked()
	s.pushFront(e)
	s.entries[key] = e
}

// pushFront makes e the most recently used entry. e must not be linked.
func (s *Store[K, V]) pushFront(e *entry[K, V]) {
	e.prev, e.next = nil, s.head
	if s.head != nil {
		s.head.prev = e
	} else {
		s.tail = e
	}
	s.head = e
}

// unlink takes e out of the recency order.
func (s *Store[K, V]) unlink(e *entry[K, V]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		s.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		s.tail = e.prev
	}
	e.prev, e.next = nil, nil
}

// toFront marks e as just used.
func (s *Store[K, V]) toFront(e *entry[K, V]) {
	if s.head != e {
		s.unlink(e)
		s.pushFront(e)
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
	s.unlink(e)
	delete(s.entries, key)
}
