<p align="center">
  <img src="https://raw.githubusercontent.com/org-runink/runi/main/assets/runi-logo.jpg" alt="Arlo, the Runink Australian Shepherd, wearing the Runi rig: AR goggles and a powered herding harness" width="220">
</p>

<h1 align="center">Runi</h1>

<p align="center">
  <strong>Arlo does the herding. Runi is the rig he wears to do it.</strong>
</p>

<p align="center">
  <em>Goggles to see what is coming. A harness to work close to the metal.<br>
  Eighteen small Go packages, zero dependencies, every claim measured.</em>
</p>

<p align="center">
  <a href="https://github.com/org-runink/runi/actions/workflows/ci.yml"><img src="https://github.com/org-runink/runi/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/org-runink/runi/actions/workflows/govulncheck.yml"><img src="https://github.com/org-runink/runi/actions/workflows/govulncheck.yml/badge.svg" alt="govulncheck"></a>
  <a href="https://github.com/org-runink/runi/actions/workflows/codeql.yml"><img src="https://github.com/org-runink/runi/actions/workflows/codeql.yml/badge.svg" alt="CodeQL"></a>
  <a href="https://github.com/org-runink/runi/actions/workflows/scorecard.yml"><img src="https://github.com/org-runink/runi/actions/workflows/scorecard.yml/badge.svg" alt="OpenSSF Scorecard"></a>
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/org-runink/runi"><img src="https://pkg.go.dev/badge/github.com/org-runink/runi.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/org-runink/runi"><img src="https://goreportcard.com/badge/github.com/org-runink/runi" alt="Go Report Card"></a>
  <img src="https://img.shields.io/badge/go-1.24%20%7C%201.25%20%7C%201.26-00ADD8" alt="Go 1.24 | 1.25 | 1.26">
  <img src="https://img.shields.io/badge/dependencies-0-success" alt="zero dependencies">
  <img src="https://img.shields.io/badge/packages-18-informational" alt="eighteen packages">
  <img src="https://img.shields.io/badge/coverage-100%25-brightgreen" alt="coverage">
  <img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="BSD-3-Clause">
</p>

---

**The Data & AI package for agentic harnesses running on Kubernetes or Linux environments.**
Standard library only — zero dependencies, in any package.

Engineered for high-throughput, low-latency microservices, local inference servers (Ollama, vLLM, llama.cpp), and modular agentic harnesses. Runi brings statistical analysis, BM25 text ranking, time-series forecasting, and LLM output repair directly into your Go handler. Eliminate heavy container images, sidecar RPC round-trips, and cluster overhead — execute data processing and agent loops natively at sub-millisecond speeds inside Kubernetes pods and Linux edge nodes.

```bash
go get github.com/org-runink/runi
```

