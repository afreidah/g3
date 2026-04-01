# -------------------------------------------------------------------------------
# g3 - Build System
#
# Author: Alex Freidah
#
# Makefile targets for building, testing, linting, and packaging the g3
# S3-compatible gateway backed by Gmail.
# -------------------------------------------------------------------------------

BINARY     := g3
VERSION    := $(shell cat .version)
GO_LDFLAGS := -s -w -X github.com/afreidah/g3/internal/telemetry.Version=$(VERSION)
LINT_VER   := v2.10.1
REGISTRY   ?= registry.munchbox.cc
WEB_IMAGE  := $(REGISTRY)/g3-web
WEB_TAG    ?= $(VERSION)
GODOC_PKGS := audit auth backend config server telemetry

# -------------------------------------------------------------------------
# DEFAULT
# -------------------------------------------------------------------------

.PHONY: help
help: ## Display available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# -------------------------------------------------------------------------
# BUILD
# -------------------------------------------------------------------------

.PHONY: build
build: ## Compile the binary
	go build -ldflags "$(GO_LDFLAGS)" -o $(BINARY) ./cmd/g3

.PHONY: run
run: build ## Build and run the server
	./$(BINARY) -config config.yaml

# -------------------------------------------------------------------------
# QUALITY
# -------------------------------------------------------------------------

.PHONY: test
test: ## Run unit tests with race detection
	go test -race -cover ./...

.PHONY: vet
vet: ## Run static analysis
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: govulncheck
govulncheck: ## Run vulnerability scanner
	govulncheck ./...

.PHONY: check
check: vet lint test ## Run all quality checks

# -------------------------------------------------------------------------
# CODE GENERATION
# -------------------------------------------------------------------------

.PHONY: generate
generate: ## Run go generate for mocks
	go generate ./...

# -------------------------------------------------------------------------
# DOCKER
# -------------------------------------------------------------------------

.PHONY: docker
docker: ## Build Docker image
	docker build -t $(BINARY):$(VERSION) -t $(BINARY):latest .

.PHONY: push
push: ## Build and push multi-arch Docker image
	docker buildx build --platform linux/amd64,linux/arm64 \
		-t ghcr.io/afreidah/$(BINARY):$(VERSION) \
		-t ghcr.io/afreidah/$(BINARY):latest \
		--push .

# -------------------------------------------------------------------------
# PACKAGING
# -------------------------------------------------------------------------

.PHONY: deb
deb: ## Build Debian package via GoReleaser
	goreleaser release --snapshot --clean

# -------------------------------------------------------------------------
# TOOLS
# -------------------------------------------------------------------------

.PHONY: tools
tools: ## Install build dependencies
	go install go.uber.org/mock/mockgen@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VER)
	go install golang.org/x/vuln/cmd/govulncheck@latest

# -------------------------------------------------------------------------
# WEBSITE
# -------------------------------------------------------------------------

.PHONY: web-godoc
web-godoc: ## Generate Go API reference markdown for the website
	@mkdir -p web/content/godoc
	@for pkg in $(GODOC_PKGS); do \
		echo "  godoc: internal/$$pkg"; \
		printf -- '---\ntitle: "%s"\n---\n\n' "$$pkg" > web/content/godoc/$$pkg.md; \
		gomarkdoc ./internal/$$pkg >> web/content/godoc/$$pkg.md; \
		sed -i '/^# '"$$pkg"'$$/d' web/content/godoc/$$pkg.md; \
	done

.PHONY: web-serve
web-serve: web-godoc ## Serve the project website locally
	cd web && hugo serve

.PHONY: web-build
web-build: web-godoc ## Build the project website
	cd web && hugo --minify

.PHONY: web-docker
web-docker: ## Build website Docker image for local architecture
	docker build --pull -f web/Dockerfile -t $(WEB_IMAGE):$(WEB_TAG) .

.PHONY: web-push
web-push: ## Build and push multi-arch website image to registry
	docker buildx build \
	  --pull \
	  --platform linux/amd64,linux/arm64 \
	  -f web/Dockerfile \
	  -t $(WEB_IMAGE):$(WEB_TAG) \
	  --output type=image,push=true \
	  .

# -------------------------------------------------------------------------
# CLEANUP
# -------------------------------------------------------------------------

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf dist/
	rm -rf web/public/
