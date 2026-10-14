# Security policy

## Reporting a vulnerability

Report privately through GitHub's
[security advisory form](https://github.com/org-runink/runi/security/advisories/new).
Please do not open a public issue for a suspected vulnerability.

Include what you can: affected package and version, what an attacker achieves,
and a reproduction. A failing test is ideal.

**What to expect:** acknowledgement within 5 working days, an assessment within
10, and a fix released before public disclosure where one is warranted. You will
be credited unless you ask not to be.

## Supported versions

The latest v1 minor release is supported. Within v1 the exported API does not
break (see [Compatibility](README.md#compatibility)), so a security fix never
requires a code change on your side beyond updating the version.

## What reduces risk here, concretely

Procurement questionnaires tend to ask these. Honest answers:

- **Zero dependencies.** `go.mod` declares no requirements, and CI fails the
  build if one appears. There is no transitive dependency tree to audit, no
  upstream maintainer to trust, and no supply-chain surface beyond the Go
  standard library and toolchain itself.
- **No network, no filesystem, no subprocesses.** No package opens a socket,
  reads a file, or executes anything. They compute over values the caller
  supplies; `barn` reads and writes only through the `Store` the caller
  passes in.
- **No `unsafe`, no cgo.** Reflection is used in exactly two places, both on
  the caller's own destination types: `toon` and `salvage` decode text a model
  wrote into the struct the caller names, under `encoding/json`'s rules. A
  hostile reply can fill fields of that type with any value, but it cannot
  name a type or reach anything outside it; the only methods it can cause to
  run are the destination type's own `UnmarshalJSON` and `UnmarshalText`.
- **Deterministic.** Same input, same output. No goroutine-dependent results
  and no global random source: `throttle.Backoff` draws only from a source the
  caller seeds. The clock is read only by `memo` (expiry), `budget` (the
  deadline) and `barn` (file names and retention), each of which takes an
  injected clock instead; every other timed type is told the time of each
  call.
- **Scanned continuously.** `govulncheck` daily, CodeQL weekly with the
  `security-and-quality` query set, OpenSSF Scorecard weekly.

## What this module does NOT do

Stated so nobody infers it:

- It is **not** a cryptographic library. `memo.Hash` uses SHA-256 to derive cache
  keys; it is a key-derivation convenience, not a security boundary, and must not
  be used to authenticate anything.
- It performs **no authentication, authorisation or input validation**. A cache
  key derived from untrusted input is still untrusted input.
- `memo` is **in-process only**. Nothing is shared between replicas, and nothing
  survives a restart. There is no network listener to attack.
