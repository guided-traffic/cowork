# Image URLs to use for building/pushing image targets
BACKEND_IMG ?= guidedtraffic/cowork-backend:latest
FRONTEND_IMG ?= guidedtraffic/cowork-frontend:latest

# Go commands
GOCMD = go
GOTEST = $(GOCMD) test
GOFMT = gofmt

# Build metadata baked into the backend binary (backend/cmd/cowork/main.go);
# both Containerfiles take the same three values as build arguments.
VERSION ?= dev
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date +%s)
LDFLAGS = -w -s -X main.version=$(VERSION) -X main.commit=$(GIT_COMMIT) -X main.buildTime=$(BUILD_TIME)

# Directories. Go runs inside BACKEND_DIR, npm inside FRONTEND_DIR; tools,
# binaries and coverage profiles land at the repository root.
BACKEND_DIR = backend
FRONTEND_DIR = frontend
COVERAGE_DIR = $(CURDIR)/coverage
BIN_DIR = $(CURDIR)/bin
HELM_CHART = deploy/helm/cowork

# Local PostgreSQL for the integration tests (make postgres-up / postgres-down).
# renovate: datasource=docker depName=postgres
POSTGRES_IMAGE ?= postgres:18
POSTGRES_CONTAINER ?= cowork-postgres
POSTGRES_PORT ?= 5432
TEST_DATABASE_URL ?= postgres://cowork:cowork@localhost:$(POSTGRES_PORT)/cowork?sslmode=disable

# Setting SHELL to bash allows bash commands like 'source' to be used
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

LOCALBIN ?= $(BIN_DIR)
## Tool Binaries
# Each path carries its tool's version, so a version bump is a missing file
# and installs itself instead of a stale unversioned binary surviving it.
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOCYCLO ?= $(LOCALBIN)/gocyclo-$(GOCYCLO_VERSION)
GOSEC ?= $(LOCALBIN)/gosec-$(GOSEC_VERSION)
GOVULNCHECK ?= $(LOCALBIN)/govulncheck-$(GOVULNCHECK_VERSION)

## Tool Versions
# renovate: datasource=go depName=github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.14.0
# renovate: datasource=go depName=github.com/fzipp/gocyclo/cmd/gocyclo
GOCYCLO_VERSION ?= v0.6.0
# renovate: datasource=go depName=github.com/securego/gosec/v2/cmd/gosec
GOSEC_VERSION ?= v2.29.0
# renovate: datasource=go depName=golang.org/x/vuln/cmd/govulncheck
GOVULNCHECK_VERSION ?= v1.8.0

# gosec bounds: the self-hosted runners share one machine across jobs.
GOSEC_CONCURRENCY ?= 4
GOSEC_MEMLIMIT ?= 1GiB

# Cyclomatic complexity threshold (recommended: 10-15)
CYCLO_THRESHOLD ?= 15

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-26s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Backend

.PHONY: fmt
fmt: ## Run gofmt against the backend.
	cd $(BACKEND_DIR) && $(GOFMT) -s -w cmd internal test

.PHONY: vet
vet: ## Run go vet against the backend, integration tests included.
	cd $(BACKEND_DIR) && $(GOCMD) vet ./... && $(GOCMD) vet -tags=integration ./test/...

.PHONY: lint
lint: golangci-lint ## Run the backend static analysis (vet, gofmt -l, golangci-lint).
	@echo "Running static analysis..."
	cd $(BACKEND_DIR) && $(GOCMD) vet ./...
	@cd $(BACKEND_DIR) && unformatted=$$($(GOFMT) -l cmd internal test); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	cd $(BACKEND_DIR) && $(GOLANGCI_LINT) run --timeout=5m
	cd $(BACKEND_DIR) && $(GOLANGCI_LINT) run --timeout=5m --build-tags=integration ./test/...

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint and apply the fixes it offers.
	cd $(BACKEND_DIR) && $(GOLANGCI_LINT) run --fix

