// SPDX-License-Identifier: BSD-3-Clause

package sustain

import (
	"math"
	"time"
)

// Slew is a rate limiter: its output moves toward its input, but by no more
// than a set amount per second of the readings' own time.
//
// A setpoint or a control output that jumps — a limit cut in half in one
// step, a replica count that doubles — can do more harm by the jump than by
// the new value. Slew passes the input through, but bounds how fast the
// output may follow: over an interval of Δt seconds between readings it rises
// by at most rise·Δt and falls by at most fall·Δt, and it never passes the
// input. A step in the input becomes a ramp; an input that stays within reach
// is followed exactly. Separate rates for rising and falling allow the usual
// asymmetric forms, such as fast to back off and slow to return; either may be
// +Inf, which removes the limit in that direction.
//
// The bound is on the readings' timestamps, not on the number of readings:
// readings close together move the output a little each, readings far apart
// a lot. The first reading sets the output to the input, since there is no
// earlier output to limit. The arithmetic is the bound itself: when limited,
// the output is exactly old ± rate·Δt as one floating-point addition rounds
// it, so it never moves farther than that rounded sum. A step smaller than
// the spacing of floating-point numbers at the output's magnitude rounds to
// no movement at all.
//
// NaN and ±Inf readings are skipped, and so are readings whose time is not
// after the last accepted reading; both are counted in Skipped and leave no
// other trace. The next accepted reading's Δt then covers the skipped
// interval, so the output may make up the movement it was allowed then.
//
// Method: the rate limiter of control engineering, the nonlinear element that
// bounds the derivative of a signal, treated among the actuator limitations
// that cause integrator windup in K. J. Åström and T. Hägglund, Advanced PID
// Control, ISA, 2006; here in sampled form, with the bound taken over each
// irregular interval.
//
// The zero value is not usable; build one with NewSlew. Safe to copy, not
// safe for concurrent use.
type Slew struct {
	rise, fall float64
	out        float64
	limited    bool
	c          clock
}

// NewSlew returns a limiter that rises by at most rise and falls by at most
// fall per second. Both must be > 0; +Inf means no limit in that direction.
func NewSlew(rise, fall float64) (Slew, error) {
	if !(rise > 0) {
		return Slew{}, invalid("rise", rise, "> 0")
	}
	if !(fall > 0) {
		return Slew{}, invalid("fall", fall, "> 0")
	}
	return Slew{rise: rise, fall: fall}, nil
}

// Update moves the output toward reading x taken at time at and returns the
// new output. It returns Value, unchanged, for a skipped reading. It does not
// allocate.
func (s *Slew) Update(x float64, at time.Time) float64 {
	if !finite(x) {
		s.c.skipped++
		return s.Value()
	}
	first, prev := !s.c.started, s.c.last
	if !s.c.accept(at) {
		return s.Value()
	}
	s.limited = false
	if first {
		s.out = x
		return s.out
	}
	dt := at.Sub(prev).Seconds()
	switch {
	case x > s.out:
		if lim := s.out + s.rise*dt; x > lim {
			s.out, s.limited = lim, true
			return s.out
		}
	case x < s.out:
		if lim := s.out - s.fall*dt; x < lim {
			s.out, s.limited = lim, true
			return s.out
		}
	}
	s.out = x
	return s.out
}

// Value is the current output, NaN before the first reading.
func (s *Slew) Value() float64 {
	if !s.c.started {
		return math.NaN()
	}
	return s.out
}

// Limited reports whether the last accepted reading was out of reach, so the
// output stopped short of it.
func (s *Slew) Limited() bool { return s.limited }

// Skipped is the number of readings refused: non-finite, or not after the
// last accepted reading.
func (s *Slew) Skipped() int64 { return s.c.skipped }

// Reset forgets the output and zeroes Skipped, keeping the rates.
func (s *Slew) Reset() { *s = Slew{rise: s.rise, fall: s.fall} }
