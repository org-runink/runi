<p align="center">
  <img src="https://raw.githubusercontent.com/org-runink/runi/main/assets/runi-logo.jpg" alt="Arlo, the Runink Australian Shepherd, wearing the Runi rig: AR goggles and a powered herding harness" width="220">
</p>

<h1 align="center">Runi</h1>

<p align="center">
  <strong>Arlo does the herding. Runi is the rig he wears to do it.</strong>
</p>

<p align="center">
  <em>Goggles to see what is coming. A harness to work close to the metal.<br>
  Three small Go packages, zero dependencies, every claim measured.</em>
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
  <img src="https://img.shields.io/badge/go-1.24%20%7C%201.25-00ADD8" alt="Go 1.24 | 1.25">
  <img src="https://img.shields.io/badge/dependencies-0-success" alt="zero dependencies">
  <img src="https://img.shields.io/badge/packages-11-informational" alt="eleven packages">
  <img src="https://img.shields.io/badge/coverage-92.2%25-brightgreen" alt="coverage">
  <img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="BSD-3-Clause">
</p>

---

**A data toolkit for Go: explore it, forecast it, search it, and make it fast.**
Standard library only — no dependencies, in any package.

```bash
go get github.com/org-runink/runi
```

| Package | One line | Coverage |
|---|---|---|
| [`runi/stats`](#runistats--the-first-ten-minutes) | Describe, split and scale a column, and test whether a relationship is real | **100%** |
| [`runi/arimax`](#runiarimax--forecasting-with-external-drivers) | Forecast a series using the things that drive it | **100%** |
| [`runi/season`](#runiseason--what-repeats-and-where-it-broke) | Find the season and the trend breaks, then split the series apart | **100%** |
| [`runi/bm25`](#runibm25--search-without-a-model) | Rank documents by the words they share | **100%** |
| [`runi/salvage`](#runisalvage--json-out-of-a-models-reply) | Get JSON out of a language model's reply, without guessing | **100%** |
| [`runi/avro`](#runiavro--apache-avro-without-the-dependency-tree) | Read and write Avro and Avro OCF files | **100%** |
| [`runi/tablelog`](#runitablelog--versioned-tables-on-any-object-store) | Append-only versioned tables with time travel, no database | 99.3% |
| [`runi/memo`](#runimemo--memoization-with-single-flight) | Don't compute the same thing twice | **100%** |
| [`runi/lazy`](#runilazy--deferred-values-you-can-start-early) | Compute it before anyone asks | **100%** |
| [`runi/budget`](#runibudget--one-deadline-shared-honestly) | Split one deadline between the steps of a request, and say which ran out | **100%** |
| [`runi/chain`](#runichain--records-nobody-can-quietly-rewrite) | Seal records so an edit, a move or a swap shows, and say which | **100%** |

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

<p align="center">
  <img src="https://raw.githubusercontent.com/org-runink/runi/main/assets/runi-wallpaper.jpg" alt="Arlo running through a neon-lit street in the rain, wearing the Runi goggles and harness" width="820">
</p>

<p align="center">
  <em>Runink's mascot is a working dog, not a logo.<br>
  These packages are built the same way: measured, documented, and honest about their limits.</em>
</p>

---

---

## Coming from Python?

If you work in pandas, scikit-learn or statsmodels, the first hour in Go is
usually the one that decides whether there is a second. Here is the map.

| What you'd write in Python | In Go, with `runi` |
|---|---|
| `df['x'].describe()` | `stats.Describe(x)` |
| `np.quantile(x, 0.9)` | `stats.Quantile(x, 0.9)` *(same definition — NumPy's default / R type 7)* |
| `df['a'].corr(df['b'])` | `stats.Pearson(a, b)` — and `stats.Spearman` for rank |
| `StandardScaler().fit(train).transform(test)` | `sc := stats.FitStandardiser(train); sc.Transform(test)` |
| `MinMaxScaler()` | `stats.FitMinMax(train)` |
| `TimeSeriesSplit(n_splits=5)` | `stats.RollingFolds(n, 5)` |
| `train_test_split(..., shuffle=False)` | `stats.Split(x, 0.8)` |
| `SARIMAX(y, exog=X, order=(1,0,1)).fit()` | `arimax.Fit(y, x, 1, arimax.Order{P:1, D:0, Q:1})` |
| `res.get_forecast(6).conf_int()` | `m.Forecast(6, xFuture, 0.05)` — point, lower, upper |
| `rank_bm25.BM25Okapi(corpus)` | `bm25.New(docs, bm25.Options{})` |
| `functools.lru_cache` | `memo.New[K,V](...)` — **plus single-flight**, which `lru_cache` has no equivalent of |
| `dask.delayed` / a lazily-evaluated future | `lazy.New(...)`, `lazy.Start`, `lazy.All` |

**What you give up:** the ecosystem. There is no seaborn here, no notebook, no
`pd.read_sql`, and nothing in this module will ever train a neural network.

**What you get:** a 6 MB static binary with no runtime and no dependency tree,
fits that are [42–54× faster than statsmodels](#against-the-python-reference-implementations)
at the same interval calibration, and real parallelism — `lazy.All` over five
80 ms values finishes in 81 ms, not 400 ms, with no GIL to work around.

**The honest recommendation:** explore in Python. Ship in Go. This module exists
for the second half of that sentence — the moment a model has to run inside a
request, per tenant, a thousand times an hour.

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
intervals covered **97.4%** of realised values over 1,800 held-out points.

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
```

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

What it does not do: **one** seasonality only (the strongest wins); the trend is
straight lines between breaks, fitted independently, so a forecast extends the
last line; no prediction intervals. Short series are refused rather than
guessed at: `Period` needs 8 points and two full cycles, `Decompose` needs 4.

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

## `runi/tablelog` — versioned tables on any object store

An append-only, versioned key/value table that lives entirely on object storage.
**No database process.** A table is a set of immutable Avro data files plus a
transaction log, and the only coordination primitive it needs is an atomic
create.

```go
tb, _ := tablelog.Open(store, "acme", "events")
v, _ := tb.Put(ctx, tablelog.Row{Key: "a", Payload: []byte("hello")})
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

**tablelog does not encrypt anything.** Whether objects are sealed at rest is a
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
| hit | **14.34** | **0** |
| miss | 694.2 | 5 |
| 64-caller stampede, cold key | 24,324 | 75 |

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
| `Get` on a resolved value | 73.91 | 0 |
| `New` + resolve | 771.1 | 3 |

Panics become an error wrapping `ErrPanic` rather than crashing whichever
goroutine happened to be forcing the value.

---

## `runi/budget` — one deadline, shared honestly

A request that must answer in three minutes usually collects a timeout per
step: two minutes here, ninety seconds there. Each is defensible, nothing adds
them up, and the slow step early on eats the time the answer needed.

```go
b, _ := budget.New(plan, nil)          // one deadline for the whole request
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

## Benchmarks

Every figure is produced by `go test` in this repository and reproduces with:

```bash
go test -bench . -benchmem -benchtime=200x ./...
go test -run 'TestParameterRecovery|TestIntervalCoverage|TestForecastBeatsNaive' -v ./arimax
```

```
goos: linux   goarch: amd64   cpu: AMD Ryzen 7 8840U
```

> **These are dev-laptop figures, not capacity numbers.** One unpinned machine,
> no quiet-system tuning.
>
> **Read the `ns/op` columns as approximate.** Repeating a benchmark on this
> machine moves wall-clock time by up to ±30% — `ACF`, for instance, was observed
> between 199 µs and 306 µs across four runs — because the host is a laptop with
> frequency scaling and other work on it. The figures below are each a single
> honest run, quoted at the precision Go prints rather than the precision they
> carry. **`B/op` and `allocs/op` are deterministic** and repeat exactly, which is
> why the allocation claims in this README are the ones stated as facts.
>
> Use these to compare *shapes* — how cost grows with n, how many allocations a
> call makes — not to size a deployment. Run them on your own hardware if a
> number matters to a decision.

### `runi/arimax` — accuracy

200 independent synthetic series, AR(1) errors, one exogenous regressor, n=500.
Synthetic data is used deliberately: estimator bias cannot be measured without
knowing the truth you are recovering.

| Parameter | true | bias | RMSE |
|---|---:|---:|---:|
| exogenous β | 2.50 | **−0.0005** | 0.0087 |
| AR(1) φ | 0.60 | −0.0079 | 0.0420 |

β is recovered essentially unbiased (−0.02% of its value) *despite* strongly
serially correlated errors — the purpose of the staged regression-with-ARIMA-errors
fit. The small negative bias in φ is the known finite-sample bias of
conditional-sum-of-squares estimation; it shrinks with n and is not corrected.

| Measure | Result |
|---|---|
| **95% interval empirical coverage** | **97.4%** over 1,800 held-out points (h=1..6, 300 series) |
| vs naive carry-forward, h=6 | **82.0% lower RMSE** (0.6188 vs 3.4424, 100 windows) |

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
| `Fit` ARIMAX(1,0,1)+1 regressor, n=100 | 182,294 | 148,018 | 321 |
| `Fit` n=500 | 659,162 | 489,577 | 235 |
| `Fit` n=2,000 | 2,275,325 | 2,033,850 | 245 |
| `Fit` n=10,000 | 12,309,872 | 10,324,230 | 249 |
| `Forecast` 24 steps | 11,830 | 72,065 | 10 |
| `ACF` 40 lags, n=5,000 | 305,932 | 761 | **1** |
| `olsQR` n=5,000, 9 columns | 879,839 | 409,682 | 11 |

`Fit` is linear in n — 20× the data for 18.7× the time — and allocation count is
flat from n=500 upward, because work per optimiser iteration is independent of
series length. Fit once, forecast often: forecasting is ~12 µs.

### `runi/memo`

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `Do` — hit | **14.34** | 0 | **0** |
| `Do` — miss (store + LRU insert) | 694.2 | 358 | 5 |
| `Do` — hit, `RunParallel` 16 threads | 134.9 | 9 | 0 |
| `Hash` — 4-field map | 1,579 | 656 | 36 |
| stampede, 8 concurrent cold callers | 5,332 | 1,287 | 19 |
| stampede, 64 concurrent cold callers | 24,324 | 4,136 | 75 |

The hit path allocates nothing, so the cache is free relative to anything worth
memoizing. **The parallel hit is 135 ns, not 14** — `Store` takes one mutex per
operation. That is stated rather than omitted: at ~7M hits/s aggregate it is far
from the bottleneck for network or model calls, and disqualifying if you are
memoizing sub-microsecond work.

The stampede row is the case single-flight exists for: 64 goroutines on a cold
key cost 24 µs and **one** execution of the function.

### `runi/lazy`

| Measurement | Result |
|---|---|
| `Get` on a cold value | 122 ms |
| `Get` after `Start` had time to run | **8 µs** |
| `All` over five 80 ms values | **81 ms** (serial: 400 ms) |
| `Get` on a resolved value | 73.91 ns, 0 allocs |
| `New` + resolve | 771.1 ns, 3 allocs |
| `Map` chain of 3 | 3,602 ns, 15 allocs |

### What these numbers do not show

- **The head-to-head covers statsmodels, and nothing else.** See
  [Against the Python reference implementations](#against-the-python-reference-implementations).
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

## Against the Python reference implementations

Time-series forecasting is Python's home ground, so the useful question is not
whether Go can do it but what you give up. We measured.

**Method, because it is the only thing that makes these numbers worth reading.**
One generator writes the datasets to CSV **once** (`benchmarks/gen.py`); both
implementations read the same files. Same model order, ARIMAX(1,0,1) with one
exogenous regressor. Same six-point held-out horizon. Same machine, same
session. The control is `naive_rmse` — the error of carrying the last value
forward, which depends only on the data and not on either library. It came out
to **3.7104740961972245 in both**, to every digit, which is how we know the two
were fitted to identical numbers.

```bash
python benchmarks/gen.py                 # write the shared datasets
python benchmarks/bench_statsmodels.py   # statsmodels 0.15.0
go run ./benchmarks/runibench             # runi/arimax
```

### `runi/arimax` vs `statsmodels` SARIMAX

200 independent series, n=500, h=6. statsmodels 0.15.0, Python 3.14, Go 1.25.

| | `runi/arimax` | `statsmodels` SARIMAX | |
|---|---:|---:|---|
| **Fit, n=500** | **0.80 ms** | 33.3 ms | **42× faster** |
| **Fit, n=2,000** | **2.20 ms** | 110.4 ms | **50× faster** |
| **Fit, n=10,000** | **11.7 ms** | 510.8 ms | **44× faster** |
| Per fit, over the 200-trial run | **0.67 ms** | 36.4 ms | **54× faster** |
| Library start-up before the first fit | **0 ms** (compiled in) | 2,063 ms | |
| | | | |
| 95% interval empirical coverage | 94.5% | 94.5% | **identical** |
| Forecast RMSE, h=6 | 1.3169 | 1.3160 | within **0.07%** |
| φ (AR) bias / RMSE | −0.0042 / 0.0508 | −0.0041 / 0.0506 | indistinguishable |
| β (exogenous) bias / RMSE | −0.0029 / 0.0666 | +0.0023 / **0.0336** | **statsmodels 2× better** |
| Naive RMSE *(control — must match)* | 3.7104740961972245 | 3.7104740961972245 | ✅ |

**Read that last-but-one row before the speed rows.** statsmodels recovers the
exogenous coefficient about **twice as precisely** as we do. That is not noise
and it is not a bug — it is the price of the estimator. statsmodels runs exact
maximum likelihood through a Kalman filter; `arimax` uses a staged regression
with ARIMA errors fitted by conditional sum of squares. The staged approach is
what makes it ~50× faster, and it costs real precision in β.

**What is genuinely equivalent:** the forecasts, and the honesty of the
intervals. A 0.07% difference in six-step RMSE is not something a decision would
turn on, and both deliver 94.5% empirical coverage against a nominal 95% — the
number that matters when a forecast informs an action.

**So, honestly: when should you use which?**

| Use `statsmodels` when | Use `runi/arimax` when |
|---|---|
| You need the coefficient itself — it is the finding, as in econometrics | You need the forecast, and the coefficient is a means to it |
| You want exact MLE, diagnostics, SARIMA, state space, a vast library | You want ARIMAX, fast, with calibrated intervals |
| You are in a notebook and 2s of import does not matter | You are in a service, fitting per request or per tenant |
| You want the ecosystem Python has and Go does not | You want one static binary, no runtime, no dependency tree |

A fit every 0.67 ms rather than every 36 ms is what changes architecture. It is
the difference between a nightly batch job that writes forecasts to a table and
**fitting a fresh model inside the request that needs it** — per tenant, per
series, on demand. That is the capability Go is buying here, not a leaderboard
position.

### `runi/memo` vs `functools.lru_cache`

`lru_cache` is the API that inspired `memo`: one decorator, one bound, nothing
to configure. Same experiment on both sides — N callers hit one cold key
simultaneously, with a 5 ms function, counting **how many times the function
actually ran**.

| | `runi/memo` | `functools.lru_cache` |
|---|---:|---:|
| Cache hit | **25.3 ns** | 77.2 ns |
| Cache miss | 553 ns | **181 ns** |
| 8 cold callers → **times the function ran** | **1** | **8** |
| 64 cold callers → **times the function ran** | **1** | **64** |
| Wall clock, 64 cold callers | **5.3 ms** | 14.9 ms |
| TTL | ✅ | ❌ |
| Single-flight | ✅ | ❌ |

The hit path is ~3× faster, and `lru_cache` is **3× faster on a miss** — its
miss is a dict insert, ours also maintains an LRU list and expiry. That is a
real loss and it is in the table.

But the row that matters is the function-ran count. With 64 callers on a cold
key, `lru_cache` calls the expensive function **64 times**; `memo` calls it
**once**. If that function is a model inference or a paid API call, the
difference is not 3× on a nanosecond — it is 64× on the expensive thing. This is
not a flaw in `lru_cache`, which never promised single-flight; it is the reason
`memo` builds it in rather than leaving it to the caller.

### What was not measured

- **Prophet was not run.** It is a different model class — additive trend plus
  seasonality, not ARIMAX — so running it on this data would have measured the
  mismatch, not the library. No claim about Prophet appears here.
- **`pmdarima`, R's `forecast`, and every other implementation** were not run.
- **One machine, one session.** The `ns`/`ms` figures move with the host; the
  ratios are more stable than the absolutes, and the accuracy figures are
  deterministic given the CSVs.
- **Synthetic data.** Correct for measuring estimator bias against a known
  truth, and *not* evidence about either library's accuracy on real series.

### On the coverage figure

Every package is at 100% of statements except `tablelog`, which is at 99.3%. Its last four lines are a sort comparator's tie-break that its own key ordering makes unreachable, two error returns on operations that cannot fail with the arguments this package gives them, and one that needs a cancellation to land inside a retry wait. Each is commented where it sits, with why. Rather than
write tests that execute a line without asserting anything, here is every
statement that is not covered and why:

| Where | What it is | Why no test |
|---|---|---|
| `memo/memo.go:219` | `if back == nil { break }` inside the eviction loop | The loop only runs while `len(entries) > Capacity`, so the LRU list cannot be empty. Unreachable by construction; kept so a future refactor cannot spin forever |
| `arimax/acf.go:68` | Durbin–Levinson bails when the denominator falls below 1e-300 | Requires an autocorrelation structure that is numerically degenerate but not constant. Reachable in principle, not constructible without writing the pathological input by hand |
| `arimax/linalg.go:54` | Householder reflector skipped when `vnorm < 1e-300` | Same: a column that is collinear to within denormal precision |
| `arimax/arimax.go:87` | `Fit` returning a `fitARMA` error | `Fit`'s own length guard is stricter than `fitARMA`'s, so by the time it is called the error cannot occur. Kept because the two guards are in different files and could drift |

All four are defensive guards against a future change, which is exactly the code
that should exist and should not be chased with a synthetic test. A test that
forces an unreachable branch tests the test, not the code.

## Assurance

For teams with a procurement or compliance review. Everything below is
continuously verified in CI, not asserted once.

| | |
|---|---|
| **Dependencies** | **Zero.** `go.mod` declares none, and CI **fails the build** if a `require` line or a non-stdlib package appears in the dependency graph. There is no transitive tree to audit. |
| **Platforms tested** | Linux, macOS, Windows × Go 1.24, 1.25 — six combinations, every push |
| **Concurrency** | `go test -race` on all platforms, every push |
| **Vulnerability scanning** | `govulncheck` **daily** and on every push |
| **Static analysis** | CodeQL weekly, `security-and-quality` query set |
| **Supply-chain posture** | OpenSSF Scorecard, published weekly |
| **Test coverage** | **99.8%** of statements across the eleven packages (the `benchmarks/` commands are excluded; they are programs, not library code); every package at 100% except `tablelog` at 99.3%. Floors are enforced **per package**, so a strong package cannot pay for a weak one |
| **Formatting** | `gofmt` clean, enforced |
| **Benchmarks** | compiled and executed in CI so published figures stay reproducible |
| **Scheduled runs** | CI runs weekly even without commits, so a green badge means "passes on current toolchains", not "passed once" |
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
| **Runink** | `arimax`, `memo`, `lazy` | The team that builds it — see [From the Runink team](#from-the-runink-team) |
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
go test ./...                                 # all three packages
go test -race ./...                           # lazy and memo are concurrent
go test -bench . -benchmem -benchtime=200x ./...
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

## Licence

BSD-3-Clause. See [LICENSE](LICENSE).

---

---

## From the Runink team

`runi` is built by the team behind **[Runink River](https://runink.org/river)**,
an open-source Linux distribution for data and analytics work. River's
documentation is at **[runink.org/river](https://runink.org/river)**, and the
wider docs are at **[docs.runink.org](https://docs.runink.org)**.

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
