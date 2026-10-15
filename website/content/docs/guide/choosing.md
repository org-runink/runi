---
title: "Choosing a package"
weight: 2
---

Seventeen packages, indexed by the job rather than the name.

## Understanding a column of numbers

| You want to | Package |
|---|---|
| Describe it — mean, median, quantiles, IQR | [`stats`](../../api/stats/) |
| Split and scale it for a model, the same way twice | [`stats`](../../api/stats/) |
| Ask whether a relationship is real, not noise | [`stats`](../../api/stats/) |

Start here. `stats` is the package most services need and the one most likely to be the
only one you take.

## A series that moves over time

| You want to | Package |
|---|---|
| Forecast it using the things that drive it | [`arimax`](../../api/arimax/) |
| Find what repeats, and where the trend broke | [`season`](../../api/season/) |
| Notice when it changes regime, as it happens | [`drift`](../../api/drift/) |
| Turn a cumulative counter into a rate, across wraps and resets | [`rate`](../../api/rate/) |
| Attribute a change to the part of a hierarchy responsible | [`tree`](../../api/tree/) |

## Deciding whether to act

| You want to | Package |
|---|---|
| Hold a decision until it stops flapping | [`sustain`](../../api/sustain/) |
| Act less often than you are asked to — rate limit, back off, trip a breaker | [`throttle`](../../api/throttle/) |
| Give a whole request one deadline, split honestly across its phases | [`budget`](../../api/budget/) |

## Working with a language model

| You want to | Package |
|---|---|
| Get JSON out of a reply, without guessing | [`salvage`](../../api/salvage/) |
| Send and receive a table that costs fewer tokens than JSON | [`toon`](../../api/toon/) |
| Rank documents by the words they share, with no model at all | [`bm25`](../../api/bm25/) |

## Storing and moving records

| You want to | Package |
|---|---|
| Read and write Avro and Avro OCF | [`avro`](../../api/avro/) |
| Keep append-only versioned tables with time travel, no database | [`barn`](../../api/barn/) |
| Make records nobody can quietly rewrite | [`chain`](../../api/chain/) |

## Making a call cheaper

| You want to | Package |
|---|---|
| Memoise it, with single-flight so a miss happens once | [`memo`](../../api/memo/) |
| Defer it until first use, and start it early if you can | [`lazy`](../../api/lazy/) |
