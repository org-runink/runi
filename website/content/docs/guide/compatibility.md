---
title: "Compatibility"
weight: 4
---

## Versioning

`runi` follows semantic versioning. Within a major version, code that compiles against
an earlier minor release compiles against a later one.

A major version is where that promise is allowed to break, and it is used deliberately
rather than avoided: it is better to settle an awkward name once, in the open, than to
carry it forever because renaming it would be a breaking change.

## What "breaking" means in practice

The Go module proxy makes a published version permanent — it cannot be withdrawn, and an
older version keeps resolving for anyone pinned to it. So an upgrade is always opt-in,
and staying on an older major version is a supported position rather than a problem to
be fixed.

If a rename is the right call, the release notes say what moved and what it is now.
[The changelog](https://github.com/org-runink/runi/blob/main/CHANGELOG.md) is the record.

## Go versions

The module declares `go 1.24` and CI tests against 1.24, 1.25 and 1.26.

Go's forward compatibility means that declaring `go 1.24` is a floor, not a preference:
the module builds with any newer toolchain. The floor is raised only when a package
genuinely needs something newer.

## Platforms

Pure Go, standard library only, no cgo and no build tags for any particular operating
system. Anything the Go toolchain targets will build, including `GOOS=linux GOARCH=arm64`
and `CGO_ENABLED=0` static builds for a scratch container.
