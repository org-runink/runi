// SPDX-License-Identifier: BSD-3-Clause

package sustain

import (
	"strconv"
	"time"
)

// Direction says which side of a level is the alarming one.
type Direction int8

const (
	// High: readings at or above the level are the alarming ones (a
	// temperature too high, a queue too long).
	High Direction = 1
	// Low: readings at or below the level are the alarming ones (free
	// memory too low, a flow that has stopped).
	Low Direction = 2
)

// String returns "high" or "low", and "Direction(n)" for anything else.
func (d Direction) String() string {
	switch d {
	case High:
		return "high"
	case Low:
		return "low"
	}
	return "Direction(" + strconv.Itoa(int(d)) + ")"
}

// sign maps a Direction to the factor that turns a Low comparison into a
// High one: −x ≥ −level is x ≤ level. Negation is exact in floating point.
func (d Direction) sign() float64 {
	if d == Low {
		return -1
	}
	return 1
}

// Hysteresis is an on/off state with separate levels for turning on and
// turning off: a Schmitt trigger.
//
// A single threshold under noise flaps: a reading that sits near it crosses
// it on every other sample, and every crossing is a change of state. Two
// levels stop that. For a High alarm the state turns on at a reading at or
// above set, and turns off only at a reading at or below clear, with
// clear < set. Readings strictly between the two change nothing. The gap
// set − clear is the deadband, in the data's units: noise whose peak-to-peak
// amplitude is less than the deadband cannot carry a reading from one level
// to the other, so it can cause at most one change, never a flap. Low is the
// mirror image: on at or below set, off at or above clear, with set < clear.
//
// Dwell is an optional minimum time in a state after a change, the minimum
// on-time and off-time of relay and thermostat practice. A reading less than
// dwell after the last change cannot change the state again; it is counted
// in Held. It is not remembered either: once the dwell has passed, the state
// changes at the first reading that is itself past the opposite level. The
// dwell guards against a fast series that is truly crossing both levels
// (a deadband narrower than the swings), which no deadband alone can stop.
// It applies only after a change: the first change, from the initial off
// state, is never delayed.
//
// The state starts off. The first reading is judged like any other, so a
// series that starts at or past set turns the state on at once.
//
// NaN and ±Inf readings are skipped, and so are readings whose time is not
// after the last accepted reading (duplicates, a clock stepped back, readings
// delivered out of order); both are counted in Skipped and leave no other
// trace.
//
// Method: O. H. Schmitt, "A thermionic trigger", Journal of Scientific
// Instruments 15(1), 1938. The deadband as an alarm setting:
// ANSI/ISA-18.2-2016, Management of Alarm Systems for the Process
// Industries, where it is the distance a signal must move back past the
// setpoint before the alarm returns to normal (clear = set − deadband for a
// high alarm).
//
// The zero value is not usable; build one with NewHysteresis. Safe to copy,
// not safe for concurrent use.
type Hysteresis struct {
	sign       float64
	set, clear float64 // multiplied by sign
	dwell      time.Duration
	on         bool
	held       int64
	c          clock
}

// NewHysteresis returns an off state for dir that turns on at set and off at
// clear, both finite, with set > clear for High and set < clear for Low. dwell
// ≥ 0 is the minimum time in a state after a change; 0 means none.
func NewHysteresis(dir Direction, set, clear float64, dwell time.Duration) (Hysteresis, error) {
	switch {
	case dir != High && dir != Low:
		return Hysteresis{}, invalid("dir", dir, "High or Low")
	case !finite(set):
		return Hysteresis{}, invalid("set", set, "finite")
	case !finite(clear):
		return Hysteresis{}, invalid("clear", clear, "finite")
	case dir == High && !(set > clear):
		return Hysteresis{}, invalid("clear", clear, "< set for High")
	case dir == Low && !(set < clear):
		return Hysteresis{}, invalid("clear", clear, "> set for Low")
	case dwell < 0:
		return Hysteresis{}, invalid("dwell", dwell, ">= 0")
	}
	s := dir.sign()
	return Hysteresis{sign: s, set: s * set, clear: s * clear, dwell: dwell}, nil
}

// Update judges reading x taken at time at and returns the state after it:
// true for on. It does not allocate.
func (h *Hysteresis) Update(x float64, at time.Time) bool {
	if !finite(x) {
		h.c.skipped++
		return h.on
	}
	if !h.c.accept(at) {
		return h.on
	}
	x *= h.sign
	if h.on && x > h.clear || !h.on && x < h.set {
		return h.on
	}
	if !h.c.settled(at, h.dwell) {
		h.held++
		return h.on
	}
	h.on = !h.on
	h.c.change(at)
	return h.on
}

// Active is the current state: true for on.
func (h *Hysteresis) Active() bool { return h.on }

// Since is the time of the reading that entered the current state: the first
// reading's time before any change, the zero time before any reading.
func (h *Hysteresis) Since() time.Time { return h.c.since }

// InState is how long the current state has lasted as of at, zero before the
// first reading and for an at before Since.
func (h *Hysteresis) InState(at time.Time) time.Duration { return h.c.inState(at) }

// Transitions is the number of changes of state.
func (h *Hysteresis) Transitions() int64 { return h.c.changes }

// Held is the number of readings past the opposite level that the dwell kept
// from changing the state.
func (h *Hysteresis) Held() int64 { return h.held }

// Skipped is the number of readings refused: non-finite, or not after the
// last accepted reading.
func (h *Hysteresis) Skipped() int64 { return h.c.skipped }

// Reset returns to the off state before any reading and zeroes every count,
// keeping the levels and the dwell.
func (h *Hysteresis) Reset() {
	*h = Hysteresis{sign: h.sign, set: h.set, clear: h.clear, dwell: h.dwell}
}
