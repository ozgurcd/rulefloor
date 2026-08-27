package gograph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"

	"github.com/ozgurcd/rulefloor/internal/reach"
	"github.com/ozgurcd/rulefloor/internal/repository"
)

const (
	maxStdoutBytes = 8 << 20
	maxStderrBytes = 64 << 10
	intention      = "verify Rulefloor protected-symbol binding"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, []byte, error)
}

type Client struct {
	runner Runner
}

func New(binary string) *Client {
	if binary == "" {
		binary = "gograph"
	}
	return &Client{runner: commandRunner{binary: binary}}
}

func NewWithRunner(runner Runner) *Client { return &Client{runner: runner} }

type graphState struct {
	SchemaVersion string `json:"schema_version"`
	Source        string `json:"source"`
	Freshness     string `json:"freshness"`
	Completeness  string `json:"completeness"`
	Precision     string `json:"precision"`
}

type envelope struct {
	SchemaVersion string          `json:"schema_version"`
	Command       string          `json:"command"`
	Status        string          `json:"status"`
	Error         string          `json:"error"`
	Results       json.RawMessage `json:"results"`
	GraphState    graphState      `json:"graph_state"`
}

type identityResult struct {
	SchemaVersion string     `json:"schema_version"`
	Status        string     `json:"status"`
	Matches       []identity `json:"matches"`
}

type identity struct {
	StableID string `json:"stable_id"`
	Kind     string `json:"kind"`
}

type coverageResult struct {
	SchemaVersion      string           `json:"schema_version"`
	Status             string           `json:"status"`
	AnalysisPrecision  string           `json:"analysis_precision"`
	TestCallResolution string           `json:"test_call_resolution"`
	MatchedTests       []identity       `json:"matched_tests"`
	Symbols            []coverageSymbol `json:"symbols"`
}

type coverageSymbol struct {
	StableID   string `json:"stable_id"`
	Resolution string `json:"resolution"`
}

func (c *Client) Resolve(ctx context.Context, repo string, queries []string) ([]string, error) {
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return nil, unavailable("cannot resolve repository: %v", err)
	}
	resolved := make([]string, 0, len(queries))
	for _, query := range queries {
		status, matches, err := c.identity(ctx, root, query)
		if err != nil {
			return nil, err
		}
		switch status {
		case "exact":
			if len(matches) != 1 || matches[0].StableID == "" {
				return nil, insufficient("Gograph returned an invalid exact identity for %q", query)
			}
			resolved = append(resolved, matches[0].StableID)
		case "ambiguous":
			return nil, ambiguous("ambiguous symbol identity %q", query)
		case "not_found":
			return nil, insufficient("protected symbol identity %q was not found", query)
		default:
			return nil, insufficient("Gograph returned identity status %q for %q", status, query)
		}
	}
	slices.Sort(resolved)
	if len(slices.Compact(resolved)) != len(resolved) {
		return nil, insufficient("multiple protected-symbol queries resolve to the same stable identity")
	}
	return resolved, nil
}

func (c *Client) ResolveTest(ctx context.Context, repo, query string) (string, error) {
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return "", unavailable("cannot resolve repository: %v", err)
	}
	status, matches, err := c.identity(ctx, root, query)
	if err != nil {
		return "", err
	}
	if status == "ambiguous" {
		return "", ambiguous("ambiguous bound test identity %q", query)
	}
	if status != "exact" || len(matches) != 1 || matches[0].Kind != "function" || matches[0].StableID == "" {
		return "", insufficient("bound test identity %q is unavailable or insufficient", query)
	}
	return matches[0].StableID, nil
}

func (c *Client) Verify(ctx context.Context, request reach.Request) (reach.Result, error) {
	root, err := repository.CanonicalRoot(request.Repository)
	if err != nil {
		return reach.Result{}, unavailable("cannot resolve repository: %v", err)
	}
	coverage, err := c.coverage(ctx, root, request.TestSymbol)
	if err != nil {
		return reach.Result{}, err
	}
	if len(coverage.MatchedTests) != 1 || coverage.MatchedTests[0].StableID != request.TestSymbol {
		return reach.Result{}, ambiguous("ambiguous bound test identity %q", request.TestSymbol)
	}

	resolutions, err := coverageResolutionIndex(coverage.Symbols)
	if err != nil {
		return reach.Result{}, err
	}

	result := reach.Result{TestSymbol: request.TestSymbol, Symbols: make([]reach.Symbol, 0, len(request.ProtectedSymbols))}
	for _, stableID := range request.ProtectedSymbols {
		resolution, ok := resolutions[stableID]
		if ok {
			result.Symbols = append(result.Symbols, reach.Symbol{StableID: stableID, Resolution: resolution})
			continue
		}
		missing, err := c.missingSymbol(ctx, root, stableID)
		if err != nil {
			return reach.Result{}, err
		}
		result.Symbols = append(result.Symbols, missing)
	}
	return result, nil
}

func coverageResolutionIndex(symbols []coverageSymbol) (map[string]reach.Resolution, error) {
	resolutions := make(map[string]reach.Resolution, len(symbols))
	for _, symbol := range symbols {
		resolution := reach.Resolution(symbol.Resolution)
		if resolution != reach.ResolutionExact && resolution != reach.ResolutionPossible {
			return nil, insufficient("Gograph returned unsupported reach resolution %q", symbol.Resolution)
		}
		if previous := resolutions[symbol.StableID]; previous != reach.ResolutionExact {
			resolutions[symbol.StableID] = resolution
		}
	}
	return resolutions, nil
}

