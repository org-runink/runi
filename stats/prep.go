// SPDX-License-Identifier: BSD-3-Clause

package stats

import (
	"errors"
	"fmt"
	"math"
)

// ErrNotFitted is returned by a scaler that has not been fitted to any data:
// the zero value, a struct literal, or one decoded from storage without going
// through [NewStandardiser] or [NewMinMaxScaler].
var ErrNotFitted = errors.New("stats: scaler has not been fitted")

// ErrInvalid is wrapped by every constructor error: a parameter that is NaN,
// infinite, or outside the range its doc names. Match it with errors.Is; the
// message text is not part of the API.
var ErrInvalid = errors.New("stats: invalid argument")

// Standardiser rescales a column to zero mean and unit standard deviation,
// z = (x − mean) / sd.
//
// Fit it on training data and apply it, unchanged, everywhere else. Its fields
// are exported so a fitted scaler can be stored alongside a model — a model is
// only valid for the scaling it was trained under. To reload one, pass the
// stored fields to [NewStandardiser]: a Standardiser built any other way,
// including a struct literal or one filled in by json.Unmarshal, is unfitted
// and its methods return [ErrNotFitted].
type Standardiser struct {
	Mean   float64
	StdDev float64
	fitted bool
}

// FitStandardiser computes the mean and sample standard deviation of x.
//
// A constant column has zero spread and cannot be standardised; its scaler
// leaves values unchanged rather than dividing by zero, so a constant feature
// becomes a column of zeros after centring instead of a column of NaNs that
// poisons everything downstream.
//
// An empty x gives an unfitted scaler.
func FitStandardiser(x []float64) *Standardiser {
	s := &Standardiser{fitted: len(x) > 0}
	if len(x) == 0 {
		return s
	}
	s.Mean = Mean(x)
	sd := StdDev(x)
	if math.IsNaN(sd) || sd == 0 {
		sd = 1 // see the doc comment: deliberate, not a fallback for an error
	}
	s.StdDev = sd
	return s
}

// NewStandardiser rebuilds a fitted Standardiser from a stored Mean and StdDev,
// for example ones saved with a model and decoded with encoding/json.
//
// mean must be finite and sd finite and positive, which holds for every scaler
// [FitStandardiser] fits to finite data: a constant column is stored with sd 1,
// never 0. Anything else returns an error wrapping [ErrInvalid].
func NewStandardiser(mean, sd float64) (Standardiser, error) {
	if math.IsNaN(mean) || math.IsInf(mean, 0) {
		return Standardiser{}, fmt.Errorf("%w: mean = %v, want finite", ErrInvalid, mean)
	}
	if !(sd > 0) || math.IsInf(sd, 0) {
		return Standardiser{}, fmt.Errorf("%w: sd = %v, want finite and > 0", ErrInvalid, sd)
	}
	return Standardiser{Mean: mean, StdDev: sd, fitted: true}, nil
}

// Transform applies the fitted scaling. It returns a new slice and does not
// modify x. An unfitted scaler returns ErrNotFitted rather than silently
// returning the input unchanged.
func (s *Standardiser) Transform(x []float64) ([]float64, error) {
	if s == nil || !s.fitted {
		return nil, ErrNotFitted
	}
	out := make([]float64, len(x))
	for i, v := range x {
		out[i] = (v - s.Mean) / s.StdDev
	}
	return out, nil
}

// Inverse undoes Transform, mapping standardised values back to the original
// units. Use it to report a prediction in the units a person understands.
func (s *Standardiser) Inverse(z []float64) ([]float64, error) {
	if s == nil || !s.fitted {
		return nil, ErrNotFitted
	}
	out := make([]float64, len(z))
	for i, v := range z {
		out[i] = v*s.StdDev + s.Mean
	}
	return out, nil
}

// MinMaxScaler rescales a column onto [0,1] by its observed range.
//
// It is the right choice when a bounded input is required and the wrong choice
// when outliers matter: one extreme value compresses everything else toward a
// single point. [Standardiser] is more robust to that; neither is robust to a
// value outside the fitted range, which Transform will map outside [0,1]
// rather than clip. Clipping silently would hide that the new data does not
// look like the training data, which is usually the most useful thing to know.
//
// As with [Standardiser], the fields are exported so a fitted scaler can be
// stored; reload one with [NewMinMaxScaler]. A struct literal or one filled in
// by json.Unmarshal is unfitted and its methods return [ErrNotFitted]. The
// fields alone cannot say whether a scaler was fitted, because fitting a column
// of zeros gives Min = Max = 0, which is also the zero value.
type MinMaxScaler struct {
	Min    float64
	Max    float64
	fitted bool
}

