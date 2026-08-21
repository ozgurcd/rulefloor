package extract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
)

type Kind string

const (
	KindPlaywright Kind = "playwright"
	KindGoTest     Kind = "go-test"
	KindVitest     Kind = "vitest"
)

type ErrorKind string

const (
	ErrorMissing     ErrorKind = "missing"
	ErrorMisplaced   ErrorKind = "misplaced"
	ErrorAmbiguous   ErrorKind = "ambiguous"
	ErrorCannotParse ErrorKind = "cannot_parse"
)

type Error struct {
	Kind    ErrorKind
	Message string
}

func (e *Error) Error() string { return e.Message }

func IsFatal(err error) bool {
	e, ok := err.(*Error)
	return ok && (e.Kind == ErrorAmbiguous || e.Kind == ErrorCannotParse)
}

type Ref struct {
	Kind     Kind
	Body     string
	FuncName string
	Modifier string
	GoSkips  bool
	Start    int
	End      int
}

func (r *Ref) Fingerprint() string {
	sum := sha256.Sum256([]byte(r.Body))
	return hex.EncodeToString(sum[:])[:12]
}

type Tag struct {
	ID     string
	Offset int
}

type Extractor interface {
	Kind() Kind
	SupportsFile(path string) bool
	DiscoversFile(path string) bool
	Extract(src, id string) (*Ref, error)
	Discover(src string) ([]Tag, error)
}

type Registry struct {
	extractors []Extractor
}

func NewRegistry(extractors ...Extractor) *Registry {
	return &Registry{extractors: append([]Extractor(nil), extractors...)}
}

func DefaultRegistry() *Registry {
	return NewRegistry(NewGo(), NewJavaScript(KindPlaywright), NewJavaScript(KindVitest))
}

func (r *Registry) ForFile(path string) (Extractor, error) {
	clean := filepath.ToSlash(path)
	for _, extractor := range r.extractors {
		if extractor.SupportsFile(clean) {
			return extractor, nil
		}
	}
	return nil, fmt.Errorf("CANNOT-EVALUATE: %q is not a *_test.go, *.spec.ts, or *.test.ts file", path)
}

func (r *Registry) ForKind(kind Kind) (Extractor, error) {
	for _, extractor := range r.extractors {
		if extractor.Kind() == kind {
			return extractor, nil
		}
	}
	return nil, fmt.Errorf("CANNOT-EVALUATE: unsupported extractor kind %q", kind)
}

func (r *Registry) Extract(path, src, id string) (*Ref, error) {
	extractor, err := r.ForFile(path)
	if err != nil {
		return nil, err
	}
	return extractor.Extract(src, id)
}
