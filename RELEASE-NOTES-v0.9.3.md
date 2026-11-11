# Rulefloor v0.9.3

Rulefloor v0.9.3 puts three bounds on every Go test process that `check`,
`validate` and `prove --run` start, so a caller that runs Rulefloor under its
own deadline can rely on it.

## Bounded capture

Rulefloor keeps at most 65,536 bytes of one child's combined output. It reads
the rest only to scan it, so a noisy test cannot grow Rulefloor's memory or
block on a full pipe. The `--- PASS:`, `--- FAIL:` and `--- SKIP:` lines of the
selected test are found by scanning the whole stream, so the verdict does not
depend on dropped bytes. A failure line past the limit is still a failure. A
pass needs exit status 0, the pass line, and a stream read to its end.

## Process-group stop

On Linux and macOS each executed child leads its own process group. The whole
group is killed on deadline, cancel, SIGINT and SIGTERM, so the test binary and
the processes it started end with `go`. After the child exits, Rulefloor waits
at most five seconds for the end of its output. If a process that the test
started still holds the pipe, the result is `cannot_evaluate` with the new
reason `output_incomplete`, never a pass.

## Deadline for validate

`validate --timeout DURATION` bounds the whole validation. The default is
`15m`. Zero, a negative value, or an unparsable value is `invalid_request`. On
expiry the result is `cannot_evaluate` with reason `deadline_exceeded`. `check`
and `prove --run` add no deadline of their own.

## Discovery

`rulefloor capabilities --json` reports the bounds in `execution_semantics`:
`capture_limit_bytes`, `process_group_stop`, `validate_timeout_flag` and
`validate_timeout_default_seconds`. A binary older than v0.9.3 omits them.

## Compatibility

- The new capability fields and the reason `output_incomplete` are additive
  inside the v1 schemas. A consumer that meets an unknown reason must act on
  `outcome`. A consumer that rejects unknown JSON fields must accept the new
  capability fields before it adopts this release.
- Test processes now run in a process group separate from Rulefloor's. A
  caller that kills only Rulefloor's group with SIGKILL no longer reaches
  them. Send SIGTERM first: Rulefloor then kills the child group before it
  ends.
- A process that outlives a test which exited normally is left running; the
  run reports `output_incomplete` when that process holds the output pipe.
- Ledger bytes, hashes, ratchets, exit codes and ordinary `check` output are
  unchanged. No migration or rehash is required.

Official archives target Linux and macOS on amd64 and arm64, use
`CGO_ENABLED=0`, and are accompanied by `checksums.txt`. Go 1.27.2 or newer is
required to build Rulefloor v0.9.3.