.PHONY: cyclo
cyclo: $(GOCYCLO) ## Fail on any backend function above the cyclomatic complexity threshold.
	@echo "Running cyclomatic complexity analysis (threshold: $(CYCLO_THRESHOLD))..."
	@cd $(BACKEND_DIR) && $(GOCYCLO) -over $(CYCLO_THRESHOLD) -ignore "_test.go" cmd internal test && echo "All functions are below complexity threshold $(CYCLO_THRESHOLD)" || (echo "Functions above complexity threshold $(CYCLO_THRESHOLD) found!" && exit 1)

.PHONY: cyclo-report
cyclo-report: $(GOCYCLO) ## Show the 20 most complex backend functions, tests included.
	@cd $(BACKEND_DIR) && $(GOCYCLO) -top 20 cmd internal test

# No -short and no testing.Short() gates: a test that CI never runs is not a test.
.PHONY: test-unit
test-unit: ## Run the backend unit tests (no database needed).
	@echo "Running unit tests..."
	cd $(BACKEND_DIR) && $(GOTEST) -v -count=1 ./...

.PHONY: test-unit-coverage
test-unit-coverage: ## Run the backend unit tests with a coverage profile in coverage/unit.out.
	@echo "Running unit tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	cd $(BACKEND_DIR) && $(GOTEST) -v -count=1 -coverprofile=$(COVERAGE_DIR)/unit.out -covermode=atomic ./...

# The integration tests need PostgreSQL 18 at COWORK_TEST_DATABASE_URL; the
# variable defaults to the container that `make postgres-up` starts.
.PHONY: test-integration
test-integration: ## Run the backend integration tests against PostgreSQL (make postgres-up first).
	@echo "Running integration tests against $${COWORK_TEST_DATABASE_URL:-$(TEST_DATABASE_URL)}..."
	cd $(BACKEND_DIR) && COWORK_TEST_DATABASE_URL="$${COWORK_TEST_DATABASE_URL:-$(TEST_DATABASE_URL)}" $(GOTEST) -v -tags=integration -count=1 -timeout=10m ./test/integration/...

.PHONY: test-integration-coverage
test-integration-coverage: ## Run the backend integration tests with a coverage profile in coverage/integration.out.
	@echo "Running integration tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	cd $(BACKEND_DIR) && COWORK_TEST_DATABASE_URL="$${COWORK_TEST_DATABASE_URL:-$(TEST_DATABASE_URL)}" $(GOTEST) -v -tags=integration -count=1 -timeout=10m -coverprofile=$(COVERAGE_DIR)/integration.out -covermode=atomic -coverpkg=./... ./test/integration/...

.PHONY: gosec
gosec: $(GOSEC) ## Run the gosec security scan on the backend.
	cd $(BACKEND_DIR) && GOFLAGS="-buildvcs=false -p=$(GOSEC_CONCURRENCY)" GOMEMLIMIT=$(GOSEC_MEMLIMIT) $(GOSEC) -concurrency=$(GOSEC_CONCURRENCY) ./...

.PHONY: vuln
vuln: $(GOVULNCHECK) ## Check the backend dependencies for known vulnerabilities.
	cd $(BACKEND_DIR) && $(GOVULNCHECK) ./...

.PHONY: build-backend
build-backend: fmt vet ## Build bin/cowork.
	cd $(BACKEND_DIR) && CGO_ENABLED=0 $(GOCMD) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/cowork ./cmd/cowork

.PHONY: run
run: ## Run the backend from source against the local PostgreSQL (COWORK_DATABASE_URL overrides).
	cd $(BACKEND_DIR) && COWORK_DATABASE_URL="$${COWORK_DATABASE_URL:-$(TEST_DATABASE_URL)}" COWORK_LOG_FORMAT="$${COWORK_LOG_FORMAT:-text}" $(GOCMD) run -ldflags="$(LDFLAGS)" ./cmd/cowork serve

