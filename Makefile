STATICCHECK_VERSION := v0.8.1
GOVULNCHECK_VERSION := v1.7.0
STATICCHECK_TAG := 2026.2.1
STATICCHECK_SOURCE_SHA256 := 8d807cd909f4481d6777f7707e5ae75dcc399e14d68ff14a3c814731826e0dfc
STATICCHECK_PATCH1 := 4ff8b865d12b49f3af67daf2294023336987dad5
STATICCHECK_PATCH1_SHA256 := 19f123d3f405f779e82a739d3d6e7e3b289659b496ac82bd699586465b06ecc4
STATICCHECK_PATCH2 := 01bcfe17fb93153091b35df4036a1d73087817ab
STATICCHECK_PATCH2_SHA256 := 52ce80f83d597020938bb7c463070f3c133802995029faeb632e3fc385fb4d8a


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
	@set -eu; export STATICCHECK_TAG="$(STATICCHECK_TAG)"; export STATICCHECK_SOURCE_SHA256="$(STATICCHECK_SOURCE_SHA256)"; export STATICCHECK_PATCH1="$(STATICCHECK_PATCH1)"; export STATICCHECK_PATCH1_SHA256="$(STATICCHECK_PATCH1_SHA256)"; export STATICCHECK_PATCH2="$(STATICCHECK_PATCH2)"; export STATICCHECK_PATCH2_SHA256="$(STATICCHECK_PATCH2_SHA256)"; export STATICCHECK_VERSION="$(STATICCHECK_VERSION)"; \
	src="$(CURDIR)/.git/achta/staticcheck-src" ; \
	mkdir -p "$$src" ; \
	curl -fsSL -o "$$src/src.tar.gz" "https://github.com/dominikh/go-tools/archive/refs/tags/$${STATICCHECK_TAG}.tar.gz" ; \
	curl -fsSL -o "$$src/p1.patch" "https://github.com/dominikh/go-tools/commit/$${STATICCHECK_PATCH1}.patch?full_index=1" ; \
	curl -fsSL -o "$$src/p2.patch" "https://github.com/dominikh/go-tools/commit/$${STATICCHECK_PATCH2}.patch?full_index=1" ; \
	printf '%s  %s\n' "$$STATICCHECK_SOURCE_SHA256" "$$src/src.tar.gz" "$$STATICCHECK_PATCH1_SHA256" "$$src/p1.patch" "$$STATICCHECK_PATCH2_SHA256" "$$src/p2.patch" | sha256sum -c - ; \
	tar -xzf "$$src/src.tar.gz" -C "$$src" ; \
	patch -d "$$src/go-tools-$${STATICCHECK_TAG}" -p1 < "$$src/p1.patch" ; \
	patch -d "$$src/go-tools-$${STATICCHECK_TAG}" -p1 < "$$src/p2.patch" ; \
	(cd "$$src/go-tools-$${STATICCHECK_TAG}" && go build -trimpath -o "$$(go env GOPATH)/bin/staticcheck" ./cmd/staticcheck) ; \
	staticcheck --version ; \
	staticcheck --version | grep -F "staticcheck $${STATICCHECK_TAG} ($${STATICCHECK_VERSION#v})"
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
