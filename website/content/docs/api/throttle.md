---
title: "runi/throttle"
weight: 17
---

```go
import "github.com/org-runink/runi/throttle"
```

Package throttle caps how often something may act: per interval, per
target, and after failures.

Anything that acts on its own — restarts a process, scales a pool, drains
a node, retries a request — can act too often when what it watches
misbehaves, and its actions can then do more harm than the fault. The
remedies are old and well understood: a rate with a burst allowance, a
fixed allowance per window, a minimum interval per target, a growing
random delay between retries, and a switch that stops trying something
that keeps failing. This package provides each as a small type that is
told the time of every call.

Which to use:

  - Bucket is a token bucket: on average rate actions per second, at most
    burst at once. Over any stretch of time t it allows at most
    burst + ⌊rate·t⌋. Kept in integer nanoseconds as the generic cell rate
    algorithm's theoretical arrival time, so it does not drift.
  - Quota is at most N actions in each window of length W, windows laid
    from an origin the caller chooses: "5 per hour, on the hour". It says
    how many remain and when the window ends.
  - Cooldown lets each key act at most once per period, for any comparable
    key; keys are forgotten once their period ends, in O(1) amortised
    time, and an optional cap bounds how many are tracked.
  - Backoff draws the delays between retries: exponential, capped, and
    randomised by full or decorrelated jitter from a source the caller
    seeds.
  - Breaker is a circuit breaker: closed, open and half-open, opening after
    consecutive failures and probing before it closes again.

They compose: each answers "may I act now?", and an action that must pass
several caps asks each of them.

limit, _ := throttle.NewBucket(0.1, 3)  // one restart per 10 s, 3 at once
each, _ := throttle.NewCooldown[string](5*time.Minute, 10000)
for ev := range unhealthy {
	if each.Allow(ev.Service, ev.At) && limit.Allow(ev.At) {
		restart(ev.Service)
	}
}

The order matters when an action is refused: a cap asked first spends its
allowance even when a later one refuses. Ask the scarcest last, or use
the query methods (Tokens, Remaining, Next) first.

Time comes only from the at argument of each call; nothing here reads the
clock or sleeps, so every type is as testable as the caller's clock is.
Backoff takes no time at all: it returns a delay, and waiting is the
caller's. Times are compared and differenced with time.Time's own
methods, which use the monotonic reading time.Now attaches, so a
wall-clock step between two such readings does not distort an interval.

One rule holds for every timed type, and tests hold each type to it: a
time before the latest one the type has seen is taken as the latest, and
counted in Late. Time never runs backwards, so a clock stepped back, or
calls delivered out of order, cannot refill a bucket, reopen a window,
end a cooldown or end an open period early; at worst they make the type
wait until the clock passes where it was. A call at the latest time is
not late. Query methods (Tokens, Next, Remaining, WindowEnd) apply the
same rule and change nothing.

Allow and AllowN on Bucket and Quota, Breaker's Allow and Record and
Backoff's Next do O(1) work and allocate nothing; so does Cooldown's
Allow, in O(1) amortised, on a key it already tracks or once its table
has reached its working size. A test holds each to zero. Bucket, Quota and
Breaker are plain values, safe to copy. A Cooldown holds a table and a
Backoff a random source by reference, and a copy shares them. None is
safe for concurrent use; give each goroutine its own, or guard it.
Constructor errors wrap ErrInvalid.

# What this package does not do

It does not choose rates, windows, periods or thresholds, and has no
defaults. They follow from how much harm one action can do and how fast
the thing acted on recovers, which only the caller knows. The literature
cited on each type describes mechanisms, not settings.

It does not wait. Next, NextN and Backoff.Next say when or how long;
sleeping, timers and contexts are the caller's.

It does not coordinate. Every cap is local to the value that holds it;
several processes each with a Bucket of rate r together allow up to
r times as many. A limit shared across processes needs shared state —
one owner the others ask — which this package does not provide.

It does not adapt. Rates and thresholds are fixed at construction; it
does not raise a rate while actions succeed or lower it when they fail,
as the additive-increase, multiplicative-decrease rule of V. Jacobson,
"Congestion Avoidance and Control", ACM SIGCOMM 1988, does for a
transport's window.

