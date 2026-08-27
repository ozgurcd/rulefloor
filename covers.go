package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/ozgurcd/rulefloor/internal/ledger"
)

const coversSchemaVersion = ledger.CoversSchemaVersion

type coversResult struct {
	SchemaVersion string              `json:"schema_version"`
	Rules         map[string][]string `json:"rules"`
}

func cmdCovers(repo string, jsonOutput bool, stdout io.Writer) error {
	model, err := loadLedger(repo)
	if err != nil {
		return err
	}
	result := coversResult{
		SchemaVersion: coversSchemaVersion,
		Rules:         ledger.CoveredSymbolsByRule((*ledger.Ledger)(model)),
	}
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			return fatalf("write covers JSON: %v", err)
		}
		return nil
	}
	for _, row := range model.Rows {
		symbols := strings.Join(row.CoveredSymbols, ", ")
		if symbols == "" {
			symbols = "-"
		}
		fmt.Fprintf(stdout, "%s: %s\n", row.ID, symbols)
	}
	return nil
}
