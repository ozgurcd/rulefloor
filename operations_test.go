package main

import (
	"bytes"
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurcd/rulefloor/internal/ledger"
	"github.com/ozgurcd/rulefloor/internal/reach"
)

func TestOperationsRefuseReachabilityMetadataBypass(t *testing.T) {
	for _, mutation := range []string{"demotion", "declare-policy", "arm-policy", "mixed", "legacy-test", "declared-test", "wrong-test", "unresolved"} {
		for _, command := range []string{"ops-validate", "ops-apply"} {
			t.Run(mutation+"/"+command, func(t *testing.T) {
				repo, _ := operationFixture(t, "R-1")
				client := &fakeGraphClient{resolution: reach.ResolutionExact}
				useGraphClient(t, client)
				row := armedOperationFixture(t, repo, "R-1")
				row.CoveredSymbols = []string{"fixture::Guard"}
				row.ReachabilityPolicy = ledger.ReachabilityExact
				row.TestSymbol = "fixture::TestRefreshSingleUse"
				base := &ledger.Ledger{Floor: 1, HasRedProofs: true, Rows: []ledger.Row{row}}
				if mutation == "declare-policy" || mutation == "declared-test" || mutation == "unresolved" {
					base.Rows = nil
					base.Floor = 0
					row = declaredOperationRow("R-1")
					row.CoveredSymbols = []string{"fixture::Guard"}
					row.ReachabilityPolicy = ledger.ReachabilityExact
				} else if mutation == "arm-policy" {
					base.Rows[0] = declaredOperationRow("R-1")
				}
				base.MaintainRedProofs()
				original := []byte(ledger.Serialize(base))
				if err := os.WriteFile(ledger.Path(repo), original, 0o644); err != nil {
					t.Fatal(err)
				}
				previous := base.Find("R-1")
				if previous != nil && previous.Armed() {
					row.Rule = "The amended rule holds."
				}
				kind, err := operationKind(previous, row)
				if err != nil {
					t.Fatal(err)
				}
				check, err := checkFileDigest(repo, &row)
				if err != nil {
					t.Fatal(err)
				}
				set := operationSet{operationSchema, "baseline-1", string(original), digest(original), "source-1", "review-1", "authority-1", []floorOperation{{ID: row.ID, Kind: kind, Old: previous, New: row, OldFingerprints: fingerprints(previous), NewFingerprints: fingerprints(&row), CheckSHA: check}}}
				valid := encodeOperationFixture(t, set)
				next := &set.Operations[0].New
				switch mutation {
				case "demotion":
					// Reviewer's exact example: only these two fields change.
					next.Rule = previous.Rule
					next.ReachabilityPolicy, next.TestSymbol = "", ""
				case "declare-policy", "arm-policy":
					next.ReachabilityPolicy, next.TestSymbol = "", ""
				case "mixed":
					next.CoveredSymbols = []string{"fixture::Guard", "legacy"}
					next.ReachabilityPolicy, next.TestSymbol = "", ""
				case "legacy-test":
					next.CoveredSymbols = []string{"legacy"}
					next.ReachabilityPolicy = ""
				case "declared-test":
					next.TestSymbol = "fixture::TestRefreshSingleUse"
				case "wrong-test":
					next.TestSymbol = "fixture::OtherTest"
				case "unresolved":
					client.err = &reach.EvidenceError{Kind: reach.ErrorUnavailable, Message: "protected symbol resolution unavailable"}
				}
				set.Operations[0].NewFingerprints = fingerprints(next)
				bad := encodeOperationFixture(t, set)
				fragment := filepath.Join(repo, "operations.json")
				if err := os.WriteFile(fragment, bad, 0o644); err != nil {
					t.Fatal(err)
				}
				args := []string{command, "--repo", repo, "--operations", fragment, "--original-id", "baseline-1", "--source-id", "source-1", "--review-id", "review-1", "--authority", "authority-1"}
				var out, stderr bytes.Buffer
				if code := run(args, &out, &stderr); code == 0 {
					t.Fatal("reachability metadata bypass accepted")
				}
				after, err := os.ReadFile(ledger.Path(repo))
				if err != nil || !bytes.Equal(after, original) {
					t.Fatal("refusal changed ledger")
				}
				retained, err := os.ReadFile(fragment)
				if err != nil || !bytes.Equal(retained, bad) {
					t.Fatal("refusal consumed fragment")
				}
				client.err = nil
				if err := os.WriteFile(fragment, valid, 0o644); err != nil {
					t.Fatal(err)
				}
				out.Reset()
				stderr.Reset()
				if code := run(args, &out, &stderr); code != 0 {
					t.Fatalf("valid exact control refused: %s", stderr.String())
				}
			})
		}
	}
}

