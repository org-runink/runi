---
title: "Documentation"
weight: 1
---

`runi` is a set of seventeen small Go packages for the data and AI work that shows up
inside a running service: describing a column, ranking documents, getting JSON out of a
language model, forecasting a series, versioning a table, deciding whether a signal has
genuinely changed.

Each package is independent. Take one, take all seventeen, or copy a file out — the
licence permits it and nothing depends on anything else.

{{< cards >}}
  {{< card link="guide/" title="User Guide" subtitle="Install it, choose a package, and understand where it is meant to run." >}}
  {{< card link="api/" title="API Reference" subtitle="Every exported symbol, generated from the source." >}}
{{< /cards >}}

## The three promises

**Zero dependencies.** Standard library only, in every package. `go.mod` has no `require`
block, so there is no transitive tree to audit and nothing to pin.

**100% coverage, enforced.** Every package carries a coverage floor in CI. The floors are
floors, not targets: a commit that drops a package below its floor fails, and no strong
package's coverage can pay for a weak one's.

**Measured, not asserted.** Performance claims in the repository come with the benchmark
that produced them and the hardware it ran on.
