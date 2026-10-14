// SPDX-License-Identifier: BSD-3-Clause

package sustain

import "time"

// Delay is how long a condition must hold before a Debounce follows it: for
// at least For, measured from the first reading of the run to the current
// one, and over at least Readings consecutive readings. Both must be met when
// both are set. The zero Delay is no delay: the state follows at the first
// reading. Readings of 0 and 1 are the same requirement.
type Delay struct {
	For      time.Duration
	Readings int
}

func (d Delay) check(name string) error {
	if d.For < 0 {
		return invalid(name+".For", d.For, ">= 0")
	}
	if d.Readings < 0 {
		return invalid(name+".Readings", d.Readings, ">= 0")
	}
	return nil
}

// Debounce is an on/off state that follows a boolean condition only once the
// condition has held, continuously, for a set time or number of readings.
//
// The condition is anything the caller decides per reading: a value over a
// level, a check that failed, the output of a Hysteresis. A condition that
// flickers — true for one reading in a run of false ones — is noise, and
// Debounce ignores it: the state turns on only after the condition has been
// true on every reading of a run that satisfies the on Delay, and turns off
// only after it has been false on every reading of a run that satisfies the
// off Delay. A single reading that agrees with the current state ends the run
// and the count starts again. The two delays are separate because the costs
// are: a late alarm and a late all-clear are different mistakes.
//
// "Continuously" means as far as the readings show. The time a run has lasted
// is the time between its first reading and the current one; what happened
// between two readings is not seen, so a run of two readings an hour apart
// has lasted an hour.
//
// Readings whose time is not after the last accepted reading (duplicates, a
// clock stepped back, readings delivered out of order) are skipped and
// counted in Skipped; they neither extend nor break a run.
//
// Method: the on-delay and off-delay timers of ANSI/ISA-18.2-2016, Management
// of Alarm Systems for the Process Industries, where an alarm is annunciated
// only after its condition has persisted for the on-delay and returns to
// normal only after the return has persisted for the off-delay. The count of
// consecutive readings is the sampled form of the same rule, the integrating
// debouncer of switch-input practice.
//
// The zero value is a Debounce with no delays, ready to use: it follows its
// condition at every reading. Safe to copy, not safe for concurrent use.
type Debounce struct {
	on, off Delay
	state   bool
	pending bool      // a run of readings disagreeing with state is open
	runAt   time.Time // time of the run's first reading
	runN    int       // readings in the run, counted up to the requirement
	c       clock
}

// NewDebounce returns an off state that turns on after the condition has held
// for on and off after it has failed for off. Every field of both must be ≥ 0.
func NewDebounce(on, off Delay) (Debounce, error) {
	if err := on.check("on"); err != nil {
		return Debounce{}, err
	}
	if err := off.check("off"); err != nil {
		return Debounce{}, err
	}
	return Debounce{on: on, off: off}, nil
}

// Update reports the condition as read at time at and returns the state after
// it: true for on. It does not allocate.
func (d *Debounce) Update(cond bool, at time.Time) bool {
	if !d.c.accept(at) {
		return d.state
	}
	if cond == d.state {
		d.pending = false
		return d.state
	}
	need := d.on
	if d.state {
		need = d.off
	}
	if !d.pending {
		d.pending, d.runAt, d.runN = true, at, 0
	}
	if d.runN < need.Readings {
		d.runN++
	}
	if d.runN >= need.Readings && at.Sub(d.runAt) >= need.For {
		d.state, d.pending = cond, false
		d.c.change(at)
	}
	return d.state
}

// Active is the current state: true for on.
func (d *Debounce) Active() bool { return d.state }

// Pending reports whether the condition currently disagrees with the state:
// a run is open that will change the state if it lasts.
func (d *Debounce) Pending() bool { return d.pending }

// Since is the time of the reading that entered the current state: the
// reading that completed the run, not the run's first. Before any change it
// is the first reading's time, and the zero time before any reading.
func (d *Debounce) Since() time.Time { return d.c.since }

// InState is how long the current state has lasted as of at, zero before the
// first reading and for an at before Since.
func (d *Debounce) InState(at time.Time) time.Duration { return d.c.inState(at) }

// Transitions is the number of changes of state.
func (d *Debounce) Transitions() int64 { return d.c.changes }

// Skipped is the number of readings not after the last accepted reading.
func (d *Debounce) Skipped() int64 { return d.c.skipped }

// Reset returns to the off state before any reading and zeroes every count,
// keeping the delays.
func (d *Debounce) Reset() { *d = Debounce{on: d.on, off: d.off} }