It does not judge results. What counts as a failure for a Breaker, and
whether an error is worth a retry, is the caller's decision.

It does not keep a log of actions. Quota counts per fixed window and
Bucket refills continuously; a sliding window over a log of every action
is exact over every stretch but costs memory per action, and is not here.

It does not persist. A process that restarts starts every cap afresh,
with full buckets and closed breakers.

## Index

- `type` [Backoff](#backoff)
- `type` [Breaker](#breaker)
- `type` [Bucket](#bucket)
- `type` [Cooldown](#cooldown)
- `type` [Jitter](#jitter)
- `type` [Quota](#quota)
- `type` [State](#state)
- `method` [Backoff.Attempt](#backoffattempt)
- `method` [Backoff.Base](#backoffbase)
- `method` [Backoff.Cap](#backoffcap)
- `method` [Backoff.Ceiling](#backoffceiling)
- `method` [Backoff.Next](#backoffnext)
- `method` [Backoff.Reset](#backoffreset)
- `method` [Breaker.Allow](#breakerallow)
- `method` [Breaker.Failures](#breakerfailures)
- `method` [Breaker.Late](#breakerlate)
- `method` [Breaker.OpenUntil](#breakeropenuntil)
- `method` [Breaker.Record](#breakerrecord)
- `method` [Breaker.Reset](#breakerreset)
- `method` [Breaker.Since](#breakersince)
- `method` [Breaker.State](#breakerstate)
- `method` [Breaker.Transitions](#breakertransitions)
- `method` [Bucket.Allow](#bucketallow)
- `method` [Bucket.Allowed](#bucketallowed)
- `method` [Bucket.AllowN](#bucketallown)
- `method` [Bucket.Burst](#bucketburst)
- `method` [Bucket.Denied](#bucketdenied)
- `method` [Bucket.Interval](#bucketinterval)
- `method` [Bucket.Late](#bucketlate)
- `method` [Bucket.Next](#bucketnext)
- `method` [Bucket.NextN](#bucketnextn)
- `method` [Bucket.Rate](#bucketrate)
- `method` [Bucket.Reset](#bucketreset)
- `method` [Bucket.Tokens](#buckettokens)
- `method` [Cooldown.Allow](#cooldownallow)
- `method` [Cooldown.Allowed](#cooldownallowed)
- `method` [Cooldown.Denied](#cooldowndenied)
- `method` [Cooldown.Full](#cooldownfull)
- `method` [Cooldown.Late](#cooldownlate)
- `method` [Cooldown.Len](#cooldownlen)
- `method` [Cooldown.Next](#cooldownnext)
- `method` [Cooldown.Period](#cooldownperiod)
- `method` [Cooldown.Reset](#cooldownreset)
- `method` [Cooldown.Sweep](#cooldownsweep)
- `const` [ErrInvalid](#errinvalid)
- `method` [Jitter.String](#jitterstring)
- `func` [NewBackoff](#newbackoff)
- `func` [NewBreaker](#newbreaker)
- `func` [NewBucket](#newbucket)
- `func` [NewCooldown](#newcooldown)
- `func` [NewQuota](#newquota)
- `method` [Quota.Allow](#quotaallow)
- `method` [Quota.Allowed](#quotaallowed)
- `method` [Quota.AllowN](#quotaallown)
- `method` [Quota.Denied](#quotadenied)
- `method` [Quota.Late](#quotalate)
- `method` [Quota.Limit](#quotalimit)
- `method` [Quota.Remaining](#quotaremaining)
- `method` [Quota.Reset](#quotareset)
- `method` [Quota.Window](#quotawindow)
- `method` [Quota.WindowEnd](#quotawindowend)
- `method` [State.String](#statestring)

### Backoff

```go
type Backoff struct
```

Backoff draws the delays to wait between retries of a failed action:
growing, capped, and random, so that many callers that failed together do
not retry together.

Plain exponential backoff, base·2ⁿ before the n-th retry, spreads one
caller's retries out but keeps callers that failed at the same moment in
step: they retry at the same moments and collide again. Randomising each
delay breaks the lockstep. Of the schemes compared in M. Brooker,
"Exponential Backoff And Jitter", AWS Architecture Blog, 2015, this
provides the two the article recommends:

  - FullJitter: uniform on [0, min(cap, base·2ⁿ)]. The delays grow as plain
    backoff does, but each is anywhere from zero to that ceiling; in the
    article's simulation it needed the least total work.
  - DecorrelatedJitter: min(cap, uniform[base, 3·prev]), starting from
    prev = base. Each delay depends on the last instead of on the attempt
    number, grows by up to three times per retry, and never falls below
    base.

Exponential backoff itself is that of R. M. Metcalfe and D. R. Boggs,
"Ethernet: Distributed Packet Switching for Local Computer Networks",
Communications of the ACM 19(7), 1976, where a station that collides
waits a random number of slots drawn from a range that doubles with each
collision; IEEE 802.3 stops doubling after 10 collisions and gives up
after 16. This package stops at cap and never gives up: how many retries
to make is the caller's decision.

Randomness comes only from the source given to NewBackoff — a
*rand.Rand, or a rand.PCG or rand.ChaCha8 from math/rand/v2, seeded by the
caller — so a Backoff with a seeded source draws the same delays on every
run. Integers in a range are drawn without bias by the multiply-and-reject
method of D. Lemire, "Fast Random Integer Generation in an Interval", ACM
Transactions on Modeling and Computer Simulation 29(1), 2019. Delays are
whole nanoseconds; every value of the range, both ends included, is
equally likely before the cap is applied.

Backoff computes delays and does nothing else: it does not sleep, and it
takes no time because no delay depends on one. The caller waits, retries,
and calls Reset after a success.

The zero value is not usable (Next panics on it); build one with
NewBackoff. A copy shares the source with the original, and so draws from
the same stream. Not safe for concurrent use, and neither are the sources
math/rand/v2 provides.

<small>Defined in <code>throttle/backoff.go</code>.</small>

### Breaker

```go
type Breaker struct
```

Breaker stops calling something that keeps failing, and tries it again,
carefully, after a while.

It has three states:

  - Closed: Allow admits every call. Record counts consecutive failures; a
    success resets the count, and threshold failures in a row open the
    breaker.
  - Open: Allow refuses every call until openFor has passed since the
    breaker opened; then the breaker is half-open.
  - HalfOpen: Allow admits up to probes calls, and refuses the rest. If any
    probe fails, the breaker opens again for another openFor; once probes
    of them have succeeded, it closes.

Every change of state is counted in Transitions, and Since is the time of
the latest. The move from Open to HalfOpen happens at the first Allow,
Record or State call at or after the end of the open period, and Since is
that call's time.

Results are matched to calls only by count. In HalfOpen a Record counts as
a probe's result only while an admitted probe has not yet reported; any
other result — from a call admitted while the breaker was still closed,
arriving late — is ignored, as is every Record while the breaker is Open.
A probe whose result is never recorded holds its place, and once every
probe is held the breaker stays half-open; record a timed-out probe as a
failure.

Method: the Circuit Breaker pattern of M. T. Nygard, Release It! Design
and Deploy Production-Ready Software, 2nd ed., Pragmatic Bookshelf, 2018,
chapter 5 (stability patterns): closed, open and half-open, a failure
threshold to open, a timeout to try again, and a trial call to decide.
Allowing more than one trial (probes > 1) and requiring all of them to
succeed is a stricter form of the same rule.

A time before the latest one the breaker has seen is taken as the latest
(and counted in Late), so a clock stepped back cannot end an open period
early.

The zero value is not usable; build one with NewBreaker. Safe to copy,
not safe for concurrent use.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Bucket

```go
type Bucket struct
```

Bucket is a token bucket: it allows on average rate actions per second,
and up to burst of them at once after a quiet spell.

The bucket holds at most burst tokens and gains one every 1/rate seconds;
an action takes one token, or n for AllowN, and is refused when there are
not enough. Over any stretch of time of length t, then, at most
burst + ⌊t·rate⌋ tokens are taken: the bucket's whole content at the start
plus what it earned during the stretch. A test holds every Bucket to that
bound over every pair of its own allowed actions.

Method: the generic cell rate algorithm (GCRA) in its virtual-scheduling
form, ATM Forum, Traffic Management Specification Version 4.0
(af-tm-0056.000), 1996, and ITU-T Recommendation I.371, Traffic control
and congestion control in B-ISDN. Rather than a count of tokens that grows
with time, GCRA keeps a theoretical arrival time (TAT): the time at which
the bucket would be full again if nothing more were taken. A request for n
tokens at time t conforms when TAT + n·T − burst·T ≤ t, T being the
interval between tokens, and then moves TAT to max(TAT, t) + n·T. That is
the token bucket of A. S. Tanenbaum and D. J. Wetherall, Computer
Networks, 5th ed., Pearson, 2011, §5.4 (traffic shaping), counted in time
instead of tokens; the leaky bucket of J. S. Turner, "New directions in
communications (or which way to the information age?)", IEEE
Communications Magazine 24(10), 1986, is the same rule seen from the
other side.

Here TAT is kept as a debt: TAT minus the latest time, in whole
nanoseconds, between 0 and burst·T. T is 1/rate rounded once, in
NewBucket, to the nearest nanosecond (Interval reports it, Rate the rate it
gives); every step after that is integer addition, subtraction and
comparison, so a bucket run for years admits exactly what the arithmetic
says, with no rounding to accumulate and nothing to overflow.

A time before the latest one the bucket has seen is taken as the latest
(and counted in Late): a clock stepped back earns no tokens, and the
bucket resumes refilling only once the clock passes where it was.

The zero value refuses every action; build one with NewBucket. Safe to
copy, not safe for concurrent use.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Cooldown

```go
type Cooldown[K comparable] struct
```

Cooldown lets each key act at most once per period: an action for a key is
allowed when the key has not been allowed one in the last period, and is
then the start of the key's next period. "Restart any one service at most
once every ten minutes."

The rule is exact. An action at t for a key last allowed at s is allowed
if and only if t − s ≥ period; a refused action does not move s. It is
the same rule as a Bucket of burst 1 and one token per period kept for
each key, and a test holds the two to the same decisions.

A key is forgotten once its period has passed, since from then on it would
be allowed anyway. Keys are kept in a queue in the order they were
allowed, which is also the order their periods end, because times never
run backwards here; so expired keys are always at the front. Every Allow
first drops the expired keys at the front, which costs O(1) amortised per
key ever allowed, and the table holds only keys allowed within the last
period. Sweep does the same dropping at a time of the caller's choosing,
for a table that may sit idle while holding many keys.

maxKeys, when positive, caps the keys tracked at once. When the table is
full of keys still within their period, an action for a new key is
refused and counted in Full: no tracked key is evicted to make room,
because an evicted key could then act again before its period ended, and
a cap on corrective actions should fail by acting less, not more. The
cap bounds memory: Go maps do not return memory on deletion, so the
table's footprint is its largest size, at most maxKeys entries.

Allow on a key already tracked does not allocate, nor does Allow on a key
whose period has ended once the table has reached its working size: the
slots expired keys leave are reused. Growing the table allocates.

A time before the latest one the cooldown has seen is taken as the
latest (and counted in Late), so a clock stepped back cannot end a period
early.

The zero value is not usable (Allow panics on it: it has no table); build
one with NewCooldown, which returns a pointer. Not safe to copy (copies
share the table) and not safe for concurrent use.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### Jitter

```go
type Jitter uint8
```

Jitter selects how a Backoff randomises its delays.

<small>Defined in <code>throttle/backoff.go</code>.</small>

### Quota

```go
type Quota struct
```

Quota allows at most a fixed number of actions in each window of fixed
length: "no more than 5 restarts per hour, on the hour".

Windows are laid end to end from an origin the caller chooses: window k
is [origin + k·length, origin + (k+1)·length), for every integer k, so
windows before the origin exist too. A time on a boundary belongs to the
window it starts. The count starts again from zero at each boundary; a
window's unused actions do not carry over.

The guarantee is per window, not per stretch of time of the window's
length: actions taken at the end of one window and at the start of the
next fall in different windows, so a stretch of one window's length that
straddles a boundary can see up to twice the limit. Where that matters,
use a Bucket, whose bound holds over every stretch, or both.

Method: the jumping window of E. P. Rathgeb, "Modeling and performance
comparison of policing mechanisms for ATM networks", IEEE Journal on
Selected Areas in Communications 9(3), 1991, which also sets out the
boundary effect above. It suits quotas people read off a calendar or a
report — a count per hour, per day, per shift — that a token bucket's
continuous refill does not match.

A time before the latest one the quota has seen is taken as the latest
(and counted in Late), so a clock stepped back cannot reopen an earlier
window. Window boundaries are computed from time.Time.Sub of the origin,
which saturates about 292 years away from it.

The zero value is not usable (it panics); build one with NewQuota. Safe
to copy, not safe for concurrent use.

<small>Defined in <code>throttle/quota.go</code>.</small>

### State

```go
type State uint8
```

State is a Breaker's state.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Backoff.Attempt

```go
func (b *Backoff) Attempt() int
```

Attempt is the number of delays drawn since NewBackoff or Reset; it stops
at math.MaxInt.

<small>Defined in <code>throttle/backoff.go</code>.</small>

### Backoff.Base

```go
func (b *Backoff) Base() time.Duration
```

Base is the smallest scale of delay: the first FullJitter ceiling, and the
DecorrelatedJitter floor.

<small>Defined in <code>throttle/backoff.go</code>.</small>

### Backoff.Cap

```go
func (b *Backoff) Cap() time.Duration
```

Cap is the largest delay Next returns.

<small>Defined in <code>throttle/backoff.go</code>.</small>

### Backoff.Ceiling

```go
func (b *Backoff) Ceiling() time.Duration
```

Ceiling is the largest delay the next call to Next can return:
min(cap, base·2ⁿ) for FullJitter, n being Attempt, and min(cap, 3·prev)
for DecorrelatedJitter. It changes nothing.

<small>Defined in <code>throttle/backoff.go</code>.</small>

### Backoff.Next

```go
func (b *Backoff) Next() time.Duration
```

Next draws the delay before the next retry and counts the attempt. It does
not allocate.

<small>Defined in <code>throttle/backoff.go</code>.</small>

### Backoff.Reset

```go
func (b *Backoff) Reset()
```

Reset starts again from the first attempt, keeping the parameters and the
source; the source is not rewound.

<small>Defined in <code>throttle/backoff.go</code>.</small>

### Breaker.Allow

```go
func (b *Breaker) Allow(at time.Time) bool
```

Allow reports whether a call may be made at time at. In HalfOpen, a true
result admits a probe, whose result the caller must Record. It does not
allocate.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Breaker.Failures

```go
func (b *Breaker) Failures() int
```

Failures is the number of consecutive failures recorded while closed.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Breaker.Late

```go
func (b *Breaker) Late() int64
```

Late is the number of calls whose time was before the latest time the
breaker had seen, and was taken as that time.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Breaker.OpenUntil

```go
func (b *Breaker) OpenUntil() time.Time
```

OpenUntil is the end of the current open period, and the zero time when
the breaker is not open.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Breaker.Record

```go
func (b *Breaker) Record(ok bool, at time.Time)
```

Record reports the result of a call at time at: ok for success. It does
not allocate.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Breaker.Reset

```go
func (b *Breaker) Reset()
```

Reset closes the breaker, forgets every time it has seen and zeroes every
count, keeping the threshold, open duration and probes.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Breaker.Since

```go
func (b *Breaker) Since() time.Time
```

Since is the time the current state was entered: the zero time before the
first change.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Breaker.State

```go
func (b *Breaker) State(at time.Time) State
```

State is the state at time at. It moves an Open breaker whose period has
passed to HalfOpen, as Allow and Record do.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Breaker.Transitions

```go
func (b *Breaker) Transitions() int64
```

Transitions is the number of changes of state.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### Bucket.Allow

```go
func (b *Bucket) Allow(at time.Time) bool
```

Allow takes one token at time at and reports whether there was one. It
does not allocate.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.Allowed

```go
func (b *Bucket) Allowed() int64
```

Allowed is the number of AllowN calls that took tokens.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.AllowN

```go
func (b *Bucket) AllowN(at time.Time, n int) bool
```

AllowN takes n tokens at time at, all or none, and reports whether there
were n. An n larger than the burst is always refused; an n ≤ 0 takes
nothing, reports true and is not counted. It does not allocate.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.Burst

```go
func (b *Bucket) Burst() int
```

Burst is the bucket's capacity.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.Denied

```go
func (b *Bucket) Denied() int64
```

Denied is the number of AllowN calls that were refused.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.Interval

```go
func (b *Bucket) Interval() time.Duration
```

Interval is the time between tokens: 1/rate rounded to the nearest
nanosecond.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.Late

```go
func (b *Bucket) Late() int64
```

Late is the number of calls whose time was before the latest time the
bucket had seen, and was taken as that time.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.Next

```go
func (b *Bucket) Next(at time.Time) time.Time
```

Next is the earliest time, not before at (nor before the latest time the
bucket has seen), at which one token is available. It changes nothing.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.NextN

```go
func (b *Bucket) NextN(at time.Time, n int) (time.Time, bool)
```

NextN is the earliest time, not before at (nor before the latest time the
bucket has seen), at which AllowN(_, n) would succeed, if nothing else is
taken first. It reports false, and the zero time, when n exceeds the burst
and no time will do. It changes nothing.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.Rate

```go
func (b *Bucket) Rate() float64
```

Rate is the rate the bucket keeps, in tokens per second: 1e9 divided by
the Interval in nanoseconds, which can differ from the rate asked of
NewBucket by the rounding of the interval.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.Reset

```go
func (b *Bucket) Reset()
```

Reset refills the bucket, forgets every time it has seen and zeroes every
count, keeping the rate and burst.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Bucket.Tokens

```go
func (b *Bucket) Tokens(at time.Time) int
```

Tokens is the number of whole tokens in the bucket at time at, 0 to burst.
It changes nothing.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### Cooldown.Allow

```go
func (cd *Cooldown[K]) Allow(key K, at time.Time) bool
```

Allow reports whether key may act at time at, and if so starts its
period. It does not allocate unless the table grows.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### Cooldown.Allowed

```go
func (cd *Cooldown[K]) Allowed() int64
```

Allowed is the number of actions allowed.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### Cooldown.Denied

```go
func (cd *Cooldown[K]) Denied() int64
```

Denied is the number of actions refused, for either reason.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### Cooldown.Full

```go
func (cd *Cooldown[K]) Full() int64
```

Full is the number of actions refused only because the table was full: the
key itself was free to act.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### Cooldown.Late

```go
func (cd *Cooldown[K]) Late() int64
```

Late is the number of calls whose time was before the latest time the
cooldown had seen, and was taken as that time.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### Cooldown.Len

```go
func (cd *Cooldown[K]) Len() int
```

Len is the number of keys tracked: those allowed within the last period,
plus any whose period has ended since the last Allow or Sweep.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### Cooldown.Next

```go
func (cd *Cooldown[K]) Next(key K, at time.Time) time.Time
```

Next is the earliest time, not before at (nor before the latest time the
cooldown has seen), at which key may act, if nothing else acts first. For
a new key refused because the table is full, it is the time the oldest
tracked key's period ends. It changes nothing.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### Cooldown.Period

```go
func (cd *Cooldown[K]) Period() time.Duration
```

Period is the time a key must wait between actions.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### Cooldown.Reset

```go
func (cd *Cooldown[K]) Reset()
```

Reset forgets every key and every time and zeroes every count, keeping the
period, the cap and the memory already allocated.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### Cooldown.Sweep

```go
func (cd *Cooldown[K]) Sweep(at time.Time) int
```

Sweep forgets every key whose period has ended by at and returns how many
it forgot: O(1) per key forgotten. Allow does the same on every call, so
Sweep is needed only to release keys while no Allow is made.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("throttle: invalid parameter")
```

ErrInvalid is wrapped by every constructor error: a parameter that is NaN,
infinite where a finite value is needed, or outside the range its doc
names. Match it with errors.Is; the message text is not part of the API.

<small>Defined in <code>throttle/errors.go</code>.</small>

### Jitter.String

```go
func (j Jitter) String() string
```

String is "full" or "decorrelated".

<small>Defined in <code>throttle/backoff.go</code>.</small>

### NewBackoff

```go
func NewBackoff(j Jitter, base, cap time.Duration, src rand.Source) (Backoff, error)
```

NewBackoff returns a backoff of the given jitter with 0 < base ≤ cap,
drawing from src, which must not be nil.

<small>Defined in <code>throttle/backoff.go</code>.</small>

### NewBreaker

```go
func NewBreaker(threshold int, openFor time.Duration, probes int) (Breaker, error)
```

NewBreaker returns a closed breaker that opens after threshold ≥ 1
consecutive failures, stays open for openFor ≥ 0, and then admits probes ≥
1 trial calls, all of which must succeed for it to close.

<small>Defined in <code>throttle/breaker.go</code>.</small>

### NewBucket

```go
func NewBucket(rate float64, burst int) (Bucket, error)
```

NewBucket returns a full bucket that gains rate tokens per second and
holds at most burst. rate must be finite and positive with 1/rate between
1 ns and 2⁶³ ns once rounded to whole nanoseconds (about 2e9 down to one
per 292 years), and burst ≥ 1 with burst/rate below 2⁶³ ns.

<small>Defined in <code>throttle/bucket.go</code>.</small>

### NewCooldown

```go
func NewCooldown[K comparable](period time.Duration, maxKeys int) (*Cooldown[K], error)
```

NewCooldown returns a cooldown of period > 0 per key, tracking at most
maxKeys ≥ 0 keys at once; 0 is no cap.

<small>Defined in <code>throttle/cooldown.go</code>.</small>

### NewQuota

```go
func NewQuota(limit int, window time.Duration, origin time.Time) (Quota, error)
```

NewQuota returns a quota of limit ≥ 1 actions per window of length
window > 0, with windows aligned to origin.

<small>Defined in <code>throttle/quota.go</code>.</small>

### Quota.Allow

```go
func (b *Quota) Allow(at time.Time) bool
```

Allow takes one action from the window that holds at and reports whether
it was within the limit. It does not allocate.

<small>Defined in <code>throttle/quota.go</code>.</small>

### Quota.Allowed

```go
func (b *Quota) Allowed() int64
```

Allowed is the number of AllowN calls that took actions.

<small>Defined in <code>throttle/quota.go</code>.</small>

### Quota.AllowN

```go
func (b *Quota) AllowN(at time.Time, n int) bool
```

AllowN takes n actions from the window that holds at, all or none, and
reports whether they were within the limit. An n ≤ 0 takes nothing,
reports true and is not counted. It does not allocate.

<small>Defined in <code>throttle/quota.go</code>.</small>

### Quota.Denied

```go
func (b *Quota) Denied() int64
```

Denied is the number of AllowN calls that were refused.

<small>Defined in <code>throttle/quota.go</code>.</small>

### Quota.Late

```go
func (b *Quota) Late() int64
```

Late is the number of calls whose time was before the latest time the
quota had seen, and was taken as that time.

<small>Defined in <code>throttle/quota.go</code>.</small>

### Quota.Limit

```go
func (b *Quota) Limit() int
```

Limit is the number of actions allowed per window.

<small>Defined in <code>throttle/quota.go</code>.</small>

### Quota.Remaining

```go
func (b *Quota) Remaining(at time.Time) int
```

Remaining is the number of actions left in the window that holds at. It
changes nothing.

<small>Defined in <code>throttle/quota.go</code>.</small>

### Quota.Reset

```go
func (b *Quota) Reset()
```

Reset empties every window, forgets every time it has seen and zeroes
every count, keeping the limit, window and origin.

<small>Defined in <code>throttle/quota.go</code>.</small>

### Quota.Window

```go
func (b *Quota) Window() time.Duration
```

Window is the length of a window.

<small>Defined in <code>throttle/quota.go</code>.</small>

### Quota.WindowEnd

```go
func (b *Quota) WindowEnd(at time.Time) time.Time
```

WindowEnd is the end of the window that holds at: the time the count
starts again. It changes nothing.

<small>Defined in <code>throttle/quota.go</code>.</small>

### State.String

```go
func (s State) String() string
```

String is "closed", "open" or "half-open".

<small>Defined in <code>throttle/breaker.go</code>.</small>
