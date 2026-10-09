# Benchmarks

Every number here was measured on one machine, on the same data, in the same
session, and every number we lose is in the table with the ones we win.

A benchmark that only shows the wins tells you nothing about whether to use the
library, because you cannot tell what it left out. These say where `runi` is
the right tool and where it is not.

## The machine

ASUS Ascent GX10 (NVIDIA GB10), 20 cores, 119 GB, `aarch64`, Linux 6.17.
Go 1.27.2. Python 3.12.3 with numpy 2.5.3, scipy 1.18.1, pandas 3.0.6,
scikit-learn 1.9.1, statsmodels 0.15.0, rank-bm25, fastavro 1.13.1,
PySpark 3.5.3.

Everything ran on CPU. Medians of repeated runs, warm-up discarded. The data is
generated, never captured: we publish code, not anyone's records.

Absolute figures are for this machine and this architecture. The ratios are the
portable part, and even those will move with your data.

## Where `runi` wins

| Operation | `runi` | Python | Faster by |
|---|---|---|---|
| ARIMAX fit, n=500 | **0.41 ms** | 15.18 ms — statsmodels | **37×** |
| ARIMAX fit, n=2,000 | **1.56 ms** | 51.48 ms — statsmodels | **33×** |
| ARIMAX fit, n=10,000 | **7.41 ms** | 257.13 ms — statsmodels | **35×** |
| OLS trend + t-test, n=100,000 | **0.15 ms** | 8.98 ms — `scipy.stats.linregress` | **58×** |
| Avro OCF write, 20,000 rows | **1.61 ms** | 15.04 ms — fastavro | **9.4×** |
| BM25 query ×200, 5,000 docs | **84.9 ms** | 737.5 ms — rank-bm25 | **8.7×** |
| Pearson correlation, n=200,000 | **0.68 ms** | 3.00 ms — `scipy.stats.pearsonr` | **4.4×** |
| Avro OCF read, 20,000 rows | **14.5 ms** | 17.4 ms — fastavro | **1.2×** |

## Where `runi` loses

| Operation | `runi` | Python | Slower by | Why |
|---|---|---|---|---|
| Seasonal decomposition, n=4,000 | 1.69 ms | **0.18 ms** — statsmodels | **9.4×** | `seasonal_decompose` is a moving average in C loops. `season.Decompose` fits a Fourier series by least squares, which costs more and gives you something a moving average cannot: a model that extrapolates. |
| Spearman correlation, n=200,000 | 243.9 ms | **33.1 ms** — scipy | **7.4×** | scipy ranks with an optimised C argsort. Ours is a plain sort. This is a real gap and the fix is ours to make, not a property of the language. |
| BM25 index build, 5,000 docs | 177.9 ms | **85.6 ms** — rank-bm25 | **2.1×** | We build more per document so queries are cheaper. Index once and query many times and we are ahead overall; index repeatedly and query rarely and we are not. |

`season.Decompose` with changepoint detection left on is 28.0 ms, because
finding structural breaks is most of the work. The 1.69 ms above is
decomposition only, which is the operation statsmodels performs.

## Accuracy, which matters more than speed

Being faster at the wrong answer is not an achievement. Over 200 trials on
identical synthetic series, against statsmodels:

| | `runi/arimax` | statsmodels |
|---|---|---|
| Prediction-interval coverage | **94.5%** | **94.5%** |
| Forecast RMSE | 1.3169 | 1.3160 |
| AR coefficient RMSE | 0.0508 | 0.0506 |
| **Regression coefficient RMSE** | **0.0666** | **0.0336** |

Coverage is identical and forecasts differ by 0.07%. The β estimates are
**twice as noisy**, and that is not a rounding difference: we fit by
conditional sum of squares where statsmodels runs exact maximum likelihood
through a Kalman filter. If you need the coefficients themselves — not the
forecast — statsmodels is the better tool today. Closing that gap is the open
work on `arimax`, and it is a bigger prize than any speed-up on this page.

## Alongside a warehouse engine

`runi` does not replace Spark, Databricks or Snowflake, and nothing here
suggests it does. It removes the round trip for work that is too small to
deserve one.

The same Pearson correlation over 200,000 rows:

| | Time |
|---|---|
| `runi/stats` | **0.68 ms** |
| scipy | 3.00 ms |
| SparkML `Correlation.corr`, warm session | 470 ms |
| SparkML, counting session start and import | 3.42 s |
| SparkML, counting the DataFrame build as well | 10.6 s |

**692× on compute alone**, and the gap widens to roughly 5,000× once the
session start a caller actually pays for is included. Spark earns that overhead
back when the data does not fit on one machine. At 200,000 rows it does not,
and the engine spends 7.2 seconds building a DataFrame for a calculation that
takes well under a millisecond.

That is the case for a Go library next to a warehouse: the SDK call, the
notebook cell and the service handler all pay cluster latency for small work.
`runi` is what you reach for when the answer is cheaper than the round trip.

## Running them

```sh
python benchmarks/gen.py            # synthesise the ARIMAX datasets
go run ./benchmarks/runibench       # runi/arimax
python benchmarks/bench_statsmodels.py

go run ./benchmarks/crossbench      # bm25, stats, season, avro
python benchmarks/baselines.py      # rank-bm25, scipy, statsmodels, fastavro
python benchmarks/spark_bench.py    # SparkML, needs a JVM
```

Results land in `benchmarks/results_*.json`, which is what the tables are built
from.
