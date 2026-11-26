# Runi

BSD-3-Clause · zero dependencies

**Eighteen small Go packages for data and AI work**

Standard library only, in every package. Measured, not asserted: every package holds a 100% coverage floor that CI enforces on each commit.

[Read the User Guide](docs/guide/README.md)

- **Nothing to vet**: No transitive tree, no ABI to pin, no supply chain to review. `go.mod` has no `require` block at all.
- **Built for services**: Designed to run inside Go services, Kubernetes pods and local agentic harnesses, where a process is long-lived and allocation matters.
- **Complements Python**: Explore and fit offline in pandas or PySpark; serve, rank and orchestrate online in Go. The definitions match where it matters.
