// SPDX-License-Identifier: BSD-3-Clause

package throttle

import (
	"math"
	"time"
)

// Bucket is a token bucket: it allows on average rate actions per second,
// and up to burst of them at once after a quiet spell.
//
// The bucket holds at most burst tokens and gains one every 1/rate seconds;
// an action takes one token, or n for AllowN, and is refused when there are
// not enough. Over any stretch of time of length t, then, at most
// burst + ⌊t·rate⌋ tokens are taken: the bucket's whole content at the start
// plus what it earned during the stretch. A test holds every Bucket to that
// bound over every pair of its own allowed actions.
//
// Method: the generic cell rate algorithm (GCRA) in its virtual-scheduling
// form, ATM Forum, Traffic Management Specification Version 4.0
// (af-tm-0056.000), 1996, and ITU-T Recommendation I.371, Traffic control
// and congestion control in B-ISDN. Rather than a count of tokens that grows
// with time, GCRA keeps a theoretical arrival time (TAT): the time at which
// the bucket would be full again if nothing more were taken. A request for n
// tokens at time t conforms when TAT + n·T − burst·T ≤ t, T being the
// interval between tokens, and then moves TAT to max(TAT, t) + n·T. That is
// the token bucket of A. S. Tanenbaum and D. J. Wetherall, Computer
// Networks, 5th ed., Pearson, 2011, §5.4 (traffic shaping), counted in time
// instead of tokens; the leaky bucket of J. S. Turner, "New directions in
// communications (or which way to the information age?)", IEEE
// Communications Magazine 24(10), 1986, is the same rule seen from the
// other side.
//
// Here TAT is kept as a debt: TAT minus the latest time, in whole
// nanoseconds, between 0 and burst·T. T is 1/rate rounded once, in
// NewBucket, to the nearest nanosecond (Interval reports it, Rate the rate it
// gives); every step after that is integer addition, subtraction and
// comparison, so a bucket run for years admits exactly what the arithmetic
// says, with no rounding to accumulate and nothing to overflow.
//
// A time before the latest one the bucket has seen is taken as the latest
// (and counted in Late): a clock stepped back earns no tokens, and the
// bucket resumes refilling only once the clock passes where it was.
//
// The zero value refuses every action; build one with NewBucket. Safe to
// copy, not safe for concurrent use.
type Bucket struct {
	interval int64 // T: nanoseconds per token
	burst    int64
	limit    int64 // burst·T: the most debt the bucket may hold
	debt     int64 // TAT − latest time, nanoseconds, in [0, limit]
	c        clock
	allowed  int64
	denied   int64
}

// NewBucket returns a full bucket that gains rate tokens per second and
// holds at most burst. rate must be finite and positive with 1/rate between
// 1 ns and 2⁶³ ns once rounded to whole nanoseconds (about 2e9 down to one
// per 292 years), and burst ≥ 1 with burst/rate below 2⁶³ ns.
func NewBucket(rate float64, burst int) (Bucket, error) {
	if !(rate > 0) || math.IsInf(rate, 1) {
		return Bucket{}, invalid("rate", rate, "> 0 and finite")
	}
	iv := math.Round(1e9 / rate)
	if iv < 1 || iv >= 0x1p63 {
		return Bucket{}, invalid("rate", rate, "1/rate between 1 ns and 2^63 ns")
	}
	t := int64(iv)
	if burst < 1 || int64(burst) > math.MaxInt64/t {
		return Bucket{}, invalid("burst", burst, ">= 1 with burst/rate below 2^63 ns")
	}
	return Bucket{interval: t, burst: int64(burst), limit: int64(burst) * t}, nil
}

// Allow takes one token at time at and reports whether there was one. It
// does not allocate.
func (b *Bucket) Allow(at time.Time) bool { return b.AllowN(at, 1) }

// AllowN takes n tokens at time at, all or none, and reports whether there
// were n. An n larger than the burst is always refused; an n ≤ 0 takes
// nothing, reports true and is not counted. It does not allocate.
func (b *Bucket) AllowN(at time.Time, n int) bool {
	b.debt = max(b.debt-int64(b.c.step(at)), 0)
	if n <= 0 {
		return true
	}
	if int64(n) > b.burst || b.debt > b.limit-int64(n)*b.interval {
		b.denied++
		return false
	}
	b.debt += int64(n) * b.interval
	b.allowed++
	return true
}

// debtAt is the debt as of at without moving the clock.
func (b *Bucket) debtAt(at time.Time) int64 {
	return max(b.debt-int64(b.c.ahead(at)), 0)
}

// Tokens is the number of whole tokens in the bucket at time at, 0 to burst.
// It changes nothing.
func (b *Bucket) Tokens(at time.Time) int {
	if b.interval == 0 {
		return 0
	}
	return int((b.limit - b.debtAt(at)) / b.interval)
}

// Next is the earliest time, not before at (nor before the latest time the
// bucket has seen), at which one token is available. It changes nothing.
func (b *Bucket) Next(at time.Time) time.Time {
	t, _ := b.NextN(at, 1)
	return t
}

// NextN is the earliest time, not before at (nor before the latest time the
// bucket has seen), at which AllowN(_, n) would succeed, if nothing else is
// taken first. It reports false, and the zero time, when n exceeds the burst
// and no time will do. It changes nothing.
func (b *Bucket) NextN(at time.Time, n int) (time.Time, bool) {
	now := b.c.now(at)
	if n <= 0 {
		return now, true
	}
	if int64(n) > b.burst {
		return time.Time{}, false
	}
	wait := max(b.debtAt(at)+int64(n)*b.interval-b.limit, 0)
	return now.Add(time.Duration(wait)), true
}

// Interval is the time between tokens: 1/rate rounded to the nearest
// nanosecond.
func (b *Bucket) Interval() time.Duration { return time.Duration(b.interval) }

// Rate is the rate the bucket keeps, in tokens per second: 1e9 divided by
// the Interval in nanoseconds, which can differ from the rate asked of
// NewBucket by the rounding of the interval.
func (b *Bucket) Rate() float64 { return 1e9 / float64(b.interval) }

// Burst is the bucket's capacity.
func (b *Bucket) Burst() int { return int(b.burst) }

// Allowed is the number of AllowN calls that took tokens.
func (b *Bucket) Allowed() int64 { return b.allowed }

// Denied is the number of AllowN calls that were refused.
func (b *Bucket) Denied() int64 { return b.denied }

// Late is the number of calls whose time was before the latest time the
// bucket had seen, and was taken as that time.
func (b *Bucket) Late() int64 { return b.c.late }

// Reset refills the bucket, forgets every time it has seen and zeroes every
// count, keeping the rate and burst.
func (b *Bucket) Reset() { *b = Bucket{interval: b.interval, burst: b.burst, limit: b.limit} }
