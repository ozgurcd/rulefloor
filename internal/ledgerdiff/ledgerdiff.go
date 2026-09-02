package ledgerdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/ozgurcd/rulefloor/internal/ledger"
	"github.com/ozgurcd/rulefloor/internal/repository"
)

const (
	SchemaVersion           = "rulefloor.ledger-diff.v1"
	maxGitOutput            = 4 << 20
	maxRuleChanges          = 1000
	maxSentenceExcerptRunes = 256
)

type ChangeKind string

type HeaderChangeKind string

const (
	ChangeRuleAdded               ChangeKind       = "rule_added"
	ChangeRuleRemoved             ChangeKind       = "rule_removed"
	ChangeSentence                ChangeKind       = "sentence_changed"
	ChangeBinding                 ChangeKind       = "binding_changed"
	ChangeProof                   ChangeKind       = "proof_changed"
	ChangeCoveredSymbols          ChangeKind       = "covered_symbols_changed"
	ChangeTestFingerprint         ChangeKind       = "test_fingerprint_changed"
	HeaderFloorChanged            HeaderChangeKind = "floor_changed"
	HeaderRedProofsChanged        HeaderChangeKind = "red_proofs_changed"
	HeaderRepairedFixturesChanged HeaderChangeKind = "repaired_fixtures_changed"
)

type HeaderState struct {
	Floor                int  `json:"floor"`
	RedProofs            *int `json:"red_proofs"`
	RepairedFixtureCount int  `json:"repaired_fixture_count"`
}

type RuleChange struct {
	RuleID                string       `json:"rule_id"`
	Changes               []ChangeKind `json:"changes"`
	BeforeSentenceExcerpt string       `json:"before_sentence_excerpt,omitempty"`
	AfterSentenceExcerpt  string       `json:"after_sentence_excerpt,omitempty"`
}

type Comparison struct {
	BaseRef          string             `json:"base_ref"`
	BaseCommit       string             `json:"base_commit"`
	Before           HeaderState        `json:"before"`
	After            HeaderState        `json:"after"`
	HeadersChanged   bool               `json:"headers_changed"`
	HeaderChanges    []HeaderChangeKind `json:"header_changes"`
	Rules            []RuleChange       `json:"rules"`
	TotalRuleChanges int                `json:"total_rule_changes"`
	Truncated        bool               `json:"truncated"`
}

func (c Comparison) Different() bool {
	return c.HeadersChanged || c.TotalRuleChanges > 0
}

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, []byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, directory string, args ...string) ([]byte, []byte, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return nil, nil, fmt.Errorf("locate git: %w", err)
	}
	stdout := &boundedBuffer{limit: maxGitOutput}
	stderr := &boundedBuffer{limit: 16 << 10}
	command := exec.CommandContext(ctx, path, args...)
	command.Dir = directory
	command.Stdout = stdout
	command.Stderr = stderr
	err = command.Run()
	if stdout.overflow || stderr.overflow {
		return stdout.Bytes(), stderr.Bytes(), errors.New("git output exceeded the configured limit")
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func Compare(ctx context.Context, runner Runner, repo, baseRef string) (Comparison, error) {
	if ctx == nil {
		return Comparison{}, errors.New("context is nil")
	}
	if runner == nil {
		return Comparison{}, errors.New("git runner is nil")
	}
	if err := validateBaseRef(baseRef); err != nil {
		return Comparison{}, err
	}
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return Comparison{}, err
	}
	current, err := ledger.Load(root)
	if err != nil {
		return Comparison{}, fmt.Errorf("read current ledger: %w", err)
	}
	commit, err := resolveCommit(ctx, runner, root, baseRef)
	if err != nil {
		return Comparison{}, err
	}
	baselineData, stderr, err := runner.Run(ctx, root, "show", "--no-textconv", commit+":./RULE-FLOOR.md")
	if err != nil {
		return Comparison{}, fmt.Errorf("read RULE-FLOOR.md at %s: %s", baseRef, commandDiagnostic(stderr, err))
	}
	baseline, err := ledger.Parse(string(baselineData))
	if err != nil {
		return Comparison{}, fmt.Errorf("parse RULE-FLOOR.md at %s: %w", baseRef, err)
	}
	return CompareLedgers(baseRef, commit, baseline, current), nil
}

