BINARY  := epm-cli
PKG     := ./cmd/epm-cli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/droidnik/epm-cli/internal/cli.Version=$(VERSION)

.PHONY: build install test vet spec generate docs snapshot image clean

build: ## Build ./bin/epm-cli for this machine
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

install: ## Install into $(go env GOPATH)/bin
	go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

test: ## Run the test suite
	go test -race ./...

vet:
	go vet ./...

spec: ## Re-download the OpenAPI spec from docs.beyondtrust.com and regenerate commands
	go run ./tools/specgen fetch -out spec/openapi-v3.json
	$(MAKE) generate

generate: ## Regenerate internal/spec/commands.json and docs/reference/ from spec/openapi-v3.json
	go run ./tools/specgen gen -spec spec/openapi-v3.json -out internal/spec/commands.json
	$(MAKE) docs

docs: ## Regenerate the command reference in docs/reference/
	go run ./tools/specgen docs -in internal/spec/commands.json -out docs/reference

snapshot: ## Build all release archives locally into dist/ (nothing is published)
	goreleaser release --snapshot --clean

image: ## Build the container image locally (needs docker)
	docker build -t epm-cli:dev --build-arg VERSION=$(VERSION) .

clean:
	rm -rf bin dist completions
