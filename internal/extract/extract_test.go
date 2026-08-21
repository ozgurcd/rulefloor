package extract

import (
	"errors"
	"strings"
	"testing"
)

func TestGoDiscoveryIgnoresStringFixturesAndCommentExamples(t *testing.T) {
	src := "package sample\n\n" +
		"const raw = `// RULE: RAW-1`\n" +
		"const interpreted = \"// RULE: STRING-1\"\n" +
		"/* // RULE: BLOCK-1 */\n" +
		"// Example text: // RULE: DOC-1\n" +
		"func helper() {}\n\n" +
		"// RULE: REAL-1\n" +
		"func TestReal(t *testing.T) {}\n"

	tags, err := NewGo().Discover(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].ID != "REAL-1" {
		t.Fatalf("discovered tags = %+v, want REAL-1 only", tags)
	}
	for _, id := range []string{"RAW-1", "STRING-1", "BLOCK-1", "DOC-1"} {
		if _, err := NewGo().Extract(src, id); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("extract %s error = %v, want not found", id, err)
		}
	}
}

func TestGoMarkerPlacementRemainsEnforced(t *testing.T) {
	src := "package sample\n\n// RULE: REAL-1\n\nfunc TestReal(t *testing.T) {}\n"
	_, err := NewGo().Extract(src, "REAL-1")
	var extractionErr *Error
	if !errors.As(err, &extractionErr) || extractionErr.Kind != ErrorMisplaced {
		t.Fatalf("error = %v, want misplaced extractor error", err)
	}
	tags, err := NewGo().Discover(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("misplaced marker was discovered as a bound tag: %+v", tags)
	}
}

func TestGoDuplicateRealTagsAreFatal(t *testing.T) {
	src := "package sample\n\n// RULE: DUP-1\nfunc TestOne(t *testing.T) {}\n\n// RULE: DUP-1\nfunc TestTwo(t *testing.T) {}\n"
	_, err := NewGo().Extract(src, "DUP-1")
	var extractionErr *Error
	if !errors.As(err, &extractionErr) || extractionErr.Kind != ErrorAmbiguous || !IsFatal(err) {
		t.Fatalf("error = %v, want fatal ambiguous extractor error", err)
	}
}

func TestGoSpanSkipAndFingerprintUseParsedFunction(t *testing.T) {
	src := "package sample\n\n// RULE: REAL-1\nfunc TestReal(t *testing.T) {\n\tconst text = `}`\n\tt.Skipf(\"later: %s\", text)\n}\n"
	ref, err := NewGo().Extract(src, "REAL-1")
	if err != nil {
		t.Fatal(err)
	}
	if ref.FuncName != "TestReal" || !ref.GoSkips || !strings.HasPrefix(ref.Body, "func TestReal") || !strings.HasSuffix(ref.Body, "}") {
		t.Fatalf("ref = %+v", ref)
	}
	if len(ref.Fingerprint()) != 12 {
		t.Fatalf("fingerprint = %q", ref.Fingerprint())
	}
}

func TestPlaywrightAndVitestExtractionRemainCompatible(t *testing.T) {
	tests := []struct {
		name       string
		kind       Kind
		source     string
		id         string
		modifier   string
		wantPrefix string
	}{
		{"playwright", KindPlaywright, "test.skip('browser rule [PW-1]', async () => { expect(1).toBe(1) });\n", "PW-1", "skip", "test.skip"},
		{"vitest", KindVitest, "it.only(\"mapping rule [VT-1]\", () => { expect(1).toBe(1) });\n", "VT-1", "only", "it.only"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			extractor := NewJavaScript(tc.kind)
			ref, err := extractor.Extract(tc.source, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if ref.Kind != tc.kind || ref.Modifier != tc.modifier || !strings.HasPrefix(ref.Body, tc.wantPrefix) {
				t.Fatalf("ref = %+v", ref)
			}
			tags, err := extractor.Discover(tc.source)
			if err != nil || len(tags) != 1 || tags[0].ID != tc.id {
				t.Fatalf("tags = %+v, err = %v", tags, err)
			}
		})
	}
}
