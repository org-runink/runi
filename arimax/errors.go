package arimax

import "errors"

var (
	errOrder   = errors.New("arimax: negative model order")
	errShort   = errors.New("arimax: series too short for the requested order")
	errExogLen = errors.New("arimax: exogenous rows do not match the series length")
	errFuture  = errors.New("arimax: forecasting with exogenous regressors requires future X for every step")
)
