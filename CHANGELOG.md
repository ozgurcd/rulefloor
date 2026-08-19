# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
- `prove ID --red-proof TEXT [--replace] [--force]` — record a watched
  proof on an already-armed `-` row. `--replace` overwrites only a cell
  the tool can see is a non-proof (a `blocked:…` note or a dateless
  cell); `--force` loudly overwrites any cell, for a proof that is real
  but not row-specific, and never raises the ratchet.
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
