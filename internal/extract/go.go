package extract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

var (
	goTestNameRe = regexp.MustCompile(`^Test[A-Za-z0-9_]*$`)
	goRuleRe     = regexp.MustCompile(`^// RULE: (.*\S)\s*$`)
)

type goExtractor struct{}

func NewGo() Extractor { return goExtractor{} }

func (goExtractor) Kind() Kind { return KindGoTest }

func (goExtractor) SupportsFile(path string) bool { return strings.HasSuffix(path, "_test.go") }

func (e goExtractor) DiscoversFile(path string) bool { return e.SupportsFile(path) }

type parsedGo struct {
	src     string
	fset    *token.FileSet
	file    *ast.File
	markers []goMarker
}

type goMarker struct {
	id       string
	offset   int
	function *ast.FuncDecl
}

func parseGo(src string) (*parsedGo, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "rulefloor_test.go", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, &Error{Kind: ErrorCannotParse, Message: fmt.Sprintf("CANNOT-EVALUATE: cannot parse Go test file: %v", err)}
	}
	p := &parsedGo{src: src, fset: fset, file: file}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			match := goRuleRe.FindStringSubmatch(comment.Text)
			if match == nil || !standaloneGoComment(src, fset.Position(comment.Slash).Offset) {
				continue
			}
			marker := goMarker{id: strings.TrimSpace(match[1]), offset: fset.Position(comment.Slash).Offset}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !goTestNameRe.MatchString(fn.Name.Name) {
					continue
				}
				if fset.Position(fn.Pos()).Line == fset.Position(comment.End()).Line+1 {
					marker.function = fn
					break
				}
			}
			p.markers = append(p.markers, marker)
		}
	}
	return p, nil
}

func standaloneGoComment(src string, offset int) bool {
	line := strings.LastIndexByte(src[:offset], '\n') + 1
	return strings.TrimSpace(src[line:offset]) == ""
}

func (goExtractor) Discover(src string) ([]Tag, error) {
	p, err := parseGo(src)
	if err != nil {
		return nil, err
	}
	var tags []Tag
	for _, marker := range p.markers {
		if marker.function != nil {
			tags = append(tags, Tag{ID: marker.id, Offset: marker.offset})
		}
	}
	return tags, nil
}

func (goExtractor) Extract(src, id string) (*Ref, error) {
	p, err := parseGo(src)
	if err != nil {
		return nil, err
	}
	markerText := "// RULE: " + id
	var found []goMarker
	for _, marker := range p.markers {
		if marker.id != id {
			continue
		}
		if marker.function == nil {
			return nil, &Error{Kind: ErrorMisplaced, Message: fmt.Sprintf("tag %q is not directly above a test function", markerText)}
		}
		found = append(found, marker)
	}
	if len(found) == 0 {
		return nil, &Error{Kind: ErrorMissing, Message: fmt.Sprintf("tag %q not found", markerText)}
	}
	if len(found) > 1 {
		return nil, &Error{Kind: ErrorAmbiguous, Message: fmt.Sprintf("CANNOT-EVALUATE: tag %q appears %d times; it must be unique", markerText, len(found))}
	}
	fn := found[0].function
	start := p.fset.Position(fn.Pos()).Offset
	end := p.fset.Position(fn.End()).Offset
	if start < 0 || end <= start || end > len(src) {
		return nil, &Error{Kind: ErrorCannotParse, Message: fmt.Sprintf("CANNOT-EVALUATE: cannot calculate source span for %s", fn.Name.Name)}
	}
	return &Ref{
		Kind:     KindGoTest,
		Body:     src[start:end],
		FuncName: fn.Name.Name,
		GoSkips:  functionSkips(fn),
		Start:    start,
		End:      end,
	}, nil
}

func functionSkips(fn *ast.FuncDecl) bool {
	skips := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Skip", "Skipf", "SkipNow":
			skips = true
			return false
		}
		return true
	})
	return skips
}