func TestOperationCLIDiscovery(t *testing.T) {
	for _, name := range []string{"ops-emit", "ops-validate", "ops-apply", "ops-overlay"} {
		var out, stderr bytes.Buffer
		if code := run([]string{name, "--help"}, &out, &stderr); code != 0 {
			t.Fatalf("%s help unavailable: %s", name, stderr.String())
		}
		if !strings.Contains(out.String(), name) {
			t.Fatalf("help missing %s", name)
		}
	}
}

func declaredOperationRow(id string) ledger.Row {
	return ledger.Row{ID: id, Rule: "The rule holds.", EnforcedBy: "-", Check: "NONE", RedProof: "-", Hash: "-", CoveredSymbols: []string{}}
}

func operationFixture(t *testing.T, id string) (string, operationSet) {
	t.Helper()
	repo := t.TempDir()
	old := []byte(ledger.Serialize(&ledger.Ledger{HasRedProofs: true}))
	if err := ledger.ReplaceExpected(repo, nil, old); err != nil {
		t.Fatal(err)
	}
	r := declaredOperationRow(id)
	set := operationSet{operationSchema, "baseline-1", string(old), digest(old), "source-1", "review-1", "authority-1", []floorOperation{{ID: id, Kind: "declare", New: r, NewFingerprints: fingerprints(&r)}}}
	return repo, set
}

func encodeOperationFixture(t *testing.T, set operationSet) []byte {
	t.Helper()
	data, err := json.Marshal(set, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func prepareOperationFixture(repo string, set operationSet) ([]byte, []byte, error) {
	return materializeOperations(repo, set, "baseline-1", "source-1", "review-1", "authority-1")
}

func TestOperationsClosedCodec(t *testing.T) {
	_, set := operationFixture(t, "R-1")
	valid := encodeOperationFixture(t, set)
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return b[:len(b)-1] },
		func(b []byte) []byte { return append(b, []byte("{}")...) },
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"schema_version":`), []byte(`"extra":true,"schema_version":`), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"schema_version":`), []byte(`"schema_version":"duplicate","schema_version":`), 1)
		},
		func(b []byte) []byte { return bytes.Replace(b, []byte(`"Hash":"-",`), nil, 1) },
		func(b []byte) []byte { return bytes.Replace(b, []byte(`"expected_old":null,`), nil, 1) },
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"authority":"authority-1"`), []byte(`"authority":null`), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"CoveredSymbols":[]`), []byte(`"CoveredSymbols":null`), 1)
		},
	} {
		if _, err := decodeOperations(mutate(bytes.Clone(valid))); err == nil {
			t.Fatal("invalid/partial document accepted")
		}
		if _, err := decodeOperations(valid); err != nil {
			t.Fatalf("valid control: %v", err)
		}
	}
}

func TestOperationsPartialConflictingAndFingerprintRefusals(t *testing.T) {
	for _, kind := range []string{"duplicate", "conflict", "fingerprint", "original", "identity", "binding", "noop", "delete"} {
		t.Run(kind, func(t *testing.T) {
			repo, set := operationFixture(t, "R-1")
			valid := encodeOperationFixture(t, set)
			original, err := os.ReadFile(ledger.Path(repo))
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "duplicate":
				set.Operations = append(set.Operations, set.Operations[0])
			case "conflict":
				other := set.Operations[0]
				other.New.Rule = "Other rule."
				set.Operations = append(set.Operations, other)
			case "fingerprint":
				set.Operations[0].NewFingerprints.Sentence = "altered"
			case "original":
				set.OriginalLedger += "altered"
			case "identity":
				set.SourceID = "other"
			case "binding":
				set.Operations[0].New.Check = "missing_test.go @ unit"
			case "noop":
				set.Operations = nil
			case "delete":
				set.Operations[0].Kind = "delete"
			}
			fragment := filepath.Join(repo, "operations.json")
			bad := encodeOperationFixture(t, set)
			if err := os.WriteFile(fragment, bad, 0o644); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			args := []string{"ops-apply", "--repo", repo, "--operations", fragment, "--original-id", "baseline-1", "--source-id", "source-1", "--review-id", "review-1", "--authority", "authority-1"}
			if code := run(args, &out, &stderr); code == 0 {
				t.Fatalf("%s accepted", kind)
			}
			after, err := os.ReadFile(ledger.Path(repo))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, original) {
				t.Fatal("partial set write")
			}
			retained, err := os.ReadFile(fragment)
			if err != nil || !bytes.Equal(retained, bad) {
				t.Fatal("fragment consumed")
			}
			if err := os.WriteFile(fragment, valid, 0o644); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			stderr.Reset()
			if code := run(args, &out, &stderr); code != 0 {
				t.Fatalf("valid control: %s", stderr.String())
			}
			if code := run(args, &out, &stderr); code == 0 {
				t.Fatal("replay/no-op accepted")
			}
		})
	}
}