// FitMinMax computes the range of x. A constant column maps to zero. An empty x
// gives an unfitted scaler.
func FitMinMax(x []float64) *MinMaxScaler {
	s := &MinMaxScaler{fitted: len(x) > 0}
	if len(x) == 0 {
		return s
	}
	s.Min, s.Max = Min(x), Max(x)
	return s
}

// NewMinMaxScaler rebuilds a fitted MinMaxScaler from a stored Min and Max,
// for example ones saved with a model and decoded with encoding/json.
//
// Both must be finite and min must not exceed max, which holds for every scaler
// [FitMinMax] fits to finite data. min == max is allowed: it is how a constant
// column is stored, and it maps every value to zero. Anything else returns an
// error wrapping [ErrInvalid].
func NewMinMaxScaler(min, max float64) (MinMaxScaler, error) {
	if math.IsNaN(min) || math.IsInf(min, 0) || math.IsNaN(max) || math.IsInf(max, 0) {
		return MinMaxScaler{}, fmt.Errorf("%w: min = %v, max = %v, want finite", ErrInvalid, min, max)
	}
	if min > max {
		return MinMaxScaler{}, fmt.Errorf("%w: min = %v > max = %v", ErrInvalid, min, max)
	}
	return MinMaxScaler{Min: min, Max: max, fitted: true}, nil
}

// Transform maps x onto [0,1] using the fitted range. Values outside the
// fitted range map outside [0,1]; see the type's documentation.
func (s *MinMaxScaler) Transform(x []float64) ([]float64, error) {
	if s == nil || !s.fitted {
		return nil, ErrNotFitted
	}
	span := s.Max - s.Min
	out := make([]float64, len(x))
	if span == 0 {
		return out, nil // constant column: all zeros
	}
	for i, v := range x {
		out[i] = (v - s.Min) / span
	}
	return out, nil
}

// Inverse maps scaled values back to the original units.
func (s *MinMaxScaler) Inverse(z []float64) ([]float64, error) {
	if s == nil || !s.fitted {
		return nil, ErrNotFitted
	}
	span := s.Max - s.Min
	out := make([]float64, len(z))
	for i, v := range z {
		out[i] = v*span + s.Min
	}
	return out, nil
}

// SplitIndex returns the index at which to cut a series so that `frac` of it is
// training data, for an ORDERED split.
//
// Use this for time series and anything else where order carries information.
// Shuffling a time series before splitting lets the model see the future, which
// inflates every score that follows and is the single most common way a
// forecasting result turns out to be wrong.
//
// It returns 0 when there is not enough data to leave at least one observation
// on each side.
func SplitIndex(n int, frac float64) int {
	if n < 2 || frac <= 0 || frac >= 1 || math.IsNaN(frac) {
		return 0
	}
	k := int(math.Round(frac * float64(n)))
	if k < 1 {
		k = 1
	}
	if k > n-1 {
		k = n - 1
	}
	return k
}

// Split cuts x into a training and a test part at [SplitIndex], preserving
// order. The returned slices share x's backing array; copy them before
// modifying either one.
func Split(x []float64, frac float64) (train, test []float64) {
	k := SplitIndex(len(x), frac)
	if k == 0 {
		return nil, nil
	}
	return x[:k], x[k:]
}

// Fold is one split of a cross-validation: the index ranges to train on and to
// validate against.
type Fold struct {
	TrainStart, TrainEnd int // [TrainStart, TrainEnd)
	TestStart, TestEnd   int // [TestStart, TestEnd)
}

// RollingFolds returns k expanding-window folds over n ordered observations:
// train on everything up to a point, validate on what comes next, then move the
// point forward.
//
// This is cross-validation for data with an order. Standard k-fold shuffles,
// which for a time series means training on Thursday to predict Wednesday — the
// scores look excellent and mean nothing. Each fold here only ever trains on
// data that precedes its validation window.
//
// It returns nil when n is too small to make k folds that each have something
// to train on and something to test.
func RollingFolds(n, k int) []Fold {
	if n < 2 || k < 1 || k >= n {
		return nil
	}

	// That guard is doing more work than it looks. With 1 <= k <= n-1 we have
	// k+1 <= n, so size >= 1; and for the final fold size*k <= n*k/(k+1) < n,
	// so its test window is never empty. Every fold below is therefore valid by
	// construction and there is nothing left to check inside the loop.
	size := n / (k + 1)

	folds := make([]Fold, 0, k)
	for i := 1; i <= k; i++ {
		trainEnd := size * i
		testEnd := size * (i + 1)
		if i == k {
			testEnd = n // the last fold takes the remainder
		}
		folds = append(folds, Fold{
			TrainStart: 0, TrainEnd: trainEnd,
			TestStart: trainEnd, TestEnd: testEnd,
		})
	}
	return folds
}
