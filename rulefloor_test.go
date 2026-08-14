package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goFixture = `package fixture

import (
	"os"
	"testing"
)

// RULE: G-1
func TestRefreshSingleUse(t *testing.T) {
	if os.Getenv("RULEFLOOR_FIXTURE_FAIL") == "1" {
		t.Fatal("forced failure")
	}
}
`

func TestCheckPassesOnCleanRepo(t *testing.T) {
	repo := newPWRepo(t)
	out := mustRun(t, "check", "--repo", repo)
	for _, want := range []string{"PASS R-1", "check OK"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestListShowUnarmed(t *testing.T) {
	repo := newPWRepo(t)
	mustRun(t, "declare", "Second rule.", "--id", "R-2", "--repo", repo)
	out := mustRun(t, "list", "--repo", repo)
	if !strings.Contains(out, "R-1") || !strings.Contains(out, "armed") || !strings.Contains(out, "declared") {
		t.Fatalf("list output:\n%s", out)
	}
	out = mustRun(t, "show", "R-2", "--repo", repo)
	if !strings.Contains(out, "check:       NONE") {
		t.Fatalf("show output:\n%s", out)
	}
	out = mustRun(t, "unarmed", "--repo", repo)
	if !strings.Contains(out, "R-2") || strings.Contains(out, "R-1") {
		t.Fatalf("unarmed output:\n%s", out)
	}
	if code, out := run2(t, "show", "R-99", "--repo", repo); code != 1 {
		t.Fatalf("show unknown: exit %d:\n%s", code, out)
	}
}

func TestRefusals(t *testing.T) {
	repo := newPWRepo(t)
	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantMsg  string
	}{
		{"init when ledger exists", []string{"init"}, 1, "already exists"},
		{"declare duplicate ID", []string{"declare", "Again.", "--id", "R-1"}, 1, "already exists"},
		{"arm already armed", []string{"arm", "R-1", "--check", "e2e/login.spec.ts @ chromium"}, 1, "already armed"},
		{"arm unknown rule", []string{"arm", "R-7", "--check", "e2e/login.spec.ts @ chromium"}, 1, "no rule R-7"},
		{"arm unknown check kind", []string{"arm", "R-1", "--check", "notes.txt @ x"}, 2, "CANNOT-EVALUATE"},
		{"declare invalid ID", []string{"declare", "Bad.", "--id", "lower-1"}, 2, "invalid ID"},
		{"rehash unknown rule", []string{"rehash", "R-7"}, 1, "no rule R-7"},
		{"check --report with --all", []string{"check", "--report", "x.json", "--all", "a,b"}, 2, "cannot be combined"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out := run2(t, append(tc.args, "--repo", repo)...)
			if code != tc.wantCode {
				t.Fatalf("exit %d, want %d:\n%s", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantMsg) {
				t.Fatalf("output missing %q:\n%s", tc.wantMsg, out)
			}
		})
	}
}

func TestArmRefusesSkippedOrUntaggedTest(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "e2e/a.spec.ts", strings.Replace(pwFixture, "test('refresh", "test.skip('refresh", 1))
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Rule one.", "--id", "R-1", "--repo", repo)
	if code, out := run2(t, "arm", "R-1", "--check", "e2e/a.spec.ts @ chromium", "--repo", repo); code != 1 || !strings.Contains(out, ".skip") {
		t.Fatalf("arm on skipped test: exit %d:\n%s", code, out)
	}
	mustRun(t, "declare", "Rule two.", "--id", "R-2", "--repo", repo)
	if code, out := run2(t, "arm", "R-2", "--check", "e2e/a.spec.ts @ chromium", "--repo", repo); code != 1 || !strings.Contains(out, "not found") {
		t.Fatalf("arm on missing tag: exit %d:\n%s", code, out)
	}
}

func TestRehashUnarmedRefusedAndLegitFlow(t *testing.T) {
	repo := newPWRepo(t)
	mustRun(t, "declare", "Second rule.", "--id", "R-2", "--repo", repo)
	if code, out := run2(t, "rehash", "R-2", "--repo", repo); code != 1 || !strings.Contains(out, "not armed") {
		t.Fatalf("rehash unarmed: exit %d:\n%s", code, out)
	}
	// Legit edit flow: body changes, check fails, rehash accepts, check passes.
	replaceInFile(t, specPath(repo), "toBe(1)", "toBe(3)")
	if code, _ := run2(t, "check", "--repo", repo); code != 1 {
		t.Fatal("check should fail after edit")
	}
	mustRun(t, "rehash", "R-1", "--repo", repo)
	mustRun(t, "check", "--repo", repo)
}

