package arimax

import "errors"

// ErrInvalid is wrapped by every argument error: a negative order, a series too
// short for the order, exogenous rows that do not match the series, or missing
// future regressors. Match it with errors.Is; the message text is not part of
// the API.
var ErrInvalid = errors.New("arimax: invalid argument")

// ErrTooShort is returned by Fit when the series has too few points for the
// requested order. It wraps ErrInvalid.
var ErrTooShort error = &invalidError{"arimax: series too short for the requested order"}

// ErrNoFutureX is returned by Model.Forecast when the model has exogenous
// regressors and xFuture does not hold a row for every forecast step. It wraps
// ErrInvalid.
var ErrNoFutureX error = &invalidError{"arimax: forecasting with exogenous regressors requires future X for every step"}

// ErrNotFitted is returned by Model.Forecast on a Model that did not come from
// Fit, such as the zero value or a nil pointer. Like stats.ErrNotFitted it
// stands alone rather than wrapping ErrInvalid: the arguments may be fine, it
// is the receiver that was never fitted.
var ErrNotFitted = errors.New("arimax: model has not been fitted")

var (
	errOrder   error = &invalidError{"arimax: negative model order"}
	errExogLen error = &invalidError{"arimax: exogenous rows do not match the series length"}
)

// invalidError is an argument error that keeps its own message and matches
// ErrInvalid under errors.Is.
type invalidError struct{ msg string }

func (e *invalidError) Error() string { return e.msg }
func (e *invalidError) Unwrap() error { return ErrInvalid }
