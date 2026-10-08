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

The latest minor release is supported. This module is pre-1.0; until v1.0.0 the
import path is stable but the API may change in a minor release, and such
changes are listed in the release notes.

## What reduces risk here, concretely

Procurement questionnaires tend to ask these. Honest answers:

- **Zero dependencies.** `go.mod` declares no requirements, and CI fails the
  build if one appears. There is no transitive dependency tree to audit, no
  upstream maintainer to trust, and no supply-chain surface beyond the Go
  standard library and toolchain itself.
- **No network, no filesystem, no subprocesses.** None of the three packages
  opens a socket, reads a file, or executes anything. They compute over values
  the caller supplies.
- **No reflection on untrusted input, no `unsafe`, no cgo.**
- **Deterministic.** Same input, same output. No goroutine-dependent results, no
  `math/rand`, no clock reads except where a clock is explicitly injected
  (`memo.Options.Now`, for testing expiry).
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
