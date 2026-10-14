// Command rulefloor maintains RULE-FLOOR.md, a machine-checked rule ledger.
// It is the only writer of that file; check verifies the ledger against the
// tagged tests it points at and never lowers FLOOR.
package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
)

// version is stamped at release time via -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usageText = `rulefloor — repository-local invariant integrity (RULE-FLOOR.md)

Usage:
  rulefloor init                              [--repo PATH]
  rulefloor list                              [--repo PATH]
  rulefloor show ID                           [--repo PATH]
  rulefloor unarmed                           [--repo PATH]
  rulefloor unproved                          [--repo PATH]
  rulefloor redproofs                         [--adopt] [--repo PATH]
  rulefloor declare "sentence" --id ID        [--red-proof TEXT] [--covers SYMBOLS] [--repo PATH]
  rulefloor amend ID "sentence"               [--covers SYMBOLS] [--execution static|execute] [--repo PATH]
  rulefloor arm ID --check "file @ profile"   --red-proof TEXT [--proof-kind KIND] [--proof-ref URL]
                                              [--covers SYMBOLS] [--execution static|execute] [--repo PATH]
  rulefloor prove ID --red-proof TEXT         [--proof-kind KIND] [--proof-ref URL] [--replace|--supersede|--force]
                                              [--run [--profile NAME] [--tags T]] [--repo PATH]
  rulefloor rehash ID                         [--repo PATH]
  rulefloor diff ID                           [--repo PATH]
  rulefloor ledger-diff                       --base REF [--json] [--repo PATH]
  rulefloor repair-fixture-row ID             [--repo PATH]
  rulefloor covers                            [--json] [--repo PATH]
  rulefloor check                             [--repo PATH] [--report pw.json] [--all "repo1,repo2"]
                                              [--run-profile NAME [--tags T]] [--only ID] [--timings]
  rulefloor validate ID --repo PATH --mode static|execute [--profile NAME] [--tags T] --json
  rulefloor capabilities [--json]
  rulefloor ops-emit --baseline PATH --original-id ID --source-id ID --review-id ID --authority ID [--repo PATH]
  rulefloor ops-validate|ops-apply|ops-overlay --operations PATH --original-id ID --source-id ID --review-id ID --authority ID [--repo PATH]
  rulefloor version [--json]                  (also: rulefloor --version)

Exit codes: 0 ok, 1 check failure or refusal, 2 fatal
(malformed ledger, missing field, CANNOT-EVALUATE, usage error).

Workflow: declare records a rule; arm binds it and records a red observation;
amend updates rule prose or covered symbols; covers reads the rule-to-symbol map;
check verifies all bindings; validate emits one rule as versioned JSON;
rehash accepts reviewed source drift; prove records red-proof debt.
`

var commandHelp = map[string]string{
	"ops-emit": `Usage: rulefloor ops-emit --baseline PATH --original-id ID --source-id ID --review-id ID --authority ID [--repo PATH]

Emit one immutable rulefloor.operations.v1 document from a retained baseline
and the current tool-written ledger. No input is consumed. Identities are
caller-supplied seals, not authenticated authorization. Unsupported deletion,
combined changes, no-op sets, lowered headers and invalid bindings refuse.
`,
	"ops-validate": `Usage: rulefloor ops-validate --operations PATH --original-id ID --source-id ID --review-id ID --authority ID [--repo PATH]

Validate expected-old logical rows, original bytes, seals, fingerprints,
proof constraints, current source and ratchets. Static diagnostic only;
no test execution and no accepted native evidence. Fragments remain intact.
`,
	"ops-apply": `Usage: rulefloor ops-apply --operations PATH --original-id ID --source-id ID --review-id ID --authority ID [--repo PATH]

Validate the whole set, then replace expected-old ledger bytes atomically.
Cooperating writers take a nonblocking directory lock; file and directory
sync precede success. A directory-sync error reports installed=true: the
whole new ledger is visible but durability is uncertain; inspect before retry.
External mediation is required against noncooperating filesystem writers.
No fragment is consumed. Native final review/gates remain required.
`,
	"ops-overlay": `Usage: rulefloor ops-overlay --operations PATH --original-id ID --source-id ID --review-id ID --authority ID [--repo PATH]