.PHONY: migrate
migrate: ## Apply the pending migrations to the local PostgreSQL (COWORK_DATABASE_URL overrides).
	cd $(BACKEND_DIR) && COWORK_DATABASE_URL="$${COWORK_DATABASE_URL:-$(TEST_DATABASE_URL)}" COWORK_LOG_FORMAT="$${COWORK_LOG_FORMAT:-text}" $(GOCMD) run ./cmd/cowork migrate

##@ Frontend

# npm ci runs only when the lockfile changed; npm writes this marker on success.
$(FRONTEND_DIR)/node_modules/.package-lock.json: $(FRONTEND_DIR)/package-lock.json
	cd $(FRONTEND_DIR) && npm ci --no-audit --no-fund

.PHONY: frontend-install
frontend-install: $(FRONTEND_DIR)/node_modules/.package-lock.json ## Install the frontend dependencies (npm ci, when the lockfile changed).

.PHONY: frontend-lint
frontend-lint: frontend-install ## Run ESLint on the frontend.
	cd $(FRONTEND_DIR) && npx ng lint

.PHONY: frontend-test
frontend-test: frontend-install ## Run the frontend unit tests once (vitest, jsdom).
	cd $(FRONTEND_DIR) && CI=true npx ng test --watch=false

.PHONY: frontend-test-coverage
frontend-test-coverage: frontend-install ## Run the frontend unit tests with coverage in frontend/coverage/.
	cd $(FRONTEND_DIR) && CI=true npx ng test --watch=false --coverage --coverage-reporters=text-summary --coverage-reporters=lcovonly --coverage-reporters=json-summary

.PHONY: frontend-build
frontend-build: frontend-install ## Build the frontend for production into frontend/dist/frontend/browser.
	cd $(FRONTEND_DIR) && npx ng build --configuration production

.PHONY: frontend-serve
frontend-serve: frontend-install ## Run the Angular dev server on :4200, proxying /api to the backend on :8080.
	cd $(FRONTEND_DIR) && npx ng serve

.PHONY: frontend-clean
frontend-clean: ## Remove the frontend build output.
	rm -rf $(FRONTEND_DIR)/dist

##@ Test

.PHONY: test
test: test-unit frontend-test ## Run the backend unit tests and the frontend tests.

.PHONY: test-release-tooling
test-release-tooling: ## Verify the semantic-release dependency set renders release notes (needs node+npm, run npm ci first).
	node hack/verify-release-tooling.mjs

.PHONY: postgres-up
postgres-up: ## Start a local PostgreSQL 18 container for the integration tests.
	@docker inspect $(POSTGRES_CONTAINER) >/dev/null 2>&1 && echo "$(POSTGRES_CONTAINER) already exists" || \
	    docker run -d --name $(POSTGRES_CONTAINER) -e POSTGRES_USER=cowork -e POSTGRES_PASSWORD=cowork -e POSTGRES_DB=cowork -p $(POSTGRES_PORT):5432 $(POSTGRES_IMAGE)
	@echo "Waiting for PostgreSQL..."
	@for i in $$(seq 1 30); do docker exec $(POSTGRES_CONTAINER) pg_isready -U cowork -d cowork >/dev/null 2>&1 && echo "PostgreSQL is ready on port $(POSTGRES_PORT)" && exit 0; sleep 1; done; echo "PostgreSQL did not become ready"; exit 1

.PHONY: postgres-down
postgres-down: ## Remove the local PostgreSQL container and its data.
	docker rm -f $(POSTGRES_CONTAINER) >/dev/null 2>&1 || true

.PHONY: coverage-merge
coverage-merge: ## Merge coverage/unit.out and coverage/integration.out into coverage/combined.out.
	@mkdir -p $(COVERAGE_DIR)
	@which gocovmerge > /dev/null || (echo "Installing gocovmerge..." && $(GOCMD) install github.com/wadey/gocovmerge@latest)
	gocovmerge $(COVERAGE_DIR)/unit.out $(COVERAGE_DIR)/integration.out > $(COVERAGE_DIR)/combined.out
	cd $(BACKEND_DIR) && $(GOCMD) tool cover -func=$(COVERAGE_DIR)/combined.out > $(COVERAGE_DIR)/combined.txt
	@echo "Combined coverage:"
	@grep "total:" $(COVERAGE_DIR)/combined.txt

