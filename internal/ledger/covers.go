package ledger

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	CoversSchemaVersion = "rulefloor.covers.v1"
	coversTokenMarker   = " <!-- rulefloor-covers-v1:"
	coversTokenEnd      = " -->"
	maxCoveredSymbols   = 128
	maxSymbolBytes      = 512
)

// ParseCoveredSymbols parses the comma-separated CLI representation and
// returns the canonical sorted symbol set. An empty value represents no
// covered symbols; callers decide whether that is meaningful for a command.
func ParseCoveredSymbols(value string) ([]string, error) {
	if value == "" {
		return []string{}, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) > maxCoveredSymbols {
		return nil, fmt.Errorf("covered-symbol list has %d entries; maximum is %d", len(parts), maxCoveredSymbols)
	}
	symbols := make([]string, 0, len(parts))
	for _, part := range parts {
		symbol := strings.TrimSpace(part)
		if err := validateCoveredSymbol(symbol); err != nil {
			return nil, err
		}
		symbols = append(symbols, symbol)
	}
	slices.Sort(symbols)
	for i := 1; i < len(symbols); i++ {
		if symbols[i] == symbols[i-1] {
			return nil, fmt.Errorf("covered symbol %q is duplicated", symbols[i])
		}
	}
	return symbols, nil
}

// CoveredSymbolsByRule returns every rule ID, including rules with an empty
// symbol set. The returned slices are detached from the ledger model.
func (l *Ledger) CoveredSymbolsByRule() map[string][]string {
	rules := make(map[string][]string, len(l.Rows))
	for _, row := range l.Rows {
		rules[row.ID] = append([]string{}, row.CoveredSymbols...)
	}
	return rules
}

func validateCoveredSymbol(symbol string) error {
	if symbol == "" {
		return fmt.Errorf("covered symbol must not be empty")
	}
	if len(symbol) > maxSymbolBytes {
		return fmt.Errorf("covered symbol %q exceeds %d bytes", symbol, maxSymbolBytes)
	}
	if !utf8.ValidString(symbol) {
		return fmt.Errorf("covered symbol is not valid UTF-8")
	}
	for _, r := range symbol {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("covered symbol %q must not contain whitespace or control characters", symbol)
		}
	}
	return nil
}

func validateCanonicalCoveredSymbols(symbols []string) error {
	if len(symbols) > maxCoveredSymbols {
		return fmt.Errorf("covered-symbol list has %d entries; maximum is %d", len(symbols), maxCoveredSymbols)
	}
	if !slices.IsSorted(symbols) {
		return fmt.Errorf("covered symbols must be in canonical sorted order")
	}
	for i, symbol := range symbols {
		if err := validateCoveredSymbol(symbol); err != nil {
			return err
		}
		if i > 0 && symbol == symbols[i-1] {
			return fmt.Errorf("covered symbol %q is duplicated", symbol)
		}
	}
	return nil
}

func joinProofAndCoveredSymbols(proof string, symbols []string) string {
	if len(symbols) == 0 {
		return proof
	}
	payload, _ := json.Marshal(symbols)
	return proof + coversTokenMarker + base64.RawURLEncoding.EncodeToString(payload) + coversTokenEnd
}

func splitProofAndCoveredSymbols(cell string) (string, []string, error) {
	marker := strings.LastIndex(cell, coversTokenMarker)
	if marker < 0 {
		return cell, nil, nil
	}
	if strings.Count(cell, coversTokenMarker) != 1 || !strings.HasSuffix(cell, coversTokenEnd) {
		return "", nil, fmt.Errorf("malformed rulefloor-covers-v1 metadata")
	}
	proof := cell[:marker]
	if proof == "" {
		return "", nil, fmt.Errorf("rulefloor-covers-v1 metadata requires a red-proof value or '-' placeholder")
	}
	encoded := strings.TrimSuffix(cell[marker+len(coversTokenMarker):], coversTokenEnd)
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, fmt.Errorf("malformed rulefloor-covers-v1 payload")
	}
	if base64.RawURLEncoding.EncodeToString(payload) != encoded {
		return "", nil, fmt.Errorf("rulefloor-covers-v1 payload encoding is not canonical")
	}
	var symbols []string
	if err := json.Unmarshal(payload, &symbols); err != nil || symbols == nil {
		return "", nil, fmt.Errorf("malformed rulefloor-covers-v1 symbol list")
	}
	if len(symbols) == 0 {
		return "", nil, fmt.Errorf("rulefloor-covers-v1 symbol list must not be empty")
	}
	if err := validateCanonicalCoveredSymbols(symbols); err != nil {
		return "", nil, fmt.Errorf("invalid rulefloor-covers-v1 metadata: %v", err)
	}
	canonical, _ := json.Marshal(symbols)
	if !slices.Equal(payload, canonical) {
		return "", nil, fmt.Errorf("rulefloor-covers-v1 payload is not canonical")
	}
	return proof, symbols, nil
}
