# Developer Guide

How to build and test `runi`, the rules every change keeps, and how to add to it. The
source is at [github.com/org-runink/runi](https://github.com/org-runink/runi), under
BSD-3-Clause.

## Build and test

`runi` is one Go module with no dependencies, so a checkout and a supported Go toolchain
(1.24, 1.25 or 1.26) are all you need:

```bash
git clone https://github.com/org-runink/runi
cd runi
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
gofmt -l .                              # must print nothing
go test -run '^$' -bench . -benchtime 1x ./...   # every benchmark still compiles and runs
```

CI runs the same commands on each supported Go version, with `GOTOOLCHAIN=local` so a
newer toolchain is never fetched behind the test's back.

## The rules a change keeps

- **Standard library only.** `go.mod` has no `require` block, and CI fails if one appears,
  if the module graph holds any other module, or if any imported package lives outside the
  standard library.
- **Per-package coverage floors.** Each package has its own coverage floor in CI. A change
  that drops a package below its floor fails; a floor is raised when coverage genuinely
  rises and never lowered.
- **Independent packages.** A package imports no other `runi` package, so any one of them
  can be taken alone or copied out. The one exception is `barn`, which stores its data in
  `avro`'s format and imports it.
- **Measured claims.** A performance claim comes with the benchmark that produced it and
  the hardware it ran on. The cross-language comparisons live in
  [`benchmarks/`](https://github.com/org-runink/runi/tree/main/benchmarks).

## Adding or changing a package

1. Put the package in its own top-level directory, with a package doc comment and runnable
   `Example` functions that show the main use.
2. Bring its coverage to the level the other packages hold, and add its floor to the
   coverage step of `.github/workflows/ci.yml`.
3. Regenerate the [API Reference](../api/README.md): the pages are generated from the source, and
   the docs workflow fails if they are stale. Add the package to the docs workflow's list of
   packages that must have an API page, and to [Choosing a package](../guide/choosing/README.md).
4. Record the change in
   [CHANGELOG.md](https://github.com/org-runink/runi/blob/main/CHANGELOG.md). Exported API
   follows the [compatibility rules](../guide/compatibility/README.md).

Sign off every commit (`git commit -s`).

## The documentation site

These pages are a Hugo site in `website/` (the Hextra theme, vendored, so the build needs no
network):

```bash
hugo --source website --minify --destination ~/.cache/runi-docs-build
```

## Security

Report a vulnerability privately, as described in
[SECURITY.md](https://github.com/org-runink/runi/blob/main/SECURITY.md), never in a public
issue.
