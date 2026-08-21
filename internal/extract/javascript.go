package extract

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	jsCallRe   = regexp.MustCompile(`\b(test|it)(?:\.([A-Za-z]+))?\s*\(`)
	titleTagRe = regexp.MustCompile(`\[([A-Z][A-Z0-9-]{0,30}[0-9])\]`)
)

type javascriptExtractor struct {
	kind   Kind
	suffix string
}

func NewJavaScript(kind Kind) Extractor {
	suffix := ".spec.ts"
	if kind == KindVitest {
		suffix = ".test.ts"
	}
	return javascriptExtractor{kind: kind, suffix: suffix}
}

func (e javascriptExtractor) Kind() Kind { return e.kind }

func (e javascriptExtractor) SupportsFile(path string) bool { return strings.HasSuffix(path, e.suffix) }

func (e javascriptExtractor) DiscoversFile(path string) bool {
	if !e.SupportsFile(path) {
		return false
	}
	return e.kind == KindPlaywright && strings.HasPrefix(path, "e2e/")
}

type jsTest struct {
	title    string
	modifier string
	start    int
	end      int
}

func (e javascriptExtractor) scan(src string) []jsTest {
	var out []jsTest
	for _, match := range jsCallRe.FindAllStringSubmatchIndex(src, -1) {
		open := match[1] - 1
		modifier := ""
		if match[4] >= 0 {
			modifier = src[match[4]:match[5]]
		}
		title, ok := leadingString(src, open+1)
		if !ok {
			continue
		}
		end := -1
		if closeIndex, err := matchDelimiter(src, open, '(', ')'); err == nil {
			end = closeIndex + 1
		}
		out = append(out, jsTest{title: title, modifier: modifier, start: lineStart(src, match[0]), end: end})
	}
	return out
}

func (e javascriptExtractor) Discover(src string) ([]Tag, error) {
	var tags []Tag
	for _, test := range e.scan(src) {
		if test.modifier == "describe" {
			continue
		}
		for _, match := range titleTagRe.FindAllStringSubmatch(test.title, -1) {
			tags = append(tags, Tag{ID: match[1], Offset: test.start})
		}
	}
	return tags, nil
}

func (e javascriptExtractor) Extract(src, id string) (*Ref, error) {
	tag := "[" + id + "]"
	var found []jsTest
	for _, test := range e.scan(src) {
		if test.modifier != "describe" && strings.Contains(test.title, tag) {
			found = append(found, test)
		}
	}
	if len(found) == 0 {
		return nil, &Error{Kind: ErrorMissing, Message: fmt.Sprintf("tag %s not found in any test title", tag)}
	}
	if len(found) > 1 {
		return nil, &Error{Kind: ErrorAmbiguous, Message: fmt.Sprintf("CANNOT-EVALUATE: tag %s appears in %d test titles; it must be unique", tag, len(found))}
	}
	test := found[0]
	if test.end < 0 {
		return nil, &Error{Kind: ErrorCannotParse, Message: fmt.Sprintf("CANNOT-EVALUATE: cannot parse the test call tagged %s (unbalanced delimiters)", tag)}
	}
	return &Ref{Kind: e.kind, Body: src[test.start:test.end], Modifier: test.modifier, Start: test.start, End: test.end}, nil
}

func leadingString(src string, i int) (string, bool) {
	for i < len(src) && strings.ContainsRune(" \t\n\r", rune(src[i])) {
		i++
	}
	if i >= len(src) || (src[i] != '\'' && src[i] != '"' && src[i] != '`') {
		return "", false
	}
	quote := src[i]
	var value strings.Builder
	for j := i + 1; j < len(src); j++ {
		if src[j] == '\\' && j+1 < len(src) {
			value.WriteByte(src[j+1])
			j++
			continue
		}
		if src[j] == quote {
			return value.String(), true
		}
		value.WriteByte(src[j])
	}
	return "", false
}

func matchDelimiter(src string, open int, openCh, closeCh byte) (int, error) {
	depth := 0
	var previous byte
	var word []byte
	for i := open; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return 0, errors.New("unterminated block comment")
			}
			i += 2 + end + 1
			continue
		case c == '/' && regexPosition(previous, word):
			if j, ok := skipRegexLiteral(src, i); ok {
				i = j
				previous = '/'
				word = word[:0]
				continue
			}
			previous = c
			word = word[:0]
			continue
		case c == '\'' || c == '"' || c == '`':
			j := i + 1
			for {
				if j >= len(src) {
					return 0, fmt.Errorf("unterminated string at offset %d", i)
				}
				if src[j] == '\\' {
					j += 2
					continue
				}
				if src[j] == c {
					break
				}
				j++
			}
			i = j
			previous = c
			word = word[:0]
			continue
		}
		switch c {
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return i, nil
			}
		}
		if strings.ContainsRune(" \t\n\r", rune(c)) {
			continue
		}
		previous = c
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			word = append(word, c)
		} else {
			word = word[:0]
		}
	}
	return 0, errors.New("unbalanced delimiters")
}

func regexPosition(previous byte, word []byte) bool {
	return previous == 0 || strings.IndexByte("(,=:!&|?;{[", previous) >= 0 || string(word) == "return"
}

func skipRegexLiteral(src string, open int) (int, bool) {
	inClass := false
	for j := open + 1; j < len(src); j++ {
		switch {
		case src[j] == '\\':
			j++
		case src[j] == '\n':
			return 0, false
		case inClass:
			if src[j] == ']' {
				inClass = false
			}
		case src[j] == '[':
			inClass = true
		case src[j] == '/':
			return j, true
		}
	}
	return 0, false
}

func lineStart(src string, i int) int { return strings.LastIndexByte(src[:i], '\n') + 1 }
