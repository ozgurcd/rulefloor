package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurcd/rulefloor/internal/ledger"
	"github.com/ozgurcd/rulefloor/internal/model"
	"github.com/ozgurcd/rulefloor/internal/reach"
)

type fakeGraphClient struct {
	resolution  reach.Resolution
	err         error
	verifyCalls int
}

func (f *fakeGraphClient) Resolve(_ context.Context, _ string, queries []string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]string{}, queries...), nil
}

func (f *fakeGraphClient) ResolveTest(context.Context, string, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "fixture::TestRefreshSingleUse", nil
}

func (f *fakeGraphClient) Verify(_ context.Context, request reach.Request) (reach.Result, error) {
	f.verifyCalls++
	if f.err != nil {
		return reach.Result{}, f.err
	}
	symbols := make([]reach.Symbol, 0, len(request.ProtectedSymbols))
	for _, symbol := range request.ProtectedSymbols {
		symbols = append(symbols, reach.Symbol{StableID: symbol, Resolution: f.resolution})
	}
	return reach.Result{TestSymbol: request.TestSymbol, Symbols: symbols}, nil
}

func TestCheckDetectsProofDecayAfterStaticIntegrityPasses(t *testing.T) {
	repo := newGoReachRepo(t)
	client := &fakeGraphClient{resolution: reach.ResolutionMissing}
	useGraphClient(t, client)
	code, out := run2(t, "check", "--only", "G-1", "--repo", repo)
	if code != 1 || !strings.Contains(out, "protected symbol no longer reached: fixture::Guard") {
		t.Fatalf("proof-decay check exit %d:\n%s", code, out)
	}
	if strings.Contains(out, "go test -run") {
		t.Fatalf("passing execution was reported as a test failure:\n%s", out)
	}
	if client.verifyCalls != 1 {
		t.Fatalf("Verify calls = %d, want 1", client.verifyCalls)
	}
}

func TestCheckDistinguishesPossibleReachAndGraphFailure(t *testing.T) {
	t.Run("possible only", func(t *testing.T) {
		repo := newGoReachRepo(t)
		useGraphClient(t, &fakeGraphClient{resolution: reach.ResolutionPossible})
		code, out := run2(t, "check", "--only", "G-1", "--repo", repo)
		if code != 1 || !strings.Contains(out, "only possible/uncertain reachability") {
			t.Fatalf("possible-only exit %d:\n%s", code, out)
		}
	})
	t.Run("evidence unavailable", func(t *testing.T) {
		repo := newGoReachRepo(t)
		useGraphClient(t, &fakeGraphClient{err: &reach.EvidenceError{Kind: reach.ErrorUnavailable, Message: "gograph not found"}})
		code, out := run2(t, "check", "--only", "G-1", "--repo", repo)
		if code != 2 || !strings.Contains(out, "CANNOT-EVALUATE: graph evidence unavailable or insufficient") {
			t.Fatalf("unavailable evidence exit %d:\n%s", code, out)
		}
	})
	t.Run("ambiguous identity", func(t *testing.T) {
		repo := newGoReachRepo(t)
		useGraphClient(t, &fakeGraphClient{err: &reach.EvidenceError{Kind: reach.ErrorAmbiguous, Message: "two matches"}})
		code, out := run2(t, "check", "--only", "G-1", "--repo", repo)
		if code != 2 || !strings.Contains(out, "CANNOT-EVALUATE: ambiguous symbol identity") {
			t.Fatalf("ambiguous evidence exit %d:\n%s", code, out)
		}
	})
}

func TestCheckSkipsGraphWhenBoundTestChanged(t *testing.T) {
	repo := newGoReachRepo(t)
	path := filepath.Join(repo, "refresh_test.go")
	replaceInFile(t, path, "if os.Getenv", "t.Log(\"changed\")\n\tif os.Getenv")
	client := &fakeGraphClient{resolution: reach.ResolutionExact}
	useGraphClient(t, client)
	code, out := run2(t, "check", "--only", "G-1", "--repo", repo)
	if code != 1 || !strings.Contains(out, "hash mismatch") {
		t.Fatalf("changed test exit %d:\n%s", code, out)
	}
	if client.verifyCalls != 0 {
		t.Fatalf("graph was queried %d times despite source integrity failure", client.verifyCalls)
	}
}

