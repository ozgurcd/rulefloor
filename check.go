package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "testdata": true}

var (
	titleTagRe  = regexp.MustCompile(`\[([A-Z][A-Z0-9-]{0,30}[0-9])\]`)
	goRuleTagRe = regexp.MustCompile(`^\s*// RULE:\s*(.*\S)\s*$`)
)

func cmdCheck(repo, reportPath, allSpec, runProfile, tags string, stdout io.Writer) error {
	if allSpec == "" {
		n, err := checkRepo(repo, reportPath, runProfile, tags, stdout)
		if err != nil {
			return err
		}
		if n > 0 {
			return failf("check FAILED: %d problem(s) in %s", n, repo)
		}
		return nil
	}
	if reportPath != "" {
		return fatalf("check: --report cannot be combined with --all")
	}
	if runProfile != "" {
		return fatalf("check: --run-profile cannot be combined with --all (a profile run belongs to one repo)")
	}
	var targets []string
	for _, p := range strings.Split(allSpec, ",") {
		if p = strings.TrimSpace(p); p != "" {
			targets = append(targets, p)
		}
	}
	if len(targets) == 0 {
		return fatalf("check: --all needs a comma-separated list of repo paths")
	}
	total := 0
	for _, tgt := range targets {
		fmt.Fprintf(stdout, "== %s ==\n", tgt)
		n, err := checkRepo(tgt, "", "", "", stdout)
		if err != nil {
			var ee exitErr
			if errors.As(err, &ee) {
				return exitErr{ee.code, tgt + ": " + ee.msg}
			}
			return err
		}
		total += n
	}
	if total > 0 {
		return failf("check FAILED: %d problem(s) across %d repos", total, len(targets))
	}
	return nil
}

// checkRepo verifies one repo's ledger. It prints per-row results and
// returns the number of problems; the error return is fatal-only.
func checkRepo(repo, reportPath, runProfile, tags string, stdout io.Writer) (int, error) {
	l, err := loadLedger(repo)
	if err != nil {
		return 0, err
	}
	problems := 0
	prob := func(format string, a ...any) {
		fmt.Fprintf(stdout, "FAIL "+format+"\n", a...)
		problems++
	}
	if len(l.Rows) < l.Floor {
		prob("ledger: %d rows is below FLOOR %d (rows deleted or FLOOR tampered)", len(l.Rows), l.Floor)
	}
	var report map[string][]string
	if reportPath != "" {
		report, err = parsePWReport(reportPath)
		if err != nil {
			return 0, err
		}
	}
	armed := 0
	for i := range l.Rows {
		r := &l.Rows[i]
		if !r.armed() {
			fmt.Fprintf(stdout, "DECLARED %s (no armed check)\n", r.ID)
			continue
		}
		armed++
		rowProbs, err := checkRow(repo, r, report, reportPath != "", runProfile, tags)
		if err != nil {
			return 0, err
		}
		if len(rowProbs) == 0 {
			fmt.Fprintf(stdout, "PASS %s (%s)\n", r.ID, r.Check)
			continue
		}
		for _, p := range rowProbs {
			prob("%s: %s", r.ID, p)
		}
	}
	orphans, err := scanOrphans(repo, l)
	if err != nil {
		return 0, err
	}
	for _, o := range orphans {
		prob("%s", o)
	}
	if problems == 0 {
		fmt.Fprintf(stdout, "check OK: %d rows (%d armed), FLOOR %d\n", len(l.Rows), armed, l.Floor)
	}
	return problems, nil
}

func checkRow(repo string, r *Row, report map[string][]string, haveReport bool, runProfile, tags string) ([]string, error) {
	file, profile, err := splitCheck(r.Check)
	if err != nil {
		return nil, fatalf("row %s: %v", r.ID, err)
	}
	kind, err := kindForFile(file)
	if err != nil {
		return nil, fatalf("row %s: %v", r.ID, err)
	}
	var probs []string
	if r.EnforcedBy != kind {
		probs = append(probs, fmt.Sprintf("enforced-by is %q but check file implies %q", r.EnforcedBy, kind))
	}
	abs := filepath.Join(repo, file)
	data, err := os.ReadFile(abs)
	if err != nil {
		return append(probs, fmt.Sprintf("check file %s cannot be read: %v", file, err)), nil
	}
	ref, err := extractTagged(string(data), r.ID, kind)
	if err != nil {
		var ee exitErr
		if errors.As(err, &ee) && ee.code == 2 {
			return nil, err
		}
		return append(probs, err.Error()), nil
	}
	// Go rows with profile "unit" execute in plain check and may not skip.
	// Any other profile is STATIC-ONLY here (file, tag, hash) — its teeth
	// live behind an explicit `check --run-profile <profile>` run, where a
	// runtime skip is CANNOT-EVALUATE, never a pass.
	unitGo := kind == kindGoTest && profile == "unit"
	if ref.Modifier == "skip" || ref.Modifier == "only" {
		probs = append(probs, fmt.Sprintf(".%s is set on the tagged test", ref.Modifier))
	}
	if ref.GoSkips && unitGo {
		probs = append(probs, "tagged Go test calls t.Skip")
	}
	if h := ref.hash(); h != r.Hash {
		probs = append(probs, fmt.Sprintf("hash mismatch: ledger %s, actual %s (body changed; review, then rehash)", r.Hash, h))
	}
	if unitGo {
		msg, err := runGoTest(abs, ref.FuncName, "", false)
		if err != nil {
			return nil, err
		}
		if msg != "" {
			probs = append(probs, msg)
		}
	} else if kind == kindGoTest && runProfile != "" && profile == runProfile {
		msg, err := runGoTest(abs, ref.FuncName, tags, true)
		if err != nil {
			return nil, err
		}
		if msg != "" {
			probs = append(probs, msg)
		}
	}
	if kind == kindPlaywright && haveReport {
		statuses, ok := report[r.ID]
		if !ok {
			probs = append(probs, "not present in the Playwright report")
		}
		for _, s := range statuses {
			if s != "passed" {
				probs = append(probs, fmt.Sprintf("Playwright report status is %q, want \"passed\"", s))
			}
		}
	}
	return probs, nil
}