func TestDisjointOperationsCommuteAndRatchet(t *testing.T) {
	var results [][]byte
	for _, ids := range [][]string{{"Z-1", "A-1"}, {"A-1", "Z-1"}} {
		repo, set := operationFixture(t, ids[0])
		second := set
		r := declaredOperationRow(ids[1])
		second.Operations = []floorOperation{{ID: r.ID, Kind: "declare", New: r, NewFingerprints: fingerprints(&r)}}
		for _, ops := range []operationSet{set, second} {
			old, next, err := prepareOperationFixture(repo, ops)
			if err != nil {
				t.Fatal(err)
			}
			if err := ledger.ReplaceExpected(repo, old, next); err != nil {
				t.Fatal(err)
			}
		}
		l, err := ledger.Load(repo)
		if err != nil {
			t.Fatal(err)
		}
		if l.Floor != 2 || l.RedProofs != 0 || len(l.Rows) != 2 {
			t.Fatal("ratchet lost or double increment")
		}
		results = append(results, l.OriginalBytes)
	}
	if !bytes.Equal(results[0], results[1]) {
		t.Fatal("disjoint additions do not commute")
	}
}

func TestOperationsOverlayRetainsCanonicalBytesAndFragment(t *testing.T) {
	repo, set := operationFixture(t, "R-1")
	fragment := filepath.Join(repo, "operations.json")
	data := encodeOperationFixture(t, set)
	if err := os.WriteFile(fragment, data, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(ledger.Path(repo))
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	args := []string{"ops-overlay", "--repo", repo, "--operations", fragment, "--original-id", "baseline-1", "--source-id", "source-1", "--review-id", "review-1", "--authority", "authority-1"}
	if code := run(args, &out, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	if !strings.Contains(out.String(), `"diagnostic_only":true`) || !strings.Contains(out.String(), `"accepted_evidence":false`) {
		t.Fatal("unmarked overlay")
	}
	after, err := os.ReadFile(ledger.Path(repo))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("overlay changed canonical ledger")
	}
	retained, err := os.ReadFile(fragment)
	if err != nil || !bytes.Equal(data, retained) {
		t.Fatal("overlay consumed fragment")
	}
	args = append(args, "--source-id", "other")
	if code := run(args, &out, &stderr); code == 0 {
		t.Fatal("duplicate identity flag accepted")
	}
}

func armedOperationFixture(t *testing.T, repo, id string) ledger.Row {
	t.Helper()
	file := strings.ToLower(id) + "_test.go"
	source := "package fixture\nimport \"testing\"\n// RULE: " + id + "\nfunc TestBound(t *testing.T) { if 1 != 1 { t.Fatal(\"bad\") } }\n"
	if err := os.WriteFile(filepath.Join(repo, file), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := resolveRef(repo, file, id, kindGoTest)
	if err != nil {
		t.Fatal(err)
	}
	r := declaredOperationRow(id)
	r.EnforcedBy = kindGoTest
	r.Check = file + " @ unit"
	r.Hash = ref.hash()
	r.RedProof = "seen red 2026-10-14: assertion inverted and restored"
	r.ExecutionPolicy = ledger.ExecutionExecute
	return r
}

func TestDisjointArmOperationsPreserveProofRatchet(t *testing.T) {
	for _, ids := range [][]string{{"A-1", "B-1"}, {"B-1", "A-1"}} {
		repo := t.TempDir()
		base := &ledger.Ledger{Floor: 2, HasRedProofs: true, Rows: []ledger.Row{declaredOperationRow("A-1"), declaredOperationRow("B-1")}}
		old := []byte(ledger.Serialize(base))
		if err := ledger.ReplaceExpected(repo, nil, old); err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			r := armedOperationFixture(t, repo, id)
			check, err := checkFileDigest(repo, &r)
			if err != nil {
				t.Fatal(err)
			}
			previous := base.Find(id)
			set := operationSet{operationSchema, "baseline-1", string(old), digest(old), "source-1", "review-1", "authority-1", []floorOperation{{ID: id, Kind: "arm", Old: previous, New: r, OldFingerprints: fingerprints(previous), NewFingerprints: fingerprints(&r), CheckSHA: check}}}
			before, next, err := prepareOperationFixture(repo, set)
			if err != nil {
				t.Fatal(err)
			}
			if err := ledger.ReplaceExpected(repo, before, next); err != nil {
				t.Fatal(err)
			}
		}
		l, err := ledger.Load(repo)
		if err != nil {
			t.Fatal(err)
		}
		if l.Floor != 2 || l.RedProofs != 2 || l.MeasuredRedProofs() != 2 {
			t.Fatal("lost proof raise or double increment")
		}
	}
}

func TestOperationsRefuseSkipNoopProofAndInvalidRowsHeaders(t *testing.T) {
	for _, kind := range []string{"skip", "source", "proof", "floor", "red", "row", "stale"} {
		t.Run(kind, func(t *testing.T) {
			repo, _ := operationFixture(t, "R-1")
			base := &ledger.Ledger{Floor: 1, HasRedProofs: true, Rows: []ledger.Row{declaredOperationRow("R-1")}}
			l, err := ledger.Load(repo)
			if err != nil {
				t.Fatal(err)
			}
			old := []byte(ledger.Serialize(base))
			if err := ledger.ReplaceExpected(repo, l.OriginalBytes, old); err != nil {
				t.Fatal(err)
			}
			r := armedOperationFixture(t, repo, "R-1")
			check, err := checkFileDigest(repo, &r)
			if err != nil {
				t.Fatal(err)
			}
			previous := base.Find("R-1")
			set := operationSet{operationSchema, "baseline-1", string(old), digest(old), "source-1", "review-1", "authority-1", []floorOperation{{ID: r.ID, Kind: "arm", Old: previous, New: r, OldFingerprints: fingerprints(previous), NewFingerprints: fingerprints(&r), CheckSHA: check}}}
			valid := encodeOperationFixture(t, set)
			switch kind {
			case "skip", "source":
				file := filepath.Join(repo, "r-1_test.go")
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "skip" {
					data = bytes.Replace(data, []byte("if 1 != 1"), []byte(`t.Skip("skip"); if 1 != 1`), 1)
				} else {
					data = append(data, []byte("// changed source\n")...)
				}
				if err := os.WriteFile(file, data, 0o644); err != nil {
					t.Fatal(err)
				}
				if kind == "skip" {
					ref, err := resolveRef(repo, "r-1_test.go", "R-1", kindGoTest)
					if err != nil {
						t.Fatal(err)
					}
					set.Operations[0].New.Hash = ref.hash()
					set.Operations[0].NewFingerprints = fingerprints(&set.Operations[0].New)
					set.Operations[0].CheckSHA = digest(data)
				}
			case "proof":
				set.Operations[0].New.RedProof = "-"
				set.Operations[0].NewFingerprints = fingerprints(&set.Operations[0].New)
			case "floor":
				if err := os.WriteFile(ledger.Path(repo), bytes.Replace(old, []byte("FLOOR: 1"), []byte("FLOOR: 2"), 1), 0o644); err != nil {
					t.Fatal(err)
				}
			case "red":
				if err := os.WriteFile(ledger.Path(repo), bytes.Replace(old, []byte("RED-PROOFS: 0"), []byte("RED-PROOFS: 1"), 1), 0o644); err != nil {
					t.Fatal(err)
				}
			case "row":
				set.Operations[0].New.Rule = "invalid | cell"
				set.Operations[0].NewFingerprints = fingerprints(&set.Operations[0].New)
			case "stale":
				l, err := ledger.Load(repo)
				if err != nil {
					t.Fatal(err)
				}
				l.Rows[0].Rule = "Other sentence."
				if err := ledger.Save(repo, l); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(ledger.Path(repo))
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = prepareOperationFixture(repo, set)
			if err == nil {
				t.Fatalf("%s accepted", kind)
			}
			after, err := os.ReadFile(ledger.Path(repo))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("refusal changed ledger")
			}
			// Restore only this fixture's deliberately invalid input, retaining the refusal assertion.
			if err := os.WriteFile(ledger.Path(repo), old, 0o644); err != nil {
				t.Fatal(err)
			}
			armedOperationFixture(t, repo, "R-1")
			control, err := decodeOperations(valid)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := prepareOperationFixture(repo, control); err != nil {
				t.Fatalf("valid control: %v", err)
			}
		})
	}
	old := armedOperationFixture(t, t.TempDir(), "R-1")
	if _, err := operationKind(&old, old); err == nil {
		t.Fatal("no-op rehash/proof accepted")
	}
	next := old
	next.RedProof = "seen red 2026-10-14: changed proof"
	if _, err := operationKind(&old, next); err == nil {
		t.Fatal("genuine proof replaced without supersession")
	}
}

func TestOperationsEmitApplyAndLegacyBytes(t *testing.T) {
	repo, _ := operationFixture(t, "R-1")
	old, err := os.ReadFile(ledger.Path(repo))
	if err != nil {
		t.Fatal(err)
	}
	// Noncanonical whitespace is accepted legacy syntax and must survive.
	old = bytes.Replace(old, []byte("FLOOR: 0\n"), []byte("  FLOOR: 0  \n\n"), 1)
	if err := os.WriteFile(ledger.Path(repo), old, 0o644); err != nil {
		t.Fatal(err)
	}
	baseline := filepath.Join(repo, "baseline.md")
	if err := os.WriteFile(baseline, old, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdDeclare(repo, "The rule holds.", "R-1", "", bindingWriteInput{}, &out); err != nil {
		t.Fatal(err)
	}
	var emitted, stderr bytes.Buffer
	if code := run([]string{"ops-emit", "--repo", repo, "--baseline", baseline, "--original-id", "baseline-1", "--source-id", "source-1", "--review-id", "review-1", "--authority", "authority-1"}, &emitted, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	if _, err := decodeOperations(emitted.Bytes()); err != nil {
		t.Fatalf("CLI emitted unusable operations: %v", err)
	}
	set, err := emitOperations(repo, baseline, "baseline-1", "source-1", "review-1", "authority-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Operations) != 1 || set.Operations[0].Kind != "declare" {
		t.Fatal("wrong emitted logical diff")
	}
	data := encodeOperationFixture(t, set)
	set, err = decodeOperations(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledger.Path(repo), old, 0o644); err != nil {
		t.Fatal(err)
	}
	before, next, err := prepareOperationFixture(repo, set)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(next, []byte("\n\nRED-PROOFS: 0\n\n| ID |")) {
		t.Fatal("unmodified legacy spacing changed")
	}
	if err := ledger.ReplaceExpected(repo, before, next); err != nil {
		t.Fatal(err)
	}
	l, err := ledger.Load(repo)
	if err != nil || l.Floor != 1 || len(l.Rows) != 1 {
		t.Fatalf("apply emitted set: %v", err)
	}
	if _, err := emitOperations(repo, ledger.Path(repo), "baseline-1", "source-1", "review-1", "authority-1"); err == nil {
		t.Fatal("no-op emission accepted")
	}
}

func TestOperationsProofSupersessionAndRehash(t *testing.T) {
	repo := t.TempDir()
	old := armedOperationFixture(t, repo, "R-1")
	proof, err := ledger.ParseProof("seen red 2026-10-14: extended assertion inverted and restored")
	if err != nil {
		t.Fatal(err)
	}
	previous, err := ledger.ParseProof(old.RedProof)
	if err != nil {
		t.Fatal(err)
	}
	proof, err = proof.Superseding(previous)
	if err != nil {
		t.Fatal(err)
	}
	next := old
	next.RedProof = proof.CanonicalText()
	if kind, err := operationKind(&old, next); err != nil || kind != "prove" {
		t.Fatalf("valid supersession: %s %v", kind, err)
	}
	next = old
	next.Hash = "000000000000"
	if kind, err := operationKind(&old, next); err != nil || kind != "rehash" {
		t.Fatalf("reviewed rehash kind: %s %v", kind, err)
	}
	next.RedProof = proof.CanonicalText()
	if _, err := operationKind(&old, next); err == nil {
		t.Fatal("combined proof/fingerprint change accepted")
	}
}

func TestOperationsAdoptLegacyHeaderAfterLeadingWhitespace(t *testing.T) {
	repo, set := operationFixture(t, "R-1")
	old := []byte("\n\n" + ledger.Serialize(&ledger.Ledger{}))
	if err := os.WriteFile(ledger.Path(repo), old, 0o644); err != nil {
		t.Fatal(err)
	}
	set.OriginalLedger = string(old)
	set.OriginalSHA = digest(old)
	before, next, err := prepareOperationFixture(repo, set)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ledger.Parse(string(next))
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.HasRedProofs || parsed.Floor != 1 || parsed.RedProofs != 0 || !bytes.HasPrefix(next, []byte("\n\n")) {
		t.Fatal("legacy header adoption changed valid bytes")
	}
	if err := ledger.ReplaceExpected(repo, before, next); err != nil {
		t.Fatal(err)
	}
}
