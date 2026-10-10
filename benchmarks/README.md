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
| ARIMAX fit, n=10,000 | **12.52 ms** | 244.9 ms — statsmodels | **19.6×** |
| ARIMAX fit, n=500 | **1.17 ms** | 14.78 ms — statsmodels | **12.6×** |
| ARIMAX fit, n=2,000 | **3.34 ms** | 49.93 ms — statsmodels | **15.0×** |
| BM25 query ×200, 5,000 docs | **3.92 ms** | 628.3 ms — rank-bm25 | **160×** |
| Pearson correlation, n=200,000 | **0.123 ms** | 5.15 ms — `scipy.stats.pearsonr` | **41.9×** |
| Avro OCF write, 20,000 rows | **1.10 ms** | 15.27 ms — fastavro | **13.9×** |
| Spearman correlation, n=200,000 | **3.51 ms** | 41.58 ms — scipy | **11.9×** |
| Index 5,000 docs | **19.3 ms** | 171.3 ms — `sklearn` `TfidfVectorizer` | **8.9×** |
| Avro OCF read, 20,000 rows | **1.96 ms** | 17.17 ms — fastavro | **8.8×** |
| Seasonal decomposition, n=4,000 | **0.0213 ms** | 0.1815 ms — statsmodels `seasonal_decompose` | **8.5×** |
| OLS trend + t-test, n=100,000 | **0.0465 ms** | 0.300 ms — `scipy.stats.linregress` | **6.4×** |
| BM25 index build, 5,000 docs | **19.3 ms** | 93.5 ms — rank-bm25 | **4.8×** |

Spearman and the BM25 index were both losses when this page was first written —
243.9 ms and 177.9 ms. `ranks` sorted through `sort.SliceStable`, paying for
reflection-based swaps, an interface call per comparison and stability it did not
need, since tied ranks are averaged; it now sorts by radix on the float's bit
pattern. The index hashed every token twice and allocated a map per document;
terms are now interned once into flat postings. Neither was a property of the
language, which is why they are no longer in the table below.

## Where the margin is thin

As of this revision there is no operation here that a Python library does
faster. That is a statement about this set of operations on this machine, not a
law, so the rows where we win by very little are listed in their own table
rather than buried among the large multiples — those are the ones most likely
to flip on your hardware, your data, or the next release of the library we are
measuring against.

| Operation | `runi` | Python | Faster by |
|---|---|---|---|
| The same seasonal split by least squares, n=4,000 | **0.155 ms** | 0.182 ms — statsmodels | **1.17×** |
| `toon` decode, 2,000 rows | **1.746 ms** | 1.806 ms — Go `encoding/json` | **1.03×** |

The seasonal row **used to be a 5.0× loss**. It is closed now, and closed
narrowly:
`season.Decompose` fits a straight line and a Fourier series by least squares,
which costs more than a centred moving average and buys something a moving
average cannot give you — a model that extrapolates. It is now 7.8× faster than
it was, which is what moved it across the line. If you want the operation
`seasonal_decompose` actually performs, that is `season.Classical` in the table
above, at 8.5×.

The `toon` decode row is a Go-against-Go comparison and wins by three per cent;
`toon` still allocates 42,030 times against `encoding/json`'s 28,022 for the
same document, and that gap is where any further work on it belongs. The reason
to reach for `toon` is the 47% smaller output, not the decode speed.

**The seasonal comparison took three goes to make honest.** First it compared our default
`Decompose`, which also runs a BIC-priced search for trend breaks that
`seasonal_decompose` does not do at all — a larger job, billed to us as a loss.
Passing `MaxChangepoints: -1` made the *timing* like for like but not the
*method*: a least-squares line plus a Fourier series is a different estimator
from a centred moving average, with a different answer. `season.Classical` is
now the moving-average method itself: 8.5× faster than `seasonal_decompose` and
agreeing with it to **2e-13** on a series of magnitude 300, which is a few ulps
of float64. Run `go run ./verify_classical && python verify_classical.py` to
check that for yourself; it writes both sides' components and compares them on
the interior, where a centred average is defined.

| `season`, n=4,000, period given | |
|---|---|
| `Classical` — *what statsmodels does, by its method* | **0.0213 ms** |
| `Decompose`, no break search — least squares + Fourier | 0.155 ms |
| …plus the BIC changepoint search (our default) | 16.9 ms |
| …plus detecting the period instead of being told it | 53.5 ms |
| `season.Period` detection on its own | 3.54 ms |

The honest summary: use `Classical` when you know the period, and it is faster
than statsmodels. `Decompose` is for the case where you do not already know the
period or where the trend broke, and it costs what it costs.

**On the absolutes.** The sub-millisecond rows move by up to ±40% between runs
on this host — `trend` was seen between 0.154 and 0.273 ms, `Pearson` between
0.75 and 1.09 ms, `season.Classical` between 0.025 and 0.089 ms over fourteen
runs — so treat the ratios as the result and the absolutes as context. Allocation counts, file sizes and every accuracy figure below are
deterministic and repeat exactly.

## Accuracy, which matters more than speed

Being faster at the wrong answer is not an achievement. Over 200 trials on
identical synthetic series, against statsmodels:

| | `runi/arimax` | statsmodels |
|---|---|---|
| Prediction-interval coverage | **94.5%** | **94.5%** |
| Forecast RMSE | 1.31600 | 1.31601 |
| AR coefficient RMSE | 0.05062 | 0.05062 |
| **Regression coefficient RMSE** | **0.03364** | **0.03361** |

Coverage is identical, forecasts differ by 0.001%, and the regression
coefficient — which used to be the one real loss on this page, at 0.0666
against 0.0336 — is now 0.1% apart.

**That row was closed, and the fix cost speed.** The old estimator was staged:
one least-squares solve for β, then one ARMA fit to whatever was left, which
estimates β as if the errors were independent. Unbiased, but inefficient, and
with ARMA(1,1) errors at φ=0.6, θ=0.3 the inefficiency is a factor of four in
variance — twice the standard error. `Fit` now alternates a prewhitened
generalised-least-squares solve with a warm-started ARMA refit, descending on
one conditional-sum-of-squares objective instead of fitting two blocks once
each. Fitting got about 2.9× slower, out of a roughly 40× margin, and the ARIMAX speed
rows above moved with it.

What remains different is the objective, not the precision: we minimise the
conditional sum of squares, statsmodels the exact likelihood through a Kalman
filter. At these series lengths that difference does not show up in any figure
in this table. Where statsmodels is still the tool is breadth — SARIMA, state
space models, diagnostics — not accuracy on this model.

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
| `runi/stats` | **0.701 ms** |
| `scipy.stats.pearsonr` | 5.14 ms |
| SparkML `Correlation.corr`, warm session | 282.9 ms |
| SparkML, counting session start and import | 2.57 s |
| SparkML, counting the DataFrame build as well | 8.46 s |

**403× on compute alone**, and roughly 3,700× once the session start a caller
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

cd benchmarks                       # these two read each other's files
go run ./verify_classical           # write both sides' seasonal components
python verify_classical.py          # ...and check them against statsmodels
```

Results land in `benchmarks/results_*.json`. Those files hold **one** run.
The tables above are medians across five runs for `runi` and three for Python,
because a single run of the sub-millisecond rows lands anywhere in a ±40%
spread; the committed JSON is there so the shape of the output is inspectable,
not so a reader can match it digit for digit against a median.
