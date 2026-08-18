// Command rulefloor maintains RULE-FLOOR.md, a machine-checked rule ledger.
// It is the only writer of that file; check verifies the ledger against the
// tagged tests it points at and never lowers FLOOR.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// version is stamped at release time via -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usageText = `rulefloor — machine-checked rule ledger (RULE-FLOOR.md)

Usage:
  rulefloor init                              [--repo PATH]
  rulefloor list                              [--repo PATH]
  rulefloor show ID                           [--repo PATH]
  rulefloor unarmed                           [--repo PATH]
  rulefloor unproved                          [--repo PATH]
  rulefloor redproofs                         [--adopt] [--repo PATH]
  rulefloor declare "sentence" --id ID        [--red-proof TEXT] [--repo PATH]
  rulefloor arm ID --check "file @ profile"   --red-proof TEXT [--repo PATH]
  rulefloor rehash ID                         [--repo PATH]
  rulefloor check                             [--repo PATH] [--report pw.json] [--all "repo1,repo2"]
                                              [--run-profile NAME [--tags T]]
  rulefloor version                           (also: rulefloor --version)

Exit codes: 0 ok, 1 check failure or refusal, 2 fatal
(malformed ledger, missing field, CANNOT-EVALUATE, usage error).
`

// exitErr carries the process exit code alongside the message.
type exitErr struct {
	code int
	msg  string
}

func (e exitErr) Error() string { return e.msg }

func failf(format string, a ...any) error  { return exitErr{1, fmt.Sprintf(format, a...)} }
func fatalf(format string, a ...any) error { return exitErr{2, fmt.Sprintf(format, a...)} }

// flagTakesValue lists every flag; false marks booleans.
var flagTakesValue = map[string]bool{
	"--repo":        true,
	"--id":          true,
	"--check":       true,
	"--report":      true,
	"--red-proof":   true,
	"--all":         true,
	"--run-profile": true,
	"--tags":        true,
	"--adopt":       false,
}

var allowedFlags = map[string]map[string]bool{
	"init":      {"--repo": true},
	"list":      {"--repo": true},
	"show":      {"--repo": true},
	"unarmed":   {"--repo": true},
	"unproved":  {"--repo": true},
	"redproofs": {"--repo": true, "--adopt": true},
	"declare":   {"--repo": true, "--id": true, "--red-proof": true},
	"arm":       {"--repo": true, "--check": true, "--red-proof": true},
	"rehash":    {"--repo": true},
	"check":     {"--repo": true, "--report": true, "--all": true, "--run-profile": true, "--tags": true},
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
	if cmd == "help" || cmd == "--help" || cmd == "-h" {
		fmt.Fprint(stdout, usageText)
		return nil
	}
	if cmd == "version" || cmd == "--version" {
		fmt.Fprintf(stdout, "rulefloor %s\n", version)
		return nil
	}
	allowed, ok := allowedFlags[cmd]
	if !ok {
		return fatalf("unknown command %q\n\n%s", cmd, usageText)
	}
	flags, pos, err := parseArgs(args[1:])
	if err != nil {
		return err
	}
	for name := range flags {
		if !allowed[name] {
			return fatalf("flag %s is not valid for %s", name, cmd)
		}
	}
	repo := flags["--repo"]
	if repo == "" {
		repo = "."
	}
	want := func(n int) error {
		if len(pos) != n {
			return fatalf("%s: wrong arguments\n\n%s", cmd, usageText)
		}
		return nil
	}
	switch cmd {
	case "init":
		if err := want(0); err != nil {
			return err
		}
		return cmdInit(repo, stdout)
	case "list":
		if err := want(0); err != nil {
			return err
		}
		return cmdList(repo, stdout)
	case "show":
		if err := want(1); err != nil {
			return err
		}
		return cmdShow(repo, pos[0], stdout)
	case "unarmed":
		if err := want(0); err != nil {
			return err
		}
		return cmdUnarmed(repo, stdout)
	case "unproved":
		if err := want(0); err != nil {
			return err
		}
		return cmdUnproved(repo, stdout)
	case "redproofs":
		if err := want(0); err != nil {
			return err
		}
		return cmdRedProofs(repo, flags["--adopt"] == "true", stdout)
	case "declare":
		if err := want(1); err != nil {
			return err
		}
		return cmdDeclare(repo, pos[0], flags["--id"], flags["--red-proof"], stdout)
	case "arm":
		if err := want(1); err != nil {
			return err
		}
		return cmdArm(repo, pos[0], flags["--check"], flags["--red-proof"], stdout)
	case "rehash":
		if err := want(1); err != nil {
			return err
		}
		return cmdRehash(repo, pos[0], stdout)
	case "check":
		if err := want(0); err != nil {
			return err
		}
		if flags["--tags"] != "" && flags["--run-profile"] == "" {
			return fatalf("check: --tags requires --run-profile")
		}
		return cmdCheck(repo, flags["--report"], flags["--all"], flags["--run-profile"], flags["--tags"], stdout)
	}
	return fatalf("unknown command %q\n\n%s", cmd, usageText)
}