func (c *Client) missingSymbol(ctx context.Context, root, stableID string) (reach.Symbol, error) {
	status, _, err := c.identity(ctx, root, stableID)
	if err != nil {
		return reach.Symbol{}, err
	}
	if status == "ambiguous" {
		return reach.Symbol{}, ambiguous("ambiguous protected symbol identity %q", stableID)
	}
	detail := "symbol exists but is not statically reached"
	if status == "not_found" {
		detail = "stable symbol identity no longer exists"
	} else if status != "exact" {
		return reach.Symbol{}, insufficient("Gograph returned identity status %q for %q", status, stableID)
	}
	return reach.Symbol{StableID: stableID, Resolution: reach.ResolutionMissing, Detail: detail}, nil
}

func (c *Client) identity(ctx context.Context, root, query string) (string, []identity, error) {
	env, err := c.run(ctx, root, "identity", query, "--json", "--intention", intention)
	if err != nil {
		return "", nil, err
	}
	if env.Command != "identity" {
		return "", nil, insufficient("Gograph returned command %q for identity", env.Command)
	}
	var result identityResult
	if err := json.Unmarshal(env.Results, &result); err != nil || result.SchemaVersion != "gograph.identity.v1" {
		return "", nil, insufficient("Gograph identity output has an unsupported schema")
	}
	return result.Status, result.Matches, nil
}

func (c *Client) coverage(ctx context.Context, root, testSymbol string) (coverageResult, error) {
	env, err := c.run(ctx, root, "coverage", testSymbol, "--json", "--intention", intention)
	if err != nil {
		return coverageResult{}, err
	}
	if env.Command != "coverage" {
		return coverageResult{}, insufficient("Gograph returned command %q for coverage", env.Command)
	}
	var result coverageResult
	if err := json.Unmarshal(env.Results, &result); err != nil || result.SchemaVersion != "gograph.coverage.v1" {
		return coverageResult{}, insufficient("Gograph coverage output has an unsupported schema")
	}
	if result.Status != "exact" {
		if result.Status == "ambiguous" {
			return coverageResult{}, ambiguous("ambiguous bound test identity %q", testSymbol)
		}
		return coverageResult{}, insufficient("Gograph coverage status is %q", result.Status)
	}
	if result.AnalysisPrecision != "precise" || result.TestCallResolution != "typed_complete" {
		return coverageResult{}, insufficient("Gograph coverage is %s with test resolution %s; precise typed_complete evidence is required", result.AnalysisPrecision, result.TestCallResolution)
	}
	return result, nil
}

func (c *Client) run(ctx context.Context, root string, args ...string) (envelope, error) {
	stdout, stderr, err := c.runner.Run(ctx, root, args...)
	if err != nil {
		message := strings.TrimSpace(string(stderr))
		if message == "" {
			message = err.Error()
		}
		return envelope{}, unavailable("Gograph command failed: %s", message)
	}
	var env envelope
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	if err := decoder.Decode(&env); err != nil {
		return envelope{}, insufficient("Gograph returned invalid JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return envelope{}, insufficient("Gograph returned more than one JSON document")
	}
	if env.SchemaVersion != "1" || env.Status != "ok" {
		return envelope{}, insufficient("Gograph command envelope is unsupported or unsuccessful: %s", env.Error)
	}
	if err := validateGraphState(env.GraphState); err != nil {
		return envelope{}, err
	}
	return env, nil
}

func validateGraphState(state graphState) error {
	if state.SchemaVersion != "gograph.graph-state.v1" {
		return insufficient("Gograph graph-state schema is unsupported")
	}
	if state.Source != "persisted" || state.Freshness != "current" || state.Completeness != "complete" || state.Precision != "precise" {
		return insufficient("Gograph evidence is source=%s freshness=%s completeness=%s precision=%s; persisted current complete precise evidence is required", state.Source, state.Freshness, state.Completeness, state.Precision)
	}
	return nil
}

func unavailable(format string, args ...any) error {
	return &reach.EvidenceError{Kind: reach.ErrorUnavailable, Message: fmt.Sprintf(format, args...)}
}

func insufficient(format string, args ...any) error {
	return &reach.EvidenceError{Kind: reach.ErrorInsufficient, Message: fmt.Sprintf(format, args...)}
}

func ambiguous(format string, args ...any) error {
	return &reach.EvidenceError{Kind: reach.ErrorAmbiguous, Message: fmt.Sprintf(format, args...)}
}

type commandRunner struct {
	binary string
}

func (r commandRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, []byte, error) {
	path, err := exec.LookPath(r.binary)
	if err != nil {
		return nil, nil, fmt.Errorf("locate %s: %w", r.binary, err)
	}
	stdout := &boundedBuffer{limit: maxStdoutBytes}
	stderr := &boundedBuffer{limit: maxStderrBytes}
	command := exec.CommandContext(ctx, path, args...)
	command.Dir = dir
	command.Stdout = stdout
	command.Stderr = stderr
	err = command.Run()
	if stdout.overflow || stderr.overflow {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("gograph output exceeded the configured limit")
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.overflow = true
		return original, nil
	}
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return original, nil
}
