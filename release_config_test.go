package main

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseConfigurationShipsSupportedBinaries(t *testing.T) {
	config, err := os.ReadFile(".goreleaser.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	for _, required := range []string{
		"CGO_ENABLED=0",
		"- linux",
		"- darwin",
		"- amd64",
		"- arm64",
		"-X main.version=v{{ .Version }}",
		"proxy: true",
		"name_template: checksums.txt",
	} {
		if !strings.Contains(text, required) {
			t.Errorf(".goreleaser.yml missing %q", required)
		}
	}
	if strings.Contains(text, "windows") {
		t.Error(".goreleaser.yml unexpectedly includes Windows")
	}

	workflow, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflowText := string(workflow)
	for _, required := range []string{"tags:", "- 'v*'", "contents: write", "goreleaser/goreleaser-action@v6", "version: v2.18.0", "release --clean"} {
		if !strings.Contains(workflowText, required) {
			t.Errorf("release workflow missing %q", required)
		}
	}
}
