// SPDX-License-Identifier: BSD-3-Clause

package drift

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

// ErrInvalid is wrapped by every constructor error: a parameter that is NaN,
// infinite, or outside the range its doc names. Match it with errors.Is; the
// message text is not part of the API.
var ErrInvalid = errors.New("drift: invalid parameter")

func invalid(name string, v float64, want string) error {
	return fmt.Errorf("%w: %s = %v, want %s", ErrInvalid, name, v, want)
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// Signal is what a detector says about the sample it was just given.
//
// It is a statistic crossing a threshold the caller chose, not a probability
// and not a verdict: Up means "the evidence for an upward shift is now larger
// than you said it had to be", and what to do about that is the caller's.
type Signal int

const (
	// None: no threshold crossed on this sample.
	None Signal = iota
	// Up: the evidence for an increase in level crossed its threshold.
	Up
	// Down: the evidence for a decrease in level crossed its threshold.
	Down
)

// String returns "none", "up" or "down", and "Signal(n)" for anything else.
func (s Signal) String() string {
	switch s {
	case None:
		return "none"
	case Up:
		return "up"
	case Down:
		return "down"
	}
	return "Signal(" + strconv.Itoa(int(s)) + ")"
}

// twoSided turns the two one-sided statistics of a detector into one Signal.
// When both are past their (shared) threshold the larger excess wins and a
// tie goes to Up: a tie between two continuous statistics is all but
// impossible on real data, and a fixed rule beats an unspecified one.
func twoSided(up, down, h float64) Signal {
	switch {
	case up > h && up >= down:
		return Up
	case down > h:
		return Down
	}
	return None
}
