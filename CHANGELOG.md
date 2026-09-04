# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.9.1] - 2026-09-04

### Added

- `rulefloor check --timings` appends a bounded human diagnostic containing
  total elapsed time, aggregate Go package-compilation time, the ten slowest
  compile groups, and the ten slowest armed-row evaluations.
- Compile timing observes the existing per-package/build-tag cache directly;
  cached executions do not appear as additional compilation attempts.

### Compatibility

- Timing output is strictly opt-in. Ordinary `check` output, ledger bytes,
  hashes, ratchets, execution order, exit codes, and every stable machine JSON
  schema remain unchanged.
- Diagnostics are transient measurements, not deterministic evidence or a new
  machine interface. Row time includes static, graph, and any executed-test
  work; selected `check --only` does not split out its direct Go invocation as
  a compile group.

## [0.9.0] - 2026-09-04

### Changed

- Full `check` now compiles one Go test binary per package and build-tag set,
  then runs each bound test in its own fresh process. This removes repeated
  package compilation without batching tests, sharing package globals, changing
  deterministic ledger-order output, or weakening the standard ten-minute test
  timeout.
- Compile results live only for one `check` invocation and temporary binaries
  are removed afterward. `validate`, `prove --run`, and selected `check --only`
  retain their existing direct single-test execution path.

### Performance

- On a representative 244-row execution-heavy ledger, the same full check fell
  from 262.75 seconds with v0.8.1 to 29.86 seconds with v0.9.0 on the same host.
  Human output was byte-for-byte identical. Results vary with package layout,
  build cache state, and test runtime.

### Security

- Go compilation and test execution continue to use direct argument vectors,
  never a shell. Each selected test remains process-isolated with a fresh
  `TestMain` and package state.
- Gograph evidence is deliberately not cached: every exact-reach evaluation
  retains its existing current, complete, precise, typed-complete fail-closed
  checks.

### Compatibility

- Ledger bytes, rule hashes, ratchets, command behavior, exit codes, and all
  stable v1 machine schemas are unchanged. No migration or rehash is required.

## [0.8.1] - 2026-09-02

### Added

- Additive `before_sentence_sha256` and `after_sentence_sha256` fields on every
  `sentence_changed` entry in `rulefloor.ledger-diff.v1`. They are full
  lowercase SHA-256 digests of the parsed sentence's UTF-8 bytes and remain
  independent of the bounded human-review excerpts.
- The `ledger-diff-sentence-sha256` binary capability token and exact machine
  conformance coverage for the new evidence.

### Compatibility

- The ledger format, test fingerprints, commands, exit codes, and existing v1
  fields are unchanged. The new JSON fields and capability token are additive.

## [0.8.0] - 2026-09-02

### Added

- `rulefloor ledger-diff --base REF [--json]` for deterministic logical ledger
  review against a committed Git baseline. It classifies sentence, binding,
  proof, covered-symbol, test-fingerprint, and ratchet-header changes without
  changing the ledger.
- Stable `rulefloor.ledger-diff.v1` machine output with exact conformance data,
  closed change/status enums, single-document JSON, and exit 0/1/2 for
  same/different/cannot-evaluate.
- Additive Go-toolchain module provenance in `rulefloor.version.v1`, including
  an explicit disagreement signal when a linker stamp conflicts with
  `debug.ReadBuildInfo()`.
- GoReleaser archives for Linux and macOS on amd64 and arm64, built with
  `CGO_ENABLED=0`, plus `checksums.txt` and a tag-triggered release workflow.

### Changed

- The existing 12-character hash remains explicitly a test-body fingerprint;
  sentence review is separate so `rehash`, `diff`, legacy ledgers, and
  `rulefloor.validation.v1` keep their established meanings.
- Release builds use the tagged module through GoReleaser's verifiable
  module-proxy mode instead of treating a caller-supplied linker value as the
  only version evidence.

### Security

- Ledger baseline lookup invokes Git without a shell, validates the resolved
  full commit identity, confines the working directory to the repository, and
  bounds subprocess output. The command does not print proof text.

### Compatibility

- RULE-FLOOR.md remains the canonical six-column ledger with no migration.
  Existing hashes, proofs, bindings, and stable v1 version/validation schemas
  are unchanged. Capabilities add only the new command and machine interface.

## [0.7.0] - 2026-08-27

