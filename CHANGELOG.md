# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
