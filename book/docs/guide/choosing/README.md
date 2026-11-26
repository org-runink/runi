# Choosing a package

Eighteen packages, indexed by the job rather than the name.

## Understanding a column of numbers

| You want to | Package |
|---|---|
| Describe it — mean, median, quantiles, IQR | [`stats`](../../api/stats/README.md) |
| Split and scale it for a model, the same way twice | [`stats`](../../api/stats/README.md) |
| Ask whether a relationship is real, not noise | [`stats`](../../api/stats/README.md) |

Start here. `stats` is the package most services need and the one most likely to be the
only one you take.

## A series that moves over time

| You want to | Package |
|---|---|
| Forecast it using the things that drive it | [`arimax`](../../api/arimax/README.md) |
| Find what repeats, and where the trend broke | [`season`](../../api/season/README.md) |
| Notice when it changes regime, as it happens | [`drift`](../../api/drift/README.md) |
| Turn a cumulative counter into a rate, across wraps and resets | [`rate`](../../api/rate/README.md) |
| Attribute a change to the part of a hierarchy responsible | [`tree`](../../api/tree/README.md) |

## Deciding whether to act

| You want to | Package |
|---|---|
| Hold a decision until it stops flapping | [`sustain`](../../api/sustain/README.md) |
| Act less often than you are asked to — rate limit, back off, trip a breaker | [`throttle`](../../api/throttle/README.md) |
| Give a whole request one deadline, split honestly across its phases | [`budget`](../../api/budget/README.md) |

## Working with a language model

| You want to | Package |
|---|---|
| Get JSON out of a reply, without guessing | [`salvage`](../../api/salvage/README.md) |
| Send and receive a table that costs fewer tokens than JSON | [`toon`](../../api/toon/README.md) |
| Rank documents by the words they share, with no model at all | [`bm25`](../../api/bm25/README.md) |

## Storing and moving records

| You want to | Package |
|---|---|
| Read and write Avro and Avro OCF | [`avro`](../../api/avro/README.md) |
| Keep append-only versioned tables with time travel, no database | [`barn`](../../api/barn/README.md) |
| Make records nobody can quietly rewrite | [`chain`](../../api/chain/README.md) |

## Reading configuration

| You want to | Package |
|---|---|
| Read a TOML file into structs, and refuse keys nobody reads | [`toml`](../../api/toml/README.md) |

## Making a call cheaper

| You want to | Package |
|---|---|
| Memoise it, with single-flight so a miss happens once | [`memo`](../../api/memo/README.md) |
| Defer it until first use, and start it early if you can | [`lazy`](../../api/lazy/README.md) |
