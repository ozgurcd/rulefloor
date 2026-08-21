STATICCHECK_VERSION := v0.8.0
GOVULNCHECK_VERSION := v1.7.0

.PHONY: verify format-check install-tools

verify: format-check
	go version
	go run . check --repo .
	go build ./...
	go test ./... -count=1 -timeout=120s
	go vet ./...
	staticcheck ./...
	govulncheck ./...
	go mod tidy -diff

format-check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt required"; gofmt -l .; exit 1; }

install-tools:
	go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
