# Benchmarks and accuracy

Every number here was produced by `go test` in this repository and can be
reproduced with the commands shown. Nothing is estimated, and the limits of each
measurement are stated next to it.

```
goos: linux   goarch: arm64   cpu: NVIDIA GB10 (Cortex-X925 / A725, 20 cores)
go 1.27.2
go test -bench . -benchmem -benchtime=200x
```

> **These are one machine's figures, not capacity numbers.** They were taken on
> an ASUS Ascent GX10, with no attempt to pin cores or quiet the machine. Use
> them to compare *shapes* — how cost grows with n, how many allocations a call
> makes — not to size a server.

## Speed

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `Fit` ARIMAX(1,0,1) + 1 regressor, n=100 | 282,392 | 334,845 | 698 |
| `Fit` ARIMAX(1,0,1) + 1 regressor, n=500 | 930,527 | 1,052,902 | 486 |
| `Fit` ARIMAX(1,0,1) + 1 regressor, n=2,000 | 3,071,516 | 4,067,645 | 472 |
| `Fit` ARIMAX(1,0,1) + 1 regressor, n=10,000 | 15,919,885 | 21,223,140 | 505 |
| `Forecast` 24 steps from a fitted n=2,000 model | 18,671 | 72,070 | 10 |
| `ACF` 40 lags over n=5,000 | 168,499 | 761 | **1** |
| `olsQR` n=5,000, 9 columns | 696,365 | 409,705 | 11 |

Reading these:

- **`Fit` is linear in n**, as it should be: 500 → 10,000 is a 20× increase in
  data for a 17.1× increase in time. The cost is dominated by repeated passes
  over the series inside the CSS objective, not by anything super-linear.
- **`Fit` costs about three times what it did when the fit was staged.** It now
  alternates a prewhitened least-squares solve with a warm-started ARMA refit,
  up to four passes, instead of running each block once. That is where the β
  precision in the accuracy section came from, and it is bought out of a
  thirty-fold speed margin against `statsmodels` rather than out of nothing.
- **Allocation count is flat in n** (472–505 from n=500 up), because the work
  per Nelder–Mead iteration allocates a fixed number of slices regardless of
  series length. The n=100 case shows *more* allocations (698) because the
  simplex takes more iterations to converge on a short, noisy series.
- **`ACF` is 1 allocation** for any lag count — the output slice. Worth knowing
  if you are scanning many series for order selection.
- **`Forecast` is ~12 µs for 24 steps.** Forecasting is cheap; fitting is the
  cost. Fit once, forecast often.

## Accuracy

Accuracy of an *estimator* is measured by generating data from a model whose
true parameters you control, then reporting what comes back. These are over
independent synthetic series with AR(1) errors and one exogenous regressor.

```
go test -run 'TestParameterRecovery|TestIntervalCoverage|TestForecastBeatsNaive|TestBetaPrecision' -v
```

### Parameter recovery — 200 series, n=500

| Parameter | true | bias | RMSE |
|---|---:|---:|---:|
| exogenous β | 2.50 | **−0.0005** | 0.0087 |
| AR(1) φ | 0.60 | −0.0079 | 0.0420 |

β is recovered essentially unbiased (−0.02% of its value) even though the errors
are strongly serially correlated. The small negative bias on φ is the known
downward bias of conditional-sum-of-squares AR estimation on finite samples; it
shrinks with n and is not corrected here.

**This case does not discriminate between estimators, and that is worth saying
out loud.** The generator's x is a smooth sinusoid, so it is strongly
autocorrelated, and against autocorrelated errors ordinary least squares is
already close to efficient: β's RMSE here is 0.0087 both with the
generalised-least-squares refinement in `Fit` and with it removed. The
efficiency loss appears when x is *not* autocorrelated.

### β precision when x is independent — 200 series, n=500, ARMA(1,1) errors

