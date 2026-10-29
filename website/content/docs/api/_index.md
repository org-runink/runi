---
title: "API Reference"
weight: 3
sidebar:
  open: true
---

Every exported symbol in every package, generated from the source so it cannot drift from it.
For the canonical reference, including example output and unexported detail,
see [pkg.go.dev](https://pkg.go.dev/github.com/org-runink/runi).

| Package | What it does |
| --- | --- |
| [`runi/stats`](stats/) | Describe, split and scale a column, and test whether a relationship is real |
| [`runi/arimax`](arimax/) | Forecast a series using the things that drive it |
| [`runi/season`](season/) | Find the season and the trend breaks, then split the series apart |
| [`runi/bm25`](bm25/) | Rank documents by the words they share |
| [`runi/salvage`](salvage/) | Get JSON out of a language model's reply, without guessing |
| [`runi/avro`](avro/) | Read and write Avro and Avro OCF files |
| [`runi/barn`](barn/) | Append-only versioned tables with time travel, no database |
| [`runi/memo`](memo/) | Memoise an expensive call, one allocation per miss |
| [`runi/lazy`](lazy/) | Compute once, on first use, safely under concurrency |
| [`runi/budget`](budget/) | One deadline for a whole request, split across its phases |
| [`runi/chain`](chain/) | Tamper-evident chaining and verification of records |
| [`runi/toon`](toon/) | A compact object notation that costs fewer tokens than JSON |
| [`runi/toml`](toml/) | Read TOML v1.0.0 configuration files into Go values, strictly if asked |
| [`runi/drift`](drift/) | Streaming change detection over a moving baseline |
| [`runi/rate`](rate/) | Cumulative counters into per-interval rates, across wraps and resets |
| [`runi/tree`](tree/) | Subtree sums and maxima of a hierarchy, kept current |
| [`runi/sustain`](sustain/) | Threshold decisions that do not flap |
| [`runi/throttle`](throttle/) | Cap how often something acts |