### Added

- Optional exact protected-symbol reach for Go bindings using Gograph stable
  identities and Gograph's persisted precise coverage evidence. Missing,
  stale, partial, fallback, or ambiguous evidence fails closed; possible-only
  reach is not accepted as exact.
- Gograph remains optional for legacy label-only cover rows; check, arm, amend,
  and rehash retain their existing behavior when it is absent from PATH.
- Additive protected-symbol and structural-reach fields in
  `rulefloor.validation.v1`, plus compiled reach support in
  `rulefloor.capabilities.v1`.

### Changed

- Newly armed rows persist explicit `static` or `execute` policy independently
  of profile naming. Untouched ledgers retain the legacy `unit` compatibility
  interpretation without migration.
- Binding policy and stable test identity use canonical `rulefloor-binding-v1`
  metadata inside the existing red-proof cell. RULE-FLOOR.md remains six
  columns, and existing label-only `covers-v1` rows remain metadata-only.

### Compatibility

- Existing six-column ledgers require no migration.
- `rulefloor.version.v1`, `rulefloor.validation.v1`,
  `rulefloor.capabilities.v1`, and `rulefloor.covers.v1` remain stable v1
  contracts. New validation and capability fields are additive.

### Security

- Gograph is invoked directly with argument vectors, repository-confined
  working directories, bounded output, strict single-document JSON decoding,
  and no shell. Stable identities and persisted binding metadata are bounded
  and validated.

## [0.6.0] - 2026-08-25

### Added

- Optional covered-symbol metadata on rules via `--covers` for `declare`,
  `arm`, and `amend`, plus deterministic `rulefloor covers [--json]` discovery
  using the new `rulefloor.covers.v1` machine schema.
- Canonical covered-symbol validation and regression coverage proving rules
  without metadata retain their existing proof, ratchet, and hash behavior.

### Compatibility

- RULE-FLOOR.md remains six columns. Covered symbols use an opaque structured
  token in the existing proof cell so v0.3.0 can still parse and check populated
  ledgers; a seventh column was rejected because it breaks the v0.3.0 parser.
- Existing version, validation, and capabilities schemas remain compatible;
  capabilities add the covers interface, command, and ledger feature.

## [0.5.0] - 2026-08-25

### Added

- `rulefloor amend ID "sentence"` changes only existing rule prose while
  preserving the ID, binding, proof, hash, FLOOR, and RED-PROOFS.
- `rulefloor check --only ID` provides fast selected-binding feedback using
  the same extraction/check/execution behavior, with loud output that it is not
  the full repository gate.
- `prove --supersede` explicitly refreshes a genuine re-watched proof and
  persists the previous proof's full SHA-256 link in backward-compatible text
  metadata.
- `prove --run` performs static integrity first and records nothing unless the
  exact selected Go test reports FAIL. Exact profiles and validated build tags
  are supported without shell execution.
- Complete command help for `rehash`, `amend`, and selected `check` workflows.

### Changed

- `--replace` remains narrow, but its refusal now directs legitimate re-watches
  to `--supersede`; `--force` is reserved for exceptional overrides.
- `rulefloor.capabilities.v1` advertises the additive `amend` command. The
  version and validation schemas and the six-column ledger remain unchanged.
- Shared execution-profile validation now drives machine validation and
  observed red-proof execution.

### Security

- `prove --run` confines the ledger/check path, validates profile and tag
  inputs, invokes Go directly with an argument vector, and treats skips,
  unsupported runners, setup failures, and missing toolchains as fatal.

## [0.4.0] - 2026-08-24

### Added

- `rulefloor diff ID` performs a read-only, bounded Git-history lookup and
  compares the current bound span with the newest revision whose extractor
  fingerprint exactly matches the ledger. It does not classify semantic or
  cosmetic change.
- Execute validation accepts optional, strictly validated `--tags TAGS` for a
  single selected Go rule. The optional request field is additive to
  `rulefloor.validation.v1`; documents without tags remain byte-compatible.
- Command-specific `arm`, `prove`, `declare`, and `diff` help, including the
  six-column ledger restriction on `|` and newlines in proof text.

### Changed

- `rulefloor.capabilities.v1` now advertises the public `diff` command while
  retaining the same schema.
