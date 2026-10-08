<p align="center">
  <img src="https://raw.githubusercontent.com/org-runink/runi/main/assets/runi-logo.jpg" alt="Arlo, the Runink Australian Shepherd, wearing the Runi rig: AR goggles and a powered herding harness" width="220">
</p>

<h1 align="center">runi</h1>

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
  <img src="https://img.shields.io/badge/coverage-87.0%25%20%7C%2082.4%25%20%7C%2093.1%25-brightgreen" alt="coverage">
  <img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="BSD-3-Clause">
</p>

---

**Three small Go packages for making expensive work cheaper: forecast it, cache
it, or start it early.** Standard library only — no dependencies, in any package.

```bash
go get github.com/org-runink/runi
```

| Package | One line | Coverage |
|---|---|---|
| [`runi/arimax`](#runiarimax--forecasting-with-external-drivers) | Forecast a series using the things that drive it | 87.0% |
| [`runi/memo`](#runimemo--memoization-with-single-flight) | Don't compute the same thing twice | 82.4% |
| [`runi/lazy`](#runilazy--deferred-values-you-can-start-early) | Compute it before anyone asks | 93.1% |

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
| 🥽 **the goggles** | [`arimax`](#runiarimax--forecasting-with-external-drivers) | See what is coming — and how far ahead the view can honestly be trusted |
| 🧠 **the memory core** | [`memo`](#runimemo--memoization-with-single-flight) | Never chase the same thing twice, even when sixty callers ask at once |
| ⚡ **the harness** | [`lazy`](#runilazy--deferred-values-you-can-start-early) | Already moving before the call comes, without computing what is never asked for |

<p align="center">
  <img src="https://raw.githubusercontent.com/org-runink/runi/main/assets/runi-wallpaper.jpg" alt="Arlo running through a neon-lit street in the rain, wearing the Runi goggles and harness" width="820">
</p>

<p align="center">
  <em>Runink's mascot is a working dog, not a logo.<br>
  These packages are built the same way: measured, documented, and honest about their limits.</em>
</p>

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

- **No head-to-head against other libraries was run.** Only our own measurements
  appear here. No claim of being faster or more accurate than any alternative is
  made, because that experiment was not performed.
- **Accuracy is measured on synthetic data**, which is correct for estimator bias
  and is *not* evidence of accuracy on real series, where the model is
  misspecified by construction.
- **CSS is not exact maximum likelihood.** They agree closely at these series
  lengths but are different estimators.
- **Prediction intervals assume Gaussian errors.** Heavy-tailed residuals produce
  intervals too narrow in the tails.

---

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

Issues and pull requests welcome. Two expectations:

1. **No dependencies.** It is why these are usable in a CLI, a sidecar, a WASM
   build or on a device. A PR adding a `require` line needs a strong argument.
2. **Claims need a test.** If you state an accuracy or performance property,
   there must be a test that fails when it stops holding.

## Licence

BSD-3-Clause. See [LICENSE](LICENSE).

---

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
