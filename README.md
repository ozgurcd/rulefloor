# rulefloor

A standalone CLI that maintains `RULE-FLOOR.md` — a machine-checked rule ledger
at a repository's root. The tool is the ledger's **only writer**; `check`
verifies every armed rule against the tagged test it points at and never lowers
`FLOOR`. Stdlib only.

## Ledger format (frozen)

```
FLOOR: N

| ID | one-sentence rule | enforced-by | check | red-proof | hash |
|---|---|---|---|---|---|
| R-1 | Refresh tokens are single use. | playwright | e2e/login.spec.ts @ chromium | - | a1b2c3d4e5f6 |
| R-2 | Not yet enforced. | - | NONE | - | - |
```

- `FLOOR: N` — minimum row count. `declare` and `arm` raise it to the row count; nothing ever lowers it.
- `check` — `<file> @ <profile>` for armed rules, or `NONE` (= declared only).
- `hash` — first 12 hex chars of the sha256 of the tagged test body; `-` when `NONE`.
- IDs match `[A-Z][A-Z0-9-]*[0-9]` (e.g. `R-1`, `SEC-12`); every field must be non-empty.

### Test tags

- **Playwright** (`*.spec.ts`): the test title contains `[ID]`, e.g.
  `test('refresh token is single use [R-1]', ...)`. The hashed body is the whole
  `test(...)` call. Files are scanned for orphan tags under `e2e/`.
- **Go** (`*_test.go`): the line `// RULE: ID` sits directly above
  `func TestXxx`. The hashed body is the whole function. Scanned repo-wide
  (skipping `.git`, `node_modules`, `vendor`, `testdata`).

## Commands

All commands take `--repo PATH` (default `.`).

| Command | Effect |
|---|---|
| `init` | Create an empty ledger. Refuses if one exists. |
| `list` | List all rules with armed/declared state. |
| `show ID` | Print every field of one rule. |
| `unarmed` | List rules whose check is `NONE`. |
| `declare "sentence" --id ID` | Append a declared (unarmed) row and raise FLOOR to the row count. Optional `--red-proof TEXT`. |
| `arm ID --check "file @ profile"` | Resolve the tag, compute the hash, set enforced-by from the file kind, raise FLOOR to the row count. Refuses skipped tests. Optional `--red-proof TEXT`. |
| `rehash ID` | Recompute an armed rule's hash after a reviewed edit. Refuses a no-op. |
| `check [--report pw.json] [--all "repo1,repo2"]` | Verify the ledger (see below). |

## check

For every armed row: the check file exists, the tag resolves to exactly one
test, no `.skip`/`.only` (Go: no `t.Skip*`), the recomputed hash matches, and
the row count is ≥ `FLOOR`. Go-tagged rows additionally run
`go test -count=1 -run '^TestXxx$'` in the file's package and require
`--- PASS`. With `--report pw.json` (a Playwright JSON report), every
Playwright-armed ID must appear with status `passed`. A tag found in
`e2e/*.spec.ts` or any `*_test.go` without a ledger row is an **orphan** and
fails the check. `--all` takes a comma-separated list of repo paths and checks
each one (e.g. `--all "../identuum-idp-oss,../identuum-ui"`); it cannot be
combined with `--report`.

## Exit codes

- `0` — ok
- `1` — check failure or refusal (init exists, duplicate ID, no-op rehash, ...)
- `2` — fatal: malformed ledger, missing field, CANNOT-EVALUATE, usage error