.PHONY: coverage-json
coverage-json: ## Write the shields.io badge JSON from coverage/combined.txt.
	@mkdir -p .github/badges
	@COVERAGE=$$(grep "total:" $(COVERAGE_DIR)/combined.txt | awk '{print $$3}' | sed 's/%//'); \
	COLOR=$$(python3 -c "c=float('$$COVERAGE'); print('brightgreen' if c>=90 else 'green' if c>=80 else 'yellow' if c>=70 else 'orange' if c>=60 else 'red')"); \
	echo "{\"schemaVersion\":1,\"label\":\"coverage\",\"message\":\"$$COVERAGE%\",\"color\":\"$$COLOR\"}" > .github/badges/coverage.json
	@cat .github/badges/coverage.json

##@ Build

.PHONY: build
build: build-backend frontend-build ## Build bin/cowork and the frontend production bundle.

.PHONY: docker-build
docker-build: docker-build-backend docker-build-frontend ## Build both container images.

.PHONY: docker-build-backend
docker-build-backend: ## Build the backend image from backend/Containerfile.
	docker build -f $(BACKEND_DIR)/Containerfile --build-arg BUILD_NUMBER=$(VERSION) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t $(BACKEND_IMG) $(BACKEND_DIR)

.PHONY: docker-build-frontend
docker-build-frontend: ## Build the frontend image from frontend/Containerfile.
	docker build -f $(FRONTEND_DIR)/Containerfile --build-arg BUILD_NUMBER=$(VERSION) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t $(FRONTEND_IMG) $(FRONTEND_DIR)

.PHONY: docker-push
docker-push: ## Push both container images.
	docker push $(BACKEND_IMG)
	docker push $(FRONTEND_IMG)

##@ Helm

.PHONY: helm-lint
helm-lint: ## Lint the chart with every values file under deploy/helm/cowork/ci/.
	helm lint $(HELM_CHART) --strict
	@for f in $(HELM_CHART)/ci/*-values.yaml; do echo "helm lint with $$f"; helm lint $(HELM_CHART) --strict -f $$f; done

.PHONY: helm-template
helm-template: ## Render the chart with every values file under deploy/helm/cowork/ci/ and print nothing unless it fails.
	@for f in $(HELM_CHART)/ci/*-values.yaml; do echo "helm template with $$f"; helm template cowork $(HELM_CHART) -f $$f > /dev/null; done

##@ Dependencies

## Location to install dependencies to
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

$(GOCYCLO): $(LOCALBIN)
	$(call go-install-tool,$(GOCYCLO),github.com/fzipp/gocyclo/cmd/gocyclo,$(GOCYCLO_VERSION))

$(GOSEC): $(LOCALBIN)
	$(call go-install-tool,$(GOSEC),github.com/securego/gosec/v2/cmd/gosec,$(GOSEC_VERSION))

$(GOVULNCHECK): $(LOCALBIN)
	$(call go-install-tool,$(GOVULNCHECK),golang.org/x/vuln/cmd/govulncheck,$(GOVULNCHECK_VERSION))

# go-install-tool 'go install's a package into $(LOCALBIN) under the versioned
# path its variable names. $1 - target path, ending in -$3; $2 - package;
# $3 - version. go install names the binary after the package, so it installs
# into a directory of its own and is moved onto the versioned path from there.
# It runs from the backend module so the Go toolchain go.mod selects is used.
define go-install-tool
@[ -f $(1) ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -rf $(1).install ;\
cd $(BACKEND_DIR) && GOBIN=$(1).install go install $${package} ;\
mv $(1).install/$$(basename $(patsubst %-$(3),%,$(1))) $(1) ;\
rm -rf $(1).install ;\
}
endef
