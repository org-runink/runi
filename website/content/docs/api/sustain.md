---
title: "runi/sustain"
weight: 17
---

```go
import "github.com/org-runink/runi/sustain"
```

Package sustain keeps a decision made on a noisy series from flapping
around its threshold.

A check of the form "x > limit" run on every reading of a live series
changes its answer every time noise carries x across the limit, and
whatever the answer drives — an alert, a throttle, a scale-up — changes
with it. The remedies are old and well understood: two levels instead of
one, a condition that must persist before it counts, a majority over the
last few readings, a bound on how fast an output may move. This package
provides each as a small value type that takes one reading at a time,
with the time the reading was taken.

Which to use:

  - Hysteresis is an on/off state with a set level and a separate clear
    level (a Schmitt trigger), High or Low, and an optional minimum dwell
    in a state after each change. The deadband between the levels absorbs
    noise smaller than itself.
  - Debounce follows a boolean condition only after it has held for a time
    and/or a number of consecutive readings, with separate delays for on
    and off: the on-delay and off-delay of alarm management.
  - NofM is on while at least Need of the last Size readings (Size ≤ 64)
    are past a level: it escalates on a condition that holds most of the
    time, which a Debounce would keep restarting.
  - Band classifies a reading into ordered levels — below, inside, above;
    normal, warning, critical — with a deadband on every edge.
  - Slew bounds how fast an output follows its input, in units per second
    of the readings' own time, with separate rates up and down.

They compose. Debounce takes any condition, so a Hysteresis's state or a
Band's level compared with a constant can feed it; a Slew in front of any
of them smooths what they see, and one behind a controller smooths what it
does.

h, _ := sustain.NewHysteresis(sustain.High, 90, 80, time.Minute)
d, _ := sustain.NewDebounce(sustain.Delay{For: 30 * time.Second}, sustain.Delay{})
for r := range readings {
	if d.Update(h.Update(r.Value, r.At), r.At) {
		log.Printf("hot for %v", d.InState(r.At))
	}
}

Time comes only from the at argument of each Update; nothing here reads
the clock or sleeps, so every type is as testable as the caller's clock
is. Times are compared with time.Time.After and differenced with
time.Time.Sub, which use the monotonic reading time.Now attaches, so a
wall-clock step between two such readings does not distort an interval.

Two rules hold for every type, and tests hold each type to them:

  - A reading whose time is not after the last accepted reading — a
    duplicate, a clock stepped back, readings delivered out of order — is
    skipped. There is no right place to put a late reading in a state
    machine that has already acted on later ones, and a duplicate would
    count twice toward a number of readings.
  - A NaN or ±Inf reading is skipped. A missed reading should not decide a
    state, and "no reading" is not evidence for either side of a level.

A skipped reading is counted in Skipped and leaves no other trace: the
state, the time of the last reading and any run or window are exactly as
if it had never been offered. A series that is mostly skipped readings is
visible as one.

Every Update does O(1) work in fixed memory — Band's is O(levels crossed)
— and allocates nothing; a test holds each to zero. Every type is safe to
copy and none is safe for concurrent use; give each goroutine its own, or
guard it. Constructor errors wrap ErrInvalid.

# What this package does not do

It does not choose levels, deadbands or delays. Those depend on the
series' noise and on what a late or a false decision costs, which only the
caller knows. ISA-18.2 and its technical report on alarm design tabulate
starting deadbands and delays by measurement type for process plant
signals; a deadband a few times the noise's standard deviation, measured
over a stretch known to be normal (Welford in
github.com/org-runink/runi/drift), is the usual first try elsewhere.

It does not detect change. A deadband stops flapping around a level you
chose; whether the series' level has itself moved is a question for the
detectors in github.com/org-runink/runi/drift, whose output can then drive
a Debounce here.

It sees only readings. A Debounce's "held for 30 s" is "every reading for
30 s agreed"; a gap between readings is not seen, so a run of two readings
a minute apart has lasted a minute. A caller who needs to treat a long gap
as breaking a run should check it (Intervals in
github.com/org-runink/runi/rate watches gaps) and call Reset.

It does not reorder or interpolate. A late reading is dropped, not placed;
a Slew's output exists only at readings, and nothing between them is
computed.

It never acts. Nothing here alerts, throttles or retries; each type
returns a state or a number, and what follows from it is the caller's.

