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
IMAGE      := g3
FULL_TAG   := $(REGISTRY)/$(IMAGE):$(VERSION)
WEB_IMAGE  := $(REGISTRY)/g3-web
WEB_TAG    ?= $(VERSION)
GODOC_PKGS := audit auth backend config server store telemetry

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
docker: ## Build Docker image for local architecture
	docker build --pull --build-arg VERSION=$(VERSION) -t $(FULL_TAG) .

.PHONY: push
push: ## Build and push multi-arch Docker image to registry
	docker buildx build \
	  --pull \
	  --platform linux/amd64,linux/arm64 \
	  --build-arg VERSION=$(VERSION) \
	  -t $(FULL_TAG) \
	  --output type=image,push=true \
	  .

# -------------------------------------------------------------------------
# PACKAGING
# -------------------------------------------------------------------------

.PHONY: deb
deb: prep-changelog ## Build Debian package via GoReleaser
	goreleaser release --snapshot --clean

.PHONY: prep-changelog
prep-changelog: ## Gzip changelog for Debian packaging
	gzip -9 -k -f packaging/changelog

APTLY_URL             ?= $(or $(APTLY_ENDPOINT),https://apt.munchbox.cc)
APTLY_REPO            ?= $(or $(APTLY_REPOSITORY),munchbox)
APTLY_USER            ?= admin
APTLY_PUBLISH_PREFIX  ?= $(or $(APTLY_PREFIX),s3:munchbox:)
APTLY_DISTRIBUTION    ?= stable
APTLY_ARCHITECTURES   ?= amd64,arm64
DEB_DIR               ?= dist
SNAPSHOT_NAME         ?= $(BINARY)-$(shell date +%Y%m%d-%H%M%S)

.PHONY: publish-deb
publish-deb: ## Upload, snapshot, and publish .deb packages to Aptly
	@if [ -z "$(APTLY_PASS)" ]; then echo "Error: APTLY_PASS not set (source munchbox-env.sh)"; exit 1; fi
	@echo "Publishing packages to $(APTLY_URL)..."
	@for deb in $(DEB_DIR)/*.deb; do \
		echo "Uploading $$(basename $$deb)..."; \
		curl -fsS -u "$(APTLY_USER):$(APTLY_PASS)" \
			-X POST -F "file=@$$deb" \
			"$(APTLY_URL)/api/files/$(BINARY)" || exit 1; \
	done
	@echo "Adding packages to repo $(APTLY_REPO)..."
	@curl -fsS -u "$(APTLY_USER):$(APTLY_PASS)" \
		-X POST "$(APTLY_URL)/api/repos/$(APTLY_REPO)/file/$(BINARY)?forceReplace=1" || exit 1
	@echo "Creating snapshot $(SNAPSHOT_NAME)..."
	@curl -fsS -u "$(APTLY_USER):$(APTLY_PASS)" \
		-X POST -H 'Content-Type: application/json' \
		-d '{"Name":"$(SNAPSHOT_NAME)"}' \
		"$(APTLY_URL)/api/repos/$(APTLY_REPO)/snapshots" || exit 1
	@echo "Updating published repo at $(APTLY_PUBLISH_PREFIX) ($(APTLY_DISTRIBUTION))..."
	@body=$$(mktemp); \
	status=$$(curl -sS -u "$(APTLY_USER):$(APTLY_PASS)" \
		-o "$$body" -w '%{http_code}' \
		-X PUT -H 'Content-Type: application/json' \
		-d '{"Snapshots":[{"Component":"main","Name":"$(SNAPSHOT_NAME)"}],"ForceOverwrite":true}' \
		'$(APTLY_URL)/api/publish/$(APTLY_PUBLISH_PREFIX)/$(APTLY_DISTRIBUTION)'); \
	if [ "$$status" = "200" ]; then \
		echo "Updated existing publication."; \
		rm -f "$$body"; \
	elif [ "$$status" = "404" ]; then \
		echo "No publication at $(APTLY_PUBLISH_PREFIX)/$(APTLY_DISTRIBUTION); bootstrapping..."; \
		rm -f "$$body"; \
		archs=$$(echo '$(APTLY_ARCHITECTURES)' | sed 's/,/","/g'); \
		curl -fsS -u "$(APTLY_USER):$(APTLY_PASS)" \
			-X POST -H 'Content-Type: application/json' \
			-d "{\"SourceKind\":\"snapshot\",\"Sources\":[{\"Component\":\"main\",\"Name\":\"$(SNAPSHOT_NAME)\"}],\"Architectures\":[\"$$archs\"],\"Distribution\":\"$(APTLY_DISTRIBUTION)\"}" \
			'$(APTLY_URL)/api/publish/$(APTLY_PUBLISH_PREFIX)' || exit 1; \
		echo "Bootstrapped publication."; \
	else \
		echo "Publish update failed: HTTP $$status"; \
		echo "Server response:"; cat "$$body"; echo; \
		rm -f "$$body"; \
		exit 1; \
	fi
	@echo "Cleaning up uploaded files..."
	@curl -fsS -u "$(APTLY_USER):$(APTLY_PASS)" \
		-X DELETE "$(APTLY_URL)/api/files/$(BINARY)" || true
	@echo "Published successfully!"

.PHONY: changelog
changelog: ## Generate CHANGELOG.md from git history
	git cliff -o CHANGELOG.md

.PHONY: release
release: ## Tag and push to trigger release workflow
	@test -n "$(VERSION)" || (echo "VERSION not set" && exit 1)
	git tag -a "$(VERSION)" -m "Release $(VERSION)"
	git push origin "$(VERSION)"

.PHONY: release-local
release-local: prep-changelog ## Dry-run GoReleaser locally
	goreleaser release --snapshot --clean --skip=publish

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