| Package | One line | Coverage |
|---|---|---|
| [`runi/stats`](#runistats--the-first-ten-minutes) | Describe, split and scale a column, and test whether a relationship is real | **100%** |
| [`runi/arimax`](#runiarimax--forecasting-with-external-drivers) | Forecast a series using the things that drive it | **100%** |
| [`runi/season`](#runiseason--what-repeats-and-where-it-broke) | Find the season and the trend breaks, then split the series apart — fitted, or by moving average | **100%** |
| [`runi/bm25`](#runibm25--search-without-a-model) | Rank documents by the words they share | **100%** |
| [`runi/salvage`](#runisalvage--json-out-of-a-models-reply) | Get JSON out of a language model's reply, without guessing | **100%** |
| [`runi/avro`](#runiavro--apache-avro-without-the-dependency-tree) | Read and write Avro and Avro OCF files | **100%** |
| [`runi/barn`](#runibarn--versioned-tables-on-any-object-store) | Append-only versioned tables with time travel, no database | **100%** |
| [`runi/memo`](#runimemo--memoization-with-single-flight) | Don't compute the same thing twice | **100%** |
| [`runi/lazy`](#runilazy--deferred-values-you-can-start-early) | Compute it before anyone asks | **100%** |
| [`runi/budget`](#runibudget--one-deadline-shared-honestly) | Split one deadline between the steps of a request, and say which ran out | **100%** |
| [`runi/chain`](#runichain--records-nobody-can-quietly-rewrite) | Seal records so an edit, a move or a swap shows, and say which | **100%** |
| [`runi/toon`](#runitoon--the-table-format-models-write) | Read and write TOON, the token-frugal table format | **100%** |
| [`runi/toml`](#runitoml--configuration-files-read-strictly) | Read TOML configuration files into structs, strictly when it matters | **100%** |
| [`runi/drift`](#runidrift--when-a-series-changes-regime) | Notice the moment a stream of readings changes regime, as each one arrives | **100%** |
| [`runi/rate`](#runirate--counters-into-rates-across-wraps-and-resets) | Turn cumulative counters into per-interval rates, across wraps and resets | **100%** |
| [`runi/tree`](#runitree--which-part-of-the-hierarchy-is-responsible) | Keep subtree sums and maxima of a hierarchy current, and name the heaviest parts | **100%** |
| [`runi/sustain`](#runisustain--decisions-that-do-not-flap) | Keep a threshold decision on a noisy series from flapping | **100%** |
| [`runi/throttle`](#runithrottle--acting-less-often-than-you-are-asked-to) | Cap how often something acts: per interval, per target, and after failures | **100%** |

They share a design stance rather than any code: **zero dependencies,
deterministic, and honest about what they do not do.** Each one documents its own
limits, and every performance or accuracy claim below has a test that fails when
it stops being true.

---

## Meet Arlo, and the rig he wears

**Arlo** is Runink's Australian Shepherd. Herding is what he is for: keep the
flock moving in one direction, notice the one that has wandered, and do it
without being told twice.

**Runi** is his rig — the goggles and the powered harness in the artwork above.
It does not do the herding. It makes the herder better at it: he sees further,
remembers the ground he has already covered, and can work right down among the
machinery without slowing down.

That is the whole design brief for these packages. They are **equipment, not a
framework.** Nothing here takes over your program's structure, starts a
goroutine you did not ask for, or reaches the network. You put the gear on, and
you are still the one doing the work.

| The gear | The package | What it gives you |
|---|---|---|
| 👁️ **the eyes** | [`stats`](#runistats--the-first-ten-minutes) | Look at the flock before doing anything: how many, how spread out, how they move together |
| 🥽 **the goggles** | [`arimax`](#runiarimax--forecasting-with-external-drivers) | See what is coming — and how far ahead the view can honestly be trusted |
| 👃 **the nose** | [`bm25`](#runibm25--search-without-a-model) | Find the one you were asked for, by name, among thousands |
| 🦴 **the mouth** | [`salvage`](#runisalvage--json-out-of-a-models-reply) | Bring back exactly what was asked for, and drop what was not |
| 📦 **the pack** | [`avro`](#runiavro--apache-avro-without-the-dependency-tree) | Carry it somewhere else, in a format other tools already read |
| 🧠 **the memory core** | [`memo`](#runimemo--memoization-with-single-flight) | Never chase the same thing twice, even when sixty callers ask at once |
| ⚡ **the harness** | [`lazy`](#runilazy--deferred-values-you-can-start-early) | Already moving before the call comes, without computing what is never asked for |
| ⏱️ **the pace** | [`budget`](#runibudget--one-deadline-shared-honestly) | Know how long is left, and turn for home in time to deliver |
| 🏷️ **the tags** | [`chain`](#runichain--records-nobody-can-quietly-rewrite) | Every stop on the route stamped and linked to the last, so a missing one shows |
| 👂 **the ears** | [`drift`](#runidrift--when-a-series-changes-regime) | Hear the moment the flock's footing changes, without staring at every step |
| 🫀 **the heartbeat** | [`rate`](#runirate--counters-into-rates-across-wraps-and-resets) | Know how fast things are going between two glances, even when the counter rolls over |
| 🗺️ **the map** | [`tree`](#runitree--which-part-of-the-hierarchy-is-responsible) | Know which part of the flock, and which stray within it, is pulling everyone off course |
| 🐾 **the paws** | [`sustain`](#runisustain--decisions-that-do-not-flap) | Plant the feet and turn only when the ground has really changed, not at every gust |
| 📜 **the orders** | [`toml`](#runitoml--configuration-files-read-strictly) | Read the instructions it was sent out with, exactly as written, and refuse a garbled line |
| 🦮 **the leash** | [`throttle`](#runithrottle--acting-less-often-than-you-are-asked-to) | Hold back from chasing every sheep at once, and stop chasing one that keeps getting away |

<p align="center">
  <img src="https://raw.githubusercontent.com/org-runink/runi/main/assets/runi-wallpaper.jpg" alt="Arlo running through a neon-lit street in the rain, wearing the Runi goggles and harness" width="820">
</p>

<p align="center">
  <em>Runink's mascot is a working dog, not a logo.<br>
  These packages are built the same way: measured, documented, and honest about their limits.</em>
</p>

---

---

## PySpark & Python Data Pipeline Integration

Python and PySpark excel at offline data science, model exploration, and distributed ETL. Runi complements Python data ecosystems by serving as the lightweight, zero-dependency execution engine inside production Go microservices, Kubernetes pods, and local agentic harnesses.

### Augmenting PySpark Data Pipelines in Go

Go microservices can process DataFrames and dataset exports produced by PySpark or Delta Lake seamlessly in memory—such as reading Apache Avro OCF files via `runi/avro` or parsing structured JSON exports with zero external dependencies:

```go
// Read PySpark DataFrame exports (Avro OCF) directly in a Go handler
f, err := os.Open("pyspark_dataframe_export.avro")
if err == nil {
    defer f.Close()
    reader, err := avro.NewOCFReader(f)
    if err == nil {
        for reader.Scan() {
            var row map[string]any
            _ = reader.Decode(&row)
            // Process, rank, or forecast DataFrames natively inside your Kubernetes pod
        }
    }
}
```

### API Mapping Reference for Python Developers

For developers transitioning workflows from pandas, scikit-learn, statsmodels, or PySpark to Go:

| What you'd write in Python | In Go, with `runi` |
|---|---|
| `df['x'].describe()` | `stats.Describe(x)` |
| `np.quantile(x, 0.9)` | `stats.Quantile(x, 0.9)` *(same definition — NumPy's default / R type 7)* |
| `df['a'].corr(df['b'])` | `stats.Pearson(a, b)` — and `stats.Spearman` for rank |
| `StandardScaler().fit(train).transform(test)` | `sc := stats.FitStandardiser(train); sc.Transform(test)` (returns the column and an error) |
| `MinMaxScaler()` | `stats.FitMinMax(train)` |
| `TimeSeriesSplit(n_splits=5)` | `stats.RollingFolds(n, 5)` |
| `train_test_split(..., shuffle=False)` | `stats.Split(x, 0.8)` |
| `SARIMAX(y, exog=X, order=(1,0,1)).fit()` | `arimax.Fit(y, x, 1, arimax.Order{P:1, D:0, Q:1})` |
| `res.get_forecast(6).conf_int()` | `m.Forecast(6, xFuture, 0.05)` — point, lower, upper |
| `rank_bm25.BM25Okapi(corpus)` | `bm25.New(docs, bm25.Options{})` |
| `functools.lru_cache` | `memo.New[K,V](...)` — **plus single-flight**, which `lru_cache` has no equivalent of |
| `dask.delayed` / a lazily-evaluated future | `lazy.New(...)`, `lazy.Start`, `lazy.All` |

**What you get:** a 6 MB static binary with zero external dependencies, fits that run at sub-millisecond latencies at calibrated confidence intervals, and native Go concurrency—allowing `lazy.All` or `memo` requests to scale effortlessly across all available CPU cores without runtime locks or GIL bottlenecks.

**Seamless integration:** Explore and build offline models in Python or PySpark; serve and orchestrate online predictions, BM25 retrieval, and agent tool execution in Go.

## Kubernetes & Linux Resource Optimization

Deploying agentic harnesses and AI data workflows in Kubernetes and Linux environments requires strict resource efficiency, fast cold-starts, and predictable latency bounds:

- **Sub-Millisecond Cold Starts & Instant Pod Autoscaling**: A 6 MB static Go binary with zero runtime initialization boots in sub-milliseconds, enabling rapid Horizontal Pod Autoscaling (HPA) and instant container startup in Kubernetes pods or edge Linux nodes.
- **Minimal CPU & Memory Footprint**: Eliminating heavy container runtimes and cgo dependencies reduces pod memory requirements from hundreds of megabytes down to a few megabytes per replica, drastically increasing node pod density and reducing cloud infrastructure costs.
- **Local Inference & Agentic Loop Sidecars**: Perfect for running alongside local inference servers (e.g. Ollama, vLLM, llama.cpp) and agentic frameworks. Modules like `salvage` (recovering structured JSON from LLM outputs), `toon` (token-frugal prompt tables), and `bm25` (in-memory document ranking) execute directly in-process alongside your agent logic.

### A whole small pipeline

Describe, split, scale, fit, forecast, score. No dependencies, nothing hidden.

```go
package main

import (
	"fmt"

	"github.com/org-runink/runi/arimax"
	"github.com/org-runink/runi/stats"
)

func main() {
	y, x := loadSeries() // your data: demand, and a driver like price

	// 1. Look at it first.
	fmt.Printf("%+v\n", stats.Describe(y))

	// 2. Split WITHOUT shuffling — order carries information here.
	k := stats.SplitIndex(len(y), 0.8)
	yTrain, yTest := y[:k], y[k:]
	xTrain, xTest := x[:k], x[k:]

	// 3. Fit on the training half only.
	m, err := arimax.Fit(yTrain, xTrain, 1, arimax.Order{P: 1, D: 1, Q: 1})
	if err != nil {
		panic(err)
	}

	// 4. Forecast the held-out span, with 95% intervals.
	point, lo, hi, err := m.Forecast(len(yTest), xTest, 0.05)
	if err != nil {
		panic(err)
	}

	// 5. Score it, and check the intervals were honest.
	fmt.Printf("RMSE %.3f\n", arimax.RMSE(yTest, point))
	inside := 0
	for i := range yTest {
		if yTest[i] >= lo[i] && yTest[i] <= hi[i] {
			inside++
		}
	}
	fmt.Printf("%d of %d actuals inside the 95%% interval\n", inside, len(yTest))
}
```

That last step is the one people skip. A forecast without a calibrated interval
is a number with no error bar, and an interval nobody checked is decoration.

## `runi/stats` — the first ten minutes

Before a model there is a column of numbers nobody has looked at. This is what
you look at it with.

```go
s := stats.Describe(revenue)
// {N:482 NaN:3 Mean:1240.7 StdDev:318.4 Min:402 Q1:1011 Median:1223 Q3:1455 Max:2890}
```

`Describe` is the only function here that skips NaN, and it **tells you how many
it skipped**. Everywhere else a NaN in gives a NaN out, because quietly dropping
values changes the denominator and the caller is almost never told.

**Scalers are fitted, then applied.** This is the package's one opinion:

```go
sc := stats.FitStandardiser(train)
trainZ, _ := sc.Transform(train)
testZ, _ := sc.Transform(test) // the TRAINING mean and sd, deliberately
```

There is deliberately no one-step "scale this slice" function. Re-fitting a
scaler on your test set leaks its distribution into the model and makes every
score after it too optimistic — a mistake that is invisible in the output and
depressingly common. The API makes the correct thing the easy thing.

**Splitting respects order.** `stats.Split` and `stats.RollingFolds` never
shuffle. Shuffling a time series before splitting lets the model see the future;
the scores come out excellent and mean nothing. `RollingFolds` gives expanding
windows where each fold only ever trains on data preceding its validation span.

**Is it real?** A correlation or a slope says how strong a relationship looks,
not whether it is there. Two unrelated random series of four points clear
|r| ≥ 0.5 about half the time; so the numbers come with their evidence:

```go
c, ok := stats.Correlate(adSpend, signups) // r, n and the two-sided p-value; ok=false under 20 points
t := stats.Trend(weeklyReturns)            // slope, standard error, t, p; t.Rising(0.05)
stats.Adjust(pairs, 0.05)                  // Benjamini–Hochberg across every pair you tested
```

Test forty-five pairs at a 5% level and you should expect two "findings" from
pure noise; `Adjust` controls the share of false findings among the ones you
keep. The Student-t tail is computed here (the standard library has none) and
is pinned to published critical values. These are tests against zero under
the usual assumptions, including independent observations: detrend two
trending series before you correlate them.

Also here: `Mean` (compensated summation, so a long series does not quietly lose
its small values), `Variance`/`StdDev` in sample and population forms,
`Quantile` matching NumPy's default definition, `Median`, `IQR`, `Pearson` and
`Spearman`.

---

## `runi/bm25` — search without a model

Ranking by the words a document and a query share. No model, no vector store,
no GPU, no embedding to re-compute when the text changes.

```go
ix := bm25.New(docs, bm25.Options{})
for _, r := range ix.Search("disk controller timeout", 10) {
	fmt.Println(r.ID, r.Score, r.Terms) // Terms says WHY it ranked
}
```

**Where this beats an embedding search:** exact tokens. Identifiers, SKUs, error
codes, version numbers. The embedding of `ERR-4021` sits right next to the
embedding of `ERR-4022`, which is precisely wrong; BM25 keeps them apart. It is
also explainable — `Result.Terms` reports each matching term's contribution, and
those contributions sum to the score — and it indexes as fast as you can read
the text.

**Where it does not:** BM25 matches words, not meaning. A query for *car* will
not find a document that only says *automobile*. If your users paraphrase, this
alone will disappoint them.

Two details worth knowing: the IDF uses the `+1` smoothing, so a term appearing
in most documents can never drive a score **negative** the way the textbook form
can; and scores are only comparable **within one query**, so rank and cut by
position rather than by a threshold.

---

## `runi/salvage` — JSON out of a model's reply

Ask a language model for a JSON object and you mostly get one, wrapped in
something: a code fence, "Sure! Here is the analysis:", a closing remark, or
two objects where you asked for one. `encoding/json` rightly refuses all of it,
and the usual fix — a regular expression from the first `{` to the last `}` —
breaks the moment the prose or a string contains a brace.

```go
var a Analysis
if err := salvage.Decode(reply, &a); err != nil {
	// nothing in the reply decoded as an Analysis — ask again
}
```

`Decode` scans the reply once, honouring string literals and escapes, and tries
the whole text and then each top-level object or array in order, stopping at the
first that decodes into your type. A value that does not fit is skipped, never
half-applied. `DecodeStrict` also skips values carrying fields your type does not
have, for when the shape itself is the signal. `Candidates` gives you the raw
values if you would rather choose.

**It extracts; it never repairs.** Trailing commas, single quotes, comments and
replies cut off mid-object are not fixed: a "repaired" document is a guess about
what the model meant, and a plausible guess that decodes is worse than an honest
failure. A value that decodes is well-formed, not correct — validate it. Fuzzed:
millions of arbitrary inputs, no panic, every candidate a balanced substring.

---

## `runi/arimax` — forecasting with external drivers

ARIMA assumes a series is explained by its own past. ARIMAX adds the drivers —
price, spend, weather, a published schedule — so their effect is estimated rather
than absorbed into noise.

```go
m, _ := arimax.Fit(y, x, 1, arimax.Order{P: 1, D: 1, Q: 1})
point, lo, hi, err := m.Forecast(12, xFuture, 0.05)
```

**It refuses to forecast without the future drivers.** An ARIMAX forecast `h`
steps ahead needs the exogenous values for those steps. Most implementations
quietly assume zero or hold the last value flat and return a confident wrong
answer. This returns an error.

**Its intervals were measured, not just derived.** Nominal 95% prediction
intervals covered **97.3%** of realised values over 1,800 held-out points.

| | true | bias | RMSE |
|---|---:|---:|---:|
| exogenous β | 2.50 | **−0.0005** | 0.0087 |
| AR(1) φ | 0.60 | −0.0079 | 0.0420 |

82.0% lower RMSE than naive carry-forward. Full tables, and a section on what
the numbers do **not** show, in [arimax/BENCHMARKS.md](arimax/BENCHMARKS.md).

## `runi/season` — what repeats, and where it broke

Before you forecast a series, two questions come first: does it repeat, and
did its trend break somewhere? Each is useful on its own, so each is its own
function.

```go
p := season.Period(y, 0)               // 7 for weekly, 0 for "no season"
breaks := season.Changepoints(y, 3)    // indices where the trend changed
d, _ := season.Decompose(y, season.Options{})
next := d.Forecast(14)                 // trend continued + season

c, _ := season.Classical(y, 7)         // the moving-average split, period known
```

**There are two decompositions, and the cheap one is usually the answer.**
`Classical` is the textbook centred moving average — the operation
`statsmodels.seasonal_decompose` performs, in one O(n) pass, 8.5× faster than
it and agreeing with it to 2e-13. It detects no period, finds no breaks and
makes no forecast; it is the function for "I know this is hourly data, split
it". `Decompose` is the fitted version, and it is what answers the two
questions above at the same time.

**It says "no season" when there is none.** Scanning dozens of lags for a peak
finds one in pure noise unless the bar is raised for the number of lags tried.
Over 500 white-noise series of 200 points, **0** were given a season; the test
fails if that rate reaches 1%.

**It does not invent breaks.** A split is kept only if it pays a price scaled
to the series' own noise, so a noisy straight line comes back with no
changepoints — tested over 40 lines and 40 pure-noise series — while a planted
slope change or level shift is found within a few points.

**The season is fitted by least squares**, so its amplitude is right even when
the series is not a whole number of cycles (recovered within 5% in the tests;
the projection shortcut is biased there).

**This is not Prophet.** Prophet is Meta's forecasting library; it adds
holidays, several seasonalities and a Bayesian treatment of changepoints. If you
want Prophet, use Prophet. This is the classical additive decomposition
underneath, with no dependencies.

**The ends are stated, not smoothed over.** A centred average of `period`
points does not exist for the first and last `period/2` points — there is no
window. statsmodels returns NaN there; `Classical` holds the nearest real
average instead, averages the season over the interior only so those held
points cannot pull it, and puts the difference in the residual, so
`trend + season + residual` still adds up to the series *at every index,
including the ends*. A NaN in a component poisons the plot, the sum and the
variance a caller takes from it; a held value with the error left visible does
not.

What it does not do: **one** seasonality only (the strongest wins); the trend is
straight lines between breaks, fitted independently, so a forecast extends the
last line; no prediction intervals; `Classical` neither detects a period nor
forecasts nor filters outliers, by design. Short series are refused rather than
guessed at: `Period` needs 8 points and two full cycles, `Decompose` needs 4,
`Classical` needs two full cycles of the period it is handed.

## `runi/avro` — Apache Avro without the dependency tree

Read and write the Apache Avro binary encoding and Object Container Files, in
standard-library Go.

```go
err := avro.WriteOCF(w, schemaJSON, avro.CodecDeflate, records, marshal)
hdr, records, err := avro.ReadOCF(r, unmarshal)
```

**Avro is not ours.** It is a format created and maintained by the Apache
Software Foundation, specified at [avro.apache.org](https://avro.apache.org).
This is an independent implementation of that public specification — not
affiliated with, endorsed by, or a product of the ASF, and "Apache Avro" is
their trademark.

**Why another implementation.** [`hamba/avro`](https://github.com/hamba/avro)
and [`linkedin/goavro`](https://github.com/linkedin/goavro) are good, cover more
of the specification, and carry dependencies. This one exists only because the
rest of this module promises zero of them, and a container format is not worth
breaking that promise for. **If you already use either of those, keep using
them.**

**What it does not do**, so you find out here rather than later: no schema
resolution between a writer's and a reader's schema, no schema registry, no RPC,
and only the `null` and `deflate` codecs — no snappy, no zstd. If you need
writer/reader schema evolution this is the wrong tool; it reads a file with the
schema that file carries.

One property worth knowing because it is a property of the **format**, not of
this code: Avro's `deflate` codec is raw DEFLATE, which carries no checksum, and
the OCF sync marker only proves where a block ended. A bit flip inside a block
therefore changes record values silently. There is a test in this package that
pins that behaviour. If record integrity matters to you, add a digest over the
file and check it on read.

---

## `runi/barn` — versioned tables on any object store

An append-only, versioned key/value table that lives entirely on object storage.
**No database process.** A table is a set of immutable Avro data files plus a
transaction log, and the only coordination primitive it needs is an atomic
create.

```go
tb, _ := barn.Open(store, "acme", "events")
v, _ := tb.Put(ctx, barn.Row{Key: "a", Payload: []byte("hello")})
rec, ok, _ := tb.Get(ctx, "a")

snap, _ := tb.Snapshot(ctx, v)      // read the table as it was at version v
chg, _ := snap.ChangesSince(ctx, "", 3)
```

**Why it exists.** Agent state, audit trails and application metadata all want
the same three things: every version kept, reads that do not need a server, and
no operational database to run. This is the Delta/Iceberg pattern scaled down
to that job — and the pattern is theirs, not ours.

**Time travel is the point.** `Snapshot(v)` reads the table as of any retained
version, so "what did the agent believe when it made that call?" is a query
rather than an archaeology project.

**It works on anything with an atomic create.** [`Store`](#) is a five-method
interface — S3, GCS, MinIO, a filesystem, your own service. [`MemStore`] is a
complete in-memory implementation, which is both the reference and enough to
run a table in a test with no infrastructure at all.

**barn does not encrypt anything.** Whether objects are sealed at rest is a
property of the Store you supply. If you do seal them, bind the ciphertext to
the object's **key** — keys here encode tenant, table and path, so a data file
copied into another tenant's prefix then fails to open instead of being served.

**What it is not:** no joins, no multi-table transactions, no secondary indexes,
no uniqueness constraints. It is a versioned table, not a database.

---

## `runi/memo` — memoization with single-flight

```go
c := memo.New[string, Answer](memo.Options{Capacity: 4096, TTL: 10 * time.Minute})
ans, err := c.Do(ctx, memo.Hash("model-v3", req), func(ctx context.Context) (Answer, error) {
    return expensive(ctx, req)
})
```

**Single-flight is inside the cache.** A plain LRU helps the *second* caller; the
real problem is the first N arriving together on a cold key. 64 concurrent
callers cost **one** execution.

**Errors are not cached by default — but are coalesced.** Caching a failure turns
a transient fault into a sticky one for the whole TTL. A stampede against a
failing dependency still produces one call, not N.

**`Hash` exists because Go randomises map iteration.** Hashing a map naively
gives a different key every run — a cache that never hits and never says why.
Keys are sorted, values type-tagged, lengths prefixed.

| | ns/op | allocs/op |
|---|---:|---:|
| hit | **20.06** | **0** |
| miss | 847.1 | 6 |
| 64-caller stampede, cold key | 29,425 | 75 |

## `runi/lazy` — deferred values you can start early

Laziness decides **whether** work happens. Starting decides **when**. Most lazy
types only do the first, so the caller who forces the value pays the full cost.

```go
v := lazy.New(func(ctx context.Context) (Report, error) { return build(ctx) })
v.Start(ctx)            // returns immediately, work proceeds
...                     // do other things
r, err := v.Get(ctx)    // already done
```

Measured in the test suite:

| | |
|---|---|
| `Get` on a cold value | **122 ms** |
| `Get` after `Start` had time to run | **8 µs** |
| `All` on 5 values of 80 ms each | **81 ms** (serial: 400 ms) |

A value that is never forced is still never computed, so a pipeline stays lazy
even where you speculate. `Map` and `Then` compose without forcing, and several
consumers of one source share a single evaluation.

| | ns/op | allocs/op |
|---|---:|---:|
| `Get` on a resolved value | 38.82 | **0** |
| `New` + resolve | 645.7 | 3 |

Panics become an error wrapping `ErrPanic` rather than crashing whichever
goroutine happened to be forcing the value.

---

## `runi/budget` — one deadline, shared honestly

A request that must answer in three minutes usually collects a timeout per
step: two minutes here, ninety seconds there. Each is defensible, nothing adds
them up, and the slow step early on eats the time the answer needed.

```go
b, _ := budget.New(plan)               // one deadline for the whole request
ctx, cancel, ok := b.Begin(parent, "search")
if ok {                                 // false: too little time left to bother
	err := search(ctx)                  // ctx expires when search's slice does
	cancel()
	b.End(ctx, "search", err)           // done, failed or overrun
}
for _, r := range b.Finalize() {        // the answer says what it is missing
	log.Printf("%s %s after %v", r.Phase.Name, r.Status, r.Elapsed)
}
```

Each phase's slice is worked out when it starts, from what is left:
`min(cap, remaining − margin − floors of later stages)`. Give the phase that
writes the answer a floor, and no earlier phase can take that time. Phases in
the same stage run side by side and hold nothing back for each other. A slice
too small to be worth starting is recorded as **skipped** rather than started
into a certain overrun.

**It does not schedule and it does not stop work.** It never runs, orders or
retries your phases. A slice is a context deadline, and a phase that ignores
its context keeps running: `Finalize` calls that an overrun, which is true,
but stopping the goroutine is up to you. The caps are a plan, not a
measurement. `Plan.CriticalPath` tells you whether the plan fits the deadline
with every phase at its cap. Whether the phases fit their caps is something to
measure on the hardware that runs them.

---

## `runi/chain` — records nobody can quietly rewrite

A log of decisions, approvals or payments is only worth keeping if an edit
shows. `chain` seals each record with a hash that covers the one before it, so
changing, moving or swapping a record breaks the chain at that point.

```go
l1 := chain.Seal(chain.Bound, "", body1)       // body: your canonical bytes
l2 := chain.Seal(chain.Bound, l1.Hash, body2)
head, err := chain.Verify(chain.Bound, links)  // err is a *chain.Break
```

`Verify` stops at the first link that does not hold and says how it broke:
**altered** (edited after sealing), **reordered** (records moved), or
**replaced** (the record it was sealed after was swapped out or removed).
`VerifyFrom` checks a segment from a known hash. `Bound` seals the previous
hash into each link. `BodyOnly` is for existing logs whose records already
carry it.

**Tamper-evident, not tamper-proof.** Nothing is signed: whoever can write the
log can rewrite it consistently from any point on, and dropping the newest
records leaves a shorter chain that still verifies. Keep each head hash
somewhere the writer cannot change. The records, their encoding and their
storage stay yours.

---

## `runi/toon` — the table format models write

TOON writes a list of uniform objects as the field names **once** and then a
row each, instead of repeating every key on every record. For the shape a model
actually returns — twenty findings that all have the same four fields — that is
most of the tokens.

```go
text, err := toon.Encode(report)        // to send
err = toon.Decode(reply, &findings)     // to read back
err = toon.DecodeStrict(reply, &findings) // and to insist the counts match

text, err = toon.EncodeWith(report, toon.Options{Delimiter: '\t'}) // cheaper on prose
```

```
findings[2]{id,severity,file}:
  1,high,cmd/server/main.go
  2,low,internal/cache.go
```

**No JSON on the way.** Structs, maps, slices, pointers and scalars are walked
directly in both directions, applying `encoding/json`'s rules — `json` tags,
`omitempty`, `-`, `[]byte` as base64, the same errors for a value that does not
fit its field — without producing a byte of JSON. A type with its own
`MarshalJSON`/`UnmarshalText` (`time.Time`), embedded structs and the `,string`
option are still handed to `encoding/json`, so its rules apply exactly; that
costs speed, never correctness. Property tests generate random struct types and
hold both routes to the same bytes, values and errors. Against the previous
release, on 2,000 rows: decoding into structs makes **85% fewer allocations**
(12.8k, was 86k), decoding into a map **76% fewer** (12k, was 50k) and encoding
structs **61% fewer** (13k, was 34k) — budgets pinned by a test. In the
package's benchmarks that made struct decoding about 3× faster, on a par with
`json.Unmarshal` while reading half the bytes. Encoding a struct got about 2×
faster but is still slower than `json.Marshal`, because each row becomes a map
before it is written.

**Delimiters, as the spec declares them.** `Options.Delimiter` is `,`, tab or
`|`, written into every header — `rows[2\t]{id\tnote}:` — so any TOON reader
splits the rows correctly. A value is quoted only when it contains the
*active* delimiter, so under tab a summary like `Checked the config, then
restarted` costs no quote characters at all; under comma it costs two per cell.
`Decode` reads all three, header by header.

`Decode` is deliberately forgiving, because the input is a model's output: it
strips a ``` fence, ignores blank lines, reads the spec's keyless root array
(`[3]: a,b,c`), and **takes the values over the declared count**, since a model
that miscounts its own list has still told you the list. `DecodeStrict` is the one
that refuses a count mismatch, and that is the difference between the two —
reach for `DecodeStrict` when the decoded value drives a decision rather than being
shown to someone.

Numbers are written in the spec's canonical form: the shortest digits that read
back exactly, no exponent between 1e-6 and 1e21, and `-0` as `0`.

Three asymmetries are documented rather than hidden, because each one is a test
in the package:

- a **top-level list** is written under the key `items`, since a TOON document
  written by this package is a mapping; a keyless root array is read, not
  written;
- an **empty object** as a field's value is written `key:` and reads back as
  null, because the parser cannot tell that from a line a model left blank, and
  guessing "empty object" would make it stricter on exactly the input it exists
  to be lenient about;
- `Parse` keeps an integer as an `int64` and so is exact; `Decode` into `any`
  gives every number as a `float64`, as `encoding/json` would, which rounds
  past 2⁵³. Decode into a typed destination, or use `Parse`.


## `runi/toml` — configuration files, read strictly

Model sampling settings, connection manifests, project configuration and
`+++` frontmatter are written in TOML. This reads TOML v1.0.0 into the same Go
values `encoding/json` would fill, with no dependency to vendor for it.

```go
err := toml.Unmarshal(data, &cfg)       // structs, maps, slices, time.Time
doc, err := toml.Parse(data)            // map[string]any

d := toml.NewDecoder(f)                 // strict: a misspelt key is an error,
d.DisallowUnknownFields()               // not a setting left at its default
err = d.Decode(&cfg)
```

```
toml: servers[1].port: expected integer, got string (line 4:1)
toml: prot: unknown key: the struct has no field for it (line 2:1)
```

**Errors say where, and in terms of the document.** Every problem is a
`*toml.Error` with the key path, array indexes included, and the line and
column, so the message points at the line to fix. In strict mode every unknown
key is reported, not only the first. Redefining a key or a table, appending
with `[[ ]]` to an array written inline, and adding to an inline table after it
closes are refused as the specification requires, and the message names where
the first definition was.

**Nothing is converted behind your back.** Keys match `toml:"name"` tags, or
the Go field name, case-sensitively, as TOML keys are. An integer that does not
fit its field, or that a float field cannot hold exactly, is an error rather than
a wrap or a rounding. A local date-time has no zone; it fills a `time.Time` in UTC,
or keeps its meaning in `toml.LocalDateTime`.

**Conformance.** The test vectors were written for this package from the v1.0.0
specification, section by section: every key, string, number, date-time, array
and table form, valid and invalid, with the exact line and column each error
must report. A fuzz target holds that no input panics and every error points
inside the document.

On an eight-model sampling configuration (4.4 KB: arrays of tables, multi-line
prompts, inline tables), median of five runs on a 22-thread x86-64 machine with
Go 1.26:

| Reading `testdata/models.toml` | Time | Allocations |
|---|---:|---:|
| `toml.Parse` into a map | 82 µs | 703 |
| `toml.Unmarshal` into structs | 84 µs | 647 |
| `encoding/json` into the same structs, from the same data as JSON | 63 µs | 134 |

It is about a third slower than `encoding/json` reading the same configuration,
and allocates about five times as often; a file read once at start-up does not
notice either. The allocation counts are pinned by a test.

What it does **not** do: write TOML; read a stream incrementally (a `Decoder`
reads all of its reader first); expand environment variables or include other
files, which belong to the application; or accept the later v1.1 additions —
times without seconds, multi-line inline tables, `\e` and `\x` escapes. A leap
second (`:60`) is an error, because `time.Time` cannot hold it as written.


## `runi/drift` — when a series changes regime

Detectors for the question you ask of a reading taken every second: **has this
moved?** Each one does constant work and holds fixed memory per reading,
allocates nothing, and is a plain value, so one per series is affordable for
thousands of series.

```go
c, _ := drift.DefaultCUSUM(target, sigma)   // k = σ/2, h = 5σ, Montgomery's values
for x := range readings {
    if s := c.Add(x); s != drift.None {
        log.Printf("%s shift, began %d samples ago", s, c.Since())
        c.Reset()
    }
}
```

| Type | For |
|---|---|
| `Welford` | the exact running mean and variance, stable far from zero, mergeable across shards |
| `EWMA` | a smoothed level that forgets, by weight or by half-life |
| `EWMAChart` | small shifts that persist, when you do not know their size |
| `CUSUM` | a shift of a size you name — the fastest detector there is for that size — and when it began |
| `PageHinkley` | a shift when there is no target, against the series' own running mean |
| `Decayed` | a cumulative counter (time stalled, bytes sent) read at irregular times, turned into the "average over the last 10/60/300 s" rate operating systems publish |

**The defaults mean what the textbook says, and a test checks it.** Monte Carlo
over 4,000 runs puts `CUSUM` at k = 0.5σ, h = 4σ at an in-control average run
length of **168.0** samples (the table says 168) and a one-sigma shift caught in
**8.3** (8.38); at h = 5σ, **463.7** and **10.3** (465 and 10.4). Nudge h by 10%
and the tests fail, which is what makes them tests.

| Add / Update | ns | allocs |
|---|---:|---:|
| `Welford` | 10.3 | **0** |
| `EWMA` | 5.3 | **0** |
| `EWMAChart` | 4.7 | **0** |
| `CUSUM` | 12.7 | **0** |
| `PageHinkley` | 9.6 | **0** |
| `Decayed` | 32.3 | **0** |

Timings are `go test -bench` on a 4-vCPU development VM, not the pinned machine
the tables further down were measured on; the allocation column is the same
everywhere.

A test asserts zero allocations for every one, and a second test shows the same
measurement catching an allocation that is there.

**What it does not do.** It does not choose thresholds; the defaults are the
literature's. It does not know about seasons — a daily cycle is a shift up every
morning — so watch the residual from `runi/season` instead. It is univariate. A
signal is a statistic crossing a line, not a probability, and the documented
false-alarm rates assume independent samples. And it never acts: it returns
numbers.

## `runi/rate` — counters into rates, across wraps and resets

Most system statistics are cumulative totals: bytes, packets, CPU nanoseconds,
microseconds stalled. `rate` turns two readings of one into the rate over the
interval between them, correctly when the counter wraps or is reset, and keeps
bounded-memory summaries of the result. Every reading carries its own
timestamp; nothing sleeps and resamples.

```go
c, _ := rate.NewCounter(32)          // a 32-bit counter wraps at 2^32
for r := range readings {
    if got, ok := c.Update(r.Total, r.At); ok {
        fmt.Println(got.Delta, got.PerSecond)
    }
}
```

| Type | For |
|---|---|
| `Counter` | the delta and rate between two readings of a counter of a stated width: a wrap is unwrapped, a reset becomes a new baseline |
| `Window` | min and max over the last n values in O(1) amortised per value (Lemire's monotonic deque) |
| `Peak` | the largest value since the last reset, and when it happened |
| `Intervals` | the gaps between readings — last, mean, min, max — to see a producer stall or burst |

**Wrap or reset is a judgement, and the doc says so.** Two readings cannot tell
one wrap from a reset to a small number. A decrease of at least half the
counter's range is taken as a wrap (the increase it implies is under half the
range); a smaller one as a reset. That is a heuristic, documented with its
failure mode: read a 32-bit byte counter at 10 Gbit/s at least every 1.7 s. A
64-bit counter never wraps in practice (at 100 Gbit/s a byte counter takes
about 47 years), so for width 64 every decrease is a reset.

Every `Add` and `Update` allocates nothing (asserted by a test, which a second
test shows can fail), from about 7 ns (`Peak`) to about 26 ns (`Intervals`) on
the development VM `drift`'s figures come from. `Intervals` sums its gaps exactly in
128 bits, so its mean cannot overflow.

**What it does not do.** It does not smooth: for "the average over the last
minute" of a counter, use `drift.Decayed`. `Intervals.Late` cannot see a gap
that is still open — a producer that stops never calls `Add` — so compare the
clock with your last reading yourself. And `Window` holds a preallocated
buffer that a copy would share, so unlike the rest of these types
`NewWindow` hands it out as a pointer.

---

## `runi/tree` — which part of the hierarchy is responsible

Many quantities are measured per node of a hierarchy but asked about per
subtree: CPU or memory per group in a hierarchy of process groups, bytes per
directory, cost per team. `tree` keeps every node's subtree sum and subtree
maximum current as single values and the structure change, so "how much does
this group use, all of it included" and "which part of it is responsible" stop
being a walk of the whole hierarchy.

```go
t := tree.New[string]()
t.AddRoot("/")
t.Add("/user", "/")
t.Add("/user/build", "/user")
t.Set("/user/build", 40)
t.Sum("/")                          // 40, O(1)
t.Max("/")                          // 40 at /user/build, O(1)
t.TopK("/", 3, tree.BySum, nil)     // the three heaviest subtrees below /
```

| | |
|---|---|
| `Set` | a node's own value; every ancestor's sum and maximum follow, in O(depth), with no allocation |
| `Sum`, `Max`, `Own` | O(1); ties in `Max` go to the smaller key, so answers are deterministic |
| `TopK` | the k heaviest descendants by own value or by subtree sum, in O(size · log k), into the caller's slice |
| `Add`, `Move`, `Remove`, `RemoveSubtree` | change the structure; a `Move` under a node's own descendant is refused with `ErrCycle` |
| `Snapshot`, `Diff` | what appeared, disappeared or moved between two polls of the hierarchy, in one merge |

**Sums do not drift away from the truth.** Updated naively, a running sum
loses a small value beside a large one for good: after a node goes from 0 to
1e16 and back, a sibling's 1 is gone from every ancestor. Each sum is kept with
Neumaier's compensated summation; a test runs a million updates over fifteen
orders of magnitude, up to 36 levels deep, against exact big-float sums and
requires every node to be within one rounding of its true sum, where a naive
running sum is off by 0.1 at the root. `Rebuild` recomputes them from scratch
if a caller wants the drift bound reset. `Set` refuses NaN and ±Inf: a NaN
added to every ancestor could never be subtracted out again.

**What it does not do.** It does not read hierarchies; the caller tells it
about each node and value. It keeps sums and maxima only — a subtree median
would need a mergeable sketch per node. Updates cost the node's depth, which
is a handful for directories and process groups but not for a chain of a
hundred thousand nodes (that is what link-cut trees are for). And a value is
the current reading, not a series: turn a cumulative counter into a rate with
`rate.Counter` first, and feed a subtree's `Sum` to a `drift` detector to ask
whether it changed regime.

---

## `runi/sustain` — decisions that do not flap

A check like "x > limit" run on every reading of a live series changes its
answer every time noise carries x across the limit, and whatever it drives —
an alert, a throttle, a scale-up — flaps with it. `sustain` has the standard
remedies as small value types, one reading at a time, each reading carrying
its own timestamp.

```go
h, _ := sustain.NewHysteresis(sustain.High, 0.90, 0.75, time.Minute) // on at ≥ 0.90, off at ≤ 0.75
for r := range readings {
    if h.Update(r.Load, r.At) {
        // over the limit, and staying there until it really comes down
    }
}
```

| Type | For |
|---|---|
| `Hysteresis` | on/off with separate set and clear levels (a Schmitt trigger), High or Low, and an optional minimum dwell after each change |
| `Debounce` | follow a condition only after it has held for a time and/or a number of readings, with separate on- and off-delays |
| `NofM` | on while at least `Need` of the last `Size` readings (`Size` ≤ 64) are past a level: the radar and control-chart run rule |
| `Band` | ordered levels — normal, warning, critical — with a deadband on every edge |
| `Slew` | bound how fast an output follows its input, in units per second, separately up and down |

**Every claim is a test.** Noise smaller than the deadband changes a
`Hysteresis` or a `Band` at most once, where a single-level comparator fed the
same noise flaps over a thousand times. No two changes are closer than the
dwell. `NofM` is checked against every bit pattern of 12 readings for every
N ≤ M ≤ 12. `Slew` never moves further than its rate allows and never passes
its input. Every `Update` allocates nothing, at about 15–37 ns.

**What it does not do.** A NaN, an infinity, or a reading timed at or before
the previous one is skipped and counted, never taken as evidence; the state
is as if it had not arrived. Nothing reads the clock, so a condition that
stops being reported keeps its state: compare the time of your last reading
with now yourself. It does not choose your levels: alarm-management
standards (ANSI/ISA-18.2) tabulate starting deadbands and delays by kind of
measurement. To decide whether a series has changed regime rather than
crossed a level, use `drift`.

---

## `runi/throttle` — acting less often than you are asked to

Anything that acts on its own — restarts a process, scales a pool, retries a
request — can act too often when what it watches misbehaves, and the actions
then do more harm than the fault. `throttle` has the standard caps as small
types, each told the time of every call.

```go
limit, _ := throttle.NewBucket(0.1, 3)                       // one restart per 10 s, 3 at once
each, _ := throttle.NewCooldown[string](5*time.Minute, 10000) // each service at most every 5 min
for ev := range unhealthy {
    if each.Allow(ev.Service, ev.At) && limit.Allow(ev.At) {
        restart(ev.Service)
    }
}
```

| Type | For |
|---|---|
| `Bucket` | a token bucket: `rate` per second on average, `burst` at once; kept as the generic cell rate algorithm in integer nanoseconds, so it never drifts |
| `Quota` | at most N actions per window of length W, windows laid from an origin you choose ("5 per hour, on the hour") |
| `Cooldown` | each key at most once per period; expired keys are dropped as it goes, and an optional cap bounds how many are tracked |
| `Backoff` | retry delays, exponential and capped, randomised by full or decorrelated jitter from a source you seed |
| `Breaker` | a circuit breaker: closed, open and half-open, with a failure threshold, an open period and a number of probes |

**Every claim is a test.** A `Bucket` never allows more than
`burst + ⌊rate·t⌋` over any stretch t, and reaches that bound; drained daily
for ten simulated years it gives exactly the count the arithmetic does. A
`Quota` never exceeds N per window. A `Cooldown` is exact to the nanosecond
and makes the same decisions as a one-token `Bucket` per key. Backoff delays
stay within their bounds and pass a chi-squared test for uniformity. Every
breaker transition is checked with its time. Each type is checked against a
naive reference over random and out-of-order calls, and nothing on the hot
paths allocates.

**What it does not do.** Nothing reads the clock: a time earlier than one
already seen is taken as the latest and counted, so a clock stepped back
cannot refill a bucket or end a cooldown early. A full `Cooldown` refuses a
new key rather than evicting one still cooling down, because an evicted key
could act again too soon. Nothing is safe for concurrent use; guard it. And
it does not decide when to act — that is what `drift` and `sustain` are for.

---

## Does this change anything for you?

A table saying we compute a correlation in 0.701 ms where scipy takes 5.14 ms is
not a reason to adopt anything. Nobody's problem is a slow Pearson, the
absolute saving is under two milliseconds, and "compiled language beats
interpreted glue" is not news. If the ratios below are all you read, you should
not use this.

Here is the honest case, which is four specific consequences. Each one names the
number that drives it and what you would otherwise do instead.

**1. A forecast can live inside the request.** `statsmodels` needs **676 ms to
import** before it fits anything, and then **16.3 ms per fit**. `arimax` needs
**0 ms** — it is compiled into your binary — and **1.58 ms**. That is not 10×
on a benchmark; it is the difference between a nightly batch that writes
forecasts to a table and fitting a fresh model per tenant, per series, inside
the handler that needs it. If you have ever built the batch job and the table
and the staleness window because fitting was too slow to do inline, this is
what removes them.

**2. One execution instead of sixty-four.** With 64 callers on a cold key,
`functools.lru_cache` runs your function **64 times** and `memo` runs it
**once**. If that function is a model inference or a metered API call, the
difference is not nanoseconds — it is the bill, and the rate limit.

The same thing, measured on the shape an event mesh actually has — 2,000
events over 50 distinct objects, released in one burst, handler costing 2 ms:

| Strategy | Handler ran | Wall |
|---|---:|---:|
| No deduplication | 2,000× | 5.03 ms |
| **A mutex and a map** | **1,880×** | 5.93 ms |
| `runi/memo` | **50×** | **3.18 ms** |

A mutex and a map is a correct cache and it is what almost everyone writes. In
a burst it removed **6%** of the redundant work, because every goroutine that
arrives while the first is still working finds the map empty and starts again.
`memo` hit the ideal exactly — one execution per object — and finished sooner
*because* it did a fortieth of the work. For a Kubernetes controller, a webhook
fan-in or a change feed, that ratio is the one that decides whether the handler
keeps up.

**3. Search with no model, no vector store, no GPU.** Ranking 5,000 documents
takes 65 ms to index and 0.4 ms a query, in-process. The alternative is not
`rank_bm25` being 4.8× slower; it is standing up an embedding service.

**4. Nothing to audit.** Zero dependencies, enforced by CI — not "few", none.
No transitive tree, no numpy ABI to pin, no supply chain to review, one static
binary with no runtime. For some teams that is the entire decision and the
speed is irrelevant.

**And you should not use it when:** you need SARIMA, state space models or
exact maximum likelihood — `arimax` minimises the *conditional* sum of squares,
and while that no longer costs it precision in β it is still not the same
estimator; you want the seasonal split done by least squares rather than the
classical moving average — that variant is only 1.17× faster than statsmodels; your
data does not fit on one machine — that is what Spark is for; or you want an
ecosystem, a notebook and a plotting library, which this will never have.

What the numbers below are actually for is proving those four claims are not
marketing, and showing every case where we lose. The correctness evidence —
[six defects the property tests found](#proof-it-is-right-not-just-fast) in code
that already had 100% coverage — matters more than any of them, because fast
and wrong is worthless.

## Benchmarks

### Against the libraries people actually reach for

One machine, one session, same inputs on both sides: an ASUS Ascent GX10
(GB10), 20 cores, aarch64, Go 1.27.2, Python 3.12.3, with one core busy on
unrelated work throughout. Each figure is the **median of five runs** for `runi`
and three or more for Python, each of which is itself a median over
repetitions.

Reproduce it with `python benchmarks/baselines.py` and
`go run ./benchmarks/crossbench`; the method is in
[benchmarks/README.md](benchmarks/README.md). The `results_*.json` files
committed beside them are **one** of those runs, not the median — a single run
of the sub-millisecond rows lands anywhere in the spread noted below, which is
the whole reason the tables quote medians.

| Operation | `runi` | The library people use | |
|---|---:|---:|---|
| BM25, 200 queries over 5,000 docs | **3.92 ms** | 628.3 ms — `rank_bm25` | **160× faster** |
| Pearson, n=200,000 | **0.123 ms** | 5.15 ms — `scipy.stats.pearsonr` | **41.9× faster** |
| Avro OCF write, 20,000 rows | **1.10 ms** | 15.27 ms — `fastavro` | **13.9× faster** |
| Spearman, n=200,000 | **3.51 ms** | 41.58 ms — `scipy.stats.spearmanr` | **11.9× faster** |
| Index 5,000 docs | **19.3 ms** | 171.3 ms — `sklearn` `TfidfVectorizer` | **8.9× faster** |
| Avro OCF read, 20,000 rows | **1.96 ms** | 17.17 ms — `fastavro` | **8.8× faster** |
| Seasonal decomposition, n=4,000 | **0.0213 ms** | 0.1815 ms — `statsmodels` `seasonal_decompose` | **8.5× faster** |
| OLS trend + t-test, n=100,000 | **0.0465 ms** | 0.300 ms — `scipy.stats.linregress` | **6.4× faster** |
| Index 5,000 docs | **19.3 ms** | 93.5 ms — `rank_bm25` | **4.8× faster** |
| …the same seasonal split by least squares instead, n=4,000 | **0.155 ms** | 0.182 ms — `statsmodels` | **1.17× faster** |

The Avro file is also 740,202 bytes against fastavro's 741,181 — the same data,
0.13% smaller, each readable by the other.

**On the absolutes:** every row is the median of five runs with both sides
pinned to the same cores. The Python side is the noisier of the two — scipy's
`pearsonr` ranged from 4.05 to 7.55 ms across those five. If you take the
**worst** `runi` run against the **best** Python run in each row, the ratios
become 136×, 30.0×, 9.9×, 10.8×, 7.6×, 7.7×, 6.2× and 4.2×. Those are the
numbers we would defend if you reproduced this once and got unlucky, and they
are the reason we are comfortable with the medians above.
Figures that *are* deterministic — allocation counts, file sizes, and every
accuracy number below — are stated as facts.

**Two bugs in our own benchmark harness are worth describing, because both of
them flattered somebody and neither was in the libraries.**

The first was ours to lose. Every Python baseline used to be measured in one
interpreter, one section after another, so what ran *earlier* in that process
moved what came later: `scipy.stats.pearsonr` measured 0.80 ms if scikit-learn
had been imported first and 2.6–6.9 ms if it had not. `baselines.py` now runs
each library in its own process.

The second was ours to gain, and it is the more embarrassing of the two. This
table once published `OLS trend` at **33×**, on a scipy figure of 8.99 ms. That
8.99 ms was real and it did reproduce — but it was not measuring
`linregress`. The call was written `med(lambda: sps.linregress(np.arange(tn), t))`,
with `np.arange(tn)` **inside** the timed lambda, so every timed call also
allocated a fresh 800 KB array — while the Go side built its series once,
before the timer started. The figure was bimodal for exactly that reason:
~0.38 ms when the allocator could reuse a warm block, ~8.99 ms when the
sections before it had churned memory and each call faulted in its pages. The
array is now built once, outside the call, as it always should have been. Fixed,
scipy does this in 0.300 ms and the honest ratio is **6.4×**.

We would rather publish a 6.4× that survives being checked than a 33× that
turns out to be a page-fault benchmark.

**The two seasonal rows are the same question asked twice, and getting them
honest took three goes.** `statsmodels.seasonal_decompose` is told the period
and computes a centred moving average. The first version of this table compared
it against our default `Decompose`, which *also* runs a BIC-priced search for
trend breaks — a larger job, reported as our loss. Turning the break search off
made the timing fair but not the method: `Decompose` still fitted a line and a
Fourier series by least squares, which is a different estimator with a
different answer. `season.Classical` is now the
moving-average method itself, and it wins by 8.5× while agreeing with
statsmodels to **2e-13** on a series of magnitude 300 — a few ulps of float64,
verified over six series by
[`benchmarks/verify_classical.py`](benchmarks/verify_classical.py). Same
method, same numbers, one pass over the series.

The least-squares route stays in the table as a loss, because it is one, and
the extra work is priced separately rather than folded in:

| `season`, n=4,000, period given | |
|---|---:|
| `Classical` — *the operation statsmodels performs, by its method* | **0.0213 ms** |
| `Decompose`, no break search — least-squares trend + Fourier season | 0.155 ms |
| …plus the BIC changepoint search (the default) | 16.9 ms |
| …plus detecting the period instead of being told it | 53.5 ms |
| `season.Period` detection on its own | 3.54 ms |

So: if you know your period and want the classical decomposition, use
`Classical`, which is faster than statsmodels at it. What the least-squares
route buys is the three things a moving average cannot do — it finds the
period, it finds where the trend broke, and it extrapolates — and those cost
what they cost.

Two other rows used to go the wrong way, and fixing them is why they no longer
do. `Spearman` was 243.9 ms because `ranks` sorted through `sort.SliceStable` —
reflection-based swaps, an interface call per comparison, and stability it did
not need, since tied ranks are averaged. It now sorts by radix on the float's
bit pattern. The BM25 index was 177.9 ms because it hashed every token twice and
allocated a map per document; terms are now interned once into flat postings.
Neither was a limit of the language.

### The other six packages

The comparison above covers six packages. The other six — `salvage`, `chain`,
`lazy`, `toon`, `barn` and `budget` — had no Python figure at all, which
made the table a selection of our best cases. Here they are, against what a
Python author would actually reach for.

| Operation | `runi` | The Python you would write | |
|---|---:|---:|---|
| `salvage` 2,000 model replies | **0.385 ms** | 1.033 ms — `json.raw_decode` loop | **2.7× faster** |
| `barn` read 10,000 rows | **2.88 ms** | 4.63 ms — `sqlite3` | **1.6× faster** |
| `barn` write 10,000 rows | **4.62 ms** | 7.12 ms — `sqlite3` | **1.5× faster** |
| `chain` seal 20,000 records | **5.68 ms** | 8.44 ms — `hashlib` | **1.5× faster** |
| `chain` verify 20,000 records | **4.73 ms** | 7.00 ms — `hashlib` | **1.5× faster** |
| `lazy`, five 80 ms values together | **80.7 ms** | 82.5 ms — `ThreadPoolExecutor` | **1.02× faster** |

`budget` has **no Python counterpart and none was invented** — a deadline split
across phases with floors and per-phase contexts is not a thing one library
does.

`toon` is compared against Go's own `encoding/json` instead, because comparing
a Go implementation to CPython's C `json` module measures the C and not the
format:

| | `toon` | Go `encoding/json` | |
|---|---:|---:|---|
| encode 2,000 rows | **0.497 ms**, **26 allocs** | 0.574 ms, 4,005 allocs | **1.16× faster** |
| decode 2,000 rows | **1.746 ms**, 42,030 allocs | 1.806 ms, 28,022 allocs | **1.03× faster** |
| bytes for the same document | **76,764** | 144,739 | **47% smaller** |

### How these were measured, and a trap worth knowing about

**Both sides are pinned to the same cores.** The machine is an ASUS Ascent GX10,
and it has two core clusters:

```
10 cores at capacity 718–731      10 cores at capacity 997–1024
```

Unpinned, a process lands on either cluster at the scheduler's discretion, and
on this workload the difference is about **2×** — for both languages. An
unpinned Go-versus-Python comparison on that box is a coin flip rather than a
measurement, and it is how an earlier version of this table reported `salvage`
as 8.3× *slower* than Python: the Go figure came from a slow core and the
Python figure from a fast one. On equal footing the old code was 3.8× slower,
and the current code is 2.7× faster.

Everything above is `taskset -c 15-19` on both sides, medians of five runs.
The `toon` rows are `go test -bench -benchtime=3s -count=5`, because five
whole-process samples of a sub-millisecond operation measure the garbage
collector as much as the code.

If you reproduce these, pin. If you do not, you will get a different answer
each time, and so will we.

### Forecasting, head to head with `statsmodels`

Time-series forecasting is Python's home ground, so the useful question is not
whether Go can do it but what you give up. 200 independent synthetic series,
ARMA(1,1) errors, one exogenous regressor, n=500, h=6, **both libraries reading
the same CSVs written once by `benchmarks/gen.py`**.

The control is `naive_rmse`, the error of carrying the last value forward: it
depends on the data and on neither library. Go reports
**3.7104740961972245** and Python **3.710474096197225** — the same `float64` to
one unit in the last place, a relative difference of 1.2e-16 that comes from
the order the two languages sum 200 values in. Nothing else in the run has that
few digits between the sides, which is how we know the two were fitted to
identical numbers.

| | `runi/arimax` | `statsmodels` SARIMAX | |
|---|---:|---:|---|
| Fit, n=500 | **1.17 ms** | 14.78 ms | **12.6× faster** |
| Fit, n=2,000 | **3.34 ms** | 49.93 ms | **15.0× faster** |
| Fit, n=10,000 | **12.52 ms** | 244.9 ms | **19.6× faster** |
| Per fit over the 200-trial run | **1.24 ms** | 15.28 ms | **12.3× faster** |
| Start-up before the first fit | **0 ms** (compiled in) | 673 ms | |
| | | | |
| 95% interval empirical coverage | 94.5% | 94.5% | **identical** |
| Forecast RMSE, h=6 | 1.31600 | 1.31601 | within **0.001%** |
| Forecast RMSE vs the naive control | **2.82× better** | 2.82× better | same skill |
| φ (AR) bias / RMSE | −0.00410 / 0.05062 | −0.00407 / 0.05062 | indistinguishable |
| β (exogenous) bias / RMSE | +0.00249 / **0.03364** | +0.00229 / **0.03361** | within **0.1%** |
| Naive RMSE *(control — must match)* | 3.7104740961972245 | 3.710474096197225 | ✅ 1 ulp |

**The β row used to be the one to read first, and it is worth saying why it
changed.** statsmodels recovered the exogenous coefficient twice as precisely —
0.0336 against 0.0666 — and that was not noise. `arimax` fitted the model in
*stages*: one least-squares solve for β, then one ARMA fit to whatever was left
over. That estimates β as if the errors were independent, which is unbiased but
inefficient, and with ARMA(1,1) errors at φ=0.6, θ=0.3 the inefficiency is a
factor of four in variance — exactly the factor of two in RMSE that showed up
in the table.

`Fit` now descends on a single objective instead. It alternates an exact
generalised-least-squares solve for the coefficients on *prewhitened* data with
a warm-started refit of the ARMA, until the coefficients stop moving — feasible
GLS, which is Cochrane–Orcutt generalised from AR(1) errors to ARMA(p,q).
Fitting became **2.9× slower** and β went from 0.06662 to 0.03364, against
statsmodels' 0.03361. Both halves of that sentence are measured the same way —
the commit before GLS (`2ec9885`) and the current one, same generated data, both
pinned to the same cores: 0.410 ms against 1.178 ms per fit, and at n=500,
0.282 ms against 1.17 ms.

That is the whole trade, and it is worth stating plainly rather than burying:
**we gave up two thirds of our speed to halve the error on β** — the
coefficient you are usually fitting the model *for*. It is still 12–20× faster
than SARIMAX afterwards, which is why it was the right way round. If you are
fitting at scale and do not need β to that precision, the staged estimator was
not wrong, only inefficient, and it was about three times quicker.

What still differs is the objective, not the precision. `arimax` minimises the
**conditional** sum of squares, which conditions on the first p observations and
on zero pre-sample shocks; statsmodels runs exact maximum likelihood through a
Kalman filter. At these series lengths the difference does not appear in any
figure in this table — forecasts agree to 0.001% in six-step RMSE, both beat the
naive control by the same 2.82×, and both deliver 94.5% empirical coverage
against a nominal 95%.

| Use `statsmodels` when | Use `runi/arimax` when |
|---|---|
| You need SARIMA, state space models, or its diagnostics | You need ARIMAX(p,d,q) with regressors, and that is the model |
| You want exact MLE, and the initial conditions matter to you | Conditional least squares is enough, as it is at these lengths |
| You are in a notebook and 0.7 s of import does not matter | You are in a service, fitting per request or per tenant |
| You want the ecosystem Python has and Go does not | You want one static binary, no runtime, no dependency tree |

A fit every 1.58 ms rather than every 16.3 ms is what changes architecture: not
a leaderboard position, but the difference between a nightly batch that writes
forecasts to a table and **fitting a fresh model inside the request that needs
it**, per tenant, per series, on demand.

### Caching, head to head with `functools.lru_cache`

`lru_cache` is the API that inspired `memo`. Same experiment on both sides: N
callers hit one cold key simultaneously, the function takes 5 ms, and what is
counted is **how many times the function actually ran**.

| | `runi/memo` | `functools.lru_cache` |
|---|---:|---:|
| Cache hit | **19.6 ns** | 36.6 ns |
| Cache miss | 725 ns | **82 ns** |
| 8 cold callers → **times the function ran** | **1** | **8** |
| 64 cold callers → **times the function ran** | **1** | **64** |
| Wall clock, 64 cold callers | **5.75 ms** | 13.17 ms |
| TTL | ✅ | ❌ |
| Single-flight | ✅ | ❌ |

The hit is 1.9× faster and **the miss is 8.8× slower** — `lru_cache`'s miss is a
dict insert keyed on the argument tuple's hash, while ours canonicalises a
structured key and maintains an LRU list and expiry. That is a real loss and it
is in the table.

The row that matters is the count. With 64 callers on a cold key, `lru_cache`
calls the expensive function **64 times** and `memo` calls it **once**. If that
function is a model inference or a metered API call, the difference is not 8.8×
on a nanosecond — it is 64× on the expensive thing. This is not a flaw in
`lru_cache`, which never promised single-flight; it is why `memo` builds it in
instead of leaving it to the caller.

### Next to a warehouse engine

`runi` does not replace Spark, Databricks or Snowflake, and nothing here
suggests it could. It removes the round trip for work too small to deserve one.
The same correlation over 200,000 rows, on the same 20-core machine:

| | Time |
|---|---:|
| `runi/stats` | **0.701 ms** |
| `scipy.stats.pearsonr` | 5.14 ms |
| SparkML, warm session | 282.9 ms |
| SparkML, including session start | 2.57 s |

**403× on compute**, and about **3,700×** once a caller pays for the session.
Spark earns all of that back the moment the data stops fitting on one machine.
At this size it fits, and it spent 7.0 s building a DataFrame for a calculation
that takes about a millisecond. The Spark figures are a single run, not a
median — session start-up is not something repetition makes more precise.

### Go benchmarks

Every figure below is produced by `go test` in this repository and reproduces
with:

```bash
go test -bench . -benchmem -benchtime=1s ./...
go test -run 'TestParameterRecovery|TestIntervalCoverage|TestForecastBeatsNaive|TestBetaPrecision' -v ./arimax
```

```
goos: linux   goarch: arm64   ASUS Ascent GX10 (GB10), 20 cores   go1.27.2
```

> **The same machine as every other number in this README**, so the figures here
> and the head-to-heads above can be read against each other. One core was busy
> with unrelated work throughout.
>
> **Read the `ns/op` columns as approximate** and use them to compare *shapes* —
> how cost grows with n, how many allocations a call makes — not to size a
> deployment. **`B/op` and `allocs/op` are deterministic** and repeat exactly,
> which is why the allocation claims in this README are the ones stated as
> facts. Run them on your own hardware if a number matters to a decision.

### `runi/arimax` — accuracy

200 independent synthetic series, AR(1) errors, one exogenous regressor, n=500.
Synthetic data is used deliberately: estimator bias cannot be measured without
knowing the truth you are recovering.

| Parameter | true | bias | RMSE |
|---|---:|---:|---:|
| exogenous β | 2.50 | **−0.0005** | 0.0087 |
| AR(1) φ | 0.60 | −0.0079 | 0.0420 |

β is recovered essentially unbiased (−0.02% of its value) *despite* strongly
serially correlated errors. The small negative bias in φ is the known
finite-sample bias of conditional-sum-of-squares estimation; it shrinks with n
and is not corrected.

This particular case does **not** distinguish one estimator from another, and
saying so is part of reporting it: the generator's x is a smooth sinusoid, so it
is strongly autocorrelated, and against autocorrelated errors ordinary least
squares is already nearly efficient — β's RMSE is 0.0087 both with the
generalised-least-squares refinement in `Fit` and with it removed. The case that
does distinguish them is independent x, where the staged estimator reaches
0.0614 and `Fit` reaches **0.0329** (200 series, n=500, ARMA(1,1) errors at
φ=0.6, θ=0.3; asymptotic theory says 0.0673 and 0.0325).

| Measure | Result |
|---|---|
| **95% interval empirical coverage** | **97.3%** over 1,800 held-out points (h=1..6, 300 series) |
| vs naive carry-forward, h=6 | **82.0% lower RMSE** (0.6187 vs 3.4424, 100 windows) |

Coverage is the figure that matters when a forecast informs a decision. An
interval claiming 95% and delivering 70% is worse than no interval, because it
invites confident wrong answers. Ours is mildly **conservative**, which is the
safe direction: it omits parameter-estimation error while the CSS residual
variance is slightly inflated at finite n, and the second effect dominates.

**This holds only when the future exogenous values are correct.** Every forecast
above was given the true future X. Where X is itself forecast, its error is not
in these intervals.

### `runi/arimax` — speed

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `Fit` ARIMAX(1,0,1)+1 regressor, n=100 | 283,296 | 334,846 | 698 |
| `Fit` n=500 | 896,873 | 1,052,894 | 486 |
| `Fit` n=2,000 | 3,135,618 | 4,067,612 | 472 |
| `Fit` n=10,000 | 16,285,489 | 21,223,045 | 505 |
| `Forecast` 24 steps | 22,827 | 72,070 | 10 |
| `ACF` 40 lags, n=5,000 | 112,244 | 359 | **1** |
| `olsQR` n=5,000, 9 columns | 705,883 | 409,707 | 11 |

`Fit` is linear in n — 20× the data for **18.2×** the time — and the allocation
count is flat from n=500 upward, because work per optimiser iteration does not
depend on series length. Fit once, forecast often: forecasting 24 steps is
~23 µs, and `ACF` over 5,000 points makes **one** allocation.

`Fit` costs about 3× what it did when the fit was staged. It now alternates a
prewhitened least-squares solve with a warm-started ARMA refit, up to four
passes, instead of running each block once; that is what bought the β precision
above, and it was paid out of the margin against `statsmodels`, not out of
nothing.

### `runi/memo`

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `Do` — hit | **20.06** | 0 | **0** |
| `Do` — miss (store + LRU insert) | 847.1 | 343 | 6 |
| `Do` — hit, `RunParallel` 20 threads | 161.0 | 0 | **0** |
| `Hash` — 4-field map | 1,338 | 656 | 36 |
| stampede, 8 concurrent cold callers | 4,502 | 1,232 | 19 |
| stampede, 64 concurrent cold callers | 29,425 | 3,922 | 75 |

The hit path allocates nothing, so the cache is free relative to anything worth
memoizing. **The parallel hit is 161 ns, not 20** — `Store` takes one mutex per
operation. That is stated rather than omitted: at ~6M hits/s aggregate it is far
from the bottleneck for a network or model call, and disqualifying if you are
memoizing sub-microsecond work.

The stampede rows are the case single-flight exists for: 64 goroutines on a cold
key cost 29 µs and **one** execution of the function.

### `runi/lazy`

| Measurement | Result |
|---|---|
| `Get` on a cold value | 122 ms |
| `Get` after `Start` had time to run | **8 µs** |
| `All` over five 80 ms values | **81 ms** (serial: 400 ms) |
| `Get` on a resolved value | 38.82 ns, **0 allocs** |
| `Get` on a resolved value, 20 threads | 265.0 ns, **0 allocs** |
| `New` + resolve | 645.7 ns, 3 allocs |
| `Map` chain of 3 | 2,780 ns, 15 allocs |

The first three rows are structural rather than hardware-dependent: `All` over
five 80 ms values finishes in the time of the slowest one, not their sum.

### Inside real applications

The tables above race `runi` against other libraries. The question a team
actually asks is different: what happens when the hand-rolled code already in
an application is replaced. Three internal Runink applications made that swap
and were measured before and after, each comparison in the same binary. The
setup was a 4-core x86-64 cloud VM (not production hardware), Go 1.26, 8 runs
per case, compared with `benchstat` at p < 0.01.

| What was replaced | Time | Memory | Allocations |
|---|---|---|---|
| A BM25 scorer, many short documents (100 to 1,000) | **1.6–2.3× faster** | **1.9–4.1× less** | **1.6–3.3× fewer** |
| Median of 3 to 21 values | **2.6–2.9× faster** | | **none** (was 1 per call) |
| Least-squares slope, 365 points (`stats.Slope`) | **2.2× faster** | | none either side |
| Hash-chained records (`chain`) | about the same | | |

Not every swap was a speed-up, and these are reported too:

- **A few long passages** cut from one large text: `bm25` is about 7% slower
  and uses about 4% more memory than the single-purpose scorer it replaced.
  That scorer did one job. `bm25` does tokenising, stop words and per-term
  explanations as well.
- **A full line fit (slope and intercept)**: `stats.Trend` is slower than the
  hand-rolled one-pass formula it replaced. That formula cancels
  catastrophically for series far from zero, and `runi`'s does not. A fused
  one-pass fit is planned.
- **`memo`**: a hit costs 26 ns and allocates nothing. The application's own
  wrapper around it costs more than `memo` does.

Measuring the apps also found a regression before release: on a 20-term query
returning every result, `bm25` allocated more than v0.8.0. It was fixed before
v1.0.0 (see the changelog).

### What these numbers do not show

- **The forecasting head-to-head covers statsmodels, and nothing else.** See
  [Forecasting, head to head with `statsmodels`](#forecasting-head-to-head-with-statsmodels).
  No comparison against `pmdarima`, R's `forecast`, or Prophet was run, so no
  claim is made about any of them.
- **Accuracy is measured on synthetic data**, which is correct for estimator bias
  and is *not* evidence of accuracy on real series, where the model is
  misspecified by construction.
- **CSS is not exact maximum likelihood.** They agree closely at these series
  lengths but are different estimators.
- **Prediction intervals assume Gaussian errors.** Heavy-tailed residuals produce
  intervals too narrow in the tails.

---

---

## Proof it is right, not just fast

Fast and wrong is worthless, so this is the half of the evidence that matters.

Every package was at 100% statement coverage before any of the work below. That
says every line **ran**. It does not say every line is **right**. So each of the
twelve packages got property-based edge testing — the standard library's
`testing/quick` plus generated inputs, because a property library would be the
twelfth dependency in a toolkit that advertises zero — stating the invariants an
example test can only sample.

That found **six real defects in code that already had full coverage.** Four
returned a confidently wrong answer rather than an error, which is the shape of
failure nobody notices. All six are fixed; they are listed here because a
package that tells you what it got wrong is worth more than one that implies it
never did.

| Package | What was wrong | Why it mattered |
|---|---|---|
| `season` | `Period` reported **every cycle of 26 or more as 3** | Not "no season", which you would question — a wrong period that `Decompose` and every forecast taken from it then built on |
| `arimax` | `PACF` **panicked** when asked for more lags than the series has points | Crashed the caller's process on the ordinary way of asking for "as many as there are" |
| `salvage` | One stray quote in the prose *around* a value swallowed the next value | Two candidates became one, so `DecodeOne` returned an answer instead of refusing an ambiguous reply |
| `salvage` | The trailing-data guard accepted `{"a":1}}` and `{"a":1}] [{"b":2}]` as a single value | A reply holding two answers reached the caller who asked `DecodeOne` to refuse exactly that |
| `stats` | `Mean` overflowed to `+Inf` on values whose mean is perfectly representable | Kahan compensation cannot help when no `float64` holds the sum |
| `budget` | `Plan.Scaled` could return a plan `New` refuses | The failure surfaced far from the `Scaled` call that caused it |

**The `season` one is worth the detail**, because it is the kind of bug that
survives review. The autocorrelation estimator divides a sum of `m−lag` products
by the variance of all `m`, so what it reports tapers by about `(1 − lag/m)`,
while a sinusoid's true autocorrelation at lag 2 is already `cos(4π/P)` — close
to 1 for a long period. Past `P ≈ 25` the taper costs lag `P` more than the
curve costs lag 2, the global maximum moves to lag 2, and the neighbour
refinement settles it at 3. `Period` now takes the highest **peak** in the
autocorrelation, which is what its own documentation already claimed. Periods 3
to 40 now come back exactly.

The same exercise found that `MinPeriodLength` was advertising a floor the
function could not answer above: the bar a lag must clear rises as the series
shortens while the estimator's ceiling falls, and below 28 points the bar sits
*above* the ceiling, so no series of any shape clears it. It is now 28, the
shortest series in which a cycle can in fact be found.

### What the properties assert

These hold for every generated input, not for the handful a table lists:

| Package | The invariant |
|---|---|
| `chain` | **Every** single-bit flip, deletion and swap is caught and correctly blamed — not the five an example test lists; every prefix of a chain still verifies, so a reader racing an appender is not a false alarm |
| `bm25` | Every score equals the per-term breakdown printed beside it; every hit shares a term with the query; padding a document can never raise its score; identical documents score identically |
| `salvage` | Every truncation of a wrapped reply **fails closed**; `Scan`'s offsets always re-slice to the JSON it reported; its values never overlap; `DecodeOne` never partially fills a destination before refusing |
| `season` | `trend + season + residual` **adds up** to the series it split, to the last bit — for `Classical` including the two ends where the moving average does not exist; no component or forecast is ever NaN; a planted sine comes back with its amplitude at every period, which is what pins the even-period window's half weights; the Hampel filter cannot invent a value outside its input and touches under 0.2% of clean noise; a Fourier fit reproduces its own harmonics — which is what pins the phase-bucketed normal equations as an *exact* rewrite of the full ones rather than an approximation |
| `stats` | Pearson stays in [−1, 1] and is symmetric; it is invariant under rescaling either input; Spearman depends only on order, which is what pins the radix ranking; ranks always sum to n(n+1)/2 |
| `avro` | Every field type round-trips; `Long` at **all 64 varint boundaries**; doubles and floats **bit for bit**, including NaN and −0; a present empty string stays present; a truncated stream always errors |
| `barn` | The table matches an independent model of its commits; a snapshot taken at an old version **never changes**; compaction preserves every visible row; prefix scans are exact; versions advance one commit at a time |
| `arimax` | Differencing shortens by exactly `d` and flattens a degree-`d` polynomial; ACF and PACF stay in [−1, 1]; a forecast interval contains its own point estimate and widens with confidence; an exact slope is recovered |
| `memo` | Capacity is never exceeded; N concurrent callers run the function **once**; errors coalesce but are not cached |
| `lazy` | Concurrent getters evaluate once; a `Map` chain stays unevaluated until forced; `Map` composes |
| `budget` | No phase is ever allowed more time than remains; the deadline never moves; a phase context never outlives the budget |

The two single-flight properties wait on the `Coalesced` counter rather than
sleeping, so they are statements about the store and not about the scheduler —
one of them failed while the benchmark machine was busy, for a reason that was
not a defect, which is its own small lesson about tests that measure the host.

### What was not measured

- **Prophet was not run.** It is a different model class — additive trend plus
  seasonality, not ARIMAX — so running it on this data would have measured the
  mismatch, not the library. No claim about Prophet appears here.
- **`pmdarima`, R's `forecast`, PyTorch and TensorFlow were not run.** None of
  them is the tool someone reaches for to fit an ARIMAX or rank documents by
  BM25, and benchmarking a deep-learning framework on a 500-point regression
  would measure the mismatch rather than either library.
- **One machine, one session, one core busy.** The absolute times move with the
  host; the ratios are far more stable, and the accuracy figures are
  deterministic given the CSVs.
- **Synthetic data.** Correct for measuring estimator bias against a known
  truth, and *not* evidence about either library's accuracy on real series.

### On the coverage figure

**Every one of the eighteen packages is at 100% of statements**, and the floor is
enforced per package so a strong one cannot pay for a weak one.

The number is not the point, and on its own it is close to meaningless — the six
defects above were all found in code it already covered. What chasing the last
few percent was good for was the questions it forced: it turned up a truncated
model reply that decoded as a clean result, a minimum-length rule stated twice so
the real one could never fire, and a change-feed comparator that was not a total
order.

Where a guard genuinely cannot be reached through the public API, it is split
into its own function and tested with the input it exists for, rather than left
uncovered or chased with a synthetic test that executes a line without asserting
anything. `levinson` in `arimax` and `meanScaled` in `stats` are both shaped that
way, and each says in its doc comment why the branch is unreachable from
outside. A test that forces an unreachable branch tests the test, not the code.

## Assurance

For teams with a procurement or compliance review. Everything below is
continuously verified in CI, not asserted once.

| | |
|---|---|
| **Dependencies** | **Zero.** `go.mod` declares none, and CI **fails the build** if a `require` line or a non-stdlib package appears in the dependency graph. There is no transitive tree to audit. |
| **Platforms tested** | Linux × Go 1.24, 1.25, 1.26, every push. macOS and Windows are no longer tested in CI (the project's own runners are Linux) |
| **Concurrency** | `go test -race` on Linux, every push |
| **Vulnerability scanning** | `govulncheck` **daily** and on every push |
| **Static analysis** | CodeQL weekly, `security-and-quality` query set |
| **Supply-chain posture** | OpenSSF Scorecard weekly, uploaded to code scanning (no longer published: its API accepts only GitHub-hosted runs) |
| **Test coverage** | **100%** of statements in every one of the eighteen packages (the `benchmarks/` commands are excluded; they are programs, not library code). Floors are enforced **per package** at 100, so a strong package cannot pay for a weak one |
| **Formatting** | `gofmt` clean, enforced |
| **Benchmarks** | compiled and executed in CI so published figures stay reproducible |
| **Scheduled runs** | CI runs weekly even without commits, so a green badge means "passes on current toolchains", not "passed once" |
| **CI runners** | The project's own `[self-hosted, public-ci]` runners, never GitHub-hosted. Jobs run only for pushes to this repository's branches and pull requests from them; a fork's pull request starts no job, so a maintainer pushes reviewed fork changes to a branch here to test them |
| **Licence** | BSD-3-Clause, single licence, no exceptions |
| **Vulnerability disclosure** | [SECURITY.md](SECURITY.md) — private advisory, 5-day acknowledgement |

**Runtime behaviour**, since questionnaires ask: no network access, no filesystem
access, no subprocesses, no `unsafe`, no cgo, no reflection over untrusted input.
Deterministic — same input, same output, with the only clock read being one the
caller injects for testing expiry.

**What this is not**, stated so nobody infers it: `memo.Hash` uses SHA-256 to
derive cache keys and is **not** a security boundary; the packages perform no
authentication, authorisation or input validation; `memo` is in-process only,
with no listener and nothing shared between replicas.

## Used by

Open-sourced for the Go community. If your organisation uses any of these, we
would like to feature you.

| Organisation | Packages | What for |
|---|---|---|
| **Runink** | `stats`, `salvage`, `barn`, `chain`, `budget`, `bm25`, `season`, `memo` | The team that builds it — see [From the Runink team](#from-the-runink-team) |
| **Logical Leap** | `arimax`, `memo` | Partner engagement — forecasting and memoization in shared work |
| _(yours could be here)_ | | |

**To be added:** open a pull request adding a row, or [open an issue](https://github.com/org-runink/runi/issues/new)
titled `Add <organisation> to Used by`. A name and one sentence is all that is
needed — no logo, case study or quote will be asked for.

## Maintainer's map

| Path | Owns | Where the subtlety is |
|---|---|---|
| `arimax/arimax.go` | `Fit`, `Forecast`, intervals | `psiWeights` controls how intervals widen; start there if they look wrong |
| `arimax/arma.go` | CSS residuals, Nelder–Mead fit, stationarity guard | |
| `arimax/linalg.go` | Householder QR | QR not normal equations — collinear regressors |
| `arimax/diff.go` | `Difference`, `integrate` | **`integrate` for `d ≥ 2`** needs the last *difference* per level, not the tail values. Real bug, caught by the round-trip test |
| `arimax/acf.go` | `ACF`, `PACF`, `LjungBox` | returns Q and dof, never a p-value |
| `memo/memo.go` | `Store`, `Do`, LRU + TTL | one mutex per op; shard if memoizing sub-µs work |
| `memo/key.go` | `Hash` canonicalisation | sorted maps, type tags, length prefixes |
| `lazy/lazy.go` | `Value`, `Start`, `Get`, `Map`, `Then`, `All` | `Get` honours ctx without cancelling the shared evaluation |

```bash
go test ./...                                 # all eighteen packages
go test -race ./...                           # several packages are concurrent
go test -bench . -benchmem -benchtime=1s ./...
gofmt -l . && go vet ./...                    # must both be silent
```

Tests use a fixed LCG rather than `math/rand`, whose stream is not guaranteed
stable across Go releases. A test whose data silently changes is worse than no
test — if you add randomness, do the same.

## Contributing

Issues and pull requests welcome.

### What belongs here

A package belongs in `runi` if it is **a primitive someone reaches for while
doing data work, which the standard library does not provide.** That is the
whole rule, and it is deliberately narrow.

The risk a module like this faces is not being too small. It is becoming a
drawer — a pile of useful-but-unrelated code that nobody can describe in a
sentence, and that therefore nobody adopts. Every package added makes the next
one easier to justify and the module harder to explain.

So before proposing one, answer two questions in the PR:

- **Who reaches for this, and in the same hour they reach for another package
  here?** `stats` and `arimax` share a user. A package serving a different
  audience entirely may be excellent and still belong in its own module.
- **Would a reader be surprised to find it here?** Surprise is the signal that
  the module's description has stopped being true.

A good package that does not fit is not a rejection. It is a module of its own,
and it will do better with a name that describes it.

### What will never be here

Set by the project owner, 2026-10-08. These are not "not yet" — they are out of
scope permanently, and a PR proposing one will be closed rather than reworked.

**Domain logic.** Logistics and industry identifiers (GS1, GTIN/SSCC/GLN,
ISO 6346 container codes), routing and route optimisation, ontologies, statute
and regulatory deadlines, catalogues. These encode knowledge of a business, not
a data primitive, and they stay with the business.

**Detection patterns, of any kind.** PII masking rules, credential and secret
scrubbing, prompt-injection and guard rules — anything that reveals what we
detect. Publishing a detector publishes its gaps: it tells a reader exactly what
is caught and therefore what is not, which helps an attacker more than it helps
a user. This holds even when the patterns are individually unremarkable.

If you are contributing from inside Runink and a package touches either of
these, it stays in the private repository. That applies no matter how clean,
well-tested or generic-looking the code is.

### The bar

1. **Zero dependencies.** It is why these work in a CLI, a sidecar, a WASM build
   or on a device. CI fails the build if a `require` line appears, and the check
   feeds itself a known dependency to prove it can still detect one. A PR adding
   a dependency needs an argument strong enough to change the module's identity.
2. **Claims need a test.** State an accuracy or performance property and there
   must be a test that fails when it stops holding. A benchmark number in a
   README with no test behind it is decoration.
3. **A coverage floor, in CI, per package.** Floors are per package rather than
   module-wide, so a strong package cannot pay for a weak one. Set yours at the
   level the PR actually reaches and raise it later — a floor you have to lower
   is worse than one that started honest.
4. **Document what it does NOT do.** Every package here has that section, and it
   is the most useful part. A user who finds the limit in your docs is a user;
   one who finds it in production is a former user.
5. **Public-clean.** No internal paths, service names, infrastructure details or
   security posture. A public commit is a publication. If a comment explains a
   limitation by describing where *we* have not fixed something, rewrite it to
   tell the reader what *they* should do.
6. **Prove a check can fail.** If you add a guard, feed it a violation and show
   it is caught. A green that could not have gone red is worth less than none.

### Opening the PR is already publishing

If you are contributing from inside Runink, note the order: **a branch pushed to
this repository is public, and a pull request certainly is.** There is no
private staging step here. Review happens on content that is already readable by
anyone, and a force-push does not unpublish it.

So: build it locally, test it locally, get the content cleared, and push once.
Not "open a draft PR and sort the boundary out in review" — by then it is out.

The same applies to tags with more force, because the Go module proxy serves
version zips **immutably**. Before any tag, run `git ls-files` and read what the
zip will actually contain, rather than reviewing only the diff. A file nobody
meant to publish ships just as permanently as one that was intended.

### Adding a package

- `doc.go` or a package comment that says what it is, what it is not, and when
  to use something else — including naming the better alternative if one exists.
- Tests, with the floor added to `.github/workflows/ci.yml`.
- A README section following the existing shape.
- An entry in the package table and the gear table.

## Compatibility

`runi` follows [semantic versioning](https://semver.org). Every change that can
break a caller is listed under **Breaking** in [CHANGELOG.md](CHANGELOG.md),
with what to do about it.

**Before v1.0.0** a minor release could break the API, and the last ones
did: v0.9.2 renamed `tablelog` to `barn`, and v1.0.0 itself settled the API
for the freeze (`barn.Store`, `budget.New` and a handful of renames). Every
such break is in the changelog with what to do about it.

**From v1.0.0**, within v1:

- **The exported API of every package stays.** A name is not removed or
  renamed, a signature does not change, and documented behaviour does not
  change, except where it was a bug. A break means `/v2`.
- **What was written can still be read.** A `barn` table written by any v1
  release opens with every later v1 release. TOON and Avro written by `toon`
  and `avro` decode with every later release; the exact bytes `Encode` produces
  may get smaller or cheaper, but never stop meaning the same value.
- **Zero dependencies.** `go.mod` has no `require` line, and CI fails if one
  appears.

Not covered: performance figures, which are measurements, not promises;
anything unexported; and the text of error messages. Match errors with
`errors.Is` against the exported sentinels (`barn.ErrCorrupt`,
`salvage.ErrTruncated`, ...), not by string.

**Go versions.** Every release from the one `go.mod` names, which is never
newer than the oldest release Go itself still supports: today 1.24, 1.25 and
1.26. CI tests each of them on Linux, macOS and Windows, with the toolchain
pinned so a newer one cannot stand in for it.

## Licence

BSD-3-Clause. See [LICENSE](LICENSE).

---

---

## From the Runink team

`runi` is built by the team behind **[Runink River](https://docs.runink.org/river)**, an open-source data and AI platform applied to become a **Linux Foundation Incubating** project under the Data & AI track. River's documentation is available at **[docs.runink.org/river](https://docs.runink.org/river)**.

These packages came out of building River and the platform around it: the
problems they solve — forecasting with drivers you already have, not paying
twice for the same answer, ranking without a model, looking at a column before
modelling it — are the ones that kept recurring, and the standard library had no
answer for any of them.

**What this is not:** `runi` is not a component of River, and River does not
depend on it. They are separate projects from the same team, under separate
licences — River is its own distribution, `runi` is BSD-3-Clause Go packages you
can use in anything. If you are here for the Go packages you never need to touch
River, and vice versa.

## The family

`runi` is the shared Go toolkit behind Runink's products. The product names
follow the water; the mascot and his rig do not.

| | |
|---|---|
| **Runink River** | the operating system |
| **Runink TIDE** | the console and control plane |
| **runi** | the Go packages both of them are built on |
| **Arlo** | Runink's Australian Shepherd, and the one doing the work |
| **Runi** | the goggles and harness Arlo wears — the gear these packages are named for |

## Artwork and marks

**Arlo**, the **Runi** rig, the Runink logo and the Runink marks are held by
**Runink** and are **not** covered by this repository's BSD-3-Clause licence. The
code is yours to use; the artwork is not. Detail, and what you may do in a fork,
in [TRADEMARKS.md](TRADEMARKS.md).

This project is not affiliated with or endorsed by Google. "Go" and the Go logo
are trademarks of Google LLC.
