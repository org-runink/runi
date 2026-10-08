<p align="center">
  <img src="assets/runi-logo.jpg" alt="Runi, the Runink Australian Shepherd, in AR goggles and a tech harness" width="200">
</p>

<h1 align="center">runi</h1>

<p align="center">
  <em>Runi is the Runink Australian Shepherd.<br>
  She keeps the flock together so nothing has to be rounded up twice.</em>
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/org-runink/runi"><img src="https://pkg.go.dev/badge/github.com/org-runink/runi.svg" alt="Go Reference"></a>
  <img src="https://img.shields.io/badge/dependencies-0-blue" alt="zero dependencies">
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

`runi` is the shared Go toolkit behind Runink's products. The names follow the
water:

| | |
|---|---|
| **Runink River** | the operating system |
| **Runink TIDE** | the console and control plane |
| **runi** | the Go packages both of them are built on |

## Artwork and marks

<p align="center">
  <img src="assets/runi-wallpaper.jpg" alt="Runi running through a neon-lit street in the rain" width="760">
</p>

Runi, the Runink logo and the Runink marks are held by **Runink** and are **not**
covered by this repository's BSD-3-Clause licence. The code is yours to use; the
artwork is not. Detail, and what you may do in a fork, in
[TRADEMARKS.md](TRADEMARKS.md).

This project is not affiliated with or endorsed by Google. "Go" and the Go logo
are trademarks of Google LLC.
