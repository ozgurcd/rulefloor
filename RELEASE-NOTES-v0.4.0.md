# Rulefloor v0.4.0

Rulefloor v0.4.0 turns three repeated review steps into explicit, bounded
interfaces without expanding the product into a source-graph or generic test
runner.

## Highlights

- `rulefloor diff ID` finds the newest Git revision whose extracted bound span
  exactly matches the ledger fingerprint and shows that span against the
  working tree. It is read-only, searches at most 200 file revisions, and does
  not label a change cosmetic or semantic.
- `rulefloor validate ID --mode execute --profile NAME --tags TAGS --json`
  can execute one build-tagged Go binding. Static validation still rejects
  execution inputs, and execute never falls back to static.
- `rulefloor help arm` and related command help now state proof text and
  replacement constraints directly, including the Markdown-table `|` limit.

## Compatibility

- The six-column ledger is unchanged; no migration is required.
- `rulefloor.version.v1`, `rulefloor.validation.v1`, and
  `rulefloor.capabilities.v1` remain stable. Validation adds only an optional
  `request.tags` field when tags were supplied. Existing no-tag JSON fixtures
  remain byte-identical.
- Capabilities now include `diff` in the public command list.

## Scope boundary

Exact-versus-possible call reachability, multi-symbol census coverage, and
integration reasoning markers remain Gograph/repository-governance concerns.
Rulefloor continues to bind one declared invariant to one extracted check span.

Go 1.27.0 or newer remains required.