With errors ARMA(1,1) at φ=0.6, θ=0.3 and unit-variance independent x, the
asymptotic standard error of β is **0.0673** if the errors are treated as
independent and **0.0325** if they are not — a factor of 2.07, which is theory,
not a measurement.

| Estimator | β bias | β RMSE |
|---|---:|---:|
| staged: one OLS solve, then one ARMA fit | +0.0059 | 0.0614 |
| `Fit` today: prewhitened GLS alternated with the ARMA refit | **−0.0005** | **0.0329** |

Both are unbiased; one has twice the standard error. This is the gap that used
to separate this package from an exact-likelihood fit, and
`TestBetaPrecisionUnderCorrelatedErrors` fails if it reopens.

### Prediction interval coverage — 300 series, 1,800 forecast points, h=1..6

**Nominal 95% intervals covered 97.3% of realised values.**

This is the number that matters when a forecast gates a decision. An interval
that claims 95% and delivers 70% is worse than no interval at all, because it
invites confident wrong answers. 97.3% is slightly *conservative* — intervals a
little wider than they strictly need to be — which is the safe direction to err.

The gap from 95% is expected and has a cause: the intervals use the psi-weight
variance with **known** parameters, so they omit parameter-estimation error,
while the CSS residual variance is itself slightly inflated on finite samples.
The second effect is the larger one here and pushes coverage up.

**This coverage holds only when the future exogenous values are correct.** Every
forecast above was given the true future X. Where X is itself forecast, its
error is not in these intervals and real coverage will be lower. See the package
documentation.

### Against the naive baseline — 100 held-out windows, h=6

| | mean RMSE |
|---|---:|
| ARIMAX(1,0,0) + 1 regressor | **0.6187** |
| naive (carry last value forward) | 3.4424 |

**82.0% lower error than naive.** A forecaster that cannot beat "tomorrow equals
today" is not earning its complexity, so this is a floor, not a boast — the
margin is large here mainly because the synthetic series has a strong exogenous
signal, which is exactly the case ARIMAX exists for.

## What these numbers do NOT show

Stated plainly, because benchmark sections usually imply more than they measured:

- **No head-to-head against other Go libraries was run.** This table contains
  only this package's own measurements. No claim is made that it is faster or
  more accurate than any alternative, because that comparison was not performed.
- **The accuracy figures are on synthetic data.** That is the correct way to
  measure estimator bias — you cannot measure bias without knowing the truth —
  but it is not evidence of accuracy on real marketing, financial or sensor
  series, where the model is misspecified by definition.
- **No exact maximum likelihood.** Every parameter minimises the conditional
  sum of squares — the regression block exactly, by QR on prewhitened data, the
  ARMA block by Nelder–Mead. CSS conditions on the first p observations and on
  zero pre-sample shocks; exact ML does not. They agree closely for the series
  lengths benchmarked here, but they are not the same estimator and this package
  does not claim ML.
- **No drift term.** With d > 0 the intercept differences away, so a
  deterministic trend left in the errors has nothing to absorb it but the AR
  root, which is then driven to the Σ|φ| < 0.999 bound. The fit's reported
  residual variance — and therefore its intervals — can collapse towards zero
  when that happens. Check `AR` against the bound before believing a very
  narrow interval.
- **Gaussian errors are assumed** by the prediction intervals. Heavy-tailed
  residuals will produce intervals that are too narrow in the tails.

## Dependencies

```
$ cat go.mod
module github.com/org-runink/runi
go 1.25
```

**Zero dependencies — standard library only**, verified by `go list -deps`. Test
coverage is **100.0% of statements**, enforced as a per-package floor in CI.

This is a deliberate constraint rather than an accident. A forecasting package
that pulls in a numerical framework becomes unusable in the places this one is
aimed at: a CLI, an agent sidecar, a WASM target, a device build. The cost is
that QR, Nelder–Mead and the inverse normal CDF are implemented here instead of
imported; each is a few dozen lines and each is covered by a test that checks it
against a known answer.
