# Rulefloor v0.8.0

Rulefloor v0.8.0 adds reviewable natural-language ledger drift and ships
checksum-addressed release binaries while preserving the six-column ledger and
all existing test-fingerprint semantics.

## Logical ledger review

`rulefloor ledger-diff --base REF` compares the current logical ledger with
`RULE-FLOOR.md` at a selected committed Git base. It reports sentence,
binding, proof, covered-symbol, test-fingerprint, and ratchet-header changes as
separate classes. Differences exit 1; unavailable or untrustworthy Git evidence
exits 2.

`rulefloor ledger-diff --base REF --json` emits exactly one deterministic
`rulefloor.ledger-diff.v1` document. JSON status values are `same`, `different`,
and `cannot_evaluate`; successful machine invocations contain no human prose on
stdout or stderr. The new command and schema are advertised by
`rulefloor.capabilities.v1`.

The command is a review aid, not semantic proof. It can establish that recorded
text changed relative to the selected baseline. It cannot determine whether a
sentence is true, whether wording still describes the bound test, who approved
the change, or whether the selected Git history is trustworthy.

## Test fingerprints remain stable

The existing 12-character row hash continues to mean only the SHA-256 prefix of
the extracted tagged test span. It does not include the English sentence. This
preserves `rehash`, test-span `diff`, legacy ledgers, and
`rulefloor.validation.v1`'s `test_fingerprint` contract. Sentence changes remain
an explicit `amend` operation and are visible through logical ledger review.

## Verifiable release binaries

Official Linux and macOS archives are published for amd64 and arm64 with
`CGO_ENABLED=0`, accompanied by `checksums.txt`. GoReleaser builds from the
tagged module so `rulefloor version --json` reports both the linker stamp and
Go toolchain module version. A conflicting stamp is reported as disagreement;
an unversioned source build remains `(devel)` rather than inheriting a release
claim from a caller-supplied flag.

## Compatibility

- RULE-FLOOR.md remains canonical, human-readable, and six columns.
- No ledger migration is required.
- Existing test hashes, red proofs, execution policies, covered symbols,
  ratchets, commands, and exit behavior are unchanged.
- `rulefloor.version.v1` and `rulefloor.validation.v1` are unchanged.
- `rulefloor.capabilities.v1` is additive: it advertises `ledger-diff` and
  `rulefloor.ledger-diff.v1`.

## Security

Git is invoked directly with argument vectors and a repository-confined working
directory. The selected ref is resolved to a validated full commit identifier
before its ledger is read. Subprocess output is bounded, and logical review
never prints proof text.

Go 1.27.0 or newer is required to build Rulefloor v0.8.0.
