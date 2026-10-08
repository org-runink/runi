# Benchmarks against the Python reference implementations

Everything here is reproducible. If a number in the root README disagrees with
what you get, the number is wrong and we want to hear about it.

## Why this is set up the way it is

The failure mode of a cross-language benchmark is that each side quietly
measures something different — its own data, its own model, its own definition
of "a fit". So:

1. **One generator.** `gen.py` writes the datasets to CSV once. Both
   implementations read the same files. Neither generates its own.
2. **One model.** ARIMAX(1,0,1) with a single exogenous regressor, on both sides.
3. **One control.** Each harness reports `naive_rmse`, the error of carrying the
   last training value forward. It depends only on the data, so if the two sides
   disagree on it, they were not fitted to the same numbers and nothing else in
   the output means anything. They currently agree to all 17 digits:
   `3.7104740961972245`.
4. **Known truth.** The data is synthetic *because* estimator bias cannot be
   measured without knowing what should have been recovered. This is the right
   choice for bias and the wrong choice for judging real-world accuracy, and the
   README says so.

## Running them

```bash
python -m venv .venv && . .venv/bin/activate
pip install numpy pandas statsmodels

python benchmarks/gen.py                 # writes benchmarks/data/*.csv
python benchmarks/bench_statsmodels.py   # -> results_statsmodels.json
go run ./benchmarks/runibench            # -> results_runi.json

python benchmarks/bench_lru.py           # -> results_lru.json
go run ./benchmarks/memobench            # -> results_memo.json
```

`benchmarks/data/` is gitignored: it is ~2.8MB, it is generated, and anything
inside a Go module tree ships in every published module zip.

## What each file does

| File | What it measures |
|---|---|
| `gen.py` | Writes the shared datasets and `meta.json` with the true parameters |
| `bench_statsmodels.py` | statsmodels SARIMAX: fit time at n=500/2k/10k, parameter recovery, interval coverage, forecast RMSE |
| `runibench/main.go` | The same, for `runi/arimax` |
| `bench_lru.py` | `functools.lru_cache`: hit, miss, and how many times the function runs under a stampede |
| `memobench/main.go` | The same experiment for `runi/memo` |

## The honest reading of the results

`runi/arimax` fits **42–54× faster** and produces forecasts that are
statistically indistinguishable from statsmodels': 0.07% apart in six-step RMSE,
with identical 94.5% empirical coverage of the nominal 95% intervals.

**statsmodels recovers the exogenous coefficient about twice as precisely**
(β RMSE 0.0336 vs 0.0666). That is the estimator, not a defect: statsmodels runs
exact maximum likelihood through a Kalman filter, `arimax` uses conditional sum
of squares on a staged regression. The speed and the precision loss are the same
trade, made once, and documented rather than buried.

If the coefficient is your finding, use statsmodels. If the forecast is your
finding and you need it inside a request rather than a nightly batch, that is
what this is for.

For `memo` the interesting number is not the nanoseconds. Under 64 concurrent
callers on a cold key, `lru_cache` runs the expensive function **64 times** and
`memo` runs it **once**. `lru_cache` never promised single-flight; that is
precisely why `memo` has it.

## Environment these were taken on

Recorded so that a different result is informative rather than confusing.

| | |
|---|---|
| CPU | AMD Ryzen 7 8840U, 16 threads |
| Memory | 25 GiB |
| OS | Linux 6.18 |
| Go | 1.25 |
| Python | 3.14.7 |
| statsmodels | 0.15.0 |
| numpy | 2.5.3 |

An unpinned laptop with other work on it. Treat the ratios as the finding and
the absolute times as approximate; the accuracy figures are deterministic given
the CSVs and do not move.
