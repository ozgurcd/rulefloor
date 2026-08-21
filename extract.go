package main

import (
	"github.com/ozgurcd/rulefloor/internal/extract"
)

const (
	kindPlaywright = string(extract.KindPlaywright)
	kindGoTest     = string(extract.KindGoTest)
	kindVitest     = string(extract.KindVitest)
)

var extractorRegistry = extract.DefaultRegistry()

type testRef extract.Ref

func (r *testRef) hash() string { return (*extract.Ref)(r).Fingerprint() }

func kindForFile(path string) (string, error) {
	extractor, err := extractorRegistry.ForFile(path)
	if err != nil {
		return "", fatalf("%v", err)
	}
	return string(extractor.Kind()), nil
}

func extractTagged(src, id, kind string) (*testRef, error) {
	extractor, err := extractorRegistry.ForKind(extract.Kind(kind))
	if err != nil {
		return nil, fatalf("%v", err)
	}
	ref, err := extractor.Extract(src, id)
	if err != nil {
		return nil, extractionExitError(err)
	}
	return (*testRef)(ref), nil
}

func extractGo(src, id string) (*testRef, error) {
	return extractTagged(src, id, kindGoTest)
}

func extractPlaywright(src, id string) (*testRef, error) {
	return extractTagged(src, id, kindPlaywright)
}

func extractionExitError(err error) error {
	if extract.IsFatal(err) {
		return fatalf("%v", err)
	}
	return failf("%v", err)
}
