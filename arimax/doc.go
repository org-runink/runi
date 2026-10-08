// Package arimax fits and forecasts ARIMAX models — ARIMA with exogenous
// regressors — using only the Go standard library.
//
// # The model
//
//	Y_t = c + Σ φ_i·Y_{t−i} + Σ θ_j·ε_{t−j} + Σ β_k·X_{t,k} + ε_t
//
// φ are the autoregressive parameters, θ the moving-average parameters, β the
// coefficients on the exogenous regressors X, and ε white noise.
//
// # How it is fitted
//
// This package uses the regression-with-ARIMA-errors formulation rather than
// stuffing X into a single joint likelihood:
//
//	Y_t = β'X_t + n_t        where n_t follows an ARIMA(p,d,q)
//
// The two are equivalent in what they express, but the staged form is far
// better behaved numerically and each stage is separately testable: ordinary
// least squares for β, then an ARMA fit to the regression errors. Hyndman &
// Athanasopoulos, Forecasting: Principles and Practice, recommends the same
// decomposition, and it is why statsmodels calls the joint variant "SARIMAX"
// and the staged one "ARIMA with exog".
//
// # Forecasting needs future X
//
// This is the part that surprises people, and the package makes it explicit in
// the API rather than hiding it: an ARIMAX forecast h steps ahead requires the
// exogenous values for those h steps. Forecast takes them as an argument and
// returns an error if they are missing. Where X is genuinely known in advance
// — planned spend, a published schedule, a committed price — this is a
// strength. Where it is not, the X forecast's error enters the Y forecast and
// the prediction intervals below UNDERSTATE the true uncertainty, because they
// condition on X being correct. Say so wherever the output is shown.
//
// # Numerical choices, stated
//
//   - OLS is solved by Householder QR, not by inverting X'X. Channel spend
//     series are strongly collinear and the normal equations square the
//     condition number.
//   - ARMA parameters minimise the conditional sum of squares (CSS) by
//     Nelder–Mead. CSS conditions on the first p observations instead of
//     running a Kalman filter for the exact likelihood. It is the standard
//     starting point, it needs no matrix algebra, and for series of the length
//     this package targets the difference from exact ML is small. It is not
//     exact ML, and this package does not claim to be.
//   - Prediction intervals come from the MA(∞) psi-weights of the fitted
//     model, so they widen with horizon in the right shape. They assume
//     Gaussian errors and known parameters; they do not include parameter
//     estimation error.
//
// Everything is deterministic: same input, same output, no concurrency, no
// randomness, no clock.
package arimax
