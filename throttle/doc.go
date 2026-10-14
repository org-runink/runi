// SPDX-License-Identifier: BSD-3-Clause

// Package throttle caps how often something may act: per interval, per
// target, and after failures.
//
// Anything that acts on its own — restarts a process, scales a pool, drains
// a node, retries a request — can act too often when what it watches
// misbehaves, and its actions can then do more harm than the fault. The
// remedies are old and well understood: a rate with a burst allowance, a
// fixed allowance per window, a minimum interval per target, a growing
// random delay between retries, and a switch that stops trying something
// that keeps failing. This package provides each as a small type that is
// told the time of every call.
//
// Which to use:
//
//   - Bucket is a token bucket: on average rate actions per second, at most
//     burst at once. Over any stretch of time t it allows at most
//     burst + ⌊rate·t⌋. Kept in integer nanoseconds as the generic cell rate
//     algorithm's theoretical arrival time, so it does not drift.
//   - Budget is at most N actions in each window of length W, windows laid
//     from an origin the caller chooses: "5 per hour, on the hour". It says
//     how many remain and when the window ends.
//   - Cooldown lets each key act at most once per period, for any comparable
//     key; keys are forgotten once their period ends, in O(1) amortised
//     time, and an optional cap bounds how many are tracked.
//   - Backoff draws the delays between retries: exponential, capped, and
//     randomised by full or decorrelated jitter from a source the caller
//     seeds.
//   - Breaker is a circuit breaker: closed, open and half-open, opening after
//     consecutive failures and probing before it closes again.
//
// They compose: each answers "may I act now?", and an action that must pass
// several caps asks each of them.
//
//	limit, _ := throttle.NewBucket(0.1, 3)  // one restart per 10 s, 3 at once
//	each, _ := throttle.NewCooldown[string](5*time.Minute, 10000)
//	for ev := range unhealthy {
//		if each.Allow(ev.Service, ev.At) && limit.Allow(ev.At) {
//			restart(ev.Service)
//		}
//	}
//
// The order matters when an action is refused: a cap asked first spends its
// allowance even when a later one refuses. Ask the scarcest last, or use
// the query methods (Tokens, Remaining, Next) first.
//
// Time comes only from the at argument of each call; nothing here reads the
// clock or sleeps, so every type is as testable as the caller's clock is.
// Backoff takes no time at all: it returns a delay, and waiting is the
// caller's. Times are compared and differenced with time.Time's own
// methods, which use the monotonic reading time.Now attaches, so a
// wall-clock step between two such readings does not distort an interval.
//
// One rule holds for every timed type, and tests hold each type to it: a
// time before the latest one the type has seen is taken as the latest, and
// counted in Late. Time never runs backwards, so a clock stepped back, or
// calls delivered out of order, cannot refill a bucket, reopen a window,
// end a cooldown or end an open period early; at worst they make the type
// wait until the clock passes where it was. A call at the latest time is
// not late. Query methods (Tokens, Next, Remaining, WindowEnd) apply the
// same rule and change nothing.
//
// Allow and AllowN on Bucket and Budget, Breaker's Allow and Record and
// Backoff's Next do O(1) work and allocate nothing; so does Cooldown's
// Allow, in O(1) amortised, on a key it already tracks or once its table
// has reached its working size. A test holds each to zero. Bucket, Budget and
// Breaker are plain values, safe to copy. A Cooldown holds a table and a
// Backoff a random source by reference, and a copy shares them. None is
// safe for concurrent use; give each goroutine its own, or guard it.
// Constructor errors wrap ErrInvalid.
//
// # What this package does not do
//
// It does not choose rates, windows, periods or thresholds, and has no
// defaults. They follow from how much harm one action can do and how fast
// the thing acted on recovers, which only the caller knows. The literature
// cited on each type describes mechanisms, not settings.
//
// It does not wait. Next, NextN and Backoff.Next say when or how long;
// sleeping, timers and contexts are the caller's.
//
// It does not coordinate. Every cap is local to the value that holds it;
// several processes each with a Bucket of rate r together allow up to
// r times as many. A limit shared across processes needs shared state —
// one owner the others ask — which this package does not provide.
//
// It does not adapt. Rates and thresholds are fixed at construction; it
// does not raise a rate while actions succeed or lower it when they fail,
// as the additive-increase, multiplicative-decrease rule of V. Jacobson,
// "Congestion Avoidance and Control", ACM SIGCOMM 1988, does for a
// transport's window.
//
// It does not judge results. What counts as a failure for a Breaker, and
// whether an error is worth a retry, is the caller's decision.
//
// It does not keep a log of actions. Budget counts per fixed window and
// Bucket refills continuously; a sliding window over a log of every action
// is exact over every stretch but costs memory per action, and is not here.
//
// It does not persist. A process that restarts starts every cap afresh,
// with full buckets and closed breakers.
package throttle
