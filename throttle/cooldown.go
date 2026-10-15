// SPDX-License-Identifier: BSD-3-Clause

package throttle

import "time"

// Cooldown lets each key act at most once per period: an action for a key is
// allowed when the key has not been allowed one in the last period, and is
// then the start of the key's next period. "Restart any one service at most
// once every ten minutes."
//
// The rule is exact. An action at t for a key last allowed at s is allowed
// if and only if t − s ≥ period; a refused action does not move s. It is
// the same rule as a Bucket of burst 1 and one token per period kept for
// each key, and a test holds the two to the same decisions.
//
// A key is forgotten once its period has passed, since from then on it would
// be allowed anyway. Keys are kept in a queue in the order they were
// allowed, which is also the order their periods end, because times never
// run backwards here; so expired keys are always at the front. Every Allow
// first drops the expired keys at the front, which costs O(1) amortised per
// key ever allowed, and the table holds only keys allowed within the last
// period. Sweep does the same dropping at a time of the caller's choosing,
// for a table that may sit idle while holding many keys.
//
// maxKeys, when positive, caps the keys tracked at once. When the table is
// full of keys still within their period, an action for a new key is
// refused and counted in Full: no tracked key is evicted to make room,
// because an evicted key could then act again before its period ended, and
// a cap on corrective actions should fail by acting less, not more. The
// cap bounds memory: Go maps do not return memory on deletion, so the
// table's footprint is its largest size, at most maxKeys entries.
//
// Allow on a key already tracked does not allocate, nor does Allow on a key
// whose period has ended once the table has reached its working size: the
// slots expired keys leave are reused. Growing the table allocates.
//
// A time before the latest one the cooldown has seen is taken as the
// latest (and counted in Late), so a clock stepped back cannot end a period
// early.
//
// The zero value is not usable (Allow panics on it: it has no table); build
// one with NewCooldown, which returns a pointer. Not safe to copy (copies
// share the table) and not safe for concurrent use.
type Cooldown[K comparable] struct {
	period  time.Duration
	maxKeys int
	last    map[K]time.Time // key → time it was last allowed
	queue   []stamp[K]      // ring of the same, in the order allowed
	head, n int
	c       clock
	allowed int64
	denied  int64
	full    int64
}

type stamp[K comparable] struct {
	key K
	at  time.Time
}

// NewCooldown returns a cooldown of period > 0 per key, tracking at most
// maxKeys ≥ 0 keys at once; 0 is no cap.
func NewCooldown[K comparable](period time.Duration, maxKeys int) (*Cooldown[K], error) {
	if period <= 0 {
		return nil, invalid("period", period, "> 0")
	}
	if maxKeys < 0 {
		return nil, invalid("maxKeys", maxKeys, ">= 0")
	}
	return &Cooldown[K]{period: period, maxKeys: maxKeys, last: make(map[K]time.Time)}, nil
}

// Allow reports whether key may act at time at, and if so starts its
// period. It does not allocate unless the table grows.
func (cd *Cooldown[K]) Allow(key K, at time.Time) bool {
	cd.c.step(at)
	now := cd.c.last
	cd.expire(now)
	if _, ok := cd.last[key]; ok {
		cd.denied++
		return false
	}
	if cd.maxKeys > 0 && cd.n >= cd.maxKeys {
		cd.full++
		cd.denied++
		return false
	}
	cd.push(key, now)
	cd.last[key] = now
	cd.allowed++
	return true
}

// expire drops the keys whose period has ended by now, from the front of the
// queue, and returns how many.
func (cd *Cooldown[K]) expire(now time.Time) int {
	removed := 0
	for cd.n > 0 {
		s := &cd.queue[cd.head]
		if now.Sub(s.at) < cd.period {
			break
		}
		delete(cd.last, s.key)
		*s = stamp[K]{}
		cd.head++
		if cd.head == len(cd.queue) {
			cd.head = 0
		}
		cd.n--
		removed++
	}
	return removed
}

// push appends a key to the back of the queue, doubling the ring when it is
// full (to at most maxKeys, when there is a cap).
func (cd *Cooldown[K]) push(key K, at time.Time) {
	if cd.n == len(cd.queue) {
		size := max(2*len(cd.queue), 8)
		if cd.maxKeys > 0 {
			size = min(size, cd.maxKeys)
		}
		q := make([]stamp[K], size)
		for i := range cd.n {
			q[i] = cd.queue[(cd.head+i)%len(cd.queue)]
		}
		cd.queue, cd.head = q, 0
	}
	i := cd.head + cd.n
	if i >= len(cd.queue) {
		i -= len(cd.queue)
	}
	cd.queue[i] = stamp[K]{key, at}
	cd.n++
}

// Next is the earliest time, not before at (nor before the latest time the
// cooldown has seen), at which key may act, if nothing else acts first. For
// a new key refused because the table is full, it is the time the oldest
// tracked key's period ends. It changes nothing.
func (cd *Cooldown[K]) Next(key K, at time.Time) time.Time {
	now := cd.c.now(at)
	if s, ok := cd.last[key]; ok {
		if end := s.Add(cd.period); end.After(now) {
			return end
		}
		return now
	}
	if cd.maxKeys > 0 && cd.n >= cd.maxKeys {
		if end := cd.queue[cd.head].at.Add(cd.period); end.After(now) {
			return end
		}
	}
	return now
}

// Sweep forgets every key whose period has ended by at and returns how many
// it forgot: O(1) per key forgotten. Allow does the same on every call, so
// Sweep is needed only to release keys while no Allow is made.
func (cd *Cooldown[K]) Sweep(at time.Time) int {
	cd.c.step(at)
	return cd.expire(cd.c.last)
}

// Len is the number of keys tracked: those allowed within the last period,
// plus any whose period has ended since the last Allow or Sweep.
func (cd *Cooldown[K]) Len() int { return cd.n }

// Period is the time a key must wait between actions.
func (cd *Cooldown[K]) Period() time.Duration { return cd.period }

// Allowed is the number of actions allowed.
func (cd *Cooldown[K]) Allowed() int64 { return cd.allowed }

// Denied is the number of actions refused, for either reason.
func (cd *Cooldown[K]) Denied() int64 { return cd.denied }

// Full is the number of actions refused only because the table was full: the
// key itself was free to act.
func (cd *Cooldown[K]) Full() int64 { return cd.full }

// Late is the number of calls whose time was before the latest time the
// cooldown had seen, and was taken as that time.
func (cd *Cooldown[K]) Late() int64 { return cd.c.late }

// Reset forgets every key and every time and zeroes every count, keeping the
// period, the cap and the memory already allocated.
func (cd *Cooldown[K]) Reset() {
	clear(cd.last)
	clear(cd.queue)
	cd.head, cd.n = 0, 0
	cd.c = clock{}
	cd.allowed, cd.denied, cd.full = 0, 0, 0
}
