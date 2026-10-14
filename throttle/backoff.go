// SPDX-License-Identifier: BSD-3-Clause

package throttle

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"strconv"
	"time"
)

// Jitter selects how a Backoff randomises its delays.
type Jitter uint8

const (
	// FullJitter draws each delay uniformly from [0, min(cap, base·2ⁿ)],
	// n being the number of delays drawn before it.
	FullJitter Jitter = iota
	// DecorrelatedJitter draws each delay as min(cap, uniform[base, 3·prev]),
	// prev being the previous delay (base before the first), so each delay
	// lies in [base, cap].
	DecorrelatedJitter
)

// String is "full" or "decorrelated".
func (j Jitter) String() string {
	switch j {
	case FullJitter:
		return "full"
	case DecorrelatedJitter:
		return "decorrelated"
	}
	return "Jitter(" + strconv.Itoa(int(j)) + ")"
}

// Backoff draws the delays to wait between retries of a failed action:
// growing, capped, and random, so that many callers that failed together do
// not retry together.
//
// Plain exponential backoff, base·2ⁿ before the n-th retry, spreads one
// caller's retries out but keeps callers that failed at the same moment in
// step: they retry at the same moments and collide again. Randomising each
// delay breaks the lockstep. Of the schemes compared in M. Brooker,
// "Exponential Backoff And Jitter", AWS Architecture Blog, 2015, this
// provides the two the article recommends:
//
//   - FullJitter: uniform on [0, min(cap, base·2ⁿ)]. The delays grow as plain
//     backoff does, but each is anywhere from zero to that ceiling; in the
//     article's simulation it needed the least total work.
//   - DecorrelatedJitter: min(cap, uniform[base, 3·prev]), starting from
//     prev = base. Each delay depends on the last instead of on the attempt
//     number, grows by up to three times per retry, and never falls below
//     base.
//
// Exponential backoff itself is that of R. M. Metcalfe and D. R. Boggs,
// "Ethernet: Distributed Packet Switching for Local Computer Networks",
// Communications of the ACM 19(7), 1976, where a station that collides
// waits a random number of slots drawn from a range that doubles with each
// collision; IEEE 802.3 stops doubling after 10 collisions and gives up
// after 16. This package stops at cap and never gives up: how many retries
// to make is the caller's decision.
//
// Randomness comes only from the source given to NewBackoff — a
// *rand.Rand, or a rand.PCG or rand.ChaCha8 from math/rand/v2, seeded by the
// caller — so a Backoff with a seeded source draws the same delays on every
// run. Integers in a range are drawn without bias by the multiply-and-reject
// method of D. Lemire, "Fast Random Integer Generation in an Interval", ACM
// Transactions on Modeling and Computer Simulation 29(1), 2019. Delays are
// whole nanoseconds; every value of the range, both ends included, is
// equally likely before the cap is applied.
//
// Backoff computes delays and does nothing else: it does not sleep, and it
// takes no time because no delay depends on one. The caller waits, retries,
// and calls Reset after a success.
//
// The zero value is not usable (Next panics on it); build one with
// NewBackoff. A copy shares the source with the original, and so draws from
// the same stream. Not safe for concurrent use, and neither are the sources
// math/rand/v2 provides.
type Backoff struct {
	jitter  Jitter
	base    time.Duration
	cap     time.Duration
	src     rand.Source
	attempt int
	prev    time.Duration // the previous delay; base before the first
}

// NewBackoff returns a backoff of the given jitter with 0 < base ≤ cap,
// drawing from src, which must not be nil.
func NewBackoff(j Jitter, base, cap time.Duration, src rand.Source) (Backoff, error) {
	if j > DecorrelatedJitter {
		return Backoff{}, invalid("jitter", j, "FullJitter or DecorrelatedJitter")
	}
	if base <= 0 {
		return Backoff{}, invalid("base", base, "> 0")
	}
	if cap < base {
		return Backoff{}, invalid("cap", cap, ">= base")
	}
	if src == nil {
		return Backoff{}, invalid("src", src, "a random source")
	}
	return Backoff{jitter: j, base: base, cap: cap, src: src, prev: base}, nil
}

// Next draws the delay before the next retry and counts the attempt. It does
// not allocate.
func (b *Backoff) Next() time.Duration {
	var d time.Duration
	if b.jitter == DecorrelatedJitter {
		hi := time.Duration(math.MaxInt64)
		if b.prev <= hi/3 {
			hi = 3 * b.prev
		}
		d = min(b.cap, uniform(b.src, b.base, hi))
	} else {
		d = uniform(b.src, 0, b.Ceiling())
	}
	b.prev = d
	if b.attempt < math.MaxInt {
		b.attempt++
	}
	return d
}

// Ceiling is the largest delay the next call to Next can return:
// min(cap, base·2ⁿ) for FullJitter, n being Attempt, and min(cap, 3·prev)
// for DecorrelatedJitter. It changes nothing.
func (b *Backoff) Ceiling() time.Duration {
	if b.jitter == DecorrelatedJitter {
		if b.prev > b.cap/3 {
			return b.cap
		}
		return 3 * b.prev
	}
	n := b.attempt
	if n >= 63 || b.base > b.cap>>n {
		return b.cap
	}
	return b.base << n
}

// Attempt is the number of delays drawn since NewBackoff or Reset; it stops
// at math.MaxInt.
func (b *Backoff) Attempt() int { return b.attempt }

// Base is the smallest scale of delay: the first FullJitter ceiling, and the
// DecorrelatedJitter floor.
func (b *Backoff) Base() time.Duration { return b.base }

// Cap is the largest delay Next returns.
func (b *Backoff) Cap() time.Duration { return b.cap }

// Reset starts again from the first attempt, keeping the parameters and the
// source; the source is not rewound.
func (b *Backoff) Reset() { b.attempt, b.prev = 0, b.base }

// uniform is a uniform draw from [lo, hi], 0 ≤ lo ≤ hi.
func uniform(src rand.Source, lo, hi time.Duration) time.Duration {
	return lo + time.Duration(below(src, uint64(hi-lo)+1))
}

// below is a uniform draw from [0, n), n > 0: the high word of x·n for a
// uniform 64-bit x, rejecting the x whose low word falls below 2⁶⁴ mod n,
// which would otherwise make some results more likely than others (Lemire,
// 2019).
func below(src rand.Source, n uint64) uint64 {
	hi, lo := bits.Mul64(src.Uint64(), n)
	if lo < n {
		thresh := -n % n
		for lo < thresh {
			hi, lo = bits.Mul64(src.Uint64(), n)
		}
	}
	return hi
}
