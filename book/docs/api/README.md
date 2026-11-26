# API Reference

Every exported symbol in every package, generated from the source so it cannot drift from it.
For the canonical reference, including example output and unexported detail,
see [pkg.go.dev](https://pkg.go.dev/github.com/org-runink/runi).

| Package | What it does |
| --- | --- |
| [`runi/stats`](stats/README.md) | Describe, split and scale a column, and test whether a relationship is real |
| [`runi/arimax`](arimax/README.md) | Forecast a series using the things that drive it |
| [`runi/season`](season/README.md) | Find the season and the trend breaks, then split the series apart |
| [`runi/bm25`](bm25/README.md) | Rank documents by the words they share |
| [`runi/salvage`](salvage/README.md) | Get JSON out of a language model's reply, without guessing |
| [`runi/avro`](avro/README.md) | Read and write Avro and Avro OCF files |
| [`runi/barn`](barn/README.md) | Append-only versioned tables with time travel, no database |
| [`runi/memo`](memo/README.md) | Memoise an expensive call, one allocation per miss |
| [`runi/lazy`](lazy/README.md) | Compute once, on first use, safely under concurrency |
| [`runi/budget`](budget/README.md) | One deadline for a whole request, split across its phases |
| [`runi/chain`](chain/README.md) | Tamper-evident chaining and verification of records |
| [`runi/toon`](toon/README.md) | A compact object notation that costs fewer tokens than JSON |
| [`runi/toml`](toml/README.md) | Read TOML v1.0.0 configuration files into Go values, strictly if asked |
| [`runi/drift`](drift/README.md) | Streaming change detection over a moving baseline |
| [`runi/rate`](rate/README.md) | Cumulative counters into per-interval rates, across wraps and resets |
| [`runi/tree`](tree/README.md) | Subtree sums and maxima of a hierarchy, kept current |
| [`runi/sustain`](sustain/README.md) | Threshold decisions that do not flap |
| [`runi/throttle`](throttle/README.md) | Cap how often something acts |
