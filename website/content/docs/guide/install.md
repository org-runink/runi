---
title: "Installing"
weight: 1
---

```bash
go get github.com/org-runink/runi
```

Then import only the packages you use:

```go
import (
    "github.com/org-runink/runi/stats"
    "github.com/org-runink/runi/bm25"
)
```

## What you are not taking on

`runi`'s `go.mod` has no `require` block. Not a short one — none. Every package is
standard library only, so adding `runi` adds exactly one module to your build graph and
nothing underneath it.

That has a practical consequence worth stating plainly: there is no transitive tree to
audit, no native ABI to pin, and no supply chain to review beyond this one repository.

## Supported Go versions

The module declares `go 1.24` and is tested against Go 1.24, 1.25 and 1.26.

Go's forward compatibility means a `go 1.24` module builds with any newer toolchain, so
declaring 1.24 is a floor rather than a preference. It is raised only when a package
genuinely needs something newer, never to keep current.

## Taking a file instead

The packages are close to independent, and are written to be read. If you want one
function rather than a dependency, the BSD-3-Clause licence permits copying the file.

There is exactly one cross-package import in the whole module: `barn` imports `avro`,
because a barn table's data and log files *are* Avro files. Everywhere else, a package
referring to another — `drift` discussing `season` residuals, `sustain` discussing
`rate` — does so in its documentation only, not in an import block. So taking `stats`
or `bm25` or `toon` on its own takes nothing with it; taking `barn` takes `avro` too.