func CompareLedgers(baseRef, baseCommit string, before, after *ledger.Ledger) Comparison {
	result := Comparison{
		BaseRef: baseRef, BaseCommit: baseCommit,
		Before: headerState(before), After: headerState(after),
		Rules: []RuleChange{},
	}
	result.HeaderChanges = compareHeaders(before, after)
	result.HeadersChanged = len(result.HeaderChanges) > 0

	beforeRows := rowsByID(before)
	afterRows := rowsByID(after)
	ids := make([]string, 0, len(beforeRows)+len(afterRows))
	for id := range beforeRows {
		ids = append(ids, id)
	}
	for id := range afterRows {
		if _, exists := beforeRows[id]; !exists {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		left, hadLeft := beforeRows[id]
		right, hadRight := afterRows[id]
		change := compareRows(id, left, hadLeft, right, hadRight)
		if len(change.Changes) > 0 {
			result.TotalRuleChanges++
			if len(result.Rules) < maxRuleChanges {
				result.Rules = append(result.Rules, change)
			}
		}
	}
	result.Truncated = result.TotalRuleChanges > len(result.Rules)
	return result
}

func compareRows(id string, before ledger.Row, hadBefore bool, after ledger.Row, hadAfter bool) RuleChange {
	change := RuleChange{RuleID: id, Changes: []ChangeKind{}}
	switch {
	case !hadBefore:
		change.Changes = append(change.Changes, ChangeRuleAdded)
		change.AfterSentenceExcerpt = sentenceExcerpt(after.Rule)
		return change
	case !hadAfter:
		change.Changes = append(change.Changes, ChangeRuleRemoved)
		change.BeforeSentenceExcerpt = sentenceExcerpt(before.Rule)
		return change
	}
	if before.Rule != after.Rule {
		change.Changes = append(change.Changes, ChangeSentence)
		change.BeforeSentenceExcerpt = sentenceExcerpt(before.Rule)
		change.AfterSentenceExcerpt = sentenceExcerpt(after.Rule)
	}
	if before.EnforcedBy != after.EnforcedBy || before.Check != after.Check ||
		before.ExecutionPolicy != after.ExecutionPolicy || before.ReachabilityPolicy != after.ReachabilityPolicy ||
		before.TestSymbol != after.TestSymbol {
		change.Changes = append(change.Changes, ChangeBinding)
	}
	if before.RedProof != after.RedProof {
		change.Changes = append(change.Changes, ChangeProof)
	}
	if !slices.Equal(before.CoveredSymbols, after.CoveredSymbols) {
		change.Changes = append(change.Changes, ChangeCoveredSymbols)
	}
	if before.Hash != after.Hash {
		change.Changes = append(change.Changes, ChangeTestFingerprint)
	}
	return change
}

func headerState(model *ledger.Ledger) HeaderState {
	state := HeaderState{
		Floor:                model.Floor,
		RepairedFixtureCount: len(model.RepairedFixtures),
	}
	if model.HasRedProofs {
		value := model.RedProofs
		state.RedProofs = &value
	}
	return state
}

func compareHeaders(left, right *ledger.Ledger) []HeaderChangeKind {
	changes := []HeaderChangeKind{}
	if left.Floor != right.Floor {
		changes = append(changes, HeaderFloorChanged)
	}
	if left.HasRedProofs != right.HasRedProofs || left.HasRedProofs && left.RedProofs != right.RedProofs {
		changes = append(changes, HeaderRedProofsChanged)
	}
	if !slices.Equal(left.RepairedFixtures, right.RepairedFixtures) {
		changes = append(changes, HeaderRepairedFixturesChanged)
	}
	return changes
}

func sentenceExcerpt(sentence string) string {
	runes := []rune(sentence)
	if len(runes) <= maxSentenceExcerptRunes {
		return sentence
	}
	return string(runes[:maxSentenceExcerptRunes]) + "…"
}

func rowsByID(model *ledger.Ledger) map[string]ledger.Row {
	rows := make(map[string]ledger.Row, len(model.Rows))
	for _, row := range model.Rows {
		rows[row.ID] = row
	}
	return rows
}

var commitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func resolveCommit(ctx context.Context, runner Runner, root, baseRef string) (string, error) {
	stdout, stderr, err := runner.Run(ctx, root, "rev-parse", "--verify", "--end-of-options", baseRef+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve Git base %q: %s", baseRef, commandDiagnostic(stderr, err))
	}
	commit := strings.TrimSpace(string(stdout))
	if !commitPattern.MatchString(commit) {
		return "", fmt.Errorf("resolve Git base %q: Git returned an invalid commit identifier", baseRef)
	}
	return commit, nil
}

func validateBaseRef(baseRef string) error {
	if baseRef == "" {
		return errors.New("--base must not be empty")
	}
	if len(baseRef) > 256 {
		return errors.New("--base exceeds 256 bytes")
	}
	for _, r := range baseRef {
		if r < 0x20 || r == 0x7f {
			return errors.New("--base must not contain control characters")
		}
	}
	return nil
}

func commandDiagnostic(stderr []byte, err error) string {
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		message = err.Error()
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, message)
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.overflow = true
		return written, nil
	}
	if len(data) > remaining {
		b.overflow = true
		data = data[:remaining]
	}
	_, _ = b.Buffer.Write(data)
	return written, nil
}
