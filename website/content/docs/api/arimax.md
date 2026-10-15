---
title: "runi/arimax"
weight: 2
---

```go
import "github.com/org-runink/runi/arimax"
```

Package arimax fits and forecasts ARIMAX models — ARIMA with exogenous
regressors — using only the Go standard library.

# The model

Y_t = c + Σ φ_i·Y_{t−i} + Σ θ_j·ε_{t−j} + Σ β_k·X_{t,k} + ε_t

φ are the autoregressive parameters, θ the moving-average parameters, β the
coefficients on the exogenous regressors X, and ε white noise.

# How it is fitted

This package uses the regression-with-ARIMA-errors formulation rather than
stuffing X into a single joint likelihood:

Y_t = β'X_t + n_t        where n_t follows an ARIMA(p,d,q)

The two are equivalent in what they express, but this form is far better
behaved numerically and each block is separately testable. Hyndman &
Athanasopoulos, Forecasting: Principles and Practice, recommends the same
decomposition, and it is why statsmodels calls the joint variant "SARIMAX"
and this one "ARIMA with exog".

# The estimator, precisely

Both blocks are fitted against ONE objective: the conditional sum of squares
of the ARIMA errors. Fit reaches it by block coordinate descent.

 1. Ordinary least squares gives a starting β, and an ARMA fit to the
    regression errors gives starting φ and θ.
 2. Those φ and θ define a whitening filter. Apply it to y and to every
    column of [1, X] and re-solve least squares on the filtered data. Because
    the filter is linear with fixed zero pre-sample values, the filtered
    residual IS the conditional residual, so this solve is the exact
    minimiser of the objective over β at those φ and θ. This is feasible
    generalised least squares — Cochrane–Orcutt generalised from AR(1)
    errors to ARMA(p,q).
 3. Refit the ARMA to the new errors, warm started, and repeat from 2 until
    the coefficients stop moving or four passes have run.

Neither half-step can raise the objective, so the whole iteration descends;
TestPropertyRefineNeverRaisesTheConditionalSumOfSquares measures that rather
than a runtime guard asserting it.

This matters because it is where the accuracy went. An earlier version of
this package STAGED the two blocks — one least-squares solve, one ARMA fit,
done — which estimates β as if the errors were independent. That is still
unbiased, but it is inefficient, and on the persistent errors this package
targets the inefficiency is large: with ARMA(1,1) errors at φ=0.6, θ=0.3 the
variance of the staged β is about four times the generalised one, so twice
the standard error. That factor was the whole of the measured accuracy gap
against an exact-likelihood fit, and closing it cost a factor of three in
speed out of a thirty-fold margin.

It is NOT exact maximum likelihood. The objective conditions on the first p
observations and on zero pre-sample shocks instead of running a Kalman filter
over the exact likelihood; the two differ by the information in those initial
conditions, which is O(1) terms against a sum over n. Where d > 0 the
intercept differences to zero, so it is not identified by the objective and
is left at its least-squares value — harmless for forecasting, where it
cancels through the integration, but it is not an estimate of a drift, and
this package has no drift parameter.

# Forecasting needs future X

This is the part that surprises people, and the package makes it explicit in
the API rather than hiding it: an ARIMAX forecast h steps ahead requires the
exogenous values for those h steps. Forecast takes them as an argument and
returns ErrNoFutureX if they are missing. Where X is genuinely known in advance
— planned spend, a published schedule, a committed price — this is a
strength. Where it is not, the X forecast's error enters the Y forecast and
the prediction intervals below UNDERSTATE the true uncertainty, because they
condition on X being correct. Say so wherever the output is shown.

# Numerical choices, stated

  - OLS is solved by Householder QR, not by inverting X'X. Channel spend
    series are strongly collinear and the normal equations square the
    condition number.
  - The ARMA block minimises the conditional sum of squares (CSS) by
    Nelder–Mead; the regression block minimises the same CSS exactly, by QR
    on prewhitened data. CSS conditions on the first p observations instead
    of running a Kalman filter for the exact likelihood. It needs no state
    space machinery, and for series of the length this package targets the
    difference from exact ML is small. It is not exact ML, and this package
    does not claim to be.
  - The AR polynomial is held inside Σ|φ| < 0.999. A fit that parks on that
    bound is telling you the series wanted a unit root — another difference,
    or a drift term this package does not have — and its reported Sigma2,
    and so its intervals, can collapse towards zero. Check AR against the
    bound before believing a very narrow interval.
  - Prediction intervals come from the MA(∞) psi-weights of the fitted
    model, so they widen with horizon in the right shape. They assume
    Gaussian errors and known parameters; they do not include parameter
    estimation error.

Everything is deterministic: same input, same output, no concurrency, no
randomness, no clock.

