# Benchmarks and accuracy

Every number here was produced by `go test` in this repository and can be
reproduced with the commands shown. Nothing is estimated, and the limits of each
measurement are stated next to it.

```
goos: linux   goarch: amd64   cpu: AMD Ryzen 7 8840U
go test -bench . -benchmem -benchtime=200x
```

> **These are dev-laptop figures, not capacity numbers.** They were taken on a
> Ryzen 7 8840U, not on the deployment hardware, and with no attempt to pin
> cores or quiet the machine. Use them to compare *shapes* — how cost grows with
> n, how many allocations a call makes — not to size a server.

## Speed

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `Fit` ARIMAX(1,0,1) + 1 regressor, n=100 | 182,294 | 148,018 | 321 |
| `Fit` ARIMAX(1,0,1) + 1 regressor, n=500 | 659,162 | 489,577 | 235 |
| `Fit` ARIMAX(1,0,1) + 1 regressor, n=2,000 | 2,275,325 | 2,033,850 | 245 |
| `Fit` ARIMAX(1,0,1) + 1 regressor, n=10,000 | 12,309,872 | 10,324,230 | 249 |
| `Forecast` 24 steps from a fitted n=2,000 model | 11,830 | 72,065 | 10 |
| `ACF` 40 lags over n=5,000 | 305,932 | 761 | **1** |
| `olsQR` n=5,000, 9 columns | 879,839 | 409,682 | 11 |

Reading these:

- **`Fit` is linear in n**, as it should be: 500 → 10,000 is a 20× increase in
  data for an 18.7× increase in time. The cost is dominated by repeated passes
  over the series inside the CSS objective, not by anything super-linear.
- **Allocation count is flat in n** (235–249 from n=500 up), because the work
  per Nelder–Mead iteration allocates a fixed number of slices regardless of
  series length. The n=100 case shows *more* allocations (321) because the
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
go test -run 'TestParameterRecovery|TestIntervalCoverage|TestForecastBeatsNaive' -v
```

### Parameter recovery — 200 series, n=500

| Parameter | true | bias | RMSE |
|---|---:|---:|---:|
| exogenous β | 2.50 | **−0.0005** | 0.0087 |
| AR(1) φ | 0.60 | −0.0079 | 0.0420 |

β is recovered essentially unbiased (−0.02% of its value) even though the errors
are strongly serially correlated — which is the entire reason for the staged
regression-with-ARIMA-errors fit. The small negative bias on φ is the known
downward bias of conditional-sum-of-squares AR estimation on finite samples; it
shrinks with n and is not corrected here.

### Prediction interval coverage — 300 series, 1,800 forecast points, h=1..6

**Nominal 95% intervals covered 97.4% of realised values.**

This is the number that matters when a forecast gates a decision. An interval
that claims 95% and delivers 70% is worse than no interval at all, because it
invites confident wrong answers. 97.4% is slightly *conservative* — intervals a
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
| ARIMAX(1,0,0) + 1 regressor | **0.6188** |
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
- **No exact maximum likelihood.** Parameters minimise the conditional sum of
  squares. CSS and exact ML agree closely for the series lengths benchmarked
  here, but they are not the same estimator and this package does not claim ML.
- **Gaussian errors are assumed** by the prediction intervals. Heavy-tailed
  residuals will produce intervals that are too narrow in the tails.

## Dependencies

```
$ cat go.mod
module github.com/org-runink/arimax
go 1.25
```

**Zero dependencies — standard library only**, verified by `go list -deps`. Test
coverage is **87.0% of statements**.

This is a deliberate constraint rather than an accident. A forecasting package
that pulls in a numerical framework becomes unusable in the places this one is
aimed at: a CLI, an agent sidecar, a WASM target, a device build. The cost is
that QR, Nelder–Mead and the inverse normal CDF are implemented here instead of
imported; each is a few dozen lines and each is covered by a test that checks it
against a known answer.
