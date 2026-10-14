// SPDX-License-Identifier: BSD-3-Clause

package throttle

import (
	"strconv"
	"time"
)

// State is a Breaker's state.
type State uint8

const (
	// Closed: calls are allowed, and consecutive failures are counted.
	Closed State = iota
	// Open: calls are refused until the open duration has passed.
	Open
	// HalfOpen: a limited number of probe calls are allowed; their results
	// decide whether the breaker closes or opens again.
	HalfOpen
)

// String is "closed", "open" or "half-open".
func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	}
	return "State(" + strconv.Itoa(int(s)) + ")"
}

// Breaker stops calling something that keeps failing, and tries it again,
// carefully, after a while.
//
// It has three states:
//
//   - Closed: Allow admits every call. Record counts consecutive failures; a
//     success resets the count, and threshold failures in a row open the
//     breaker.
//   - Open: Allow refuses every call until openFor has passed since the
//     breaker opened; then the breaker is half-open.
//   - HalfOpen: Allow admits up to probes calls, and refuses the rest. If any
//     probe fails, the breaker opens again for another openFor; once probes
//     of them have succeeded, it closes.
//
// Every change of state is counted in Transitions, and Since is the time of
// the latest. The move from Open to HalfOpen happens at the first Allow,
// Record or State call at or after the end of the open period, and Since is
// that call's time.
//
// Results are matched to calls only by count. In HalfOpen a Record counts as
// a probe's result only while an admitted probe has not yet reported; any
// other result — from a call admitted while the breaker was still closed,
// arriving late — is ignored, as is every Record while the breaker is Open.
// A probe whose result is never recorded holds its place, and once every
// probe is held the breaker stays half-open; record a timed-out probe as a
// failure.
//
// Method: the Circuit Breaker pattern of M. T. Nygard, Release It! Design
// and Deploy Production-Ready Software, 2nd ed., Pragmatic Bookshelf, 2018,
// chapter 5 (stability patterns): closed, open and half-open, a failure
// threshold to open, a timeout to try again, and a trial call to decide.
// Allowing more than one trial (probes > 1) and requiring all of them to
// succeed is a stricter form of the same rule.
//
// A time before the latest one the breaker has seen is taken as the latest
// (and counted in Late), so a clock stepped back cannot end an open period
// early.
//
// The zero value is not usable; build one with NewBreaker. Safe to copy,
// not safe for concurrent use.
type Breaker struct {
	threshold   int
	openFor     time.Duration
	probes      int
	state       State
	since       time.Time
	failures    int // consecutive failures while closed
	admitted    int // probes admitted in this half-open state
	pending     int // admitted probes not yet reported
	successes   int // probes reported successful
	transitions int64
	c           clock
}

// NewBreaker returns a closed breaker that opens after threshold ≥ 1
// consecutive failures, stays open for openFor ≥ 0, and then admits probes ≥
// 1 trial calls, all of which must succeed for it to close.
func NewBreaker(threshold int, openFor time.Duration, probes int) (Breaker, error) {
	if threshold < 1 {
		return Breaker{}, invalid("threshold", threshold, ">= 1")
	}
	if openFor < 0 {
		return Breaker{}, invalid("openFor", openFor, ">= 0")
	}
	if probes < 1 {
		return Breaker{}, invalid("probes", probes, ">= 1")
	}
	return Breaker{threshold: threshold, openFor: openFor, probes: probes}, nil
}

// advance moves the clock to at and an Open breaker whose period has passed
// to HalfOpen.
func (b *Breaker) advance(at time.Time) {
	b.c.step(at)
	if b.state == Open && b.c.last.Sub(b.since) >= b.openFor {
		b.to(HalfOpen)
	}
}

func (b *Breaker) to(s State) {
	b.state, b.since = s, b.c.last
	b.failures, b.admitted, b.pending, b.successes = 0, 0, 0, 0
	b.transitions++
}

// Allow reports whether a call may be made at time at. In HalfOpen, a true
// result admits a probe, whose result the caller must Record. It does not
// allocate.
func (b *Breaker) Allow(at time.Time) bool {
	b.advance(at)
	switch b.state {
	case Closed:
		return true
	case HalfOpen:
		if b.admitted < b.probes {
			b.admitted++
			b.pending++
			return true
		}
	}
	return false
}

// Record reports the result of a call at time at: ok for success. It does
// not allocate.
func (b *Breaker) Record(ok bool, at time.Time) {
	b.advance(at)
	switch b.state {
	case Closed:
		if ok {
			b.failures = 0
			return
		}
		b.failures++
		if b.failures >= b.threshold {
			b.to(Open)
		}
	case HalfOpen:
		if b.pending == 0 {
			return
		}
		b.pending--
		if !ok {
			b.to(Open)
			return
		}
		b.successes++
		if b.successes >= b.probes {
			b.to(Closed)
		}
	}
}

// State is the state at time at. It moves an Open breaker whose period has
// passed to HalfOpen, as Allow and Record do.
func (b *Breaker) State(at time.Time) State {
	b.advance(at)
	return b.state
}

// Since is the time the current state was entered: the zero time before the
// first change.
func (b *Breaker) Since() time.Time { return b.since }

// OpenUntil is the end of the current open period, and the zero time when
// the breaker is not open.
func (b *Breaker) OpenUntil() time.Time {
	if b.state != Open {
		return time.Time{}
	}
	return b.since.Add(b.openFor)
}

// Failures is the number of consecutive failures recorded while closed.
func (b *Breaker) Failures() int { return b.failures }

// Transitions is the number of changes of state.
func (b *Breaker) Transitions() int64 { return b.transitions }

// Late is the number of calls whose time was before the latest time the
// breaker had seen, and was taken as that time.
func (b *Breaker) Late() int64 { return b.c.late }

// Reset closes the breaker, forgets every time it has seen and zeroes every
// count, keeping the threshold, open duration and probes.
func (b *Breaker) Reset() {
	*b = Breaker{threshold: b.threshold, openFor: b.openFor, probes: b.probes}
}
