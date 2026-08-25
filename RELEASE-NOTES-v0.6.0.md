# Rulefloor v0.6.0

Rulefloor v0.6.0 adds an optional, machine-readable link from each rule to the
repository-defined product symbols it protects while keeping the canonical
ledger backward-compatible.

## Highlights

- `declare`, `arm`, and `amend` accept an optional comma-separated `--covers`
  symbol set.
- `rulefloor covers` presents the rule-to-symbol mapping for humans.
- `rulefloor covers --json` emits the deterministic
  `rulefloor.covers.v1` machine contract, including every rule ID and `[]` for
  rules without symbol metadata.
- `check` rejects malformed, duplicate, oversized, or noncanonical covered
  symbol metadata.
- Rulefloor's own ledger now dogfoods covered-symbol metadata for every active
  rule.

## Compatibility

- RULE-FLOOR.md remains a human-readable six-column table. Covered symbols are
  stored in a canonical structured token inside the existing red-proof cell.
- Rules without covered symbols behave exactly as before: their serialized
  rows, hashes, proofs, unproved state, and ratchet measurements do not change.
- Actual v0.3.0 binaries can parse and check populated ledgers because the
  table shape is unchanged. Older binaries do not expose the mapping, and old
  proof-writing workflows should be upgraded before metadata is backfilled.
- `rulefloor.version.v1` and `rulefloor.validation.v1` are unchanged.
  `rulefloor.capabilities.v1` additively advertises the new command, schema, and
  ledger feature.

## Scope boundary

Covered symbols are repository-defined identifiers. Rulefloor validates their
representation but does not resolve symbols, calculate source reachability, or
claim that a bound test reaches them.

Go 1.27.0 or newer is required.
