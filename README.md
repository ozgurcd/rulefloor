# rulefloor

**A machine-checked rule ledger for your repository.**

You have rules you never want to lose: *"refresh tokens are single use"*,
*"lockout looks exactly like a wrong password"*. Each one is enforced by a
test — until someone edits the test, adds a `.skip`, or deletes it, and the
rule silently stops existing. rulefloor makes that decay loud: every rule is
pinned to the exact body of the test that proves it, and `check` fails the
moment that body changes, skips, or disappears.

One file at your repo root — `RULE-FLOOR.md` — human-readable, diff-friendly,
and written **only** by this tool. Stdlib-only Go, no dependencies.

```
$ rulefloor check
PASS SEC-1 (e2e/login.spec.ts @ chromium)
PASS SEC-2 (lockout_test.go @ unit)
check OK: 2 rows (2 armed), FLOOR 2
```

---

- [How it works](#how-it-works)
- [Install](#install)
- [Five-minute tutorial](#five-minute-tutorial)
- [Concepts](#concepts)
- [Command reference](#command-reference)
- [`check`, in depth](#check-in-depth)
- [Wiring into your entrypoints](#wiring-into-your-entrypoints)
- [Exit codes](#exit-codes)
- [Troubleshooting](#troubleshooting)
- [Guarantees and honest limits](#guarantees-and-honest-limits)
- [Ledger format specification](#ledger-format-specification)

---

## How it works

Three verbs, one loop:

1. **`declare`** a rule — one sentence, one ID. It sits in the ledger as
   *declared* (visible debt, not yet enforced).
2. **`arm`** it — point the rule at a tagged test. rulefloor extracts the
   test's body, stores the first 12 hex chars of its sha256, and raises the
   FLOOR (the minimum row count) so the row can never quietly vanish.
3. **`check`** — re-derives everything from the working tree and compares it
   to the ledger. Any drift is a failure: changed body, skipped test, missing
   tag, deleted row, tag without a row.

A legitimate, reviewed edit to an armed test is accepted with **`rehash`** —
an explicit, auditable step, never an automatic one.

## Install

Homebrew (the tap goes live right after the repo):

```bash
brew install ozgurcd/tap/rulefloor
```

Or with Go:

```bash
go install github.com/ozgurcd/rulefloor@latest
```

Or build from source. **Requires Go 1.26.6 or newer** — the module's
`go` directive is `1.26.6`, a pinned floor. Stdlib only, no
dependencies. This floor matters beyond this repo: the entrypoint gates
below build this tool on first use, and a toolchain under the floor (that
cannot fetch 1.26.6 via `GOTOOLCHAIN=auto`) turns those gates into
CANNOT-EVALUATE, which is a hard failure by design.

```bash
git clone https://github.com/ozgurcd/rulefloor.git
cd rulefloor && go build -o rulefloor .
```

Put the binary on your PATH, or call it by path — the entrypoint wiring below
builds it on first use automatically.

## Five-minute tutorial

Every transcript below is a real run, unedited.

Start with a repo containing a Playwright spec and a Go test:

```ts
// e2e/login.spec.ts — the tag is [SEC-1] in the title
test('a refresh token can only be used once [SEC-1]', async ({ page }) => {
  await page.goto('/login');
  // ... sign in, capture the refresh token, use it twice ...
  expect(secondUseStatus).toBe(401);
});
```

```go
// lockout_test.go — the tag is the comment line directly above the func
// RULE: SEC-2
func TestLockoutLooksLikeWrongPassword(t *testing.T) {
	if answerForLockedOut() != answerForWrongPassword() {
		t.Fatal("lockout must be indistinguishable from a wrong password")
	}
}
```

**Create the ledger, declare, arm:**

```
$ rulefloor init
initialized RULE-FLOOR.md (FLOOR: 0)
$ rulefloor declare "Refresh tokens are single use." --id SEC-1
declared SEC-1 (unarmed); 1 rows, FLOOR 1
$ rulefloor declare "Lockout answers exactly like a wrong password." --id SEC-2
declared SEC-2 (unarmed); 2 rows, FLOOR 2
$ rulefloor arm SEC-1 --check "e2e/login.spec.ts @ chromium"
armed SEC-1: e2e/login.spec.ts @ chromium (hash a8fc29914de5); FLOOR 2
$ rulefloor arm SEC-2 --check "lockout_test.go @ unit"
armed SEC-2: lockout_test.go @ unit (hash eec4772a9625); FLOOR 2
$ rulefloor check
PASS SEC-1 (e2e/login.spec.ts @ chromium)
PASS SEC-2 (lockout_test.go @ unit)
check OK: 2 rows (2 armed), FLOOR 2
```

The ledger now reads:

```
FLOOR: 2

| ID | one-sentence rule | enforced-by | check | red-proof | hash |
|---|---|---|---|---|---|
| SEC-1 | Refresh tokens are single use. | playwright | e2e/login.spec.ts @ chromium | - | a8fc29914de5 |
| SEC-2 | Lockout answers exactly like a wrong password. | go-test | lockout_test.go @ unit | - | eec4772a9625 |
```

**Now someone weakens the assertion** (`toBe(401)` → `toBe(200)`):

```
$ rulefloor check
FAIL SEC-1: hash mismatch: ledger a8fc29914de5, actual 817a26be968d (body changed; review, then rehash)
PASS SEC-2 (lockout_test.go @ unit)
rulefloor: check FAILED: 1 problem(s) in .    # exit 1
```

**Or skips the test** (`test(` → `test.skip(`):

```
FAIL SEC-1: .skip is set on the tagged test
FAIL SEC-1: hash mismatch: ledger a8fc29914de5, actual 0c44037af79f (body changed; review, then rehash)
PASS SEC-2 (lockout_test.go @ unit)
rulefloor: check FAILED: 2 problem(s) in .    # exit 1
```

**A reviewed, legitimate edit is accepted explicitly:**

```
$ rulefloor rehash SEC-1
rehashed SEC-1: a8fc29914de5 -> 817a26be968d
$ rulefloor check
PASS SEC-1 (e2e/login.spec.ts @ chromium)
PASS SEC-2 (lockout_test.go @ unit)
check OK: 2 rows (2 armed), FLOOR 2
$ rulefloor rehash SEC-1   # again, unchanged
rulefloor: refusing: no-op rehash for SEC-1 (hash unchanged: 817a26be968d)   # exit 1
```

**A tagged test without a ledger row is an orphan — also a failure:**

```
$ rulefloor check
PASS SEC-1 (e2e/login.spec.ts @ chromium)
PASS SEC-2 (lockout_test.go @ unit)
DECLARED SEC-3 (no armed check)
FAIL e2e/login.spec.ts: orphan tag SEC-9 (no ledger row)
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
it appears in `list`, `unarmed`, and check output (`DECLARED SEC-3 (no armed
check)`), but nothing enforces it yet. An **armed** rule points at a tagged
test and carries its body hash. `unarmed` is your work queue.

### FLOOR

`FLOOR` is the minimum number of rows the ledger may ever have. Both
`declare` and `arm` raise it to the current row count; **nothing ever lowers
it** — not even the tool. Delete a row by hand and `check` fails with
`rows is below FLOOR`. This is what makes the ledger append-only in practice.

### Tagging tests

| Kind | File | Tag |
|---|---|---|
| Playwright | `*.spec.ts` | `[ID]` anywhere in the test title string: `test('... [SEC-1]', ...)` |
| Go | `*_test.go` | the exact line `// RULE: ID` **directly above** `func TestXxx` |

A tag must resolve to exactly one test — two tests carrying the same tag is a
fatal error, never a guess. IDs match `[A-Z][A-Z0-9-]*[0-9]` (e.g. `SEC-1`,
`LOGIN-PIN-1`): uppercase, ends in a digit, hyphens allowed.

### The hash

The first 12 hex chars of the sha256 of the test's exact source span — for
Playwright the whole `test(...)` call, for Go the whole `func TestXxx {...}`.
Any byte of drift changes it. The extractor is string- and comment-aware, so
parentheses and braces inside strings, template literals, and comments don't
confuse the span.

### The check field

`<file> @ <profile>`. The file is relative to the repo root and determines
the kind by suffix (`.spec.ts` → playwright, `_test.go` → go-test). The
profile names *where the test actually runs* — a Playwright project
(`chromium`), or something like `e2e-run` when the proof only executes in
a full e2e environment (see
[honest limits](#guarantees-and-honest-limits)).

**For go-test rows the profile is semantic.** `unit` means the test is
hermetic: plain `check` executes it and refuses `t.Skip` in its body. Any
other profile (say `integration`) marks a test with an environmental
precondition — a database, a live stack: plain `check` verifies it
STATICALLY only (file, tag, hash), `t.Skip` guards are allowed, and the
teeth live behind an explicit run:

```bash
rulefloor check --run-profile integration --tags integration
```

`--run-profile NAME` additionally executes every go-test row whose profile
is NAME with `go test -tags T -count=1 -run '^TestXxx$'`. There, a test
that SKIPS at runtime is CANNOT-EVALUATE (exit 2) — its precondition was
absent and a skip is not a proof; a failure is a failure (exit 1). Unit
rows behave exactly as in plain check. `--tags` requires `--run-profile`;
`--run-profile` cannot combine with `--all`. No flag weakens any existing
check — the mode only ADDS execution.

## Command reference

Every command takes `--repo PATH` (default `.`).

| Command | Effect |
|---|---|
| `init` | Create an empty ledger. Refuses if one exists. |
| `list` | All rules, with armed/declared state. |
| `show ID` | Every field of one rule. |
| `unarmed` | Rules that still need a test — the work queue. |
| `unproved` | Armed rows whose red-proof cell is still `-` — the historical proof debt. |
| `redproofs [--adopt]` | Ratchet status; `--adopt` writes `RED-PROOFS:` onto a legacy ledger at the **measured** count. |
| `declare "sentence" --id ID` | Append a declared row; raise FLOOR. Optional `--red-proof TEXT`. |
| `arm ID --check "file @ profile" --red-proof TEXT` | Pin the tagged test's hash; set enforced-by; raise FLOOR and RED-PROOFS. Refuses skipped tests. `--red-proof` is **required**. |
| `prove ID --red-proof TEXT [--replace]` | Record a watched proof on an already-armed `-` row (the debt burndown path); raises RED-PROOFS. Refuses to replace an existing proof — except with `--replace`, which overwrites ONLY a cell the tool can see is a non-proof (a `blocked:…` pre-arming note or a dateless cell), never a genuine dated proof. |
| `rehash ID` | Accept a reviewed body change. Refuses a no-op, refuses skipped tests. |
| `check [--report pw.json] [--all "repo1,repo2"]` | Verify everything (below). |

Refusals you will meet, all deliberate:

- `init` when a ledger exists · `declare` a duplicate ID · `arm` an
  already-armed rule (that's what `rehash` is for) · `arm` without a
  `--red-proof` (or with the `-` placeholder) · `arm`/`rehash` a test
  that has `.skip`/`.only` or calls `t.Skip*` · `rehash` when nothing
  changed · `redproofs --adopt` when the header already exists ·
  `prove --replace` over a genuine dated proof (only a `blocked:…` or
  dateless non-proof may be overwritten).

### The red-proof obligation

A check nobody has ever watched FAIL proves nothing — it can be armed,
green, and false (an assertion that matches every state passes vacuously).
`--red-proof TEXT` records where the test was seen *red* (a mutation
watched failing, a commit, a run URL). Since the RED-PROOFS ratchet:

- **`arm` requires it.** Every newly armed row carries a real proof text —
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
  debt; it shrinks only through `prove` (a genuinely watched failure,
  recorded dated on the row) or new arms.
  (Any write operation — declare, arm, rehash — also adopts the header on
  a legacy ledger, at the same measured-count rule.)

The proof TEXT itself is documentation: the tool ratchets the count, it
cannot judge whether the words describe a genuinely watched failure.

## `check`, in depth

For the ledger itself: strict parse (any malformed or missing field is
fatal), row count ≥ FLOOR, and — once the header is adopted — the measured
count of red-proved armed rows ≥ RED-PROOFS.

For every **armed** row:

1. The check file exists and its suffix matches `enforced-by`.
2. The tag resolves to exactly one test.
3. No `.skip`/`.only` modifier (Playwright), no `t.Skip`/`t.Skipf`/
   `t.SkipNow` in the body (Go).
4. The recomputed body hash equals the ledger hash.
5. **Go rows only:** `go test -count=1 -run '^TestXxx$'` runs in the file's
   package and must report `--- PASS`. Your rule's proof executes on every
   check.
6. **Playwright rows, with `--report pw.json`:** the ID must appear in the
   Playwright JSON report with every result `passed`. Use this in the job
   that runs your e2e suite: `playwright test --reporter=json > pw.json`,
   then `rulefloor check --report pw.json`.

**Orphan scan:** every `[ID]`-shaped tag in `e2e/**/*.spec.ts` titles and
every `// RULE:` line in `**/*_test.go` (skipping `.git`, `node_modules`,
`vendor`, `testdata`) must have a ledger row. A tag without a row means a
proof nobody is tracking — that's a failure, not a warning.

**`--all "path1,path2"`** checks several repos in one invocation (paths as
given, comma-separated) and fails if any fails. Not combinable with
`--report` (a report belongs to one repo's run).

## Wiring into your entrypoints

A ledger nobody checks is decoration. Put `check` inside the commands people
already run.

**Makefile** (verbatim from a repo using it in production; builds the sibling
checkout's binary on first use, and a missing sibling is a hard error — a
gate that cannot measure must not look green):

```make
RULEFLOOR_DIR ?= ../rulefloor

rulefloor-check:
	@if [ ! -d "$(RULEFLOOR_DIR)" ]; then \
		echo "rulefloor-check: CANNOT-EVALUATE — no rulefloor checkout at $(RULEFLOOR_DIR)" >&2; \
		exit 2; \
	fi
	@if [ ! -x "$(RULEFLOOR_DIR)/rulefloor" ]; then \
		echo "rulefloor-check: building $(RULEFLOOR_DIR)/rulefloor"; \
		(cd "$(RULEFLOOR_DIR)" && go build -o rulefloor .) || { \
			echo "rulefloor-check: CANNOT-EVALUATE — go build of the rulefloor tool failed" >&2; \
			exit 2; \
		}; \
	fi
	@"$(RULEFLOOR_DIR)/rulefloor" check --repo .

verify:
	@$(MAKE) --no-print-directory rulefloor-check
	# ... your other gates
```

**package.json:**

```json
"scripts": {
  "rulefloor": "(test -x ../rulefloor/rulefloor || (cd ../rulefloor && go build -o rulefloor .)) && ../rulefloor/rulefloor check --repo ."
}
```

Run it early — it is cheap (hashing plus a few single-test `go test` runs),
so a tampered rule fails the aggregate before the expensive gates spend their
minutes.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | OK. |
| `1` | A rule failed or a command refused (tamper, skip, orphan, below FLOOR, duplicate, no-op rehash, …). |
| `2` | Fatal — the tool could not evaluate: malformed ledger, missing field, unknown file kind, ambiguous tag, unreadable report, missing toolchain. A gate that cannot measure must never pass. |

## Troubleshooting

**`tag [SEC-1] not found in any test title`** — the tag isn't in the file you
armed, or it's in a `test.describe` title (tags go on `test`/`it` titles),
or the ID is spelled differently. For Go: the line must be exactly
`// RULE: SEC-1` (that spacing), *directly* above the `func TestXxx` line —
no blank line between.

**`CANNOT-EVALUATE: tag [SEC-1] appears in 2 test titles; it must be unique`**
— one rule, one test. Give the second test its own rule, or drop its tag.

**`refusing: rule SEC-1 is already armed (use rehash to accept a changed
body)`** — you wanted `rehash`. `arm` is a one-time act; accepting body
changes is deliberately a separate verb.

**`refusing: no-op rehash`** — the body hasn't changed, so there is nothing
to accept. If you expected a change, you edited a different test.

**`check file "../x" must be a relative path inside the repo`** — check files
never escape the repo root; move the test or the ledger.

**`hash mismatch` right after formatting/prettier ran** — formatters change
bytes, bytes are the pin. Review the diff, then `rehash`. This is working as
intended: the ledger notices *every* change, and you decide which ones are
fine.

**`go test ran but did not report "--- PASS: TestXxx"`** — the tagged func
exists (the tag check passed) but the run didn't execute it; usually a
build-tag or platform constraint on the file. Arm a test that runs where
`check` runs.

## Guarantees and honest limits

**Guarantees:**

- rulefloor is the ledger's only writer, and every write keeps the format
  parseable by its own strict parser.
- FLOOR never decreases. Rows never silently disappear.
- RED-PROOFS never decreases once adopted; arming demands a red-proof.
- No command or flag lowers `check`'s strictness.
- CANNOT-EVALUATE is always fatal (exit 2), never a skip.

**Honest limits — what the static check cannot see:**

- A **conditional** skip (`test.skip(env.CI, ...)` inside the body, or a
  guarded `t.Skip()` that the arm-time scan didn't match) is part of the
  hashed body but not statically decidable. For such specs, name the profile
  after the run that actually executes them (e.g. `e2e-run`) and enforce via
  `--report` from that run — do not claim static protection.
- A **describe-level or block-level conditional gate** — Playwright
  `test.skip(cond, ...)` at `test.describe` scope — sits entirely OUTSIDE
  the hashed test span: `arm` accepts the test and `check` cannot see the
  gate at all, not even as changed bytes. Rows gated this way are enforced
  only by their profile run (e.g. `e2e-run`), never by the static check.
- The hash pins the **test's own span**. Helpers it calls live outside the
  span; gut a helper and the hash won't notice — the test *run* (Go rows,
  `--report` rows) is the second line of defense.
- Playwright rows without `--report` are pinned but not executed by `check`.
- Vitest rows (`*.test.ts`, kind `vitest`) are pinned (tag + hash + no
  `.skip`/`.only`) but never executed by `check`, and their title tags are
  not orphan-scanned — the vitest suite itself runs in the repo's own CI
  gate.
- Go rows on a non-`unit` profile are pinned but not executed by plain
  `check` — enforcement of their runtime truth is the explicit
  `--run-profile` invocation, which someone must actually run (wire it into
  the pipeline that provisions the environment).
- The RED-PROOFS ratchet counts proofs; it does not name them. A single
  hand edit that empties one row's proof while adding text to another
  keeps the count and passes — hand edits are forbidden by the only-writer
  contract, and this is the same trust boundary FLOOR has. A hand-LOWERED
  header is likewise invisible (measured ≥ header passes), exactly as a
  hand-lowered FLOOR is.
- The proof text is unverified prose: the ratchet guarantees a proof was
  *recorded*, not that the recorded failure was genuinely watched.

## Ledger format specification

Frozen. The parser rejects anything else, fatally.

```
FLOOR: <non-negative integer>
RED-PROOFS: <non-negative integer>       (optional on legacy ledgers)
<blank line>
| ID | one-sentence rule | enforced-by | check | red-proof | hash |
|---|---|---|---|---|---|
| <ID> | <sentence> | <kind or -> | <file @ profile, or NONE> | <text or -> | <12 hex, or -> |
```

- Six columns, every cell non-empty (`-` is the explicit placeholder).
- `RED-PROOFS`: at most once, directly after `FLOOR`; absent only on
  ledgers that predate the ratchet (`redproofs --adopt` migrates).
- `ID`: `[A-Z][A-Z0-9-]{0,30}[0-9]`, unique per ledger.
- `enforced-by`: `playwright` | `go-test` | `vitest` for armed rows; `-`
  for declared.
- `check`: `<repo-relative file> @ <profile>`, or `NONE` (declared).
- `hash`: exactly 12 lowercase hex chars for armed rows; `-` for declared
  rows (enforced both ways).
- Cells cannot contain `|` or newlines (input validation refuses them).