func TestRehashRefusesToAcceptProofDecay(t *testing.T) {
	repo := newGoReachRepo(t)
	path := filepath.Join(repo, "refresh_test.go")
	replaceInFile(t, path, "if os.Getenv", "t.Log(\"changed\")\n\tif os.Getenv")
	before, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	oldHash := before.find("G-1").Hash
	useGraphClient(t, &fakeGraphClient{resolution: reach.ResolutionMissing})
	code, out := run2(t, "rehash", "G-1", "--repo", repo)
	if code != 1 || !strings.Contains(out, "refusing exact protected-symbol binding") {
		t.Fatalf("rehash proof-decay exit %d:\n%s", code, out)
	}
	after, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	if after.find("G-1").Hash != oldHash {
		t.Fatal("failed proof-decay rehash changed the persisted fingerprint")
	}
}

func TestArmPersistsStableReachAndExplicitExecutionPolicy(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module fixture\n\ngo 1.27.0\n")
	writeFile(t, repo, "refresh_test.go", goFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Refresh tokens are single use.", "--id", "G-1", "--repo", repo)
	client := &fakeGraphClient{resolution: reach.ResolutionExact}
	useGraphClient(t, client)
	mustRun(t, "arm", "G-1", "--check", "refresh_test.go @ unit", "--execution", "static", "--covers", "fixture::Guard", "--red-proof", fixtureProof, "--repo", repo)

	l, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	row := l.find("G-1")
	if row.ExecutionPolicy != model.ExecutionStatic || row.ReachabilityPolicy != model.ReachabilityExact || row.TestSymbol != "fixture::TestRefreshSingleUse" {
		t.Fatalf("persisted binding = %+v", row)
	}
	t.Setenv("RULEFLOOR_FIXTURE_FAIL", "1")
	out := mustRun(t, "check", "--only", "G-1", "--repo", repo)
	if !strings.Contains(out, "PASS G-1") {
		t.Fatalf("explicit static policy executed the unit-profile test:\n%s", out)
	}
}

func TestMixedLegacyAndStableCoversAreRejected(t *testing.T) {
	repo := t.TempDir()
	mustRun(t, "init", "--repo", repo)
	code, out := run2(t, "declare", "A rule.", "--id", "R-1", "--covers", "legacy-label,fixture::Guard", "--repo", repo)
	if code != 2 || !strings.Contains(out, "only Gograph stable identities or only legacy labels") {
		t.Fatalf("mixed covers exit %d:\n%s", code, out)
	}
}

func TestProofDecayStillReportsBoundTestFailureSeparately(t *testing.T) {
	repo := newGoReachRepo(t)
	useGraphClient(t, &fakeGraphClient{resolution: reach.ResolutionExact})
	t.Setenv("RULEFLOOR_FIXTURE_FAIL", "1")
	code, out := run2(t, "check", "--only", "G-1", "--repo", repo)
	if code != 1 || !strings.Contains(out, "go test -run ^TestRefreshSingleUse$ failed") || strings.Contains(out, "protected symbol no longer reached") {
		t.Fatalf("test failure exit %d:\n%s", code, out)
	}
}

func TestMachineValidationUsesTheSameProofDecayEvaluator(t *testing.T) {
	repo := newGoReachRepo(t)
	useGraphClient(t, &fakeGraphClient{resolution: reach.ResolutionMissing})
	code, stdout, stderr := runSeparate("validate", "G-1", "--repo", repo, "--mode", "static", "--json")
	if code != 1 || stderr != "" {
		t.Fatalf("machine proof-decay exit %d stderr %q: %s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, `"schema_version":"rulefloor.validation.v1"`) ||
		!strings.Contains(stdout, `"reason":"protected_symbol_unreached"`) ||
		!strings.Contains(stdout, `"structural_reach":{"required":true,"status":"fail"`) {
		t.Fatalf("machine proof-decay output:\n%s", stdout)
	}
}

func TestGographIsOptionalForLegacyLabelWorkflows(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module fixture\n\ngo 1.27.0\n")
	writeFile(t, repo, "refresh_test.go", goFixture)
	t.Setenv("PATH", "")

	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Refresh tokens are single use.", "--id", "G-1", "--covers", "refresh.go:Consume", "--repo", repo)
	mustRun(t, "arm", "G-1", "--check", "refresh_test.go @ integration", "--red-proof", fixtureProof, "--repo", repo)
	if out := mustRun(t, "check", "--repo", repo); !strings.Contains(out, "check OK: 1 rows (1 armed), FLOOR 1, RED-PROOFS 1") {
		t.Fatalf("label-only check without Gograph:\n%s", out)
	}
	mustRun(t, "amend", "G-1", "Refresh tokens remain single use.", "--covers", "refresh.go:Consume", "--repo", repo)
	path := filepath.Join(repo, "refresh_test.go")
	replaceInFile(t, path, "if os.Getenv", "t.Log(\"reviewed extension\")\n\tif os.Getenv")
	mustRun(t, "rehash", "G-1", "--repo", repo)
	if out := mustRun(t, "check", "--repo", repo); !strings.Contains(out, "PASS G-1") {
		t.Fatalf("label-only check after amend/rehash without Gograph:\n%s", out)
	}

	l, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	row := l.find("G-1")
	row.CoveredSymbols = []string{"fixture::Consume"}
	row.ReachabilityPolicy = model.ReachabilityExact
	row.TestSymbol = "fixture::TestRefreshSingleUse"
	if err := saveLedger(repo, l); err != nil {
		t.Fatal(err)
	}
	code, out := run2(t, "check", "--repo", repo)
	if code != 2 || !strings.Contains(out, "CANNOT-EVALUATE: graph evidence unavailable or insufficient") || !strings.Contains(out, "locate gograph") {
		t.Fatalf("exact reach without Gograph exit %d:\n%s", code, out)
	}
}

func TestExactReachArmFailsClearlyWithoutGograph(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module fixture\n\ngo 1.27.0\n")
	writeFile(t, repo, "refresh_test.go", goFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Refresh tokens are single use.", "--id", "G-1", "--repo", repo)
	t.Setenv("PATH", "")
	code, out := run2(t, "arm", "G-1", "--check", "refresh_test.go @ integration", "--covers", "fixture::Consume", "--red-proof", fixtureProof, "--repo", repo)
	if code != 2 || !strings.Contains(out, "CANNOT-EVALUATE: graph evidence unavailable or insufficient") || !strings.Contains(out, "locate gograph") {
		t.Fatalf("exact arm without Gograph exit %d:\n%s", code, out)
	}
	l, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	if l.find("G-1").Armed() {
		t.Fatal("failed exact-reach arm changed the declared row")
	}
}

func newGoReachRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module fixture\n\ngo 1.27.0\n")
	writeFile(t, repo, "refresh_test.go", goFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Refresh tokens are single use.", "--id", "G-1", "--repo", repo)
	mustRun(t, "arm", "G-1", "--check", "refresh_test.go @ unit", "--red-proof", fixtureProof, "--repo", repo)
	l, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	row := l.find("G-1")
	row.CoveredSymbols = []string{"fixture::Guard"}
	row.ReachabilityPolicy = ledger.ReachabilityExact
	row.TestSymbol = "fixture::TestRefreshSingleUse"
	if err := saveLedger(repo, l); err != nil {
		t.Fatal(err)
	}
	return repo
}

func useGraphClient(t *testing.T, client graphClient) {
	t.Helper()
	previous := newGraphClient
	newGraphClient = func() graphClient { return client }
	t.Cleanup(func() { newGraphClient = previous })
}
