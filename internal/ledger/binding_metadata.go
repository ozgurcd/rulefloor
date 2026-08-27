package ledger

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ozgurcd/rulefloor/internal/model"
)

const (
	bindingTokenMarker = " <!-- rulefloor-binding-v1:"
	bindingTokenEnd    = " -->"
	maxStableIDBytes   = 2048
)

type bindingMetadataV1 struct {
	ExecutionPolicy    model.ExecutionPolicy    `json:"execution_policy,omitempty"`
	ReachabilityPolicy model.ReachabilityPolicy `json:"reachability_policy,omitempty"`
	TestSymbol         string                   `json:"test_symbol,omitempty"`
}

func joinProofAndBindingMetadata(proof string, row Row) string {
	metadata := bindingMetadataV1{
		ExecutionPolicy: row.ExecutionPolicy, ReachabilityPolicy: row.ReachabilityPolicy,
		TestSymbol: row.TestSymbol,
	}
	if metadata == (bindingMetadataV1{}) {
		return proof
	}
	payload, _ := json.Marshal(metadata)
	return proof + bindingTokenMarker + base64.RawURLEncoding.EncodeToString(payload) + bindingTokenEnd
}

func splitProofAndBindingMetadata(cell string) (string, bindingMetadataV1, error) {
	marker := strings.LastIndex(cell, bindingTokenMarker)
	if marker < 0 {
		return cell, bindingMetadataV1{}, nil
	}
	if strings.Count(cell, bindingTokenMarker) != 1 || !strings.HasSuffix(cell, bindingTokenEnd) {
		return "", bindingMetadataV1{}, fmt.Errorf("malformed rulefloor-binding-v1 metadata")
	}
	proof := cell[:marker]
	if proof == "" {
		return "", bindingMetadataV1{}, fmt.Errorf("rulefloor-binding-v1 metadata requires a red-proof value or '-' placeholder")
	}
	encoded := strings.TrimSuffix(cell[marker+len(bindingTokenMarker):], bindingTokenEnd)
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != encoded {
		return "", bindingMetadataV1{}, fmt.Errorf("malformed or noncanonical rulefloor-binding-v1 payload")
	}
	var metadata bindingMetadataV1
	if err := json.Unmarshal(payload, &metadata); err != nil || metadata == (bindingMetadataV1{}) {
		return "", bindingMetadataV1{}, fmt.Errorf("malformed or empty rulefloor-binding-v1 document")
	}
	canonical, _ := json.Marshal(metadata)
	if !slices.Equal(payload, canonical) {
		return "", bindingMetadataV1{}, fmt.Errorf("rulefloor-binding-v1 payload is not canonical")
	}
	return proof, metadata, nil
}

func applyAndValidateBindingMetadata(row *Row, metadata bindingMetadataV1) error {
	row.ExecutionPolicy = metadata.ExecutionPolicy
	row.ReachabilityPolicy = metadata.ReachabilityPolicy
	row.TestSymbol = metadata.TestSymbol
	if row.ExecutionPolicy != "" && row.ExecutionPolicy != model.ExecutionStatic && row.ExecutionPolicy != model.ExecutionExecute {
		return fmt.Errorf("unsupported execution policy %q", row.ExecutionPolicy)
	}
	if row.Armed() && metadata != (bindingMetadataV1{}) && row.ExecutionPolicy == "" {
		return fmt.Errorf("armed binding-v1 row requires execution_policy")
	}
	if !row.Armed() && row.ExecutionPolicy != "" {
		return fmt.Errorf("declared row must not persist execution_policy before it is armed")
	}
	if row.ReachabilityPolicy != "" && row.ReachabilityPolicy != model.ReachabilityExact {
		return fmt.Errorf("unsupported reachability policy %q", row.ReachabilityPolicy)
	}
	if row.ReachabilityPolicy == model.ReachabilityExact {
		if len(row.CoveredSymbols) == 0 {
			return fmt.Errorf("exact reachability requires protected symbols")
		}
		for _, symbol := range row.CoveredSymbols {
			if err := validateStableID(symbol); err != nil {
				return fmt.Errorf("invalid protected symbol: %v", err)
			}
		}
		if row.Armed() && row.TestSymbol == "" {
			return fmt.Errorf("armed exact-reach row requires test_symbol")
		}
	} else if row.TestSymbol != "" {
		return fmt.Errorf("test_symbol requires exact reachability")
	}
	if row.TestSymbol != "" {
		if err := validateStableID(row.TestSymbol); err != nil {
			return fmt.Errorf("invalid test_symbol: %v", err)
		}
	}
	return nil
}

func validateStableID(stableID string) error {
	if stableID == "" || len(stableID) > maxStableIDBytes || !utf8.ValidString(stableID) {
		return fmt.Errorf("stable symbol identity is empty, oversized, or invalid UTF-8")
	}
	for _, r := range stableID {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("stable symbol identity %q contains whitespace or control characters", stableID)
		}
	}
	return nil
}
