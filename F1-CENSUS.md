# F1 operation and writer census

Goal: agent-tools-mw-f1-r2. Source anchor: 3a1e7cf2e43c6e8d7fc609c65c9d1ff3a30ca526.
The discovery floor is the operation codec/CLI, shared ledger writer, connected
tests and native documentation. No consumer manifest schema or ordering changes.

Before implementation, Gograph precise source/query/callers/impact identified
internal/ledger.Save as the shared writer used by main.saveLedger and all legacy
mutation commands. Load supplies the expected-old bytes; model.Ledger holds the
snapshot. main.commandSpecs, commandHelp, usageText and flagTakesValue own CLI
discovery. internal/capabilities.New owns the capability document. README owns
the format specification; there is no repo-local instruction file, separate
PROJECT_SPEC, or owned wiki. The parent wiki owns repos/rulefloor.md.

In-scope modifications: internal/ledger/ledger.go, internal/model/model.go,
main.go, internal/capabilities/capabilities.go, capabilities_test.go,
testdata/machine/rulefloor.capabilities.v1.golden.json, Makefile, README.md.
New files: internal/ledger/atomic.go, internal/ledger/atomic_test.go,
operations.go, operations_test.go, and this native census record.
No file deletion. RULE-FLOOR.md remains unchanged and tool-written only.

Save's direct package consumers are main, internal/check,
internal/capabilities, internal/drift, internal/ledgerdiff and
internal/validation. Model's direct consumers also include internal/check,
internal/ledger, main and internal/validation. Focused race iteration covers
those packages plus internal/model. The final native verify recipe retains
all existing checks and strengthens its full-suite Go test call with -race.

Proof IDs (native test functions; no network, database or real credential use):
- F1-RED-INITIAL: TestSaveRejectsStaleSnapshot and TestOperationCLIDiscovery
  failed on the anchor behavior, then passed after implementation.
- F1-ATOMIC: TestReplacementFailureSemantics injects file-sync, rename,
  directory-sync and an interposed foreign write; TestReplacementOccupiedAndCreateOnly
  retains occupied-lock and create-only refusal controls.
- F1-CLOSED: TestOperationsClosedCodec checks unknown/duplicate/missing/null,
  truncated and trailing data, retaining a valid decode after each refusal.
- F1-SET: TestOperationsPartialConflictingAndFingerprintRefusals covers duplicate,
  conflicting, altered original/fingerprint/identity/binding/kind and empty sets,
  unchanged canonical bytes, retained fragments, valid apply and replay refusal.
- F1-COMMUTE: TestDisjointOperationsCommuteAndRatchet compares both addition
  orders' exact bytes; TestDisjointArmOperationsPreserveProofRatchet measures
  FLOOR/RED-PROOFS and measured proofs after both arming orders.
- F1-INTEGRITY: TestOperationsRefuseSkipNoopProofAndInvalidRowsHeaders covers
  source/skip/proof/row/ratchet/stale failures and valid restored controls;
  TestOperationsProofSupersessionAndRehash checks protected supersession and
  refuses combined changes.
- F1-DIAGNOSTIC: TestOperationsOverlayRetainsCanonicalBytesAndFragment marks
  diagnostic output, preserves inputs and refuses duplicate identity flags.
- F1-EMIT: TestOperationsEmitApplyAndLegacyBytes validates actual CLI emission,
  applies retained operations, preserves untouched spacing and refuses no-op emit.

Threat boundary for independent review: expected-old replacement serializes
cooperating writers. File-sync/rename failures publish nothing; a post-rename
directory-sync error explicitly reports installed=true and uncertain durability.
No advisory lock defeats a filesystem writer that ignores it. Host mediation,
source sealing, caller authentication and final native review remain external.
Operation identity strings and recomputable fingerprints are not signatures.

Final graph session: agenttoolsmwf1r2_20261014_141357_a0e094a660daf8fd.
Capabilities, precise builds, plan and final review --uncommitted completed;
the session ended before the native gate. Raw audit summary:

Status          : Completed
Total Commands  : 85
Successful      : 75
Failed          : 10
Invocation errors: 0 (excluded from compliance)
Success Rate    : 88.2%
Plan Rule Run   : true (Weight: 35%)
Review Rule Run : true (Weight: 35%)
Composability   : 21.9% (Weight: 30%)
Overall Score   : 76.6%
Compliance Grade: C (Needs Improvement)

F1-ITERATION: focused ordinary and race tests passed for the changed packages
and direct consumers. F1-ONLY: WRITE-GUARDS-1 passed through check --only;
its output explicitly remains a selected check, not a full repository gate.
F1-DEV-CAP: the additive human capability fixture initially failed after new
capabilities appeared; expected values were updated with exact equality retained.
No full gate had run during development.

Tool findings: ten exploratory Gograph source/review responses were unsuccessful:
ambiguous short selectors, nonexistent helper guesses, and a changed helper
absent from the graph. Exact selectors and the mandated precise rebuild resolved
the applicable queries; final review succeeded. The audit retains every call.
An apply_patch request targeting the same golden path twice failed before writing;
the subsequent single-path update succeeded. No permission/classifier denial.

Process deviation: relevant recent parent-log entries were read after initial
edits, rather than before them; source, plan, decisions, queue and instructions
had been inspected beforehand. The late log reads revealed no F1 scope conflict.
Formatting used the native gofmt formatter; source edits used apply_patch.