Emit rulefloor.overlay.v1 JSON containing a proposed ledger and explicit
diagnostic_only=true, accepted_evidence=false. Never writes the canonical
ledger, executes a test, consumes fragments or creates accepted evidence.
`,
	"arm": `Usage: rulefloor arm ID --check "file @ profile" --red-proof TEXT [--proof-kind KIND] [--proof-ref URL] [--covers SYMBOLS] [--execution static|execute] [--repo PATH]

Bind a declared rule to its tagged check and record an observed red proof.
Proof text must be non-empty, fit on one line, and cannot contain "|" because
RULE-FLOOR.md uses a six-column Markdown table. Proof kinds are
manual_observation, mutation_observation, and ci_reference. References are
optional HTTP(S) URLs and are recorded but never fetched or verified.
--covers accepts a comma-separated set of protected product symbols. An
all-Gograph-stable-ID set opts a Go binding into exact reach enforcement;
legacy labels remain metadata. Mixed identity modes are refused.
--execution persists the binding's default check policy. When omitted, the
legacy default is interpreted once and stored for backward compatibility.
`,
	"prove": `Usage: rulefloor prove ID --red-proof TEXT [--proof-kind KIND] [--proof-ref URL] [--replace|--supersede|--force] [--run [--profile NAME] [--tags T]] [--repo PATH]

Record red-proof evidence for an existing rule. Proof text must be non-empty,
fit on one line, and cannot contain "|". --replace refuses to overwrite a
genuine proof. --supersede explicitly replaces a genuine re-watched proof and
links the new record to its full SHA-256 fingerprint. --force remains the loud
emergency override. --run records nothing unless the selected Go test reports
FAIL; non-unit profiles require an exact --profile and may use --tags.
`,
	"declare": `Usage: rulefloor declare "sentence" --id ID [--red-proof TEXT] [--covers SYMBOLS] [--repo PATH]

Declare one invariant. Optional proof text must fit on one line and cannot
contain "|" because the ledger is a six-column Markdown table. --covers stores
an optional comma-separated symbol set without changing that table shape.
All-Gograph-stable-ID sets are resolved now and require exact reach when armed.
`,
	"amend": `Usage: rulefloor amend ID "sentence" [--covers SYMBOLS] [--execution static|execute] [--repo PATH]

Replace an existing rule's sentence and, when --covers is present, its covered
symbol set. Use --covers= to clear that set. The ID, binding, hash, proof,
FLOOR, and RED-PROOFS are preserved. --execution explicitly migrates an armed
legacy binding. Stable Gograph IDs require current exact reach for armed Go
rows. A no-op amendment is refused; rules cannot be deleted.
`,
	"covers": `Usage: rulefloor covers [--json] [--repo PATH]

Read the optional rule-to-symbol map. JSON output is one deterministic
rulefloor.covers.v1 document and includes every rule ID; unmapped rules have [].
`,
	"rehash": `Usage: rulefloor rehash ID [--repo PATH]

Accept the current extracted test span after review. Rehash refuses unknown or
unarmed rules, restricted/skipped tests, and a no-op unchanged fingerprint. It does
not replace red-proof evidence; use prove --supersede after re-watching an
extended test, or prove --force only for an exceptional explicit override.
`,
	"check": `Usage: rulefloor check [--repo PATH] [--report pw.json] [--all "repo1,repo2"] [--run-profile NAME [--tags T]] [--only ID] [--timings]

Without --only, verify the complete repository gate including ratchets and
orphans. --only evaluates one armed binding for fast feedback and is not a
replacement for the full gate. Legacy unit Go rows execute; other Go profiles
execute only with a matching --run-profile. --timings appends a bounded human
diagnostic with total time, package compilation time, and the 10 slowest row
evaluations. Row time includes static, graph, and any executed-test work.
`,
	"diff": `Usage: rulefloor diff ID [--repo PATH]

Show the current bound span against the newest Git revision whose extracted
fingerprint matches the ledger. This read-only comparison does not classify a
change as cosmetic or semantic.
`,
	"ledger-diff": `Usage: rulefloor ledger-diff --base REF [--json] [--repo PATH]