- Documentation explicitly assigns exact/possible reachability, census
  symbols, coverage manifests, and integration-reasoning conventions to
  Gograph and repository governance rather than Rulefloor's ledger.

### Security

- Go build tags are length/count bounded and restricted to Go tag characters;
  subprocesses continue to use argument vectors without shell execution.

## [0.3.0] - 2026-08-21

### Added

- Stable, single-rule machine interfaces: `rulefloor.version.v1` and
  `rulefloor.validation.v1`, with explicit static/execute modes, stable reason
  and exit semantics, and bounded JSON-only diagnostics.
- Extractor-owned Go, Playwright, and Vitest discovery/extraction. Go rule
  markers now use Go parser comment data, so marker-looking fixture strings do
  not create orphan rules.
- Explicit internal execution policy with a compatibility interpretation for
  legacy Go `unit` and non-unit profiles.
- Structured `proof-v1` red-proof records with narrow proof kinds, optional
  validated HTTP(S) references, and deterministic full SHA-256 fingerprints.
- The narrow `repair-fixture-row` migration and retained `REPAIRED-FIXTURES`
  audit metadata for fixture-only rows created by the old Go orphan scanner.
- CI coverage for the complete `make verify` gate.
- Repository-independent `rulefloor capabilities [--json]` discovery with the
  stable `rulefloor.capabilities.v1` schema.

### Changed

- Reusable ledger, extraction, checking, and validation behavior is separated
  from CLI rendering while all commands share the same extractor and checking
  semantics.
- Public documentation now defines Rulefloor as a repository-local binding
  integrity tool and states the limits of fingerprints, executed tests,
  red-proof records, and current-file checks.
- `arm` and `prove` accept optional `--proof-kind` and `--proof-ref` flags;
  legacy free-text proofs and the six-column ledger remain compatible.
- Go 1.27.0 is the official module, CI, and development baseline. Machine JSON
  writing uses deterministic Go 1.27 `encoding/json/v2` while exact v1 wire
  fixtures preserve the existing version and validation contracts.

### Fixed

- Go `RULE:` text inside raw/interpreted strings, block comments, and comment
  examples is no longer treated as a real source tag.
- Execute validation never silently falls back to static validation; unsupported
  execution and unavailable environments return `cannot_evaluate`.

## [0.2.0] - 2026-08-19

### Breaking

- **`arm` now requires `--red-proof`.** Arming a check nobody has watched
  fail is refused, as is the `-` placeholder.
  - Old (v0.1.0): `rulefloor arm ID --check "file @ profile" [--red-proof TEXT]`
  - New: `rulefloor arm ID --check "file @ profile" --red-proof TEXT`
- **Ledgers gain a `RED-PROOFS: N` header and populated red-proof cells.**
  Every write by this version adopts the header (line 2, directly after
  `FLOOR`) onto a legacy ledger at the measured count. The v0.1.0 binary
  refuses such ledgers and cannot read them:
  `rulefloor: <path>/RULE-FLOOR.md: line 2: malformed header` — exit 2.
  Upgrade every binary that touches a shared ledger before the first
  v0.2.0 write to it.

### Added

- `unproved` — lists armed rows whose red-proof cell is still `-` (the
  historical proof debt).
- `redproofs [--adopt]` — ratchet status; `--adopt` writes the
  `RED-PROOFS:` header onto a legacy ledger at the measured count,
  never inventing history for pre-existing `-` rows.
- `prove ID --red-proof TEXT [--replace] [--force]` — record a failure
  observation on an already-armed `-` row. `--replace` overwrites only a cell
  the tool can see is a non-proof (a `blocked:…` note or a dateless
  cell); `--force` loudly overwrites any cell, for a prior observation that is
  not row-specific, and never raises the ratchet.
- The RED-PROOFS ratchet in `check`: monotonic like FLOOR; `check` fails
  when the measured count of proved rows sits below the header.
- Vitest kind: `*.test.ts` rows (`enforced-by: vitest`) are pinned —
  title tag, body hash, no `.skip`/`.only` — but never executed by
  `check` and not orphan-scanned; the vitest suite runs in the consuming
  repo's own gate.
- README rewritten as mechanism-only documentation: consumer-neutral
  examples throughout, a "What belongs here, what belongs in your repo"
  layering section, and every transcript re-recorded against the current
  binary.

## [0.1.0] - 2026-08-14

- Initial public release.
