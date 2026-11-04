# Rulefloor v0.9.2

Rulefloor v0.9.2 adds a light check mode and an opt-in load guard for routine
use, without changing the gate form of `check`.

## Light check

`rulefloor check --light` validates the ledger, FLOOR and RED-PROOFS ratchets,
test-span hashes, protected-symbol graph evidence and orphans, but does not
execute the bound Go tests. It prints a `LIGHT: ... not a gate` line and ends
with `light check OK (armed suites NOT run): ...` (or `light selected check OK
(armed suite NOT run; not a gate)` under `--only`). It never prints the gate's
`check OK:` line, so a gate pattern anchored on `^check OK:` cannot accept it.
Gates must run ordinary `check`, which still executes every armed suite.
`--light` cannot combine with `--run-profile`.

## Load guard and build cap

- `--max-load N` / `RULEFLOOR_MAX_LOAD` waits before each test execution while
  load1 is at or above N, bounded by `--load-wait` (default 5m, then exit 2
  `CANNOT-EVALUATE`), polled every `--load-poll` (default 5s).
- `--jobs N` / `RULEFLOOR_JOBS` caps `go test -c` build parallelism.
- All are off unless set.

## Also included

The release also carries the immutable floor operations (`ops-emit`,
`ops-validate`, `ops-apply`) and CI pin updates already merged to `main` after
v0.9.1.

## Compatibility

- Ordinary `check` output, exit codes, ledger serialization, hashes, ratchets
  and every stable machine JSON schema are unchanged. `check` has no JSON
  output.
- `--light` is opt-in; no repository script or Makefile in the owner's tool set
  calls it.

Official archives target Linux and macOS on amd64 and arm64, use
`CGO_ENABLED=0`, and are accompanied by `checksums.txt`. Go 1.27.0 or newer is
required to build Rulefloor v0.9.2.