Compare the current logical ledger with RULE-FLOOR.md at a committed Git base.
Sentence, binding, proof, covered-symbol, and test-fingerprint changes are
classified separately. A difference exits 1; unavailable or invalid Git
evidence exits 2. This detects recorded text changes, not whether prose is true.
`,
	"validate": `Usage: rulefloor validate ID --repo PATH --mode static|execute [--profile NAME] [--tags T] --json

Validate one selected binding as a strict rulefloor.validation.v1 document.
Static mode never executes and rejects --profile/--tags. Execute mode performs
static validation first, never downgrades, and supports validated Go build tags.
`,
}

// exitErr carries the process exit code alongside the message.
type exitErr struct {
	code int
	msg  string
}

func (e exitErr) Error() string { return e.msg }

func failf(format string, a ...any) error  { return exitErr{1, fmt.Sprintf(format, a...)} }
func fatalf(format string, a ...any) error { return exitErr{2, fmt.Sprintf(format, a...)} }

// silentExit carries a machine-command exit code after its JSON result has
// already been written. run must not add human diagnostics to stderr.
type silentExit struct{ code int }

func (e silentExit) Error() string { return "" }

// flagTakesValue lists every flag; false marks booleans.
var flagTakesValue = map[string]bool{
	"--baseline":    true,
	"--operations":  true,
	"--original-id": true,
	"--source-id":   true,
	"--review-id":   true,
	"--authority":   true,
	"--repo":        true,
	"--id":          true,
	"--check":       true,
	"--report":      true,
	"--only":        true,
	"--red-proof":   true,
	"--all":         true,
	"--run-profile": true,
	"--tags":        true,
	"--mode":        true,
	"--profile":     true,
	"--proof-kind":  true,
	"--proof-ref":   true,
	"--covers":      true,
	"--execution":   true,
	"--base":        true,
	"--json":        false,
	"--adopt":       false,
	"--replace":     false,
	"--force":       false,
	"--supersede":   false,
	"--run":         false,
	"--timings":     false,
}

type commandInvocation struct {
	repo   string
	flags  map[string]string
	pos    []string
	stdout io.Writer
}

type commandSpec struct {
	positionals int
	flags       map[string]bool
	run         func(commandInvocation) error
}

var commandSpecs = map[string]commandSpec{
	"ops-emit":     {0, operationFlags(true), func(c commandInvocation) error { return cmdOperations(c, "ops-emit") }},
	"ops-validate": {0, operationFlags(false), func(c commandInvocation) error { return cmdOperations(c, "ops-validate") }},
	"ops-apply":    {0, operationFlags(false), func(c commandInvocation) error { return cmdOperations(c, "ops-apply") }},
	"ops-overlay":  {0, operationFlags(false), func(c commandInvocation) error { return cmdOperations(c, "ops-overlay") }},
	"init": {0, map[string]bool{"--repo": true}, func(c commandInvocation) error {
		return cmdInit(c.repo, c.stdout)
	}},
	"list": {0, map[string]bool{"--repo": true}, func(c commandInvocation) error {
		return cmdList(c.repo, c.stdout)
	}},
	"show": {1, map[string]bool{"--repo": true}, func(c commandInvocation) error {
		return cmdShow(c.repo, c.pos[0], c.stdout)
	}},
	"unarmed": {0, map[string]bool{"--repo": true}, func(c commandInvocation) error {
		return cmdUnarmed(c.repo, c.stdout)
	}},
	"unproved": {0, map[string]bool{"--repo": true}, func(c commandInvocation) error {
		return cmdUnproved(c.repo, c.stdout)
	}},
	"redproofs": {0, map[string]bool{"--repo": true, "--adopt": true}, func(c commandInvocation) error {
		return cmdRedProofs(c.repo, c.flags["--adopt"] == "true", c.stdout)
	}},
	"declare": {1, map[string]bool{"--repo": true, "--id": true, "--red-proof": true, "--covers": true}, func(c commandInvocation) error {
		covers, coversSet := c.flags["--covers"]
		return cmdDeclare(c.repo, c.pos[0], c.flags["--id"], c.flags["--red-proof"], bindingWriteInput{CoversSpec: covers, CoversSet: coversSet}, c.stdout)
	}},
	"amend": {2, map[string]bool{"--repo": true, "--covers": true, "--execution": true}, func(c commandInvocation) error {
		covers, coversSet := c.flags["--covers"]
		execution, executionSet := c.flags["--execution"]
		return cmdAmend(c.repo, c.pos[0], c.pos[1], bindingWriteInput{CoversSpec: covers, CoversSet: coversSet, ExecutionSpec: execution, ExecutionSet: executionSet}, c.stdout)
	}},
	"arm": {1, map[string]bool{"--repo": true, "--check": true, "--red-proof": true, "--proof-kind": true, "--proof-ref": true, "--covers": true, "--execution": true}, func(c commandInvocation) error {
		covers, coversSet := c.flags["--covers"]
		execution, executionSet := c.flags["--execution"]
		return cmdArm(c.repo, c.pos[0], c.flags["--check"], proofInput{
			Text: c.flags["--red-proof"], Kind: c.flags["--proof-kind"], Reference: c.flags["--proof-ref"],
		}, bindingWriteInput{CoversSpec: covers, CoversSet: coversSet, ExecutionSpec: execution, ExecutionSet: executionSet}, c.stdout)
	}},
	"prove": {1, map[string]bool{"--repo": true, "--red-proof": true, "--proof-kind": true, "--proof-ref": true, "--replace": true, "--supersede": true, "--force": true, "--run": true, "--profile": true, "--tags": true}, func(c commandInvocation) error {
		return cmdProve(c.repo, c.pos[0], proofInput{
			Text: c.flags["--red-proof"], Kind: c.flags["--proof-kind"], Reference: c.flags["--proof-ref"],
		}, proveOptions{
			Replace: c.flags["--replace"] == "true", Supersede: c.flags["--supersede"] == "true", Force: c.flags["--force"] == "true",
			Run: c.flags["--run"] == "true", Profile: c.flags["--profile"], Tags: c.flags["--tags"],
		}, c.stdout)
	}},
	"rehash": {1, map[string]bool{"--repo": true}, func(c commandInvocation) error {
		return cmdRehash(c.repo, c.pos[0], c.stdout)
	}},
	"diff": {1, map[string]bool{"--repo": true}, func(c commandInvocation) error {
		return cmdDiff(c.repo, c.pos[0], c.stdout)
	}},
	"ledger-diff": {0, map[string]bool{"--repo": true, "--base": true, "--json": true}, func(c commandInvocation) error {
		return cmdLedgerDiff(c.repo, c.flags["--base"], c.flags["--json"] == "true", c.stdout)
	}},
	"repair-fixture-row": {1, map[string]bool{"--repo": true}, func(c commandInvocation) error {
		return cmdRepairFixtureRow(c.repo, c.pos[0], c.stdout)
	}},
	"covers": {0, map[string]bool{"--repo": true, "--json": true}, func(c commandInvocation) error {
		return cmdCovers(c.repo, c.flags["--json"] == "true", c.stdout)
	}},
	"check": {0, map[string]bool{"--repo": true, "--report": true, "--all": true, "--run-profile": true, "--tags": true, "--only": true, "--timings": true}, func(c commandInvocation) error {
		if c.flags["--tags"] != "" && c.flags["--run-profile"] == "" {
			return fatalf("check: --tags requires --run-profile")
		}
		return cmdCheck(c.repo, checkOptions{
			ReportPath: c.flags["--report"], AllSpec: c.flags["--all"], RunProfile: c.flags["--run-profile"], Tags: c.flags["--tags"], OnlyID: c.flags["--only"], Timings: c.flags["--timings"] == "true",
		}, c.stdout)
	}},
}

func publicCommandNames() []string {
	names := slices.Sorted(maps.Keys(commandSpecs))
	names = append(names, "capabilities", "help", "validate", "version")
	slices.Sort(names)
	return names
}

// parseArgs splits args into flag values and positionals. Flags may appear
// before or after positionals; all values are --flag VALUE or --flag=VALUE.
func parseArgs(args []string) (map[string]string, []string, error) {
	flags := map[string]string{}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		name, val, hasEq := strings.Cut(a, "=")
		takes, known := flagTakesValue[name]
		if !known {
			return nil, nil, fatalf("unknown flag %s\n\n%s", name, usageText)
		}
		switch {
		case takes && hasEq:
			flags[name] = val
		case takes:
			i++
			if i >= len(args) {
				return nil, nil, fatalf("flag %s needs a value", name)
			}
			flags[name] = args[i]
		case hasEq:
			return nil, nil, fatalf("flag %s does not take a value", name)
		default:
			flags[name] = "true"
		}
	}
	return flags, pos, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	err := dispatch(args, stdout)
	if err == nil {
		return 0
	}
	var silent silentExit
	if errors.As(err, &silent) {
		return silent.code
	}
	code := 2
	var ee exitErr
	if errors.As(err, &ee) {
		code = ee.code
	}
	fmt.Fprintln(stderr, "rulefloor: "+err.Error())
	return code
}

func dispatch(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fatalf("missing command\n\n%s", usageText)
	}
	cmd := args[0]
	if cmd == "help" {
		return dispatchHelp(args[1:], stdout)
	}
	if cmd == "--help" || cmd == "-h" {
		fmt.Fprint(stdout, usageText)
		return nil
	}
	if cmd == "version" || cmd == "--version" {
		return dispatchVersion(args[1:], stdout)
	}
	if cmd == "capabilities" {
		return dispatchCapabilities(args[1:], stdout)
	}
	if cmd == "validate" {
		if len(args) == 2 && args[1] == "--help" {
			return printCommandHelp(cmd, stdout)
		}
		return dispatchValidate(args[1:], stdout)
	}
	spec, ok := commandSpecs[cmd]
	if !ok {
		return fatalf("unknown command %q\n\n%s", cmd, usageText)
	}
	if len(args) == 2 && args[1] == "--help" {
		return printCommandHelp(cmd, stdout)
	}
	if strings.HasPrefix(cmd, "ops-") {
		seen := map[string]bool{}
		for i := 1; i < len(args); i++ {
			name, _, hasEq := strings.Cut(args[i], "=")
			if !strings.HasPrefix(name, "--") {
				continue
			}
			if seen[name] {
				return fatalf("duplicate operation flag %s", name)
			}
			seen[name] = true
			if flagTakesValue[name] && !hasEq {
				i++
			}
		}
	}
	flags, pos, err := parseArgs(args[1:])
	if err != nil {
		return err
	}
	for name := range flags {
		if !spec.flags[name] {
			return fatalf("flag %s is not valid for %s", name, cmd)
		}
	}
	repo := flags["--repo"]
	if repo == "" {
		repo = "."
	}
	if len(pos) != spec.positionals {
		return fatalf("%s: wrong arguments\n\n%s", cmd, usageText)
	}
	return spec.run(commandInvocation{repo: repo, flags: flags, pos: pos, stdout: stdout})
}

func dispatchVersion(args []string, stdout io.Writer) error {
	if len(args) == 1 && args[0] == "--json" {
		if err := writeJSON(stdout, runtimeVersionResult()); err != nil {
			return fatalf("write version JSON: %v", err)
		}
		return nil
	}
	if len(args) != 0 {
		return fatalf("version: wrong arguments\n\n%s", usageText)
	}
	fmt.Fprintf(stdout, "rulefloor %s\n", version)
	return nil
}

func dispatchHelp(args []string, stdout io.Writer) error {
	switch len(args) {
	case 0:
		fmt.Fprint(stdout, usageText)
		return nil
	case 1:
		return printCommandHelp(args[0], stdout)
	default:
		return fatalf("help: wrong arguments\n\n%s", usageText)
	}
}

func printCommandHelp(command string, stdout io.Writer) error {
	help, ok := commandHelp[command]
	if !ok {
		return fatalf("no detailed help for command %q\n\n%s", command, usageText)
	}
	fmt.Fprint(stdout, help)
	return nil
}
