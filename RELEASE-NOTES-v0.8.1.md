# Rulefloor v0.8.1

Rulefloor v0.8.1 adds exact sentence-content fingerprints to logical ledger
review without changing the ledger, test fingerprints, or existing machine
contracts.

## Exact sentence evidence

For every `sentence_changed` rule, `rulefloor ledger-diff --base REF --json`
now includes `before_sentence_sha256` and `after_sentence_sha256`. They remain
inside the stable `rulefloor.ledger-diff.v1` schema as additive optional fields;
they are always present for a sentence change and omitted for unrelated change
classes.

Each value is the full 64-character lowercase SHA-256 of the UTF-8 bytes of the
corresponding parsed sentence (`ledger.Row.Rule`). The existing parser first
trims surrounding Markdown cell whitespace. The digest step performs no further
trimming, case folding, newline rewriting, or Unicode normalization. Bounded
sentence excerpts remain unchanged, so automation can bind to exact content
without duplicating Rulefloor's ledger parser.

These fingerprints establish byte identity only. They do not establish that a
sentence is true, accurately describes its binding, or was properly approved.

## Capability discovery

`rulefloor capabilities` and `rulefloor capabilities --json` advertise
`ledger-diff-sentence-sha256` in the deterministic ledger feature list.
Capabilities remain repository-independent and do not inspect a ledger.

## Compatibility

- `RULE-FLOOR.md` remains canonical, human-readable, and six columns.
- No ledger migration or rehash is required.
- Existing test fingerprints, proofs, bindings, ratchets, commands, and exit
  behavior are unchanged.
- `rulefloor.version.v1` and `rulefloor.validation.v1` are unchanged.
- `rulefloor.capabilities.v1` and `rulefloor.ledger-diff.v1` are extended only
  with additive data.

Official v0.8.1 archives target Linux and macOS on amd64 and arm64, require no
CGO, and are accompanied by `checksums.txt`. Go 1.27.0 or newer is required to
build Rulefloor v0.8.1.
