package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	kindPlaywright = "playwright"
	kindGoTest     = "go-test"
)

// kindForFile maps a check file to its test kind by suffix.
func kindForFile(path string) (string, error) {
	switch {
	case strings.HasSuffix(path, "_test.go"):
		return kindGoTest, nil
	case strings.HasSuffix(path, ".spec.ts"):
		return kindPlaywright, nil
	}
	return "", fatalf("CANNOT-EVALUATE: %q is neither a *_test.go nor a *.spec.ts file", path)
}

// testRef is one extracted tagged test.
type testRef struct {
	Kind     string
	Body     string // exact source span that gets hashed
	FuncName string // go-test only
	Modifier string // playwright only: "", "skip", "only", ...
	GoSkips  bool   // go-test only: body calls t.Skip/Skipf/SkipNow
}

func (r *testRef) hash() string {
	sum := sha256.Sum256([]byte(r.Body))
	return hex.EncodeToString(sum[:])[:12]
}

func extractTagged(src, id, kind string) (*testRef, error) {
	if kind == kindGoTest {
		return extractGo(src, id)
	}
	return extractPlaywright(src, id)
}

// ---- Playwright ----

var pwCallRe = regexp.MustCompile(`\b(test|it)(?:\.([A-Za-z]+))?\s*\(`)

type pwTest struct {
	Title    string
	Modifier string
	Start    int // offset of the start of the call's line
	End      int // offset just past the closing ')', -1 if unparseable
}

// pwScan finds every test/it (and test.describe etc.) call whose first
// argument is a string literal on heuristic, comment- and string-aware terms.
func pwScan(src string) []pwTest {
	var out []pwTest
	for _, m := range pwCallRe.FindAllStringSubmatchIndex(src, -1) {
		open := m[1] - 1 // the regex ends at '('
		mod := ""
		if m[4] >= 0 {
			mod = src[m[4]:m[5]]
		}
		title, ok := leadingString(src, open+1)
		if !ok {
			continue
		}
		end := -1
		if closeIdx, err := matchDelim(src, open, '(', ')', true); err == nil {
			end = closeIdx + 1
		}
		out = append(out, pwTest{Title: title, Modifier: mod, Start: lineStart(src, m[0]), End: end})
	}
	return out
}

func extractPlaywright(src, id string) (*testRef, error) {
	tag := "[" + id + "]"
	var found []pwTest
	for _, t := range pwScan(src) {
		if t.Modifier == "describe" {
			continue
		}
		if strings.Contains(t.Title, tag) {
			found = append(found, t)
		}
	}
	if len(found) == 0 {
		return nil, failf("tag %s not found in any test title", tag)
	}
	if len(found) > 1 {
		return nil, fatalf("CANNOT-EVALUATE: tag %s appears in %d test titles; it must be unique", tag, len(found))
	}
	t := found[0]
	if t.End < 0 {
		return nil, fatalf("CANNOT-EVALUATE: cannot parse the test call tagged %s (unbalanced delimiters)", tag)
	}
	return &testRef{Kind: kindPlaywright, Body: src[t.Start:t.End], Modifier: t.Modifier}, nil
}

// leadingString reads a ' " or ` string literal starting at the first
// non-whitespace byte at or after i.
func leadingString(src string, i int) (string, bool) {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	if i >= len(src) {
		return "", false
	}
	q := src[i]
	if q != '\'' && q != '"' && q != '`' {
		return "", false
	}
	var b strings.Builder
	for j := i + 1; j < len(src); j++ {
		c := src[j]
		if c == '\\' && j+1 < len(src) {
			b.WriteByte(src[j+1])
			j++
			continue
		}
		if c == q {
			return b.String(), true
		}
		b.WriteByte(c)
	}
	return "", false
}

// matchDelim finds the close matching the delimiter at src[open], skipping
// strings and // and /* */ comments. backtickEscapes is true for JS/TS
// (template literals honor backslash escapes) and false for Go raw strings.
func matchDelim(src string, open int, openCh, closeCh byte, backtickEscapes bool) (int, error) {
	depth := 0
	for i := open; i < len(src); i++ {
		c := src[i]
		switch c {
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					i++
				}
			} else if i+1 < len(src) && src[i+1] == '*' {
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return 0, errors.New("unterminated block comment")
				}
				i += 2 + end + 1
			}
		case '\'', '"', '`':
			esc := c != '`' || backtickEscapes
			j := i + 1
			for {
				if j >= len(src) {
					return 0, fmt.Errorf("unterminated string at offset %d", i)
				}
				if esc && src[j] == '\\' {
					j += 2
					continue
				}
				if src[j] == c {
					break
				}
				j++
			}
			i = j
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, errors.New("unbalanced delimiters")
}

func lineStart(src string, i int) int {
	return strings.LastIndexByte(src[:i], '\n') + 1
}

// ---- Go ----

var (
	goTestFuncRe = regexp.MustCompile(`^func (Test[A-Za-z0-9_]*)\s*\(`)
	goSkipRe     = regexp.MustCompile(`\.Skip(f|Now)?\s*\(`)
)

// extractGo finds the test function directly below the line "// RULE: <id>".
func extractGo(src, id string) (*testRef, error) {
	marker := "// RULE: " + id
	lines := strings.Split(src, "\n")
	offsets := make([]int, len(lines))
	off := 0
	for i, ln := range lines {
		offsets[i] = off
		off += len(ln) + 1
	}
	var refs []*testRef
	for i, ln := range lines {
		if strings.TrimSpace(ln) != marker {
			continue
		}
		if i+1 >= len(lines) {
			return nil, failf("tag %q is not directly above a test function", marker)
		}
		m := goTestFuncRe.FindStringSubmatch(lines[i+1])
		if m == nil {
			return nil, failf("tag %q is not directly above a \"func TestXxx\" line", marker)
		}
		funcStart := offsets[i+1]
		openBrace := strings.IndexByte(src[funcStart:], '{')
		if openBrace < 0 {
			return nil, fatalf("CANNOT-EVALUATE: no body found for %s", m[1])
		}
		closeIdx, err := matchDelim(src, funcStart+openBrace, '{', '}', false)
		if err != nil {
			return nil, fatalf("CANNOT-EVALUATE: cannot parse body of %s: %v", m[1], err)
		}
		body := src[funcStart : closeIdx+1]
		refs = append(refs, &testRef{
			Kind:     kindGoTest,
			Body:     body,
			FuncName: m[1],
			GoSkips:  goSkipRe.MatchString(body),
		})
	}
	if len(refs) == 0 {
		return nil, failf("tag %q not found", marker)
	}
	if len(refs) > 1 {
		return nil, fatalf("CANNOT-EVALUATE: tag %q appears %d times; it must be unique", marker, len(refs))
	}
	return refs[0], nil
}
