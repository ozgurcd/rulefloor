# rulefloor v0.3.0

## [0.3.0] - 2026-08-21

### Added

- Stable, single-rule machine interfaces: `rulefloor.version.v1` and
  `rulefloor.validation.v1`, with explicit static/execute modes, stable reason
  and exit semantics, and bounded JSON-only diagnostics.
- Repository-independent `rulefloor capabilities [--json]` discovery with the
  stable `rulefloor.capabilities.v1` schema.
- Extractor-owned Go, Playwright, and Vitest discovery/extraction. Go rule
  markers use Go parser comment data, so marker-looking fixture strings do not
  create orphan rules.
- Explicit internal execution policy with compatibility interpretation for
  legacy Go `unit` and non-unit profiles.
- Structured `proof-v1` red-proof records with narrow proof kinds, optional
  validated HTTP(S) references, and deterministic full SHA-256 fingerprints.
- The narrow `repair-fixture-row` migration and retained `REPAIRED-FIXTURES`
  audit metadata for fixture-only rows created by the old Go orphan scanner.
- CI coverage for the complete `make verify` gate.

### Changed

- Go 1.27.0 is the official module, CI, and development baseline.
- Machine JSON writing uses deterministic Go 1.27 `encoding/json/v2`; exact
  conformance fixtures preserve the existing version and validation v1 wires.
- Reusable ledger, extraction, checking, validation, capabilities, and
  repository-confinement behavior is separated from CLI rendering.
- Execution support is determined by test kind rather than arbitrary profile
  names, while legacy ledger behavior remains compatible.
- Public documentation precisely limits Rulefloor's guarantees around static
  integrity, executed tests, fingerprints, red-proof observations, helper and
  dependency drift, and Git history.
- `arm` and `prove` accept optional `--proof-kind` and `--proof-ref` flags;
  legacy free-text proofs and the six-column ledger remain compatible.

### Fixed

- Go `RULE:` text inside raw/interpreted strings, block comments, and comment
  examples is no longer treated as a real source tag.
- Execute validation never silently falls back to static validation; unsupported
  execution and unavailable environments return `cannot_evaluate`.
- Ledger and check-file operations reject symlinks that escape the repository.

### Compatibility

- `rulefloor.version.v1` and `rulefloor.validation.v1` remain compatible.
- Existing six-column ledgers, legacy red proofs, legacy `unit` execution, and
  non-unit profile behavior remain supported.
- Go 1.27.0 or newer is required to build Rulefloor v0.3.0.
