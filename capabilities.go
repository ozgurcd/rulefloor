package main

import (
	"fmt"
	"io"
	"strings"

	binarycap "github.com/ozgurcd/rulefloor/internal/capabilities"
)

func dispatchCapabilities(args []string, stdout io.Writer) error {
	flags, positionals, err := parseArgs(args)
	if err != nil {
		return err
	}
	for name := range flags {
		if name != "--json" {
			return fatalf("flag %s is not valid for capabilities", name)
		}
	}
	if len(positionals) != 0 {
		return fatalf("capabilities: wrong arguments\n\n%s", usageText)
	}
	return cmdCapabilities(flags["--json"] == "true", stdout)
}

func cmdCapabilities(jsonOutput bool, stdout io.Writer) error {
	result := binarycap.New(version, publicCommandNames())
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			return fatalf("write capabilities JSON: %v", err)
		}
		return nil
	}

	fmt.Fprintf(stdout, "rulefloor %s capabilities\n", result.RulefloorVersion)
	fmt.Fprintf(stdout, "machine schemas: %s\n", strings.Join(result.MachineInterfaces, ", "))
	kinds := make([]string, 0, len(result.TestKinds))
	for _, kind := range result.TestKinds {
		support := "static"
		if kind.Execution {
			support += ", execute"
		}
		kinds = append(kinds, fmt.Sprintf("%s (%s)", kind.Kind, support))
	}
	fmt.Fprintf(stdout, "test kinds: %s\n", strings.Join(kinds, "; "))
	fmt.Fprintf(stdout, "validation modes: %s\n", strings.Join(result.ValidationModes, ", "))
	fmt.Fprintf(stdout, "proof kinds: %s\n", strings.Join(result.ProofKinds, ", "))
	fmt.Fprintf(stdout, "ledger features: %s\n", strings.Join(result.LedgerFeatures, ", "))
	fmt.Fprintf(stdout, "structural reach: %s exact stable IDs for %s (possible is insufficient)\n", result.StructuralReach.Provider, strings.Join(result.StructuralReach.TestKinds, ", "))
	fmt.Fprintf(stdout, "commands: %s\n", strings.Join(result.Commands, ", "))
	fmt.Fprintln(stdout, "execution: persisted policy; legacy profiles compatible; static never executes; execute never falls back to static")
	return nil
}
