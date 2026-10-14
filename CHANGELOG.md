# Changelog

What changed in each release of `github.com/org-runink/runi`, newest first. A
change that can break a caller is listed under **Breaking** and says what to
do about it. The compatibility promise these entries are measured against is
in the README, under [Compatibility](README.md#compatibility).

Releases before the first entry here are listed only as tags; their commit
messages are the record.

## Unreleased

### Breaking

- **`tablelog` is now `barn`.** The import path is
  `github.com/org-runink/runi/barn`, the package name is `barn`, and error
  messages start `barn:`. Rename the import and the identifier; nothing else
  in the API changed. **Tables already on disk are unaffected:** the schemas
  written into new files say `org.runink.store.barn` where they used to say
  `org.runink.store.tablelog`, but files are decoded by field position and the
  stored schema is not compared, so a table written by v0.8.0 opens, reads,
  time-travels and accepts writes. `TestTablesWrittenAsTablelogStillOpen`
  rewrites every file of a table the way v0.8.0 wrote it and checks all four.

### Added

- **`toon.EncodeWith` and `toon.Options`.** The delimiter can be a comma (the
  default), a tab or a pipe, declared in each array header the way the TOON
  spec does (`rows[2\t]{id\tnote}:`). A value is quoted only when it contains
  the active delimiter, so under tab, prose with commas needs no quotes.
- **`toon.Decode` reads every delimiter**, header by header, and the spec's
  keyless root array (`[3]: a,b,c`, `[2]{id,name}:`). `Encode` still writes a
  top-level list under the key `items`.
- **`stats.Slope`**, the least-squares slope of a series against its index:
  `Trend(y).Slope` bit for bit, without the residual pass and t-distribution
  tail `Trend` spends most of its time on. About 3× faster than `Trend` at 365
  points; use `Trend` when the slope has to be tested.

### Changed

- **`memo.Store` misses allocate once instead of six times.** The recency
  order is a list threaded through the entries rather than a
  `container/list` (which cost an element and an interface box per store),
  a full store reuses the struct of the entry it evicts, and the channel that
  wakes coalesced callers is made only when a caller actually waits. A miss
  at capacity went from about 690 ns, 6 allocations and 336 B to about 355 ns,
  1 allocation and 48 B; hits still allocate nothing. Behaviour is unchanged
  and pinned by the existing LRU, capacity and single-flight tests;
  `TestAllocationBudgets` pins the allocations.
- **`stats.Quantile` and `stats.Median` allocate at most once.** They no longer
  go through `Quantiles`: a column of up to 32 values is ordered on the stack
  (no allocation), a longer one is copied once and only the order statistics
  the quantile reads are put in place. The median of three values went from
  3 allocations and about 100 ns to none and about 20 ns. Results are those of
  `Quantiles`, held to it by a test.
- **`toon` no longer goes through JSON.** Structs, maps, slices, pointers and
  scalars are converted directly, in both directions, under `encoding/json`'s
  naming rules and with its errors. Types with their own JSON or text
  marshalling (`time.Time`), embedded structs, and the `,string` and
  `,omitzero` options are still handed to `encoding/json`. On 2,000 rows,
  decoding into structs makes 12.8k allocations instead of 86k and decoding
  into a map 12k instead of 50k (both pinned by `TestAllocationBudgets`).
- **`toon` writes `-0` as `0`**, the spec's canonical form. Every other float
  is written with the same digits as before.
- **Faster, with the same results:** `barn` commits no longer re-list the whole
  log and reads no longer pay for a compressor per file; `salvage` decodes a
  reply in one pass; `bm25` builds its index in one pass per token and no longer
  allocates a map per document a query touches; `stats` fits `Trend` in two
  passes and `Pearson` in one, and ranks `Spearman` without a rank column;
  `season`'s `Classical`, `Decompose` and outlier filter do less work for
  bit-identical output. Each commit message records its profile and figures.

### Fixed

- **`season.Decompose` kept a season's sharp features and stopped inventing
  breaks.** Before the season was fitted, each point was checked against its
  neighbours in time, so a feature that recurs every cycle -- a two-day
  weekend dip in a weekly series -- was replaced as an outlier every week, and
  the fitted season lost its most distinctive shape (on a clean series with a
  weekend dip of -12 and -13, a season of -1.7 and -3.6, and residuals of about
  -12 on those days). Points are now checked against the same phase in the
  neighbouring cycles, which keeps a recurring feature and still removes a
  one-off spike. Separately, the first-pass trend line was a least-squares fit,
  which takes part of the season and is tilted by a spike; on a clean series
  the error left a small ripple that the break search paid to explain with
  changepoints that do not exist (`[7 14 21]` on a straight trend). It is now a
  seasonal Theil–Sen line: the median of differences one period apart, which a
  season cancels exactly and a spike barely moves. Both clean series now
  decompose exactly, with no changepoints; the spike and outlier tests are
  unchanged and pass. The cost is within measurement noise.
- **`barn` orders the live file set by commit version, not by the clock.** File
  names start with a creation timestamp, and ordering by name could put two
  files from the same microsecond in the wrong order, which surfaced as a
  merge returning rows in the wrong order on Windows.
- **`stats.Pearson`** returns NaN for a constant column whose mean is not
  exactly representable, as documented; it used to correlate the rounding
  residue and report a confident 1. Columns of very large or very small
  magnitude (around 1e152 or 1e-157) no longer overflow or underflow.
- **`stats.Spearman`** ranks -0 and +0 as equal and places every NaN above every
  number; the radix sort used to rank the two zeroes apart and put a negative
  NaN below negative infinity.
- **Go 1.24 is really supported.** `go.mod` said `go 1.25`, which a 1.24
  toolchain refuses; CI's 1.24 jobs passed only because the go command quietly
  downloaded 1.25. `go.mod` now says `go 1.24`, every test passes on go1.24.0,
  and CI pins the toolchain the matrix names. Go 1.26 joins the matrix.
- Four test comments credited a defect to an internal product; they now say
  what the defect was and not who found it.

## Earlier releases

v0.1.0 to v0.8.0 are tagged in this repository. Their changes are in the
commit history between tags (`git log v0.7.2..v0.8.0`, for example).
