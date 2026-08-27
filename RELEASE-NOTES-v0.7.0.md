# Rulefloor v0.7.0

Rulefloor v0.7.0 adds opt-in structural proof-decay detection while preserving
the existing repository-local ledger and every stable v1 machine interface.

## Protected production symbols

- A Go rule can use Gograph canonical `stable_id` values in `--covers` to name
  one or more production symbols protected by its bound test.
- `declare`, `arm`, and `amend` resolve stable identities through Gograph.
- `arm` records the bound test's stable identity and refuses a binding that
  does not already reach every protected symbol through exact edges.
- `check`, `rehash`, and single-rule machine validation share the same
  structural reach evaluator.
- If the test fingerprint still matches but an exact edge disappears, the row
  fails as `protected_symbol_unreached`. Possible/uncertain-only reach fails as
  `protected_symbol_possible`.

Gograph remains evidence rather than an authority override. Rulefloor accepts
only persisted, current, complete, precise graph evidence with typed-complete
test-call resolution. Missing, stale, partial, fallback, unsupported, or
ambiguous evidence returns `cannot_evaluate`; it never crashes or silently
passes.

## Gograph remains optional

Existing label-style covers such as `file.go:Symbol` remain metadata-only.
With Gograph absent from PATH, label-only `check`, `arm`, `amend`, and `rehash`
retain their previous behavior. Only a row that explicitly stores exact-reach
metadata requires Gograph evidence.

## Explicit persisted execution policy

New arms persist `static` or `execute` independently from the user-defined
profile name. `--execution static|execute` selects it explicitly. When omitted,
Rulefloor interprets the historical profile behavior once and stores the
result. Untouched legacy rows continue to use the compatibility layer: Go
`unit` executes during normal check, while other profiles remain static unless
explicitly selected.

## Ledger and machine compatibility

- RULE-FLOOR.md remains the canonical human-readable six-column ledger.
- Canonical `rulefloor-binding-v1` metadata stores execution policy, exact
  reach policy, and bound-test stable identity inside the existing proof cell.
- Existing `rulefloor-covers-v1` metadata and logical proof fingerprints remain
  compatible; binding metadata is excluded from proof counting and proof
  fingerprints.
- `rulefloor.validation.v1` additively reports `protected_symbols` and
  `structural_reach` only for exact-reach rows.
- `rulefloor.capabilities.v1` additively reports persisted execution policy and
  compiled Gograph structural-reach support.
- `rulefloor.version.v1` and `rulefloor.covers.v1` are unchanged.

## Verification

The release gate includes Rulefloor self-check, `go test ./...`, `go vet`,
Staticcheck, govulncheck, module-tidiness verification, machine JSON goldens,
PATH-empty optionality regressions, and Gograph architecture review.

Go 1.27.0 or newer is required to build Rulefloor v0.7.0.
