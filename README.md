# rulefloor

**A repository-local invariant integrity tool.**

You have rules you never want to lose: *"an emptied cart totals zero"*,
*"invoice numbers are never reused"*. Each one is bound to a concrete check —
until someone edits the check, adds a `.skip`, or deletes it, and the binding
silently stops protecting the rule. rulefloor makes that decay loud: every
armed rule is pinned to the exact source span of its tagged test, and `check`
fails when that binding changes, skips, or disappears.

One file at your repo root — `RULE-FLOOR.md` — human-readable, diff-friendly,
and written **only** by this tool. Rulefloor is a dependency-free Go binary;
Gograph is optional and used only by rows that opt into exact structural reach.

```
$ rulefloor check
PASS CART-1 (e2e/cart.spec.ts @ chromium)
PASS INV-1 (invoice_test.go @ unit)
check OK: 2 rows (2 armed), FLOOR 2, RED-PROOFS 2 (measured 2)
```

---

- [How it works](#how-it-works)
- [Install](#install)
- [Go 1.27 standard-library review](#go-127-standard-library-review)
- [Five-minute tutorial](#five-minute-tutorial)
- [Concepts](#concepts)
- [Command reference](#command-reference)
- [Review ledger changes](#review-ledger-changes)
- [Covered product symbols](#covered-product-symbols)
- [Binary capabilities](#binary-capabilities)
- [Machine-readable validation](#machine-readable-validation)
- [`check`, in depth](#check-in-depth)
- [Wiring into your entrypoints](#wiring-into-your-entrypoints)
- [What belongs here, what belongs in your repo](#what-belongs-here-what-belongs-in-your-repo)
- [Exit codes](#exit-codes)
- [Troubleshooting](#troubleshooting)
- [Guarantees and honest limits](#guarantees-and-honest-limits)
- [Ledger format specification](#ledger-format-specification)

---

## How it works

Three verbs, one loop:

1. **`declare`** a rule — one sentence, one ID. It sits in the ledger as
   *declared* (visible debt, not yet enforced).
2. **`arm`** it — point the rule at a tagged test and record an observation
   that the test failed (`--red-proof`, required). rulefloor
   extracts the test's body, stores the first 12 hex chars of its sha256,
   and raises the FLOOR (the minimum row count) so the row can never
   quietly vanish.
3. **`check`** — re-derives everything from the working tree and compares it
   to the ledger. Any drift is a failure: changed body, skipped test, missing
   tag, deleted row, tag without a row. For a Go binding that declares stable
   protected-symbol identities, it also requires current precise Gograph
   evidence that the tagged test still reaches every symbol through exact
   edges.

A legitimate, reviewed edit to an armed test is accepted with **`rehash`** —
an explicit, auditable step, never an automatic one. Before accepting drift,
**`diff`** can recover the newest matching bound span from Git and compare it
with the working span; it reports bytes and does not guess whether the change
is cosmetic or semantic.

The rule ID is permanent, but its human sentence can be clarified with
**`amend`**. Amendment changes that sentence and, only when explicitly passed,
the optional covered-symbol set; the binding, proof, hash, FLOOR, and
RED-PROOFS remain unchanged. Legitimate rule deletion remains intentionally
unavailable because it would violate the ledger ratchet.

Before merging a ledger edit, **`ledger-diff --base REF`** compares the current
logical ledger with its committed form at a selected Git base. It classifies a
sentence change separately from binding, proof, protected-symbol, and test-body
fingerprint changes. This makes prose drift visible to review; it does not
decide whether either sentence is true.

## Install

Published releases include checksum-addressed archives for Linux and macOS on
amd64 and arm64. Download the archive for your platform from the release page,
verify it against `checksums.txt`, then place `rulefloor` on `PATH`. These are
the canonical release binaries; they are built with `CGO_ENABLED=0` from the
tagged module version.

Homebrew:

```bash
brew install ozgurcd/tap/rulefloor
```

Upgrade an existing tap installation and confirm the running release:

```bash
brew update
brew upgrade ozgurcd/tap/rulefloor
rulefloor version --json
```

Releases and per-version changes are recorded in
[CHANGELOG.md](CHANGELOG.md). Note that the tap serves the latest
published release, which may trail this repository's main branch.

Or with Go:

```bash
go install github.com/ozgurcd/rulefloor@latest
```

Or build from source. A source build is not a shipped release binary even when
a builder supplies a release-looking linker stamp: check `version --json` and
expect `toolchain_version` to reveal `(devel)` when the Go toolchain has no
module release identity. **Requires Go 1.27.0 or newer** — the module's
`go` directive is `1.27.0`, the official baseline. A toolchain below it that
cannot fetch Go 1.27 through `GOTOOLCHAIN=auto` will fail to build the tool,
deliberately: an under-floor build environment should fail loudly, not
produce a subtly different binary.

```bash
git clone https://github.com/ozgurcd/rulefloor.git
cd rulefloor && go build -o rulefloor .
```

Put the binary on your PATH, or call it by path.

## Go 1.27 standard-library review

Rulefloor uses Go 1.27's `encoding/json/v2` writer for machine documents with
deterministic encoding explicitly enabled, while retaining the existing v1
wire bytes through conformance fixtures. Public command discovery uses
`maps.Keys` with `slices.Sorted`, and capability snapshots clone and sort their
inputs before publication.

Candidate changes intentionally not adopted:

- Existing JSON readers remain on `encoding/json` because changing accepted
  ledger-adjacent or Playwright report input semantics is outside this
  compatibility pass.
- `os.Root` would require a broader file-access and subprocess-path redesign;
  the existing tested canonical-root and symlink-confinement boundary remains.
- Generic methods, embedded-field selectors, `bytes.CutLast`, `uuid`, SIMD,
  ML-DSA, and `go/scanner.Scanner.End` do not simplify Rulefloor's current
  responsibilities enough to justify a rewrite.

## Five-minute tutorial

Every transcript below is a real run, unedited.

Start with a repo containing a Playwright spec and a Go test:

```ts
// e2e/cart.spec.ts — the tag is [CART-1] in the title
test('an emptied cart totals zero [CART-1]', async ({ page }) => {
  await page.goto('/cart');
  // ... add two items, remove both ...
  expect(await total.textContent()).toBe('0.00');
});
```

```go
// invoice_test.go — the tag is the comment line directly above the func
// RULE: INV-1
func TestInvoiceNumbersAreNeverReused(t *testing.T) {
	if issueInvoice() == issueInvoice() {
		t.Fatal("two invoices must never share a number")
	}
}
```

**Create the ledger, declare, arm.** `arm` requires `--red-proof` — a record
of a failure observation (see
[the red-proof obligation](#the-red-proof-obligation)):

```
$ rulefloor init
initialized RULE-FLOOR.md (FLOOR: 0, RED-PROOFS: 0)
$ rulefloor declare "An emptied cart shows a zero total." --id CART-1
declared CART-1 (unarmed); 1 rows, FLOOR 1
$ rulefloor declare "Invoice numbers are never reused." --id INV-1
declared INV-1 (unarmed); 2 rows, FLOOR 2
$ rulefloor arm CART-1 --check "e2e/cart.spec.ts @ chromium" --red-proof "seen red 2026-08-19: zero-total assertion inverted, spec watched failing, restored"
armed CART-1: e2e/cart.spec.ts @ chromium (hash fca893d6ef24); FLOOR 2, RED-PROOFS 1
$ rulefloor arm INV-1 --check "invoice_test.go @ unit" --red-proof "seen red 2026-08-19: reuse guard removed, test watched failing, restored"
armed INV-1: invoice_test.go @ unit (hash 0c13dd2998ce); FLOOR 2, RED-PROOFS 2
$ rulefloor check
PASS CART-1 (e2e/cart.spec.ts @ chromium)
PASS INV-1 (invoice_test.go @ unit)
check OK: 2 rows (2 armed), FLOOR 2, RED-PROOFS 2 (measured 2)
```

The ledger now reads:

```
FLOOR: 2
RED-PROOFS: 2

| ID | one-sentence rule | enforced-by | check | red-proof | hash |
|---|---|---|---|---|---|
| CART-1 | An emptied cart shows a zero total. | playwright | e2e/cart.spec.ts @ chromium | seen red 2026-08-19: zero-total assertion inverted, spec watched failing, restored | fca893d6ef24 |
| INV-1 | Invoice numbers are never reused. | go-test | invoice_test.go @ unit | seen red 2026-08-19: reuse guard removed, test watched failing, restored | 0c13dd2998ce |
```

**Now someone weakens the assertion** (`toBe('0.00')` → `toContain('0')`):

```
$ rulefloor check
FAIL CART-1: hash mismatch: ledger fca893d6ef24, actual 172395533814 (body changed; review, then rehash)
PASS INV-1 (invoice_test.go @ unit)
rulefloor: check FAILED: 1 problem(s) in .    # exit 1
```

**Or skips the test** (`test(` → `test.skip(`):

```
FAIL CART-1: .skip is set on the tagged test
FAIL CART-1: hash mismatch: ledger fca893d6ef24, actual c77b2f2d3138 (body changed; review, then rehash)
PASS INV-1 (invoice_test.go @ unit)
rulefloor: check FAILED: 2 problem(s) in .    # exit 1
```

**A reviewed, legitimate edit is accepted explicitly:**

```
$ rulefloor diff CART-1
drift CART-1: e2e/cart.spec.ts (ledger fca893d6ef24, working 172395533814, baseline 72ee48d124b1)
@@ -1,3 +1,3 @@
 test('an emptied cart totals zero [CART-1]', async ({ page }) => {
-  expect(total).toBe('0.00');
+  expect(total).toContain('0');
 });
$ rulefloor rehash CART-1
rehashed CART-1: fca893d6ef24 -> 172395533814
$ rulefloor check
PASS CART-1 (e2e/cart.spec.ts @ chromium)
PASS INV-1 (invoice_test.go @ unit)
check OK: 2 rows (2 armed), FLOOR 2, RED-PROOFS 2 (measured 2)
$ rulefloor rehash CART-1   # again, unchanged
rulefloor: refusing: no-op rehash for CART-1 (hash unchanged: 172395533814)   # exit 1
```

**A tagged test without a ledger row is an orphan — also a failure:**

```
$ rulefloor check
PASS CART-1 (e2e/cart.spec.ts @ chromium)
PASS INV-1 (invoice_test.go @ unit)
DECLARED CART-2 (no armed check)
FAIL e2e/cart.spec.ts: orphan tag CART-9 (no ledger row)
rulefloor: check FAILED: 1 problem(s) in .    # exit 1
```

## Concepts

### The ledger file

`RULE-FLOOR.md` at the repo root. A `FLOOR: N` line plus one markdown table
row per rule. It is designed to be *read* by humans and *written* only by
rulefloor — hand edits are how ledgers rot, and most hand edits will simply
fail the strict parser. Commit it like any other file; its diffs are the
audit trail of your rules.

### Declared vs armed

A **declared** rule (`check` = `NONE`, `hash` = `-`) is a stated intention —
it appears in `list`, `unarmed`, and check output (`DECLARED CART-2 (no
armed check)`), but nothing enforces it yet. An **armed** rule points at a
tagged test and carries its body hash. `unarmed` is your work queue.

### FLOOR

`FLOOR` is the minimum effective ledger count. Normally that is the row count;
`declare` raises it and **nothing ever lowers it** — not even the tool. Delete a
row by hand and `check` fails with `rows is below FLOOR`. A
`REPAIRED-FIXTURES` audit entry counts only historical non-rule rows removed by
the narrow `repair-fixture-row` migration; it preserves the ratchet rather than
lowering it.

### Tagging tests

| Kind | File | Tag |
|---|---|---|
| Playwright | `*.spec.ts` | `[ID]` anywhere in the test title string: `test('... [CART-1]', ...)` |
| Go | `*_test.go` | the exact line `// RULE: ID` **directly above** `func TestXxx` |

A tag must resolve to exactly one test — two tests carrying the same tag is a
fatal error, never a guess. IDs match `[A-Z][A-Z0-9-]*[0-9]` (e.g. `CART-1`,
`AUDIT-LOG-2`): uppercase, ends in a digit, hyphens allowed.

Go tags are read from Go's parsed comment syntax. Marker-looking text inside a
raw or interpreted string, a block comment, or an ordinary comment example is
not a rule tag. The marker must be a standalone line comment immediately above
the intended top-level `TestXxx` function.

### The hash

The first 12 hex chars of the sha256 of the test's exact source span — for
Playwright the whole `test(...)` call, for Go the whole `func TestXxx {...}`.
Any byte of drift changes it. The extractor is string- and comment-aware, so
parentheses and braces inside strings, template literals, and comments don't
confuse the span.

The hash deliberately does **not** include the English rule sentence. It is a
test fingerprint, including in `rulefloor.validation.v1`; combining prose with
it would make sentence edits indistinguishable from test-body drift and would
break `rehash` and `diff` semantics. Use `ledger-diff --base REF` to detect and
review recorded sentence changes. Neither mechanism validates the sentence's
meaning.

### The check field

`<file> @ <profile>`. The file is relative to the repo root and determines
the kind by suffix (`.spec.ts` → playwright, `_test.go` → go-test). The
profile names *where the test actually runs*.

**The profile name is your repo's vocabulary, not the execution mechanism.**
Pick anything — a Playwright project name (`chromium`), a run mode of your own
(`full-stack`, `nightly`). Execution support is determined by test kind: this
release can directly execute Go tests, but not Playwright or Vitest tests.

For backward compatibility with existing six-column ledgers, a Go row whose
declared profile is exactly `unit` has legacy execute policy: plain `check`
executes it and refuses `t.Skip` in its body. Other Go profile names retain
their historical static policy in plain `check`; execute them explicitly:

```bash
rulefloor check --run-profile needs-db --tags dbtest
```

`--run-profile NAME` additionally executes every Go row whose declared profile
is NAME with `go test -tags T -count=1 -run '^TestXxx$'`. There, a test
that SKIPS at runtime is CANNOT-EVALUATE (exit 2) — its precondition was
absent and a skip is not a proof; a failure is a failure (exit 1). Unit
rows behave exactly as in plain check. `--tags` requires `--run-profile`;
`--run-profile` cannot combine with `--all`. No flag weakens any existing
check — the mode only ADDS execution.

New arms persist an explicit `static` or `execute` policy. Use
`arm ... --execution static|execute` to choose it; when omitted, Rulefloor
interprets the historical profile behavior once and stores that result.
`amend ID "sentence" --execution ...` migrates an already armed legacy row
without changing its profile, test fingerprint, or proof. Untouched legacy
rows continue to use the compatibility interpretation above.

## Command reference

Every command takes `--repo PATH` (default `.`).

| Command | Effect |
|---|---|
| `init` | Create an empty ledger. Refuses if one exists. |
| `list` | All rules, with armed/declared state. |
| `show ID` | The canonical six-column values of one rule; use `covers` for symbol metadata. |
| `unarmed` | Rules that still need a test — the work queue. |
| `unproved` | Armed rows whose red-proof cell is still `-` — the historical proof debt. |
| `redproofs [--adopt]` | Ratchet status; `--adopt` writes `RED-PROOFS:` onto a legacy ledger at the **measured** count. |
| `declare "sentence" --id ID [--covers SYMBOLS]` | Append a declared row; raise FLOOR. Optional `--red-proof TEXT`; stable Gograph IDs in `--covers` opt a future Go binding into exact structural reach. |
| `amend ID "sentence" [--covers SYMBOLS] [--execution static\|execute]` | Replace an existing rule sentence and optionally its covered-symbol set or persisted execution policy. Preserve its ID, binding, proof, hash, FLOOR, and RED-PROOFS; use `--covers=` to clear and refuse a complete no-op. |
| `arm ID --check "file @ profile" --red-proof TEXT [--covers SYMBOLS] [--execution static\|execute]` | Pin the tagged test's hash; persist execution policy; set enforced-by; raise FLOOR and RED-PROOFS. Refuses skipped tests. `--red-proof` is **required**; optional `--proof-kind` and `--proof-ref` create a structured proof record. `--covers` sets or replaces the symbol set; omission retains a declaration-time set. |
| `prove ID --red-proof TEXT [--replace\|--supersede\|--force] [--run]` | Record an observation on an armed row. `--replace` remains limited to a non-proof. `--supersede` replaces a genuine re-watched proof and stores its full fingerprint link. `--force` is the loud exceptional override. `--run` executes one Go binding and writes only after that selected test reports FAIL. |
| `rehash ID` | Accept a reviewed body change. Refuses a no-op, refuses skipped tests. |
| `diff ID` | Read-only comparison of the working bound span with the newest Git revision whose extracted fingerprint exactly matches the ledger. It never classifies drift as cosmetic or semantic. |
| `ledger-diff --base REF [--json]` | Compare the current logical ledger with its committed form at `REF`. Classify sentence, binding, proof, covered-symbol, and test-fingerprint changes separately. Differences exit 1; unavailable evidence exits 2. |
| `repair-fixture-row ID` | One-time migration for an unarmed row explicitly described as `Fixture marker, not a rule:`. Refuses if the ID is still a real extractor-discovered tag; removes the row and records its ID in `REPAIRED-FIXTURES` so FLOOR is preserved. |
| `covers [--json]` | Read every rule ID and its optional covered-symbol set. JSON is a deterministic `rulefloor.covers.v1` document. |
| `check [--report pw.json] [--all "repo1,repo2"]` | Verify the complete repository gate (below). |
| `check --only ID` | Fast human feedback for one armed binding. This intentionally omits other rows, global ratchet evaluation, and orphan scanning, so it never replaces full `check`. A matching `--run-profile` executes a non-unit Go row. |
| `validate ID --mode static\|execute --json` | Validate exactly one rule and emit `rulefloor.validation.v1` JSON. Optional execute-only `--profile NAME` selects the declared profile; optional `--tags TAGS` supplies validated Go build tags. |
| `capabilities [--json]` | Describe features compiled into this binary without reading a repository or ledger. |
| `version --json` | Emit `rulefloor.version.v1` JSON using the same release-stamped version as human output. |

Refusals you will meet, all deliberate:

- `init` when a ledger exists · `declare` a duplicate ID · `arm` an
  already-armed rule (that's what `rehash` is for) · `arm` without a
  `--red-proof` (or with the `-` placeholder) · `arm`/`rehash` a test
  that has `.skip`/`.only` or calls `t.Skip*` · `rehash` when nothing
  changed · `redproofs --adopt` when the header already exists ·
  `amend` with an unchanged or invalid sentence · `prove --replace` over a
  genuine proof (only a `blocked:…` or dateless non-proof may be replaced) ·
  `prove --supersede` without a genuine prior record · `prove --run` when the
  selected test passes or cannot execute.

## Review ledger changes

`rulefloor ledger-diff --base REF` is a read-only review gate for changes to
`RULE-FLOOR.md`. It resolves `REF` to a commit, reads that commit's ledger with
Git, parses both ledgers through the same strict parser, and compares their
logical models:

```text
$ rulefloor ledger-diff --base origin/main
ledger differs from origin/main (0123456789ab)
CHECKOUT-1: sentence_changed
  before: "An emptied cart usually totals zero."
  after:  "An emptied cart always totals zero."
```

The closed change classifications are `rule_added`, `rule_removed`,
`sentence_changed`, `binding_changed`, `proof_changed`,
`covered_symbols_changed`, and `test_fingerprint_changed`. Header changes to
`FLOOR`, `RED-PROOFS`, or `REPAIRED-FIXTURES` are reported separately as
`floor_changed`, `red_proofs_changed`, or `repaired_fixtures_changed`. Proof
text is never printed; only its logical change is identified.

`--json` emits one deterministic `rulefloor.ledger-diff.v1` document. Its
`status` is `same`, `different`, or `cannot_evaluate`; the corresponding exit
codes are 0, 1, and 2. Successful and cannot-evaluate JSON responses do not mix
human prose into stdout or stderr. The schema includes the selected base ref,
resolved commit, before/after header state, and sorted per-rule changes. Sentence
values are bounded excerpts, and documents report at most 1,000 rule changes
with explicit `total_rule_changes` and `truncated` fields.

The command invokes Git directly with argument vectors—never through a shell—
and resolves the caller-provided ref to a validated full commit identifier
before reading `RULE-FLOOR.md`. Git and a committed baseline are required only
for this command, not for normal ledger operations.

This is a review aid, not semantic verification. It catches that recorded text
changed relative to the selected baseline. It cannot detect a false sentence
that was already present at that baseline, decide whether new wording matches
the bound test, establish who approved it, or stop someone from selecting a
different base. Repository history and review remain the authority for those
questions.

### The red-proof obligation

A green check can still be vacuous. `--red-proof TEXT` records an observation
that the bound test failed (for example after a mutation, or in a cited CI
run). This is evidence recorded by a human or workflow, not semantic proof
verified by Rulefloor. Since the RED-PROOFS ratchet:

- **`arm` requires it.** Every newly armed row carries non-empty proof text —
  arming with none, or with the `-` placeholder, is refused.
- **`RED-PROOFS: N`** is a second header line the tool maintains: the
  count of armed rows whose red-proof cell is not `-`. Like FLOOR it is
  monotonic — every write raises it to the measured count when that grew,
  and nothing the tool does lowers it. `check` fails when the measured
  count sits below the header: a proof was emptied back to `-`, or a
  proven row was deleted.
- **Legacy ledgers adopt without inventing history.** `redproofs --adopt`
  writes the header at the count measured *right then*; pre-existing `-`
  rows stay `-` — backfilling a proof text without re-watching the failure
  is exactly the lie the ratchet exists against. `unproved` lists that
  debt; it shrinks only through `prove` (a newly recorded observation) or new
  arms.
  (Any write operation — declare, amend, arm, prove, rehash — also adopts the header on
  a legacy ledger, at the same measured-count rule.)

The proof text itself is documentation: the tool validates its representation,
fingerprints it, and ratchets the count, but cannot establish that the reported
failure happened. What a proof must cite is your repo's convention (see
[what belongs where](#what-belongs-here-what-belongs-in-your-repo)). HTTP(S)
references are stored and syntax-checked only; Rulefloor does not fetch or
verify them. Proof text is one ledger cell: it cannot contain a newline or `|`.
`rulefloor help arm` and `rulefloor help prove` show these constraints before
a write is attempted.

When a test extension has been legitimately re-watched, `prove --supersede`
records a new proof whose text begins with
`supersedes-sha256:<previous-full-fingerprint>`. This is compatible with older
proof-v1 readers and preserves a deterministic link to the prior record; Git
history remains the source for that record's full text. It does not mean the
prior observation was false. `--force` remains available for exceptional
correction but deliberately creates no supersession claim.

`prove --run` is a narrow observation aid, not a mutation engine. It first
checks the selected binding's current tag and hash, then invokes only its Go
test with an argument vector. It records the supplied proof only when Go reports
that exact test as failed. A pass is refused and a skip, compile/setup failure,
unsupported kind, or unavailable toolchain is fatal CANNOT-EVALUATE. For a
non-unit profile, pass the exact declared `--profile` and any required `--tags`.
When `--proof-kind` is omitted, a successful `--run` observation is stored as
`manual_observation`; an explicit supported kind remains available.
Rulefloor does not infer which assertion failed and does not claim the observed
failure proves the natural-language rule.

## Covered product symbols

`--covers` records the repository's asserted rule-to-product-symbol link. It is
a comma-separated set, canonicalized into sorted order. Existing labels without
`::` remain v0.6-compatible metadata only. Gograph is optional for those
ledgers: `check`, `arm`, `amend`, and `rehash` do not try to locate it. A set
made entirely of Gograph stable identities (for example,
`example.com/product/internal/cart::(*Cart).Total`) opts a Go binding into
exact structural-reach enforcement. Mixed stable IDs and legacy labels are
rejected so enforcement cannot be disabled accidentally.

The read path is `rulefloor covers --json`:

```json
{"schema_version":"rulefloor.covers.v1","rules":{"CART-1":["Cart.Empty","Cart.Total"],"INV-1":[]}}
```

Every ledger rule is present; a rule without metadata has `[]`. Human
`rulefloor covers` prints the same mapping compactly. `declare`, `arm`, and
`amend` accept `--covers`; omitting it preserves current behavior and metadata.
`amend ID "unchanged sentence" --covers ...` is the deterministic backfill
path and does not move the test hash, proof, FLOOR, or RED-PROOFS.

Before writing or checking an exact-reach row, build the repository graph with
`gograph build . --precise`. Rulefloor invokes Gograph's `identity` and
`coverage` interfaces; it does not reproduce graph analysis. It accepts only
persisted, current, complete, precise evidence with typed-complete test-call
resolution. A missing Gograph binary, stale or partial graph, fallback
precision, unresolved identity, or ambiguous identity is `cannot_evaluate`,
never a crash or silent pass. A
possible-only edge is an evaluated failure, not exact reach. On arm, Rulefloor
resolves and stores the tagged test's stable identity and refuses a protected
symbol the test does not already reach exactly.

A typical opt-in workflow is:

```bash
gograph build . --precise
gograph identity 'example.com/product/internal/cart::(*Cart).Total' --json
rulefloor amend CART-1 "An emptied cart shows a zero total." \
  --covers 'example.com/product/internal/cart::(*Cart).Total'
rulefloor check --only CART-1
```

Use the canonical `stable_id` returned by Gograph. Display names and file/line
positions are not stable identities. Rebuild the precise graph after source
changes and before `arm`, protected-symbol `amend`, `rehash`, `check`, or
`validate` evaluates an exact-reach row.

This detects proof decay: an unchanged, passing tagged test fails when it no
longer structurally reaches a declared protected symbol. Static reach is not
runtime branch coverage or semantic proof; it establishes only the exact
call-graph relationship reported by trusted Gograph evidence.

Backward compatibility determines the storage shape. RULE-FLOOR.md remains a
six-column table. A canonical base64url JSON token is stored as an HTML-comment
suffix inside the existing red-proof cell. New Rulefloor separates that token
from the logical proof before proof counting and fingerprinting. Older v0.3.0
binaries parse and check the ledger because the row still has six columns; they
treat the covers and binding metadata suffixes as opaque proof text and do not
enforce the symbol map or explicit execution policy. A
seventh table column was rejected because v0.3.0 rejects an altered header.

## Binary capabilities

`rulefloor capabilities` is binary feature discovery, not repository status or
validation. It works outside a Rulefloor project, does not accept `--repo`, and
never reads `RULE-FLOOR.md`, repository files, profiles, rule counts, ledger
health, or installed Go toolchain state.

Human output concisely lists the running version, machine schemas, supported
test kinds and execution support, validation modes, proof kinds, ledger
features, commands, execution semantics, and compiled structural-reach support.
`rulefloor capabilities --json`
emits exactly one `rulefloor.capabilities.v1` document containing the same data.
Its `rulefloor_version` matches the backward-compatible `version` field from
`rulefloor version --json`. Binary provenance details live only in the version
document.

The v1 object contains `schema_version`, `rulefloor_version`,
`machine_interfaces`, per-kind `test_kinds` entries with
`static_validation`/`execution` booleans, `validation_modes`, `proof_kinds`,
`ledger_features`, `commands`, structured `execution_semantics`, and
`structural_reach`. The latter describes compiled support and does not inspect
whether Gograph or a graph is available in the current environment. Arrays use
the deterministic order shown by the exact conformance fixture in
[`testdata/machine`](testdata/machine).

Execution capability is binary support by test kind: Go tests support static
and executed validation; Playwright and Vitest support static validation only.
Static mode never executes, and execute mode never falls back to static.
Repository status remains the responsibility of `check` and `validate`.

## Machine-readable validation

`rulefloor version --json` writes one JSON document to stdout:

```json
{"schema_version":"rulefloor.version.v1","version":"v0.8.0","toolchain_version":"v0.8.0"}
```

`rulefloor.version.v1`, `rulefloor.validation.v1`,
`rulefloor.capabilities.v1`, `rulefloor.covers.v1`, and
`rulefloor.ledger-diff.v1` are stable v1 contracts.
Within v1, required fields, field meanings, outcome/reason meanings, and exit
mapping will not be removed or renamed. Compatible releases may add optional
fields. Consumers must reject an unknown `schema_version` rather than guessing.
The existing `version` value remains the release-time `main.version` stamp for
compatibility. `toolchain_version` is the main-module version embedded by the
Go toolchain and read with `debug.ReadBuildInfo()`. An unversioned source or
tarball build reports `(devel)`. When the two claims differ, the document also
contains `"version_disagreement":true`; that field is absent when they agree.
`dev` and `(devel)` are the equivalent honest development state.

Official release archives are built from the tagged module through GoReleaser's
module-proxy mode and stamp `v{{ .Version }}`, so both values name the same tag.
The release also publishes `checksums.txt`. Verify that checksum separately:
embedded build information identifies the module version but does not validate
the bytes of the file carrying it.

Build information cannot establish who ran a build, whether the release tag is
trusted, whether a downloaded binary matches a published checksum, or whether
the local repository and dependencies are the ones an operator intended. It
also cannot turn a source-archive build into a tagged module build: `(devel)` is
reported rather than guessed away.
Exact v1 conformance documents are kept in [`testdata/machine`](testdata/machine).

`rulefloor validate RULE-ID --repo PATH --mode static|execute [--profile NAME] [--tags TAGS] --json`
evaluates exactly one ledger row and writes one `rulefloor.validation.v1`
document to stdout. It does not fail because another well-formed row has a
hash, skip, execution, or proof problem. A malformed ledger remains
`cannot_evaluate`, because the requested row cannot be trusted if its
container cannot be parsed.

The result records the Rulefloor version and timestamp; canonical repository
root; ledger path and full SHA-256; requested rule, mode, profile, and optional
Go build tags; whether
the rule exists and is armed; check kind, file, and declared profile; expected
and actual 12-character test-body hashes; red-proof presence and, when present,
the full SHA-256 of its exact canonical ledger cell; static and execution
status; optional `protected_symbols` and `structural_reach` for an exact-reach
row; a stable reason code; and bounded structured diagnostics. Output is one
strict JSON document, diagnostic text is bounded, and no human prose is mixed
into JSON stdout.

- `static` performs only selected-row integrity checks: existence, armed state,
  check shape and kind, confined regular check file, unique tag, skip/only
  restrictions, body hash, red-proof presence, and any declared exact
  protected-symbol reach. It never executes the test.
- `execute` performs static validation first, then runs a selected Go test with
  explicit `go test` arguments. For legacy compatibility, a `unit` Go row needs
  no `--profile`; another profile requires an exact declared-profile match.
  Execution support is based on test kind, not the profile string. Rulefloor
  does not accept test commands, shell fragments, arbitrary flags, or profile
  substitution. `--tags` is execute-only, accepts a bounded comma-separated
  list of Go build-tag names, and is passed as one argument without a shell.
  A build-tagged test omitted from the selected build context is
  `cannot_evaluate`; Rulefloor never downgrades that request to static.
- Playwright and Vitest rows can pass `static`; direct `execute` is
  `cannot_evaluate` because Rulefloor has no native runner for them. There is no
  silent execute-to-static downgrade.

Machine outcomes and process exits are exact: `pass` → 0, evaluated `fail` → 1,
and `cannot_evaluate` → 2.

Stable `evaluation.reason` codes are:

| Reason | Outcome | Meaning |
|---|---|---|
| `rule_passed` | `pass` | Requested static or executed validation passed. |
| `rule_failed` | `fail` | The selected bound test ran and failed. |
| `hash_mismatch`, `test_restricted`, `red_proof_missing` | `fail` | The selected binding was evaluated and failed static integrity. |
| `test_skipped` | `fail` or `cannot_evaluate` | A statically prohibited skip is a binding failure; a runtime skip means requested execution could not be evaluated. |
| `protected_symbol_unreached`, `protected_symbol_possible` | `fail` | Source integrity passed, but a protected symbol is absent from exact test reach or is reached only through possible/uncertain edges. |
| `invalid_request`, `ledger_invalid`, `rule_not_found`, `rule_unarmed`, `profile_mismatch` | `cannot_evaluate` | The request or selected ledger state could not be evaluated. |
| `check_file_missing`, `tag_missing`, `tag_ambiguous`, `cannot_parse_test` | `cannot_evaluate` | Source discovery or extraction could not produce one trustworthy binding. |
| `graph_evidence_unavailable`, `graph_evidence_insufficient`, `symbol_identity_ambiguous` | `cannot_evaluate` | Required Gograph evidence or stable identity cannot be trusted enough to evaluate exact reach. |
| `execution_unsupported`, `toolchain_unavailable`, `execution_failed`, `context_canceled`, `deadline_exceeded` | `cannot_evaluate` | Requested execution could not be completed reliably. |

Diagnostics use the same reason when applicable; `ledger_unavailable` is the
specific diagnostic code for an unavailable ledger while the result reason is
`ledger_invalid`. Missing rules, runtime skips, and setup or compile errors are
therefore `cannot_evaluate`, never an evaluated failure.

The guarantee is deliberately narrow. Static validation confirms the integrity
of the selected rule binding and its extracted source fingerprint and, when the
row opts in, exact protected-symbol reach from trusted Gograph evidence. Executed
validation first establishes that static integrity and then runs the supported
bound test. Neither mode proves the natural-language rule, unrelated code, or
the repository globally correct. A proof fingerprint identifies recorded proof
metadata; it does not establish that the record is truthful.

## `check`, in depth

For the ledger itself: strict parse (any malformed or missing field is fatal),
rows plus explicit repaired-fixture audit entries ≥ FLOOR, and — once the
header is adopted — the measured count of red-proved armed rows ≥ RED-PROOFS.

For every **armed** row:

1. The check file exists and its suffix matches `enforced-by`.
2. The tag resolves to exactly one test.
3. No `.skip`/`.only` modifier (Playwright), no `t.Skip`/`t.Skipf`/
   `t.SkipNow` in the body (Go).
4. The recomputed body hash equals the ledger hash.
5. **Exact protected-symbol bindings:** Gograph must provide persisted,
   current, complete, precise evidence that the stored test identity reaches
   every stored production identity through exact edges. Possible-only edges
   fail; unavailable or ambiguous evidence is CANNOT-EVALUATE.
6. **Rows with persisted execute policy, compatible legacy `unit` Go rows,
   plus a selected non-unit `--run-profile`:**
   `go test -count=1 -run '^TestXxx$'` runs in the file's package and must
   report `--- PASS`. The bound test executes; the prose rule is not itself an
   executable object.
7. **Playwright rows, with `--report pw.json`:** the ID must appear in the
   Playwright JSON report with every result `passed`. Use this in the job
   that runs your e2e suite: `playwright test --reporter=json > pw.json`,
   then `rulefloor check --report pw.json`.

**Orphan scan:** every `[ID]`-shaped tag in `e2e/**/*.spec.ts` titles and every
extractor-discovered Go rule comment in `**/*_test.go` (skipping `.git`,
`node_modules`, `vendor`, `testdata`) must have a ledger row. Go discovery uses
the standard parser, so fixture strings are not comments. A tag without a row
means a binding nobody is tracking — that's a failure, not a warning.

**`--all "path1,path2"`** checks several repos in one invocation (paths as
given, comma-separated) and fails if any fails. Not combinable with
`--report` (a report belongs to one repo's run).

**`--only ID`** uses the same row extraction, static checks, and execution
behavior for one armed binding, avoiding one subprocess per unrelated unit row.
Its output states that it is not a full repository gate. It deliberately skips
other rows, FLOOR/RED-PROOFS comparison, and orphan discovery; run ordinary
`check` before merging. It cannot combine with `--all` or `--report`.

## Wiring into your entrypoints

A ledger nobody checks is decoration. Put `check` inside the commands people
already run — and put it early. Full `check` launches one isolated `go test`
per executable Go row in deterministic ledger order; large ledgers may take
minutes. Use `check --only ID` for local iteration, but retain full `check` in
the gate so unrelated drift and orphan tags cannot hide.

**Makefile:**

```make
verify:
	rulefloor check
	# ... your other gates
```

**package.json:**

```json
"scripts": {
  "rulefloor": "rulefloor check"
}
```

How the binary gets there — PATH, a checkout your gate builds on first
use, a pinned release download — is your repo's decision, documented in
your repo. Whatever you choose, make a missing or unbuildable binary a
hard error (exit 2), never a silent pass: a gate that cannot measure must
not look green.

## What belongs here, what belongs in your repo

rulefloor is a layer below the projects that use it, and this document
stays on its own side of that line.

**This README documents MECHANISM:** the ledger format, the commands and
their refusals, the exit codes, the guarantees, and the honest limits.
Nothing here names a consuming project, and no example is normative beyond
the mechanism it demonstrates — the IDs, sentences, and profile names in
the transcripts above are placeholders, not conventions.

**Your repo documents its own POLICY:** the profile vocabulary it chose
and what each profile means operationally; its red-proof conventions
(what an observation must cite, dating, escalation notes); its burndown method
for historical `-` rows; and the entrypoint wiring that runs `check` (and
any `--run-profile` runs) in its pipelines. Those choices belong in the
consuming repo's docs, next to the people who made them — not here, where
every consumer would inherit one project's conventions as if they were
the tool's.

Rulefloor stores optional stable production-symbol identities and consumes
Gograph as structural evidence for exact test reach. Gograph remains the graph
analysis authority: Rulefloor neither duplicates its analysis nor overrides
missing or uncertain evidence. Census completeness and repository-specific
integration conventions still belong to consuming-repository governance;
Rulefloor checks only the symbols explicitly declared on each row.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | OK. |
| `1` | A rule failed or a command refused (tamper, skip, orphan, below FLOOR, duplicate, no-op rehash, …). |
| `2` | Fatal — the tool could not evaluate: malformed ledger, missing field, unknown file kind, ambiguous tag or symbol, unreadable report, missing toolchain, or unavailable/insufficient required graph evidence. A gate that cannot measure must never pass. |

## Troubleshooting

**`tag [CART-1] not found in any test title`** — the tag isn't in the file you
armed, or it's in a `test.describe` title (tags go on `test`/`it` titles),
or the ID is spelled differently. For Go: the line must be exactly
`// RULE: CART-1` (that spacing), *directly* above the `func TestXxx` line —
no blank line between.

**`CANNOT-EVALUATE: tag [CART-1] appears in 2 test titles; it must be unique`**
— one rule, one test. Give the second test its own rule, or drop its tag.

**`refusing: rule CART-1 is already armed (use rehash to accept a changed
body)`** — you wanted `rehash`. `arm` is a one-time act; accepting body
changes is deliberately a separate verb.

**`refusing: no-op rehash`** — the body hasn't changed, so there is nothing
to accept. If you expected a change, you edited a different test.

**`CANNOT-EVALUATE: diff ... no matching bound span`** — `diff` could not find
the ledger fingerprint in the newest 200 revisions that changed the bound
file. The repository may be shallow, the baseline may be uncommitted or older,
or Git may be unavailable. Review with normal repository history; do not
rehash merely to silence this diagnostic.

**`check file "../x" must be a relative path inside the repo`** — check files
never escape the repo root; move the test or the ledger.

**`hash mismatch` right after formatting/prettier ran** — formatters change
bytes, bytes are the pin. Review the diff, then `rehash`. This is working as
intended: the ledger notices *every* change, and you decide which ones are
fine.

**`go test ran but did not report "--- PASS: TestXxx"`** — the tagged func
exists (the tag check passed) but the run didn't execute it; usually a
build-tag or platform constraint on the file. For selected machine validation,
pass the repository's explicit `--tags`; for normal checks, use the matching
`--run-profile NAME --tags TAGS` gate.

**`CANNOT-EVALUATE: graph evidence unavailable or insufficient`** — this row
uses Gograph stable IDs and therefore requires a persisted, current, complete,
precise graph with typed-complete test-call resolution. Install Gograph if it is
missing, run `gograph build . --precise`, and retry. Label-only cover rows do
not require Gograph and never attempt to locate it.

## Guarantees and honest limits

Rulefloor checks current repository files. It is not a formal verifier, a
business-truth oracle, or a claim that every dependency of a test is unchanged.

**Guarantees:**

- Rulefloor is the ledger's intended writer, and every tool write keeps the format
  parseable by its own strict parser.
- FLOOR never decreases. Rows never silently disappear.
- Historical fixture-only row repair is explicit and retained in the ledger;
  it cannot be used for armed or ordinary declared rules.
- RED-PROOFS never decreases once adopted; arming demands a red-proof.
- Sentence amendment cannot change a binding or either ratchet, and legitimate
  row deletion remains unavailable.
- No command or flag lowers `check`'s strictness.
- CANNOT-EVALUATE is always fatal (exit 2), never a skip.

**Honest limits — what Rulefloor cannot establish:**

- Static validation confirms the selected binding, tag, restrictions, proof
  presence, and source fingerprint. Executed validation additionally runs the
  supported bound test. Neither semantically proves the natural-language rule.
- A red-proof record means a failure observation was recorded. Rulefloor cannot
  verify the truthfulness, provenance, or external reference behind that record.
- Rulefloor checks the current ledger and working tree. Git history, signed
  commits, protected branches, and repository review are the trust boundary for
  who changed a ledger, whether `rehash` was justified, and whether proof
  replacement was appropriately reviewed.
- `version --json` exposes both the legacy builder stamp and Go's embedded main
  module version. Agreement is useful provenance, not a signature or checksum;
  verify shipped binaries against the release's `checksums.txt`.
- `prove --run` establishes only that the selected Go test reported failure in
  that invocation. It cannot establish why the test failed, whether a mutation
  was appropriate, or whether the human proof text is truthful.
- `rehash` deliberately accepts the current tagged test span after review; it
  does not decide whether that edit preserves the rule's meaning.
- `diff` uses Git only to locate source bytes whose extracted fingerprint
  exactly matches the ledger. It is bounded to the newest 200 revisions of the
  check file, reads only the bound span, and does not establish authorship,
  intent, or semantic equivalence.
- `ledger-diff` compares two recorded logical ledgers. It detects that a
  sentence changed relative to the selected committed base, but cannot judge
  whether the old or new sentence is true or whether the binding still proves
  the words. A user who changes both the sentence and review baseline has
  crossed the Git/review trust boundary, not defeated a semantic proof.

- A **conditional** skip (`test.skip(env.CI, ...)` inside the body, or a
  guarded `t.Skip()` that the arm-time scan didn't match) is part of the
  hashed body but not statically decidable. For such specs, name the profile
  after the run that actually executes them (a run mode of your own naming,
  `full-stack` say) and enforce via `--report` from that run — do not claim
  static protection.
- A **describe-level or block-level conditional gate** — Playwright
  `test.skip(cond, ...)` at `test.describe` scope — sits entirely OUTSIDE
  the hashed test span: `arm` accepts the test and `check` cannot see the
  gate at all, not even as changed bytes. Rows gated this way are enforced
  only by their profile run, never by the static check.
- The hash pins the **test's own span**. Helpers, generated inputs, environment,
  toolchains, and dependencies live outside that span. Their changes may alter
  semantics without changing the fingerprint; an executed test can detect some
  resulting behavior changes, but Rulefloor does not fingerprint that closure.
- Playwright rows without `--report` are pinned but not executed by `check`.
- Vitest rows (`*.test.ts`, kind `vitest`) are pinned (tag + hash + no
  `.skip`/`.only`) but never executed by `check`, and their title tags are
  not orphan-scanned — the vitest suite itself runs in the repo's own CI
  gate.
- Go rows on a non-`unit` profile are pinned but not executed by plain
  `check` — their runtime evaluation is the explicit
  `--run-profile` invocation, which someone must actually run (wire it into
  the pipeline that provisions the environment).
- The RED-PROOFS ratchet counts proofs; it does not name them. A single
  hand edit that empties one row's proof while adding text to another
  keeps the count and passes — hand edits are forbidden by the only-writer
  contract, and this is the same trust boundary FLOOR has. A hand-LOWERED
  header is likewise invisible (measured ≥ header passes), exactly as a
  hand-lowered FLOOR is.
- The proof text and optional reference are recorded metadata. Their fingerprint
  detects representation drift; it is not evidence that the observation is true.
- A supersession fingerprint links the active proof to the exact prior ledger
  cell. The prior text remains in Git history rather than an in-ledger history
  database; the link does not classify either observation as true or false.

## Ledger format specification

The six-column table is the stable canonical format. The parser rejects
malformed or extra columns fatally.

```
FLOOR: <non-negative integer>
RED-PROOFS: <non-negative integer>       (optional on legacy ledgers)
REPAIRED-FIXTURES: <ID>[,<ID>...]        (optional migration audit)
<blank line>
| ID | one-sentence rule | enforced-by | check | red-proof | hash |
|---|---|---|---|---|---|
| <ID> | <sentence> | <kind or -> | <file @ profile, or NONE> | <text or -> | <12 hex, or -> |
```

- Six columns, every cell non-empty (`-` is the explicit placeholder).
- `RED-PROOFS`: at most once, directly after `FLOOR`; absent only on
  ledgers that predate the ratchet (`redproofs --adopt` migrates).
- `REPAIRED-FIXTURES`: migration-only audit metadata, at most once between the
  ratchet headers and table. IDs are unique, cannot also be rows, and each
  counts toward FLOOR. Existing entries are permanent so migrated ledgers keep
  their ratchet history. New entries can be added only by the narrow
  `repair-fixture-row` compatibility command; ordinary workflows never grow it.
- `ID`: `[A-Z][A-Z0-9-]{0,30}[0-9]`, unique per ledger.
- `enforced-by`: `playwright` | `go-test` | `vitest` for armed rows; `-`
  for declared.
- `check`: `<repo-relative file> @ <profile>`, or `NONE` (declared).
- `rulefloor-binding-v1`: optional canonical base64url JSON metadata in the
  red-proof cell stores `execution_policy` (`static` or `execute`) and, for an
  exact-reach Go row, `reachability_policy: "exact"` plus the bound test's
  Gograph stable identity. Newly armed rows always persist execution policy;
  rows without this token retain legacy profile compatibility.
- `red-proof`: `-`, legacy free text, or a canonical
  `[proof-v1 kind=KIND ref=URL] TEXT` record (`ref` is optional). Proof-v1 kinds
  are `manual_observation`, `mutation_observation`, and `ci_reference`;
  legacy free text reads as `legacy_manual`. Structured records retain the text,
  optional parseable recorded time from that text, and optional HTTP(S)
  reference. Missing time or provenance remains unknown; it is never invented.
  References cannot contain whitespace, credentials, fragments, or non-HTTP(S)
  schemes, and are stored without being fetched or verified.
  A proof created with `--supersede` prefixes its text with
  `supersedes-sha256:<64 lowercase hex>`, linking to the full fingerprint of the
  prior genuine proof without changing the six-column or proof-v1 format.
  Optional covered symbols are encoded after the logical proof as
  `<!-- rulefloor-covers-v1:<base64url canonical JSON array> -->`. The token is
  validated by `check`, is not counted as proof, and is omitted entirely for an
  empty symbol set. Label-only arrays remain metadata. Stable Gograph identities
  paired with binding-v1 exact policy are resolved and enforced.
- `hash`: exactly 12 lowercase hex chars for armed rows; `-` for declared
  rows (enforced both ways).
- Cells cannot contain `|` or newlines (input validation refuses them).
