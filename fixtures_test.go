package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pwFixture = `import { test, expect } from '@playwright/test';

test('refresh token is single use [R-1]', async ({ page }) => {
  await page.goto('/x');
  expect(1).toBe(1);
});
`

const pwExtraTestTmpl = `
test('extra rule holds [%s]', async ({ page }) => {
  await page.goto('/y');
  expect(2).toBe(2);
});
`

func run2(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	code := run(args, &buf, &buf)
	return code, buf.String()
}

func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	code, out := run2(t, args...)
	if code != 0 {
		t.Fatalf("%v exited %d:\n%s", args, code, out)
	}
	return out
}

func writeFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceInFile(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendToFile(t *testing.T, path, s string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte(s)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newPWRepo builds a fixture repo with one armed Playwright-backed rule.
func newPWRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, repo, "e2e/login.spec.ts", pwFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Refresh token is single use.", "--id", "R-1", "--repo", repo)
	mustRun(t, "arm", "R-1", "--check", "e2e/login.spec.ts @ chromium", "--repo", repo)
	return repo
}

func specPath(repo string) string { return filepath.Join(repo, "e2e", "login.spec.ts") }
func ledgerFP(repo string) string { return filepath.Join(repo, "RULE-FLOOR.md") }
func addPWTest(t *testing.T, repo, id string) {
	t.Helper()
	appendToFile(t, specPath(repo), fmt.Sprintf(pwExtraTestTmpl, id))
}

func deleteLedgerRow(t *testing.T, repo, id string) {
	t.Helper()
	data, err := os.ReadFile(ledgerFP(repo))
	if err != nil {
		t.Fatal(err)
	}
	var keep []string
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(ln, "| "+id+" |") {
			continue
		}
		keep = append(keep, ln)
	}
	if err := os.WriteFile(ledgerFP(repo), []byte(strings.Join(keep, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFixtureTable is the required table of tamper/refusal fixtures. Each case
// starts from a fresh repo with one armed rule (R-1) and must fail or refuse.
func TestFixtureTable(t *testing.T) {
	cases := []struct {
		name     string
		prep     func(t *testing.T, repo string)
		cmd      []string // defaults to {"check"}
		wantCode int
		wantMsg  string
		post     func(t *testing.T, repo string)
	}{
		{
			name: "malformed row: wrong field count",
			prep: func(t *testing.T, repo string) {
				appendToFile(t, ledgerFP(repo), "| R-2 | broken | - | NONE | - |\n")
			},
			wantCode: 2,
			wantMsg:  "malformed row",
		},
		{
			name: "malformed row: missing field",
			prep: func(t *testing.T, repo string) {
				appendToFile(t, ledgerFP(repo), "| R-2 | broken |  | NONE | - | - |\n")
			},
			wantCode: 2,
			wantMsg:  "missing field",
		},
		{
			name: "tampered body",
			prep: func(t *testing.T, repo string) {
				replaceInFile(t, specPath(repo), "toBe(1)", "toBe(2)")
			},
			wantCode: 1,
			wantMsg:  "hash mismatch",
		},
		{
			name: ".skip added",
			prep: func(t *testing.T, repo string) {
				replaceInFile(t, specPath(repo), "test('refresh", "test.skip('refresh")
			},
			wantCode: 1,
			wantMsg:  ".skip is set",
		},
		{
			name: ".only added",
			prep: func(t *testing.T, repo string) {
				replaceInFile(t, specPath(repo), "test('refresh", "test.only('refresh")
			},
			wantCode: 1,
			wantMsg:  ".only is set",
		},
		{
			name: "deleted row",
			prep: func(t *testing.T, repo string) {
				deleteLedgerRow(t, repo, "R-1")
			},
			wantCode: 1,
			wantMsg:  "below FLOOR",
		},
		{
			name: "floor shrink: write path never lowers FLOOR",
			prep: func(t *testing.T, repo string) {
				replaceInFile(t, ledgerFP(repo), "FLOOR: 1", "FLOOR: 5")
				mustRun(t, "declare", "Second rule.", "--id", "R-2", "--repo", repo)
				addPWTest(t, repo, "R-2")
			},
			cmd:      []string{"arm", "R-2", "--check", "e2e/login.spec.ts @ chromium"},
			wantCode: 0,
			wantMsg:  "FLOOR 5",
			post: func(t *testing.T, repo string) {
				data, err := os.ReadFile(ledgerFP(repo))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(data), "FLOOR: 5") {
					t.Fatalf("FLOOR was lowered:\n%s", data)
				}
				code, out := run2(t, "check", "--repo", repo)
				if code != 1 || !strings.Contains(out, "below FLOOR") {
					t.Fatalf("check after shrink attempt: exit %d:\n%s", code, out)
				}
			},
		},
		{
			name: "orphan playwright tag",
			prep: func(t *testing.T, repo string) {
				addPWTest(t, repo, "R-9")
			},
			wantCode: 1,
			wantMsg:  "orphan tag R-9",
		},
		{
			name: "orphan go tag",
			prep: func(t *testing.T, repo string) {
				writeFile(t, repo, "x_test.go", "package x\n\n// RULE: G-9\nfunc TestX(t *testing.T) {}\n")
			},
			wantCode: 1,
			wantMsg:  "orphan tag G-9",
		},
		{
			name: "declared row deleted after arm (append-only)",
			prep: func(t *testing.T, repo string) {
				mustRun(t, "declare", "Second rule.", "--id", "R-2", "--repo", repo)
				deleteLedgerRow(t, repo, "R-2")
			},
			wantCode: 1,
			wantMsg:  "below FLOOR",
		},
		{
			name:     "no-op rehash refused",
			cmd:      []string{"rehash", "R-1"},
			wantCode: 1,
			wantMsg:  "no-op",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPWRepo(t)
			if tc.prep != nil {
				tc.prep(t, repo)
			}
			cmd := tc.cmd
			if cmd == nil {
				cmd = []string{"check"}
			}
			code, out := run2(t, append(cmd, "--repo", repo)...)
			if code != tc.wantCode {
				t.Fatalf("exit %d, want %d:\n%s", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantMsg) {
				t.Fatalf("output missing %q:\n%s", tc.wantMsg, out)
			}
			if tc.post != nil {
				tc.post(t, repo)
			}
		})
	}
}
