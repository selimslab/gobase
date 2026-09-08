# gobase — see `make help`.
.DEFAULT_GOAL := help

MODULE       := github.com/selimslab/gobase
BINARY       := server
BUILD_DIR    := bin
COVER_FILE   := coverage.out

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

VERSION_PKG := $(MODULE)/internal/platform/version
LDFLAGS     := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).BuildTime=$(BUILD_TIME)

# Tools are pinned by exact version, so every machine and CI run agree.
GOLANGCI_LINT_VERSION := v2.13.2
GOFUMPT_VERSION       := v0.12.0
GOVULNCHECK_VERSION   := v1.7.0
GITLEAKS_VERSION      := v8.30.1
LEFTHOOK_VERSION      := v2.1.12

GOBIN ?= $(shell go env GOPATH)/bin

DOCKER_IMAGE ?= gobase
DOCKER_TAG   ?= $(VERSION)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: tools
tools: ## Install the pinned dev tools into GOBIN
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	go install github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION)
	go install github.com/evilmartians/lefthook/v2@$(LEFTHOOK_VERSION)
	@echo "installed into $(GOBIN)"

.PHONY: fmt
fmt: ## Format the code
	$(GOBIN)/gofumpt -l -w .
	go mod tidy

.PHONY: lint
lint: ## Run the linters
	$(GOBIN)/golangci-lint run ./...

.PHONY: lint-fix
lint-fix: ## Run the linters and apply every fix they offer
	$(GOBIN)/golangci-lint run --fix ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: layers
layers: ## Verify the layering rules
	./scripts/check-layers.sh

.PHONY: test
test: ## Run the tests with the race detector
	go test -race -shuffle=on -covermode=atomic -coverprofile=$(COVER_FILE) ./...

.PHONY: cover
cover: test ## Run the tests and open the coverage report
	go tool cover -func=$(COVER_FILE) | tail -1
	go tool cover -html=$(COVER_FILE)

.PHONY: bench
bench: ## Run the benchmarks
	go test -run '^$$' -bench=. -benchmem ./...

.PHONY: build
build: ## Build the binary with the version stamped in
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/server

.PHONY: run
run: ## Run the service
	go run -ldflags "$(LDFLAGS)" ./cmd/server

.PHONY: docker-build
docker-build: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

.PHONY: docker-run
docker-run: ## Run the container image
	docker run --rm -p 8080:8080 \
		--read-only --cap-drop=ALL --security-opt=no-new-privileges \
		$(DOCKER_IMAGE):$(DOCKER_TAG)

.PHONY: sec
sec: ## Check for known vulnerabilities and leaked secrets
	go mod verify
	$(GOBIN)/govulncheck ./...
	$(GOBIN)/gitleaks dir . --redact --no-banner

.PHONY: hooks
hooks: ## Install the git hooks
	$(GOBIN)/lefthook install

.PHONY: ci
ci: fmt lint vet layers test sec ## Everything CI runs, locally

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf $(BUILD_DIR) $(COVER_FILE)
	go clean -testcache