func TestGoKindEndToEnd(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module fixture\n\ngo 1.21\n")
	writeFile(t, repo, "refresh_test.go", goFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Refresh tokens are single use.", "--id", "G-1", "--repo", repo)
	mustRun(t, "arm", "G-1", "--check", "refresh_test.go @ unit", "--repo", repo)
	out := mustRun(t, "check", "--repo", repo)
	if !strings.Contains(out, "PASS G-1") {
		t.Fatalf("check output:\n%s", out)
	}
	t.Setenv("RULEFLOOR_FIXTURE_FAIL", "1")
	code, out := run2(t, "check", "--repo", repo)
	if code != 1 || !strings.Contains(out, "go test -run ^TestRefreshSingleUse$ failed") {
		t.Fatalf("check with failing go test: exit %d:\n%s", code, out)
	}
}

func TestGoKindRefusesSkip(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module fixture\n\ngo 1.21\n")
	writeFile(t, repo, "skip_test.go", `package fixture

import "testing"

// RULE: G-2
func TestSkippy(t *testing.T) {
	t.Skip("later")
}
`)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Skipped rule.", "--id", "G-2", "--repo", repo)
	if code, out := run2(t, "arm", "G-2", "--check", "skip_test.go @ unit", "--repo", repo); code != 1 || !strings.Contains(out, "t.Skip") {
		t.Fatalf("arm on t.Skip test: exit %d:\n%s", code, out)
	}
}

func pwReport(status string) string {
	return fmt.Sprintf(`{"suites":[{"title":"login.spec.ts","specs":[{"title":"refresh token is single use [R-1]","tests":[{"results":[{"status":%q}]}]}]}]}`, status)
}

func TestPlaywrightReport(t *testing.T) {
	cases := []struct {
		name     string
		report   string
		wantCode int
		wantMsg  string
	}{
		{"passed", pwReport("passed"), 0, "check OK"},
		{"failed status", pwReport("failed"), 1, `status is "failed"`},
		{"ID missing from report", `{"suites":[]}`, 1, "not present in the Playwright report"},
		{"invalid JSON", `{nope`, 2, "not valid JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPWRepo(t)
			rp := filepath.Join(repo, "pw.json")
			if err := os.WriteFile(rp, []byte(tc.report), 0o644); err != nil {
				t.Fatal(err)
			}
			code, out := run2(t, "check", "--repo", repo, "--report", rp)
			if code != tc.wantCode {
				t.Fatalf("exit %d, want %d:\n%s", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantMsg) {
				t.Fatalf("output missing %q:\n%s", tc.wantMsg, out)
			}
		})
	}
}

func TestCheckAllRepos(t *testing.T) {
	parent := t.TempDir()
	repoA := filepath.Join(parent, "alpha")
	repoB := filepath.Join(parent, "beta")
	for _, r := range []string{repoA, repoB} {
		if err := os.MkdirAll(r, 0o755); err != nil {
			t.Fatal(err)
		}
		mustRun(t, "init", "--repo", r)
	}
	all := repoA + "," + repoB
	out := mustRun(t, "check", "--all", all)
	if strings.Count(out, "check OK") != 2 {
		t.Fatalf("expected two OK repos:\n%s", out)
	}
	replaceInFile(t, filepath.Join(repoB, "RULE-FLOOR.md"), "FLOOR: 0", "FLOOR: 3")
	if code, out := run2(t, "check", "--all", all); code != 1 || !strings.Contains(out, "below FLOOR") {
		t.Fatalf("broken repo B: exit %d:\n%s", code, out)
	}
	if err := os.Remove(filepath.Join(repoA, "RULE-FLOOR.md")); err != nil {
		t.Fatal(err)
	}
	if code, out := run2(t, "check", "--all", all); code != 2 || !strings.Contains(out, "cannot read") {
		t.Fatalf("missing ledger: exit %d:\n%s", code, out)
	}
}

func TestLedgerRoundTrip(t *testing.T) {
	l := &Ledger{Floor: 2, Rows: []Row{
		{"R-1", "Rule one.", "playwright", "e2e/a.spec.ts @ chromium", "-", "abcdef012345"},
		{"R-2", "Rule two.", "-", "NONE", "-", "-"},
	}}
	got, err := parseLedger(l.serialize())
	if err != nil {
		t.Fatal(err)
	}
	if got.Floor != 2 || len(got.Rows) != 2 || got.Rows[0] != l.Rows[0] || got.Rows[1] != l.Rows[1] {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestParseLedgerRejects(t *testing.T) {
	header := "| ID | one-sentence rule | enforced-by | check | red-proof | hash |\n|---|---|---|---|---|---|\n"
	cases := []struct {
		name string
		data string
		want string
	}{
		{"no FLOOR line", header, `expected "FLOOR: N"`},
		{"negative FLOOR", "FLOOR: -1\n" + header, "invalid FLOOR"},
		{"bad header", "FLOOR: 0\n| a | b | c | d | e | f |\n|---|---|---|---|---|---|\n", "malformed header"},
		{"truncated", "FLOOR: 0\n", "truncated"},
		{"duplicate ID", "FLOOR: 0\n" + header + "| R-1 | x | - | NONE | - | - |\n| R-1 | y | - | NONE | - | - |\n", "duplicate"},
		{"bad hash on armed row", "FLOOR: 0\n" + header + "| R-1 | x | playwright | e2e/a.spec.ts @ p | - | zz |\n", "invalid hash"},
		{"hash on declared row", "FLOOR: 0\n" + header + "| R-1 | x | - | NONE | - | abcdef012345 |\n", `must have hash "-"`},
		{"escaping check path", "FLOOR: 0\n" + header + "| R-1 | x | playwright | ../evil.spec.ts @ p | - | abcdef012345 |\n", "inside the repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseLedger(tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestExtractPlaywrightAmbiguousTagIsFatal(t *testing.T) {
	src := pwFixture + strings.Replace(pwFixture, "import { test, expect } from '@playwright/test';", "", 1)
	_, err := extractPlaywright(src, "R-1")
	var ee exitErr
	if !errors.As(err, &ee) || ee.code != 2 {
		t.Fatalf("want fatal, got %v", err)
	}
}

func TestExtractGoMisplacedTag(t *testing.T) {
	src := "package x\n\n// RULE: G-1\nvar x = 1\n\nfunc TestFoo(t *testing.T) {}\n"
	_, err := extractGo(src, "G-1")
	if err == nil || !strings.Contains(err.Error(), "not directly above") {
		t.Fatalf("err = %v", err)
	}
}

func TestExtractPlaywrightTrickyStrings(t *testing.T) {
	src := "test('has (parens) and \\' quote [R-1]', async () => {\n" +
		"  const s = \"a ) b\";\n" +
		"  const u = `x ${1} )`;\n" +
		"});\n"
	ref, err := extractPlaywright(src, "R-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(ref.Body, "})") {
		t.Fatalf("body span wrong:\n%s", ref.Body)
	}
	if len(ref.hash()) != 12 {
		t.Fatalf("hash %q", ref.hash())
	}
}

// Fixture cut from the real identuum-ui login.spec.ts:488 shape: an array of
// regex literals whose bodies contain quotes, braces, and escaped slashes.
// The pre-fix extractor treated the '"' inside the first regex as a string
// opener and reported CANNOT-EVALUATE: unbalanced delimiters.
const pwRegexFixture = `import { test, expect } from '@playwright/test';

test('page body never leaks credential terms [NL-1]', async ({ page }) => {
  await page.goto('/login');
  const body = await page.content();
  const forbidden: RegExp[] = [
    /challenge"?\s*:\s*"/i,
    /allowCredentials/i,
    /publicKey"?\s*:\s*\{/i,
    /"signature"\s*:\s*"/i,
    /"credentialId"\s*:\s*"/i,
    /Bearer\s+[A-Za-z0-9._-]{8,}/,
    /otpauth:\/\//i,
  ];
  for (const pat of forbidden) {
    expect(body, ` + "`" + `page body must not match ${pat}` + "`" + `).not.toMatch(pat);
  }
});
`

func TestExtractPlaywrightRegexLiteralBody(t *testing.T) {
	ref, err := extractPlaywright(pwRegexFixture, "NL-1")
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if !strings.HasSuffix(ref.Body, "})") {
		t.Fatalf("body span wrong:\n%s", ref.Body)
	}
	if len(ref.hash()) != 12 {
		t.Fatalf("hash %q", ref.hash())
	}
}

// Fixture for the profile model: a build-tagged integration test that guards
// on its environment (t.Skip) and can be forced to fail.
const goItgFixture = `//go:build integration

package fixture

import (
	"os"
	"testing"
)

// RULE: IG-1
func TestIntegrationTeeth(t *testing.T) {
	if os.Getenv("RULEFLOOR_FIXTURE_DB") == "" {
		t.Skip("no database")
	}
	if os.Getenv("RULEFLOOR_FIXTURE_ITG_FAIL") == "1" {
		t.Fatal("teeth bite")
	}
}
`

func TestGoProfileStaticAndRunProfile(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module fixture\n\ngo 1.21\n")
	writeFile(t, repo, "itg_test.go", goItgFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "The DB teeth hold at the schema layer.", "--id", "IG-1", "--repo", repo)
	// Arming a skipping test at the unit profile stays REFUSED.
	if code, out := run2(t, "arm", "IG-1", "--check", "itg_test.go @ unit", "--repo", repo); code != 1 || !strings.Contains(out, "t.Skip") {
		t.Fatalf("unit arm of skipping test: exit %d:\n%s", code, out)
	}
	// The integration profile accepts the guarded skip and arms.
	mustRun(t, "arm", "IG-1", "--check", "itg_test.go @ integration", "--repo", repo)
	// Plain check is STATIC-ONLY for the row: green with no env, no run.
	mustRun(t, "check", "--repo", repo)
	// Run-profile with the precondition absent: the runtime skip is fatal.
	if code, out := run2(t, "check", "--repo", repo, "--run-profile", "integration", "--tags", "integration"); code != 2 || !strings.Contains(out, "SKIPPED under --run-profile") {
		t.Fatalf("run-profile with absent env: exit %d:\n%s", code, out)
	}
	// Precondition present: the row runs and passes.
	t.Setenv("RULEFLOOR_FIXTURE_DB", "up")
	mustRun(t, "check", "--repo", repo, "--run-profile", "integration", "--tags", "integration")
	// A runtime failure bites.
	t.Setenv("RULEFLOOR_FIXTURE_ITG_FAIL", "1")
	if code, out := run2(t, "check", "--repo", repo, "--run-profile", "integration", "--tags", "integration"); code != 1 || !strings.Contains(out, "failed") {
		t.Fatalf("run-profile failing test: exit %d:\n%s", code, out)
	}
	// Static tamper still bites in plain check (no weakening).
	replaceInFile(t, filepath.Join(repo, "itg_test.go"), "teeth bite", "teeth biteX")
	if code, out := run2(t, "check", "--repo", repo); code != 1 || !strings.Contains(out, "hash mismatch") {
		t.Fatalf("plain check after tamper: exit %d:\n%s", code, out)
	}
}

func TestRunProfileLeavesUnitRowsUnchanged(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module fixture\n\ngo 1.21\n")
	writeFile(t, repo, "refresh_test.go", goFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Refresh tokens are single use.", "--id", "G-1", "--repo", repo)
	mustRun(t, "arm", "G-1", "--check", "refresh_test.go @ unit", "--repo", repo)
	// Unit rows still EXECUTE under --run-profile mode, exactly as in
	// plain check: a failing unit test fails the profile run too.
	t.Setenv("RULEFLOOR_FIXTURE_FAIL", "1")
	if code, out := run2(t, "check", "--repo", repo, "--run-profile", "integration", "--tags", "integration"); code != 1 || !strings.Contains(out, "go test -run ^TestRefreshSingleUse$ failed") {
		t.Fatalf("unit row under run-profile: exit %d:\n%s", code, out)
	}
}

func TestRunProfileFlagValidation(t *testing.T) {
	repo := t.TempDir()
	if code, out := run2(t, "check", "--repo", repo, "--tags", "integration"); code != 2 || !strings.Contains(out, "--tags requires --run-profile") {
		t.Fatalf("--tags alone: exit %d:\n%s", code, out)
	}
	if code, out := run2(t, "check", "--all", "a,b", "--run-profile", "integration"); code != 2 || !strings.Contains(out, "cannot be combined with --all") {
		t.Fatalf("--run-profile with --all: exit %d:\n%s", code, out)
	}
}
