---
title: "Documentation"
weight: 1
---

`runi` is a set of eighteen small Go packages for the data and AI work that shows up
inside a running service: describing a column, ranking documents, getting JSON out of a
language model, forecasting a series, versioning a table, deciding whether a signal has
genuinely changed.

Each package stands on its own. Take one, take all eighteen, or copy a file out — the
licence permits it, nothing needs more than the standard library, and only `barn` uses
another package of the set (`avro`).

{{< cards >}}
  {{< card link="guide/" title="User Guide" subtitle="Install it, choose a package, and understand where it is meant to run." >}}
  {{< card link="guide/where-it-runs/" title="Running it in production" subtitle="Services, pods and inference harnesses; versioning and upgrades in Compatibility." >}}
  {{< card link="api/" title="API Reference" subtitle="Every exported symbol and its configuration, generated from the source." >}}
  {{< card link="develop/" title="Developer Guide" subtitle="Build and test, the rules a change keeps, adding a package." >}}
{{< /cards >}}

## The three promises

**Zero dependencies.** Standard library only, in every package. `go.mod` has no `require`
block, so there is no transitive tree to audit and nothing to pin.

**100% coverage, enforced.** Every package carries a coverage floor in CI. The floors are
floors, not targets: a commit that drops a package below its floor fails, and no strong
package's coverage can pay for a weak one's.

**Measured, not asserted.** Performance claims in the repository come with the benchmark
that produced them and the hardware it ran on.
