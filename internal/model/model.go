package model

import (
	"fmt"
	"path/filepath"
	"strings"
)

type ExecutionPolicy string

const (
	ExecutionStatic  ExecutionPolicy = "static"
	ExecutionExecute ExecutionPolicy = "execute"
)

type ReachabilityPolicy string

const ReachabilityExact ReachabilityPolicy = "exact"

// Row is Rulefloor's storage-neutral persisted rule model. Markdown-specific
// encoding belongs to internal/ledger; checking code consumes this model.
type Row struct {
	ID                 string
	Rule               string
	EnforcedBy         string
	Check              string
	RedProof           string
	Hash               string
	CoveredSymbols     []string
	ExecutionPolicy    ExecutionPolicy
	ReachabilityPolicy ReachabilityPolicy
	TestSymbol         string
}

func (r *Row) Armed() bool { return r.Check != "NONE" }

type Ledger struct {
	Floor            int
	Rows             []Row
	RedProofs        int
	HasRedProofs     bool
	RepairedFixtures []string
}

func (l *Ledger) Find(id string) *Row {
	for i := range l.Rows {
		if l.Rows[i].ID == id {
			return &l.Rows[i]
		}
	}
	return nil
}

func (l *Ledger) IsRepairedFixture(id string) bool {
	for _, repaired := range l.RepairedFixtures {
		if repaired == id {
			return true
		}
	}
	return false
}

func (l *Ledger) EffectiveCount() int { return len(l.Rows) + len(l.RepairedFixtures) }

func (l *Ledger) RaiseFloor(n int) {
	if n > l.Floor {
		l.Floor = n
	}
}

func (l *Ledger) MeasuredRedProofs() int {
	n := 0
	for _, row := range l.Rows {
		if row.Armed() && row.RedProof != "-" {
			n++
		}
	}
	return n
}

func (l *Ledger) MaintainRedProofs() {
	measured := l.MeasuredRedProofs()
	if !l.HasRedProofs {
		l.RedProofs = measured
		l.HasRedProofs = true
		return
	}
	if measured > l.RedProofs {
		l.RedProofs = measured
	}
}

type Binding struct {
	File      string
	Kind      string
	Profile   string
	Execution ExecutionPolicy
	Legacy    bool
}

// InterpretBinding reads explicit persisted execution policy when present.
// Rows written before binding-v1 retain their historical compatibility rule.
func InterpretBinding(row *Row) (Binding, error) {
	file, profile, err := SplitCheck(row.Check)
	if err != nil {
		return Binding{}, err
	}
	execution := row.ExecutionPolicy
	legacy := execution == ""
	if legacy {
		execution = ExecutionStatic
		if row.EnforcedBy == "go-test" && profile == "unit" {
			execution = ExecutionExecute
		}
	}
	if execution != ExecutionStatic && execution != ExecutionExecute {
		return Binding{}, fmt.Errorf("unsupported execution policy %q", execution)
	}
	return Binding{
		File: file, Kind: row.EnforcedBy, Profile: profile,
		Execution: execution, Legacy: legacy,
	}, nil
}

func SplitCheck(spec string) (file, profile string, err error) {
	if strings.Count(spec, " @ ") != 1 {
		return "", "", fmt.Errorf("check %q must be \"<file> @ <profile>\"", spec)
	}
	file, profile, _ = strings.Cut(spec, " @ ")
	file = strings.TrimSpace(file)
	profile = strings.TrimSpace(profile)
	if file == "" || profile == "" {
		return "", "", fmt.Errorf("check %q must be \"<file> @ <profile>\"", spec)
	}
	if !filepath.IsLocal(file) {
		return "", "", fmt.Errorf("check file %q must be a relative path inside the repo", file)
	}
	return file, profile, nil
}