# Measured against statsmodels

200 independent synthetic series, ARMA(1,1) errors, one exogenous regressor,
n=500, h=6, both libraries reading the same generated CSVs. The control is
the naive carry-forward error, which depends on the data and on neither
library: 3.7104740961972245 against 3.710474096197225, which is the same
number to one unit in the last place — a difference of 1.2e-16, from the
order the two languages sum 200 values in. That is how the two are known to
have been fitted to identical data.

                              arimax    SARIMAX
fit, n=500                   1.35 ms   15.79 ms    12x faster
fit, n=2,000                 4.25 ms   53.79 ms    13x faster
fit, n=10,000               18.31 ms  267.83 ms    15x faster
per fit over the 200 trials   1.58 ms   16.32 ms    10x faster
start-up before first fit        0 ms     676 ms
95% interval coverage          94.5%      94.5%    identical
forecast RMSE, h=6           1.31600    1.31601    within 0.001%
AR phi bias / RMSE   -0.00410/0.05062  -0.00407/0.05062
beta bias / RMSE     +0.00249/0.03364  +0.00229/0.03361

The exogenous coefficient used to be the row to read first: a staged fit
estimated it with an RMSE of 0.06662, twice the exact-likelihood figure. With
the generalised-least-squares descent described above it is 0.03364 against
0.03361 — 0.1% apart, which is well inside what 200 trials can resolve. The
remaining difference between the two estimators is not visible in any figure
in this table.

What the ten-fold speed margin now buys is the same answer per request rather
than per batch. What it does not buy is exact ML, SARIMA, state space models,
or the diagnostics statsmodels ships; where those are the job, use it.

Measured on an ASUS Ascent GX10, 20 cores, aarch64, Go 1.27.2, statsmodels
0.15.0 on Python 3.12.3. See benchmarks/README.md in the repository for the
method and the raw results.

## Index

