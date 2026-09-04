# Rulefloor v0.9.1

Rulefloor v0.9.1 adds opt-in timing diagnostics for investigating slow checks
without changing normal gate behavior or any stable machine contract.

## Bounded check timings

Run:

```text
rulefloor check --timings
```

After the ordinary check output, Rulefloor reports total elapsed time,
aggregate package-compilation time, and at most the ten slowest compile groups
and ten slowest armed-row evaluations. Compile attempts are observed inside the
existing per-package/build-tag cache, so later rows that reuse a compiled test
binary do not inflate the compilation count.

An armed-row duration covers that row's complete evaluation: source extraction,
static binding integrity, protected-symbol graph evidence, and any selected Go
test execution. The first executed row in a compile group also bears its
compilation delay. `check --only` retains its direct single-test path and
therefore reports total and row time without a separate compile group.

Timing values depend on host load, Go build caches, graph state, and test
behavior. They are human diagnostics, not a versioned JSON interface or
reproducible evidence.

## Compatibility

- Without `--timings`, human output is unchanged.
- Rule evaluation order, process isolation, exit codes, ledger serialization,
  test fingerprints, proofs, FLOOR, and RED-PROOFS are unchanged.
- `rulefloor.version.v1`, `rulefloor.validation.v1`,
  `rulefloor.capabilities.v1`, `rulefloor.covers.v1`, and
  `rulefloor.ledger-diff.v1` are unchanged.
- External graph evidence remains optional for rows that do not request exact
  structural reach.

Official archives target Linux and macOS on amd64 and arm64, use
`CGO_ENABLED=0`, and are accompanied by `checksums.txt`. Go 1.27.0 or newer is
required to build Rulefloor v0.9.1.