## Index

- `type` [Band](#band)
- `type` [Debounce](#debounce)
- `type` [Delay](#delay)
- `type` [Direction](#direction)
- `type` [Edge](#edge)
- `type` [Hysteresis](#hysteresis)
- `type` [NofM](#nofm)
- `type` [Slew](#slew)
- `method` [Band.Held](#bandheld)
- `method` [Band.InState](#bandinstate)
- `method` [Band.Level](#bandlevel)
- `method` [Band.Levels](#bandlevels)
- `method` [Band.Reset](#bandreset)
- `method` [Band.Since](#bandsince)
- `method` [Band.Skipped](#bandskipped)
- `method` [Band.Transitions](#bandtransitions)
- `method` [Band.Update](#bandupdate)
- `method` [Debounce.Active](#debounceactive)
- `method` [Debounce.InState](#debounceinstate)
- `method` [Debounce.Pending](#debouncepending)
- `method` [Debounce.Reset](#debouncereset)
- `method` [Debounce.Since](#debouncesince)
- `method` [Debounce.Skipped](#debounceskipped)
- `method` [Debounce.Transitions](#debouncetransitions)
- `method` [Debounce.Update](#debounceupdate)
- `method` [Direction.String](#directionstring)
- `const` [ErrInvalid](#errinvalid)
- `method` [Hysteresis.Active](#hysteresisactive)
- `method` [Hysteresis.Held](#hysteresisheld)
- `method` [Hysteresis.InState](#hysteresisinstate)
- `method` [Hysteresis.Reset](#hysteresisreset)
- `method` [Hysteresis.Since](#hysteresissince)
- `method` [Hysteresis.Skipped](#hysteresisskipped)
- `method` [Hysteresis.Transitions](#hysteresistransitions)
- `method` [Hysteresis.Update](#hysteresisupdate)
- `const` [MaxSize](#maxsize)
- `func` [NewBand](#newband)
- `func` [NewDebounce](#newdebounce)
- `func` [NewHysteresis](#newhysteresis)
- `func` [NewNofM](#newnofm)
- `func` [NewSlew](#newslew)
- `method` [NofM.Active](#nofmactive)
- `method` [NofM.Count](#nofmcount)
- `method` [NofM.InState](#nofminstate)
- `method` [NofM.Len](#nofmlen)
- `method` [NofM.Need](#nofmneed)
- `method` [NofM.Reset](#nofmreset)
- `method` [NofM.Since](#nofmsince)
- `method` [NofM.Size](#nofmsize)
- `method` [NofM.Skipped](#nofmskipped)
- `method` [NofM.Transitions](#nofmtransitions)
- `method` [NofM.Update](#nofmupdate)
- `method` [Slew.Limited](#slewlimited)
- `method` [Slew.Reset](#slewreset)
- `method` [Slew.Skipped](#slewskipped)
- `method` [Slew.Update](#slewupdate)
- `method` [Slew.Value](#slewvalue)

### Band

```go
type Band struct
```

Band classifies each reading into one of an ordered set of levels — below,
inside and above a band; normal, warning and critical — with hysteresis at
every boundary.

The levels are numbered 0 to len(edges), lowest values first, and edge k
separates level k from level k+1. Each edge is a Schmitt trigger with its
own pair of levels, so a reading that sits near a boundary does not flip
between the levels on either side of it: noise whose peak-to-peak amplitude
is less than an edge's Up − Down cannot carry the level across that edge
both ways. A reading may move the level several steps at once; a reading
strictly between an edge's Down and Up leaves that edge where it was.

For a band where the middle is normal, give two edges: level 0 is below,
1 inside, 2 above. For severities that rise with the value, level 0 is
normal and each edge is a more severe alarm; for severities that rise as
the value falls, number them from the top (severity = len(edges) − level).

The edges must be in order: both Up and Down strictly increasing from one
edge to the next. Then the level is always the number of edges whose
trigger is on, as if each edge were a separate Hysteresis, and the tests
hold Band to exactly that.

Dwell is an optional minimum time at a level after a change, as for
Hysteresis: a reading less than dwell after the last change cannot move the
level, is counted in Held, and is not remembered. It applies only after a
change; the first move from level 0 is never delayed. With a dwell, the
level is no longer the count of separate triggers, since one change holds
back every edge.

The level starts at 0. NaN and ±Inf readings are skipped, and so are
readings whose time is not after the last accepted reading; both are
counted in Skipped and leave no other trace.

Method: the multi-level alarm with a deadband on each setpoint of
ANSI/ISA-18.2-2016, Management of Alarm Systems for the Process Industries;
each edge is the trigger of O. H. Schmitt, "A thermionic trigger", Journal
of Scientific Instruments 15(1), 1938.

NewBand copies the edges; Update never changes them, so a copy of a Band
shares them safely and is independent of the original. The zero value
has no edges and is always at level 0; build one with NewBand. Not safe for
concurrent use.

<small>Defined in <code>sustain/band.go</code>.</small>

### Debounce

```go
type Debounce struct
```

Debounce is an on/off state that follows a boolean condition only once the
condition has held, continuously, for a set time or number of readings.

The condition is anything the caller decides per reading: a value over a
level, a check that failed, the output of a Hysteresis. A condition that
flickers — true for one reading in a run of false ones — is noise, and
Debounce ignores it: the state turns on only after the condition has been
true on every reading of a run that satisfies the on Delay, and turns off
only after it has been false on every reading of a run that satisfies the
off Delay. A single reading that agrees with the current state ends the run
and the count starts again. The two delays are separate because the costs
are: a late alarm and a late all-clear are different mistakes.

"Continuously" means as far as the readings show. The time a run has lasted
is the time between its first reading and the current one; what happened
between two readings is not seen, so a run of two readings an hour apart
has lasted an hour.

Readings whose time is not after the last accepted reading (duplicates, a
clock stepped back, readings delivered out of order) are skipped and
counted in Skipped; they neither extend nor break a run.

Method: the on-delay and off-delay timers of ANSI/ISA-18.2-2016, Management
of Alarm Systems for the Process Industries, where an alarm is annunciated
only after its condition has persisted for the on-delay and returns to
normal only after the return has persisted for the off-delay. The count of
consecutive readings is the sampled form of the same rule, the integrating
debouncer of switch-input practice.

The zero value is a Debounce with no delays, ready to use: it follows its
condition at every reading. Safe to copy, not safe for concurrent use.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### Delay

```go
type Delay struct
```

Delay is how long a condition must hold before a Debounce follows it: for
at least For, measured from the first reading of the run to the current
one, and over at least Readings consecutive readings. Both must be met when
both are set. The zero Delay is no delay: the state follows at the first
reading. Readings of 0 and 1 are the same requirement.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### Direction

```go
type Direction int8
```

Direction says which side of a level is the alarming one.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### Edge

```go
type Edge struct
```

Edge is one boundary between two adjacent levels of a Band, with its own
deadband: a reading at or above Up carries the level up past the edge, and
only a reading at or below Down carries it back. Down < Up.

<small>Defined in <code>sustain/band.go</code>.</small>

### Hysteresis

```go
type Hysteresis struct
```

Hysteresis is an on/off state with separate levels for turning on and
turning off: a Schmitt trigger.

A single threshold under noise flaps: a reading that sits near it crosses
it on every other sample, and every crossing is a change of state. Two
levels stop that. For a High alarm the state turns on at a reading at or
above set, and turns off only at a reading at or below clear, with
clear < set. Readings strictly between the two change nothing. The gap
set − clear is the deadband, in the data's units: noise whose peak-to-peak
amplitude is less than the deadband cannot carry a reading from one level
to the other, so it can cause at most one change, never a flap. Low is the
mirror image: on at or below set, off at or above clear, with set < clear.

Dwell is an optional minimum time in a state after a change, the minimum
on-time and off-time of relay and thermostat practice. A reading less than
dwell after the last change cannot change the state again; it is counted
in Held. It is not remembered either: once the dwell has passed, the state
changes at the first reading that is itself past the opposite level. The
dwell guards against a fast series that is truly crossing both levels
(a deadband narrower than the swings), which no deadband alone can stop.
It applies only after a change: the first change, from the initial off
state, is never delayed.

The state starts off. The first reading is judged like any other, so a
series that starts at or past set turns the state on at once.

NaN and ±Inf readings are skipped, and so are readings whose time is not
after the last accepted reading (duplicates, a clock stepped back, readings
delivered out of order); both are counted in Skipped and leave no other
trace.

Method: O. H. Schmitt, "A thermionic trigger", Journal of Scientific
Instruments 15(1), 1938. The deadband as an alarm setting:
ANSI/ISA-18.2-2016, Management of Alarm Systems for the Process
Industries, where it is the distance a signal must move back past the
setpoint before the alarm returns to normal (clear = set − deadband for a
high alarm).

The zero value is not usable; build one with NewHysteresis. Safe to copy,
not safe for concurrent use.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### NofM

```go
type NofM struct
```

NofM is on while at least Need of the last Size readings are past a level:
the N of M of its name.

It sits between a plain threshold, which fires on one reading, and a
Debounce, which needs every reading of a run: a condition that holds most
of the time but not always — three readings over in five, with one dip
back under between them — escalates here and would keep restarting a
Debounce. For a High level a reading is past it when it is at or above it;
for Low, at or below.

The last Size outcomes are kept as bits of one word, newest in the lowest
bit, so an update is a shift, a mask and a population count: no buffer and
no allocation, and the count is exact. Before Size readings have been
accepted the window holds those there are, and Need of them suffice. NofM
has no deadband of its own; the window is its smoothing.

NaN and ±Inf readings are skipped and take no slot, so "the last Size"
means the last Size finite readings. Readings whose time is not after the last
accepted reading are skipped too. Both are counted in Skipped.

Method: the M-of-N rule is the "binary integration" (double-threshold)
detector of radar, M. I. Skolnik, Introduction to Radar Systems,
McGraw-Hill; the same rule over a
control chart's zones gives the run rules of the Western Electric
Statistical Quality Control Handbook, 1956 ("two out of three beyond two
sigma", "four out of five beyond one sigma"), as tabulated in D. C.
Montgomery, Introduction to Statistical Quality Control, Wiley.

The zero value is not usable; build one with NewNofM. Safe to copy, not
safe for concurrent use.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### Slew

```go
type Slew struct
```

Slew is a rate limiter: its output moves toward its input, but by no more
than a set amount per second of the readings' own time.

A setpoint or a control output that jumps — a limit cut in half in one
step, a replica count that doubles — can do more harm by the jump than by
the new value. Slew passes the input through, but bounds how fast the
output may follow: over an interval of Δt seconds between readings it rises
by at most rise·Δt and falls by at most fall·Δt, and it never passes the
input. A step in the input becomes a ramp; an input that stays within reach
is followed exactly. Separate rates for rising and falling allow the usual
asymmetric forms, such as fast to back off and slow to return; either may be
+Inf, which removes the limit in that direction.

The bound is on the readings' timestamps, not on the number of readings:
readings close together move the output a little each, readings far apart
a lot. The first reading sets the output to the input, since there is no
earlier output to limit. The arithmetic is the bound itself: when limited,
the output is exactly old ± rate·Δt as one floating-point addition rounds
it, so it never moves farther than that rounded sum. A step smaller than
the spacing of floating-point numbers at the output's magnitude rounds to
no movement at all.

NaN and ±Inf readings are skipped, and so are readings whose time is not
after the last accepted reading; both are counted in Skipped and leave no
other trace. The next accepted reading's Δt then covers the skipped
interval, so the output may make up the movement it was allowed then.

Method: the rate limiter of control engineering, the nonlinear element that
bounds the derivative of a signal, treated among the actuator limitations
that cause integrator windup in K. J. Åström and T. Hägglund, Advanced PID
Control, ISA, 2006; here in sampled form, with the bound taken over each
irregular interval.

The zero value is not usable; build one with NewSlew. Safe to copy, not
safe for concurrent use.

<small>Defined in <code>sustain/slew.go</code>.</small>

### Band.Held

```go
func (b *Band) Held() int64
```

Held is the number of readings that would have changed the level but came
within the dwell.

<small>Defined in <code>sustain/band.go</code>.</small>

### Band.InState

```go
func (b *Band) InState(at time.Time) time.Duration
```

InState is how long the current level has lasted as of at, zero before the
first reading and for an at before Since.

<small>Defined in <code>sustain/band.go</code>.</small>

### Band.Level

```go
func (b *Band) Level() int
```

Level is the current level, 0 to Levels() − 1.

<small>Defined in <code>sustain/band.go</code>.</small>

### Band.Levels

```go
func (b *Band) Levels() int
```

Levels is the number of levels: one more than the number of edges.

<small>Defined in <code>sustain/band.go</code>.</small>

### Band.Reset

```go
func (b *Band) Reset()
```

Reset returns to level 0 before any reading and zeroes every count, keeping
the edges and the dwell.

<small>Defined in <code>sustain/band.go</code>.</small>

### Band.Since

```go
func (b *Band) Since() time.Time
```

Since is the time of the reading that entered the current level: the first
reading's time before any change, the zero time before any reading.

<small>Defined in <code>sustain/band.go</code>.</small>

### Band.Skipped

```go
func (b *Band) Skipped() int64
```

Skipped is the number of readings refused: non-finite, or not after the
last accepted reading.

<small>Defined in <code>sustain/band.go</code>.</small>

### Band.Transitions

```go
func (b *Band) Transitions() int64
```

Transitions is the number of changes of level; a move of several levels in
one reading counts once.

<small>Defined in <code>sustain/band.go</code>.</small>

### Band.Update

```go
func (b *Band) Update(x float64, at time.Time) int
```

Update judges reading x taken at time at and returns the level after it.
It does not allocate.

<small>Defined in <code>sustain/band.go</code>.</small>

### Debounce.Active

```go
func (d *Debounce) Active() bool
```

Active is the current state: true for on.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### Debounce.InState

```go
func (d *Debounce) InState(at time.Time) time.Duration
```

InState is how long the current state has lasted as of at, zero before the
first reading and for an at before Since.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### Debounce.Pending

```go
func (d *Debounce) Pending() bool
```

Pending reports whether the condition currently disagrees with the state:
a run is open that will change the state if it lasts.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### Debounce.Reset

```go
func (d *Debounce) Reset()
```

Reset returns to the off state before any reading and zeroes every count,
keeping the delays.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### Debounce.Since

```go
func (d *Debounce) Since() time.Time
```

Since is the time of the reading that entered the current state: the
reading that completed the run, not the run's first. Before any change it
is the first reading's time, and the zero time before any reading.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### Debounce.Skipped

```go
func (d *Debounce) Skipped() int64
```

Skipped is the number of readings not after the last accepted reading.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### Debounce.Transitions

```go
func (d *Debounce) Transitions() int64
```

Transitions is the number of changes of state.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### Debounce.Update

```go
func (d *Debounce) Update(cond bool, at time.Time) bool
```

Update reports the condition as read at time at and returns the state after
it: true for on. It does not allocate.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### Direction.String

```go
func (d Direction) String() string
```

String returns "high" or "low", and "Direction(n)" for anything else.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("sustain: invalid parameter")
```

ErrInvalid is wrapped by every constructor error: a parameter that is NaN,
infinite where a finite value is needed, or outside the range its doc
names. Match it with errors.Is; the message text is not part of the API.

<small>Defined in <code>sustain/errors.go</code>.</small>

### Hysteresis.Active

```go
func (h *Hysteresis) Active() bool
```

Active is the current state: true for on.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### Hysteresis.Held

```go
func (h *Hysteresis) Held() int64
```

Held is the number of readings past the opposite level that the dwell kept
from changing the state.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### Hysteresis.InState

```go
func (h *Hysteresis) InState(at time.Time) time.Duration
```

InState is how long the current state has lasted as of at, zero before the
first reading and for an at before Since.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### Hysteresis.Reset

```go
func (h *Hysteresis) Reset()
```

Reset returns to the off state before any reading and zeroes every count,
keeping the levels and the dwell.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### Hysteresis.Since

```go
func (h *Hysteresis) Since() time.Time
```

Since is the time of the reading that entered the current state: the first
reading's time before any change, the zero time before any reading.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### Hysteresis.Skipped

```go
func (h *Hysteresis) Skipped() int64
```

Skipped is the number of readings refused: non-finite, or not after the
last accepted reading.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### Hysteresis.Transitions

```go
func (h *Hysteresis) Transitions() int64
```

Transitions is the number of changes of state.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### Hysteresis.Update

```go
func (h *Hysteresis) Update(x float64, at time.Time) bool
```

Update judges reading x taken at time at and returns the state after it:
true for on. It does not allocate.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### MaxSize

```go
const MaxSize = 64
```

MaxSize is the largest window NofM keeps, the largest Size: one bit per
reading in a uint64.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NewBand

```go
func NewBand(dwell time.Duration, edges ...Edge) (Band, error)
```

NewBand returns a band at level 0 with the given edges, at least one, every
value finite, Down < Up within each edge and both strictly increasing from
edge to edge. dwell ≥ 0 is the minimum time at a level after a change; 0
means none.

<small>Defined in <code>sustain/band.go</code>.</small>

### NewDebounce

```go
func NewDebounce(on, off Delay) (Debounce, error)
```

NewDebounce returns an off state that turns on after the condition has held
for on and off after it has failed for off. Every field of both must be ≥ 0.

<small>Defined in <code>sustain/debounce.go</code>.</small>

### NewHysteresis

```go
func NewHysteresis(dir Direction, set, clear float64, dwell time.Duration) (Hysteresis, error)
```

NewHysteresis returns an off state for dir that turns on at set and off at
clear, both finite, with set > clear for High and set < clear for Low. dwell
≥ 0 is the minimum time in a state after a change; 0 means none.

<small>Defined in <code>sustain/hysteresis.go</code>.</small>

### NewNofM

```go
func NewNofM(dir Direction, level float64, need, size int) (NofM, error)
```

NewNofM returns a window over the last size readings, 1 ≤ size ≤ MaxSize,
that is on while at least need of them, 1 ≤ need ≤ size, are past level in
direction dir. level must be finite.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NewSlew

```go
func NewSlew(rise, fall float64) (Slew, error)
```

NewSlew returns a limiter that rises by at most rise and falls by at most
fall per second. Both must be > 0; +Inf means no limit in that direction.

<small>Defined in <code>sustain/slew.go</code>.</small>

### NofM.Active

```go
func (w *NofM) Active() bool
```

Active is the current state: true while at least Need of the last Size
readings are past the level.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NofM.Count

```go
func (w *NofM) Count() int
```

Count is how many of the readings in the window are past the level.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NofM.InState

```go
func (w *NofM) InState(at time.Time) time.Duration
```

InState is how long the current state has lasted as of at, zero before the
first reading and for an at before Since.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NofM.Len

```go
func (w *NofM) Len() int
```

Len is the number of readings in the window: those accepted since Reset,
up to Size.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NofM.Need

```go
func (w *NofM) Need() int
```

Need is the number of readings past the level, among the last Size, that
turns the state on.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NofM.Reset

```go
func (w *NofM) Reset()
```

Reset empties the window and zeroes every count, keeping the level, Need
and Size.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NofM.Since

```go
func (w *NofM) Since() time.Time
```

Since is the time of the reading that entered the current state: the first
reading's time before any change, the zero time before any reading.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NofM.Size

```go
func (w *NofM) Size() int
```

Size is the window's capacity in readings.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NofM.Skipped

```go
func (w *NofM) Skipped() int64
```

Skipped is the number of readings refused: non-finite, or not after the
last accepted reading.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NofM.Transitions

```go
func (w *NofM) Transitions() int64
```

Transitions is the number of changes of state.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### NofM.Update

```go
func (w *NofM) Update(x float64, at time.Time) bool
```

Update adds reading x taken at time at to the window and returns the state
after it: true while at least Need of the last Size readings are past the
level.
It does not allocate.

<small>Defined in <code>sustain/nofm.go</code>.</small>

### Slew.Limited

```go
func (s *Slew) Limited() bool
```

Limited reports whether the last accepted reading was out of reach, so the
output stopped short of it.

<small>Defined in <code>sustain/slew.go</code>.</small>

### Slew.Reset

```go
func (s *Slew) Reset()
```

Reset forgets the output and zeroes Skipped, keeping the rates.

<small>Defined in <code>sustain/slew.go</code>.</small>

### Slew.Skipped

```go
func (s *Slew) Skipped() int64
```

Skipped is the number of readings refused: non-finite, or not after the
last accepted reading.

<small>Defined in <code>sustain/slew.go</code>.</small>

### Slew.Update

```go
func (s *Slew) Update(x float64, at time.Time) float64
```

Update moves the output toward reading x taken at time at and returns the
new output. It returns Value, unchanged, for a skipped reading. It does not
allocate.

<small>Defined in <code>sustain/slew.go</code>.</small>

### Slew.Value

```go
func (s *Slew) Value() float64
```

Value is the current output, NaN before the first reading.

<small>Defined in <code>sustain/slew.go</code>.</small>
