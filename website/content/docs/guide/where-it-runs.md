---
title: "Where it runs"
weight: 3
---

`runi` is written for code that runs *inside* a long-lived Go process — a service, a
Kubernetes pod, an agent harness next to a local inference server. That shapes the
design more than any individual API does.

## Why a long-lived process changes the design

A notebook fits a model once and the cost disappears into the session. A service fits it
per request, or per tenant, thousands of times an hour, and the cost is the service.

So the packages here are built to be constructed cheaply and reused, to allocate on a
miss rather than on every call, and to hold no global state that would make two tenants
in one process interfere. Where a package has a hot path, the repository carries the
benchmark that measures it rather than a claim that it is fast.

## Alongside Python, not against it

Python and PySpark are better than Go at offline data science, model exploration and
distributed ETL, and nothing here is trying to replace them.

The split that works is: explore and fit offline in pandas, scikit-learn, statsmodels or
PySpark; then serve, rank and orchestrate online in Go. `runi` is the second half.

Where a definition could differ between the two, it does not: `stats.Quantile` matches
NumPy's default (R type 7), so a threshold you set in a notebook means the same thing in
the service that enforces it. That property is the point of the package, and the
repository tests it rather than asserting it.

## In a Kubernetes pod

Two things matter in a pod that do not matter on a workstation.

**The dependency tree is your attack surface and your image size.** A zero-dependency
module adds one module to the build graph and nothing underneath it: no transitive tree
to audit, no native ABI to pin, one static binary at the end.

**Memory is a limit, not a suggestion.** The scheduler places a pod on its *requests*
and the kernel kills it on its *use*, so a library that allocates per call rather than
per miss is the difference between a pod that fits its limit and one that is OOMKilled
under load. That is why `memo` is single-flight and why the allocation counts in this
repository are tested, not estimated.

## Next to a local inference server

An agent harness around a local model spends its time on a small number of jobs that
`runi` covers directly: getting structured output back out of a reply
([`salvage`](../../api/salvage/), [`toon`](../../api/toon/)), retrieving context without
a second model ([`bm25`](../../api/bm25/)), holding one deadline across a multi-phase
turn ([`budget`](../../api/budget/)), and backing off when the engine is saturated
([`throttle`](../../api/throttle/)).

All four are the kind of thing an individual harness tends to re-implement badly, and
all four are standard-library-only here — which matters when the harness itself is
meant to stay dependency-light.