- `type` [Model](#model)
- `type` [Order](#order)
- `func` [ACF](#acf)
- `func` [Difference](#difference)
- `const` [ErrInvalid](#errinvalid)
- `const` [ErrNoFutureX](#errnofuturex)
- `const` [ErrNotFitted](#errnotfitted)
- `const` [ErrSingular](#errsingular)
- `const` [ErrTooShort](#errtooshort)
- `func` [Fit](#fit)
- `func` [LjungBox](#ljungbox)
- `func` [MAE](#mae)
- `func` [MAPE](#mape)
- `method` [Model.Forecast](#modelforecast)
- `func` [PACF](#pacf)
- `func` [RMSE](#rmse)
- `func` [SeasonalDifference](#seasonaldifference)

### Model

```go
type Model struct
```

Model is a fitted ARIMAX. Use Fit to build one: a Model built any other way,
including the zero value, holds none of the fitted state Forecast needs, and
Forecast returns ErrNotFitted for it.

<small>Defined in <code>arimax/arimax.go</code>.</small>

### Order

```go
type Order struct
```

Order is the (p,d,q) specification: autoregressive terms, differences, and
moving-average terms.

<small>Defined in <code>arimax/arimax.go</code>.</small>

### ACF

```go
func ACF(y []float64, maxLag int) []float64
```

ACF returns the sample autocorrelation function of y for lags 0..maxLag.
The value at index 0 is always 1. Use it to read the moving-average order:
an MA(q) series has an ACF that cuts off after lag q.

A constant series has zero variance, so every autocorrelation beyond lag 0 is
0/0 and undefined. Rather than return NaNs, ACF reports lag 0 as 1 — which is
true by definition and keeps the documented contract — and every later lag as
0, which reads as "no structure" to the order-selection it feeds.

<small>Defined in <code>arimax/acf.go</code>.</small>

### Difference

```go
func Difference(y []float64, d int) []float64
```

Difference applies d-th order differencing: the series of successive
changes, repeated d times. It is how a trending series is made stationary,
and it is the "I" in ARIMA.

Each pass shortens the series by one, so the result has len(y)−d elements.
Difference(y, 0) returns a copy.

<small>Defined in <code>arimax/diff.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("arimax: invalid argument")
```

ErrInvalid is wrapped by every argument error: a negative order, a series too
short for the order, exogenous rows that do not match the series, or missing
future regressors. Match it with errors.Is; the message text is not part of
the API.

<small>Defined in <code>arimax/errors.go</code>.</small>

### ErrNoFutureX

```go
var ErrNoFutureX error = &invalidError
```

ErrNoFutureX is returned by Model.Forecast when the model has exogenous
regressors and xFuture does not hold a row for every forecast step. It wraps
ErrInvalid.

<small>Defined in <code>arimax/errors.go</code>.</small>

### ErrNotFitted

```go
var ErrNotFitted = errors.New("arimax: model has not been fitted")
```

ErrNotFitted is returned by Model.Forecast on a Model that did not come from
Fit, such as the zero value or a nil pointer. Like stats.ErrNotFitted it
stands alone rather than wrapping ErrInvalid: the arguments may be fine, it
is the receiver that was never fitted.

<small>Defined in <code>arimax/errors.go</code>.</small>

### ErrSingular

```go
var ErrSingular = errors.New("arimax: design matrix is rank deficient")
```

ErrSingular is returned when the design matrix has no full-rank solution.

<small>Defined in <code>arimax/linalg.go</code>.</small>

### ErrTooShort

```go
var ErrTooShort error = &invalidError
```

ErrTooShort is returned by Fit when the series has too few points for the
requested order. It wraps ErrInvalid.

<small>Defined in <code>arimax/errors.go</code>.</small>

### Fit

```go
func Fit(y []float64, x []float64, k int, ord Order) (*Model, error)
```

Fit estimates an ARIMAX model of y with optional exogenous regressors.

x is row-major with one row per observation of y and k columns, or nil for a
plain ARIMA. The form fitted is regression with ARIMA errors, as described in
the package documentation; the estimator is conditional least squares over
the regression and ARMA parameters jointly, reached by alternating an exact
generalised-least-squares solve for the coefficients on prewhitened data
with a Nelder-Mead refit of the ARMA. It is not exact maximum likelihood.

<small>Defined in <code>arimax/arimax.go</code>.</small>

### LjungBox

```go
func LjungBox(resid []float64, h, dof int) (q float64, df int)
```

LjungBox computes the Ljung–Box Q statistic on the first h lags of the
residual autocorrelations, with dof degrees of freedom consumed by the fitted
model (p+q). A large Q means the residuals still carry structure, so the
model is misspecified.

It returns Q and the degrees of freedom. The package deliberately does not
return a p-value: that needs a chi-squared CDF, and shipping an approximation
of one invites people to read significance off a number that is only roughly
right. Compare Q against a chi-squared table with the returned dof.

<small>Defined in <code>arimax/acf.go</code>.</small>

### MAE

```go
func MAE(actual, pred []float64) float64
```

MAE is the mean absolute error between actual and predicted.

<small>Defined in <code>arimax/metrics.go</code>.</small>

### MAPE

```go
func MAPE(actual, pred []float64) (pct float64, skipped int)
```

MAPE is the mean absolute percentage error, in percent.

Observations where actual is zero are SKIPPED, not treated as infinite, and
the count of skipped points is returned. MAPE is undefined at zero and the
usual silent handling (dropping them, or letting the result become +Inf) has
misled people reading a dashboard. For channel data with genuine zero-spend
days this matters: check skipped before quoting the figure.

<small>Defined in <code>arimax/metrics.go</code>.</small>

### Model.Forecast

```go
func (m *Model) Forecast(h int, xFuture []float64, alpha float64) (point, lo, hi []float64, err error)
```

Forecast predicts h steps beyond the fitted series.

xFuture is row-major with h rows and the same number of columns as the X used
to fit, or nil when the model has no regressors. It is REQUIRED: an ARIMAX
forecast cannot be produced without the exogenous values for the horizon, and
this returns an error rather than silently assuming they are zero or held
flat — both of which produce a confident, wrong answer.

The returned intervals are (1−alpha) prediction intervals from the model's
psi-weights. They assume Gaussian errors and KNOWN parameters, and they
condition on xFuture being correct, so where xFuture is itself a forecast the
true uncertainty is wider than reported.

Missing or short xFuture returns ErrNoFutureX, which wraps ErrInvalid. A
Model that did not come from Fit returns ErrNotFitted. h <= 0 on a fitted
model returns all nil.

<small>Defined in <code>arimax/arimax.go</code>.</small>

### PACF

```go
func PACF(y []float64, maxLag int) []float64
```

PACF returns the sample partial autocorrelation function for lags 0..maxLag,
computed by the Durbin–Levinson recursion. Index 0 is 1. Use it to read the
autoregressive order: an AR(p) series has a PACF that cuts off after lag p.

<small>Defined in <code>arimax/acf.go</code>.</small>

### RMSE

```go
func RMSE(actual, pred []float64) float64
```

RMSE is the root mean squared error between actual and predicted.

<small>Defined in <code>arimax/metrics.go</code>.</small>

### SeasonalDifference

```go
func SeasonalDifference(y []float64, m int) []float64
```

SeasonalDifference applies one seasonal difference of period m: y_t − y_{t−m}.
Use it for weekly (m=7) or monthly (m=12) patterns before, or instead of,
ordinary differencing.

<small>Defined in <code>arimax/diff.go</code>.</small>