// runGoTest runs the single tagged test in the package of testFile.
// It returns a non-empty problem string on test failure and an error only
// for CANNOT-EVALUATE conditions. tags, when non-empty, is passed as
// -tags. skipIsFatal marks --run-profile mode: a test that SKIPS there did
// not prove anything — its precondition (a database, an environment) is
// absent, and that is CANNOT-EVALUATE, never a pass.
func runGoTest(testFile, funcName, tags string, skipIsFatal bool) (string, error) {
	args := []string{"test", "-count=1", "-v", "-run", "^" + funcName + "$"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Dir(testFile)
	out, err := cmd.CombinedOutput()
	if skipIsFatal && strings.Contains(string(out), "--- SKIP: "+funcName) {
		return "", fatalf("CANNOT-EVALUATE: %s SKIPPED under --run-profile — its precondition is absent and a skip is not a proof:\n%s", funcName, tail(string(out), 10))
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fatalf("CANNOT-EVALUATE: go toolchain not found: %v", err)
		}
		return fmt.Sprintf("go test -run ^%s$ failed:\n%s", funcName, tail(string(out), 15)), nil
	}
	if !strings.Contains(string(out), "--- PASS: "+funcName) {
		return fmt.Sprintf("go test ran but did not report \"--- PASS: %s\"", funcName), nil
	}
	return "", nil
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return "    " + strings.Join(lines, "\n    ")
}

// scanOrphans finds RULE tags in test files that have no ledger row.
// Playwright titles are scanned under e2e/, Go RULE comments repo-wide.
func scanOrphans(repo string, l *Ledger) ([]string, error) {
	known := map[string]bool{}
	for _, r := range l.Rows {
		known[r.ID] = true
	}
	var probs []string
	walkErr := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != repo && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		slashRel := filepath.ToSlash(rel)
		switch {
		case strings.HasSuffix(path, "_test.go"):
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, line := range strings.Split(string(data), "\n") {
				m := goRuleTagRe.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				id := m[1]
				if !idRe.MatchString(id) {
					probs = append(probs, fmt.Sprintf("%s: invalid RULE tag %q", slashRel, id))
					continue
				}
				if !known[id] {
					probs = append(probs, fmt.Sprintf("%s: orphan tag %s (no ledger row)", slashRel, id))
				}
			}
		case strings.HasSuffix(path, ".spec.ts") && strings.HasPrefix(slashRel, "e2e/"):
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, t := range pwScan(string(data)) {
				for _, m := range titleTagRe.FindAllStringSubmatch(t.Title, -1) {
					if !known[m[1]] {
						probs = append(probs, fmt.Sprintf("%s: orphan tag %s (no ledger row)", slashRel, m[1]))
					}
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, fatalf("CANNOT-EVALUATE: orphan scan: %v", walkErr)
	}
	return probs, nil
}

// parsePWReport extracts, per rule ID, the final status of every test whose
// spec title carries the [ID] tag, from a Playwright JSON report.
func parsePWReport(path string) (map[string][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fatalf("CANNOT-EVALUATE: cannot read report %s: %v", path, err)
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fatalf("CANNOT-EVALUATE: report %s is not valid JSON: %v", path, err)
	}
	out := map[string][]string{}
	var walk func(node any)
	walk = func(node any) {
		switch n := node.(type) {
		case []any:
			for _, e := range n {
				walk(e)
			}
		case map[string]any:
			title, hasTitle := n["title"].(string)
			tests, hasTests := n["tests"].([]any)
			if hasTitle && hasTests {
				var ids []string
				for _, m := range titleTagRe.FindAllStringSubmatch(title, -1) {
					ids = append(ids, m[1])
				}
				if len(ids) > 0 {
					for _, tv := range tests {
						tm, ok := tv.(map[string]any)
						if !ok {
							continue
						}
						status := testFinalStatus(tm)
						for _, id := range ids {
							out[id] = append(out[id], status)
						}
					}
				}
			}
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(root)
	return out, nil
}

func testFinalStatus(tm map[string]any) string {
	if rs, ok := tm["results"].([]any); ok && len(rs) > 0 {
		if last, ok := rs[len(rs)-1].(map[string]any); ok {
			if s, ok := last["status"].(string); ok {
				return s
			}
		}
	}
	if s, ok := tm["status"].(string); ok {
		return s
	}
	return "unknown"
}
