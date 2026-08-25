# Rulefloor v0.5.0

Rulefloor v0.5.0 closes the remaining high-friction rule-maintenance and
single-rule feedback gaps without adding source-graph or census semantics.

## Highlights

- `rulefloor amend ID "sentence"` safely clarifies rule prose without changing
  its stable identity, binding, proof, hash, or ratchets.
- `rulefloor check --only ID` evaluates one armed binding quickly and says
  explicitly that it is not the complete repository gate.
- `prove --supersede` distinguishes a legitimate re-watched refresh from an
  exceptional force overwrite. The new proof stores the prior proof's full
  SHA-256 fingerprint as a deterministic compatibility-safe link.
- `prove --run` runs one selected Go binding and writes the supplied proof only
  after that exact test reports FAIL. It never treats pass, skip, setup failure,
  or unsupported execution as red evidence.
- `rehash --help` now explains no-op, skipped-test, supersession, and force
  behavior instead of returning a missing-help error.

## Compatibility

- `rulefloor.version.v1`, `rulefloor.validation.v1`, and
  `rulefloor.capabilities.v1` retain their schemas. Capabilities add only the
  public `amend` command to the command inventory.
- RULE-FLOOR.md remains the same human-readable six-column format.
- Supersession is encoded inside proof text, so v0.4.0 proof-v1 readers remain
  able to parse the ledger. No migration is required.
- FLOOR and RED-PROOFS remain monotonic; no legitimate delete command is added.

## Scope boundary

Exact-versus-possible reachability, multi-symbol coverage manifests, and
integration census conventions remain Gograph/repository-governance concerns.

Go 1.27.0 or newer is required.
