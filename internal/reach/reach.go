package reach

import "context"

type Resolution string

const (
	ResolutionExact    Resolution = "exact"
	ResolutionPossible Resolution = "possible"
	ResolutionMissing  Resolution = "missing"
)

type ErrorKind string

const (
	ErrorUnavailable  ErrorKind = "graph_evidence_unavailable"
	ErrorInsufficient ErrorKind = "graph_evidence_insufficient"
	ErrorAmbiguous    ErrorKind = "symbol_identity_ambiguous"
)

type EvidenceError struct {
	Kind    ErrorKind
	Message string
}

func (e *EvidenceError) Error() string { return e.Message }

type Symbol struct {
	StableID   string
	Resolution Resolution
	Detail     string
}

type Result struct {
	TestSymbol string
	Symbols    []Symbol
}

type Request struct {
	Repository       string
	TestSymbol       string
	ProtectedSymbols []string
}

type Verifier interface {
	Verify(context.Context, Request) (Result, error)
}

type Resolver interface {
	Resolve(context.Context, string, []string) ([]string, error)
	ResolveTest(context.Context, string, string) (string, error)
}
