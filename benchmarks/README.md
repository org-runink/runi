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

Medians of five runs for `runi`, three for Python, each itself a median over
repetitions.

| Operation | `runi` | Python | Faster by |
|---|---|---|---|
| ARIMAX fit, n=500 | **0.41 ms** | 15.06 ms — statsmodels | **37×** |
| ARIMAX fit, n=2,000 | **1.77 ms** | 51.13 ms — statsmodels | **29×** |
| ARIMAX fit, n=10,000 | **7.92 ms** | 260.3 ms — statsmodels | **33×** |
| OLS trend + t-test, n=100,000 | **0.272 ms** | 8.991 ms — `scipy.stats.linregress` | **33×** |
| Avro OCF write, 20,000 rows | **1.73 ms** | 15.35 ms — fastavro | **8.9×** |
| BM25 query ×200, 5,000 docs | **79.0 ms** | 648.5 ms — rank-bm25 | **8.2×** |
| Pearson correlation, n=200,000 | **1.06 ms** | 2.99 ms — `scipy.stats.pearsonr` | **2.8×** |
| Index 5,000 docs | **65.4 ms** | 166.7 ms — `sklearn` `TfidfVectorizer` | **2.6×** |
| Spearman correlation, n=200,000 | **17.9 ms** | 34.0 ms — scipy | **1.9×** |
| BM25 index build, 5,000 docs | **65.4 ms** | 91.2 ms — rank-bm25 | **1.4×** |
| Avro OCF read, 20,000 rows | **16.4 ms** | 18.0 ms — fastavro | **1.1×** |

Spearman and the BM25 index were both losses when this page was first written —
243.9 ms and 177.9 ms. `ranks` sorted through `sort.SliceStable`, paying for
reflection-based swaps, an interface call per comparison and stability it did not
need, since tied ranks are averaged; it now sorts by radix on the float's bit
pattern. The index hashed every token twice and allocated a map per document;
terms are now interned once into flat postings. Neither was a property of the
language, which is why they are no longer in the table below.

## Where `runi` loses

| Operation | `runi` | Python | Slower by | Why |
|---|---|---|---|---|
| Seasonal decomposition, n=4,000 | 1.74 ms | **0.182 ms** — statsmodels | **9.5×** | `seasonal_decompose` is a centred moving average in C loops. Given the same period and with the break search off, `season.Decompose` fits a straight line and a Fourier series by least squares, which costs more and gives something a moving average cannot: a model that extrapolates. |

**This comparison is deliberately narrowed to make it fair.** Our default
`Decompose` also runs a BIC-priced search for trend breaks, which
`seasonal_decompose` does not do at all, so comparing the defaults measured a
larger job and billed the difference to us as a loss on this one. The benchmark
passes `MaxChangepoints: -1` for the row above and reports the rest separately:

| `season.Decompose`, n=4,000 | |
|---|---|
| Period given, no break search — *what statsmodels does* | **1.74 ms** |
| …plus the BIC changepoint search (our default) | 18.5 ms |
| …plus detecting the period instead of being told it | 55.4 ms |
| `season.Period` detection on its own | 4.32 ms |

The honest summary: statsmodels is faster at the decomposition. `season` is for
the case where you do not already know the period or where the trend broke.

**On the absolutes.** The sub-millisecond rows move by up to ±40% between runs
on this host — `trend` was seen between 0.154 and 0.273 ms, `Pearson` between
0.75 and 1.09 ms — so treat the ratios as the result and the absolutes as
context. Allocation counts, file sizes and every accuracy figure below are
deterministic and repeat exactly.

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

## Event handling: the number that decides whether a handler keeps up

An event mesh — a Kubernetes controller, a webhook fan-in, a change feed — does
not receive distinct work. It receives the SAME object several times: once per
watch, per replica, per retry, per relist. So the question that decides whether
the handler keeps up is not how fast one handler runs. It is **how many times
the expensive part runs for work that was already in flight.**

2,000 events over 50 distinct objects, released in one burst, with a handler
that costs 2 ms of I/O. The ideal is 50 executions, one per object.

| Strategy | Handler ran | Wall |
|---|---|---|
| No deduplication | 2,000× | 5.03 ms |
| **A mutex and a map** | **1,880×** | 5.93 ms |
| `runi/memo` | **50×** | **3.18 ms** |

**The middle row is the point.** A mutex and a map is what almost everyone
writes, it is a correct cache, and in a burst it removed 6% of the redundant
work — because every goroutine that arrives while the first is still working
finds the map empty and starts again. The stampede is invisible in a
sequential test, which is why it survives review.

`memo` hit the ideal exactly, and finished *sooner* despite the coordination,
because it did a fortieth of the work. That is 40× fewer calls to whatever the
handler calls — an API server with a rate limit, a webhook, a model with a
bill — not 40× on a nanosecond.

The alternatives are implemented inside the benchmark rather than imported, so
this needs no dependency the module refuses to take. Reproduce with
`go run ./benchmarks/eventmesh`.

## Alongside a warehouse engine

`runi` does not replace Spark, Databricks or Snowflake, and nothing here
suggests it does. It removes the round trip for work that is too small to
deserve one.

The same Pearson correlation over 200,000 rows:

| | Time |
|---|---|
| `runi/stats` | **1.06 ms** |
| `scipy.stats.pearsonr` | 2.99 ms |
| SparkML `Correlation.corr`, warm session | 408.7 ms |
| SparkML, counting session start and import | 3.18 s |
| SparkML, counting the DataFrame build as well | 10.2 s |

**387× on compute alone**, and roughly 3,000× once the session start a caller
actually pays for is included. Spark earns that overhead back when the data does
not fit on one machine. At 200,000 rows it does, and the engine spent 7.0
seconds building a DataFrame for a calculation that takes about a millisecond.
These are a single run: session start-up is not something repetition measures
more precisely.

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

Results land in `benchmarks/results_*.json`. Those files hold **one** run.
The tables above are medians across five runs for `runi` and three for Python,
because a single run of the sub-millisecond rows lands anywhere in a ±40%
spread; the committed JSON is there so the shape of the output is inspectable,
not so a reader can match it digit for digit against a median.
