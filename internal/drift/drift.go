package drift

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
	"github.com/ozgurcd/rulefloor/internal/extract"
	"github.com/ozgurcd/rulefloor/internal/ledger"
	"github.com/ozgurcd/rulefloor/internal/repository"
)

const historyLimit = 200

var revisionPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`)

type Result struct {
	RuleID           string
	File             string
	ExpectedHash     string
	ActualHash       string
	BaselineRevision string
	BaselineBody     string
	CurrentBody      string
}

func Compare(ctx context.Context, runner checkengine.CommandRunner, registry *extract.Registry, repo, ruleID string) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("context is nil")
	}
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return Result{}, err
	}
	model, err := ledger.Load(root)
	if err != nil {
		return Result{}, err
	}
	row := model.Find(ruleID)
	if row == nil {
		return Result{}, fmt.Errorf("rule %s does not exist", ruleID)
	}
	if !row.Armed() {
		return Result{}, fmt.Errorf("rule %s is not armed", ruleID)
	}
	binding, err := ledger.InterpretBinding(row)
	if err != nil {
		return Result{}, err
	}
	resolved, err := repository.ConfinedRegularFile(root, binding.File)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", binding.File, err)
	}
	current, err := registry.Extract(binding.File, string(data), ruleID)
	if err != nil {
		return Result{}, err
	}
	result := Result{RuleID: ruleID, File: binding.File, ExpectedHash: row.Hash, ActualHash: current.Fingerprint(), CurrentBody: current.Body}
	if result.ActualHash == result.ExpectedHash {
		return result, nil
	}
	revisions, err := runner.Run(ctx, root, "git", "rev-list", fmt.Sprintf("--max-count=%d", historyLimit), "HEAD", "--", filepath.ToSlash(binding.File))
	if err != nil {
		return Result{}, fmt.Errorf("inspect Git history: %w", err)
	}
	for _, revision := range strings.Fields(string(revisions)) {
		if !revisionPattern.MatchString(revision) {
			return Result{}, fmt.Errorf("git returned an invalid revision identifier")
		}
		object := revision + ":./" + filepath.ToSlash(binding.File)
		oldData, showErr := runner.Run(ctx, root, "git", "show", "--no-textconv", object)
		if showErr != nil {
			continue
		}
		oldRef, extractErr := registry.Extract(binding.File, string(oldData), ruleID)
		if extractErr == nil && oldRef.Fingerprint() == row.Hash {
			result.BaselineRevision = revision
			result.BaselineBody = oldRef.Body
			return result, nil
		}
	}
	return Result{}, fmt.Errorf("no matching bound span found in the newest %d Git revisions", historyLimit)
}
