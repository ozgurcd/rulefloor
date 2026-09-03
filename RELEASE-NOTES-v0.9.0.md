# Rulefloor v0.9.0

Rulefloor v0.9.0 makes execution-heavy full checks substantially faster while
preserving rule isolation, ledger bytes, and every stable machine contract.

## Compile once, execute in isolation

During a full `rulefloor check`, Go bindings that share a package and build-tag
set now share one compiled test binary. Rulefloor still starts a fresh process
for every bound test, in deterministic ledger order. Tests are not batched or
parallelized, package globals cannot leak from one rule execution to the next,
and every selected test receives a fresh `TestMain`. The standard ten-minute Go
test timeout and skip/failure classification remain in force.

The compile cache is memory-only and scoped to one command. Temporary binaries
are created in a private temporary directory and removed at the end of the
check. Subprocesses continue to use direct argument vectors without a shell.
Single-rule `validate`, `prove --run`, and `check --only` retain their existing
direct execution behavior.

## Measured result

On one representative 244-row execution-heavy ledger, the same full check took
262.75 seconds with v0.8.1 and 29.86 seconds with v0.9.0 on the same host. The
human output was byte-for-byte identical. Actual gains depend on how many rows
share packages and build tags, the Go build cache, and the tests themselves.

## Freshness and trust boundary

Rulefloor does not persist compiled binaries or cache test results. It also does
not cache Gograph responses: exact protected-symbol checks continue to require
current, complete, precise, typed-complete evidence on every evaluation and
fail closed when that evidence is unavailable or insufficient.

Compile reuse does not expand Rulefloor's semantic claims. Rulefloor still
fingerprints the tagged test span, not its helper/dependency closure, and an
executed passing test is evidence of that test run rather than proof of an
arbitrary natural-language invariant.

## Compatibility

- `RULE-FLOOR.md` remains canonical, human-readable, and six columns.
- Existing hashes, proofs, covered symbols, execution policies, FLOOR, and
  RED-PROOFS values are unchanged; no migration or rehash is required.
- Human pass/fail ordering and exit semantics are unchanged.
- `rulefloor.version.v1`, `rulefloor.validation.v1`,
  `rulefloor.capabilities.v1`, `rulefloor.covers.v1`, and
  `rulefloor.ledger-diff.v1` are unchanged.
- Gograph remains optional for rows that do not request exact structural reach.

Official v0.9.0 archives target Linux and macOS on amd64 and arm64, require no
CGO, and are accompanied by `checksums.txt`. Go 1.27.0 or newer is required to
build Rulefloor v0.9.0.
