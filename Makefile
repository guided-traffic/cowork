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

# Local PostgreSQL (make postgres-up / postgres-down). The container's
# superuser is the administrative role: the integration tests create their own
# roles and database through it, and `make dev-seed` writes its fixture rows
# with it. `make postgres-up` also creates the development database `cowork`
# with its two roles (docs/adr/0021 D2): cowork_owner owns it and runs the
# migrations, cowork_app is the runtime role `make run` serves as. Every
# password here is a development value.
# renovate: datasource=docker depName=postgres
POSTGRES_IMAGE ?= postgres:18
POSTGRES_CONTAINER ?= cowork-postgres
POSTGRES_PORT ?= 5432
TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:$(POSTGRES_PORT)/postgres?sslmode=disable
DEV_DATABASE_URL ?= postgres://cowork_app:cowork_app@localhost:$(POSTGRES_PORT)/cowork?sslmode=disable
DEV_DATABASE_OWNER_URL ?= postgres://cowork_owner:cowork_owner@localhost:$(POSTGRES_PORT)/cowork?sslmode=disable
DEV_ADMIN_URL ?= postgres://postgres:postgres@localhost:$(POSTGRES_PORT)/cowork?sslmode=disable

# Local S3-compatible storage (make minio-up / minio-down) for the attachment
# tests: the MinIO build Chainguard publishes, pinned by digest. Its
# entrypoint is the minio binary without arguments, so `server /data` is its
# command. The keys are development values.
# renovate: datasource=docker depName=cgr.dev/chainguard/minio
MINIO_IMAGE ?= cgr.dev/chainguard/minio:latest@sha256:4cf4831a2bbcf13ddca09c1cbcc9faff716dd3c4247e0babc32864b8ee8e0034
MINIO_CONTAINER ?= cowork-minio
MINIO_PORT ?= 9000
MINIO_ACCESS_KEY ?= cowork
MINIO_SECRET_KEY ?= cowork-secret
TEST_S3_ENDPOINT ?= http://localhost:$(MINIO_PORT)

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
SQLC ?= $(LOCALBIN)/sqlc-$(SQLC_VERSION)
OAPI_CODEGEN ?= $(LOCALBIN)/oapi-codegen-$(OAPI_CODEGEN_VERSION)

## Tool Versions
# renovate: datasource=go depName=github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.14.0
# renovate: datasource=go depName=github.com/fzipp/gocyclo/cmd/gocyclo
GOCYCLO_VERSION ?= v0.6.0
# renovate: datasource=go depName=github.com/securego/gosec/v2/cmd/gosec
GOSEC_VERSION ?= v2.29.0
# renovate: datasource=go depName=golang.org/x/vuln/cmd/govulncheck
GOVULNCHECK_VERSION ?= v1.8.0
# renovate: datasource=go depName=github.com/sqlc-dev/sqlc/cmd/sqlc
SQLC_VERSION ?= v1.31.1
# renovate: datasource=go depName=github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
OAPI_CODEGEN_VERSION ?= v2.8.0

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

# Generated Go code is excluded from the complexity and security scans by
# name; it is checked by the drift check instead.
GENERATED_GO = _test.go|\.gen\.go|/readq/|/writeq/

.PHONY: generate
generate: $(SQLC) $(OAPI_CODEGEN) ## Regenerate the data layer (sqlc), the API code (oapi-codegen) and the files generated from the problem catalogue.
	cd $(BACKEND_DIR) && $(GOCMD) run ./tools/problemdoc
	cd $(BACKEND_DIR) && $(GOCMD) run ./tools/specbundle
	cd $(BACKEND_DIR) && $(OAPI_CODEGEN) -config api/oapi-codegen.yaml api/openapi.gen.json
	cd $(BACKEND_DIR) && $(SQLC) generate

.PHONY: generate-check
generate-check: generate ## Fail when a generated file differs from what make generate writes (docs/adr/0027 D1, docs/adr/0046 D2).
	@git diff --exit-code -- $(BACKEND_DIR) README.md || { echo "generated files are out of date: run make generate and commit the result"; exit 1; }
	@untracked=$$(git ls-files --others --exclude-standard -- $(BACKEND_DIR)); if [ -n "$$untracked" ]; then echo "make generate wrote untracked files:"; echo "$$untracked"; exit 1; fi

.PHONY: fmt
fmt: ## Run gofmt against the backend.
	cd $(BACKEND_DIR) && $(GOFMT) -s -w api cmd internal test tools

.PHONY: vet
vet: ## Run go vet against the backend, integration tests included.
	cd $(BACKEND_DIR) && $(GOCMD) vet ./... && $(GOCMD) vet -tags=integration ./test/...

.PHONY: lint
lint: golangci-lint $(SQLC) ## Run the backend static analysis (vet, gofmt -l, golangci-lint, sqlc compile).
	@echo "Running static analysis..."
	cd $(BACKEND_DIR) && $(GOCMD) vet ./...
	@cd $(BACKEND_DIR) && unformatted=$$($(GOFMT) -l api cmd internal test tools); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	cd $(BACKEND_DIR) && $(GOLANGCI_LINT) run --timeout=5m
	cd $(BACKEND_DIR) && $(GOLANGCI_LINT) run --timeout=5m --build-tags=integration ./test/...
	cd $(BACKEND_DIR) && $(SQLC) compile

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint and apply the fixes it offers.
	cd $(BACKEND_DIR) && $(GOLANGCI_LINT) run --fix

.PHONY: cyclo
cyclo: $(GOCYCLO) ## Fail on any backend function above the cyclomatic complexity threshold.
	@echo "Running cyclomatic complexity analysis (threshold: $(CYCLO_THRESHOLD))..."
	@cd $(BACKEND_DIR) && $(GOCYCLO) -over $(CYCLO_THRESHOLD) -ignore "$(GENERATED_GO)" api cmd internal test tools && echo "All functions are below complexity threshold $(CYCLO_THRESHOLD)" || (echo "Functions above complexity threshold $(CYCLO_THRESHOLD) found!" && exit 1)

.PHONY: cyclo-report
cyclo-report: $(GOCYCLO) ## Show the 20 most complex backend functions, tests included.
	@cd $(BACKEND_DIR) && $(GOCYCLO) -top 20 -ignore "$(GENERATED_GO)" api cmd internal test tools

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

# The integration tests need PostgreSQL 18 at COWORK_TEST_DATABASE_URL and an
# S3-compatible server at COWORK_TEST_S3_*; the variables default to the
# containers `make postgres-up` and `make minio-up` start.
TEST_ENV = COWORK_TEST_DATABASE_URL="$${COWORK_TEST_DATABASE_URL:-$(TEST_DATABASE_URL)}" \
	COWORK_TEST_S3_ENDPOINT="$${COWORK_TEST_S3_ENDPOINT:-$(TEST_S3_ENDPOINT)}" \
	COWORK_TEST_S3_ACCESS_KEY_ID="$${COWORK_TEST_S3_ACCESS_KEY_ID:-$(MINIO_ACCESS_KEY)}" \
	COWORK_TEST_S3_SECRET_ACCESS_KEY="$${COWORK_TEST_S3_SECRET_ACCESS_KEY:-$(MINIO_SECRET_KEY)}"

.PHONY: test-integration
test-integration: ## Run the backend integration tests against PostgreSQL and S3 (make postgres-up minio-up first).
	@echo "Running integration tests against $${COWORK_TEST_DATABASE_URL:-$(TEST_DATABASE_URL)} and $${COWORK_TEST_S3_ENDPOINT:-$(TEST_S3_ENDPOINT)}..."
	cd $(BACKEND_DIR) && $(TEST_ENV) $(GOTEST) -v -tags=integration -count=1 -timeout=10m ./test/integration/...

.PHONY: test-integration-coverage
test-integration-coverage: ## Run the backend integration tests with a coverage profile in coverage/integration.out.
	@echo "Running integration tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	cd $(BACKEND_DIR) && $(TEST_ENV) $(GOTEST) -v -tags=integration -count=1 -timeout=10m -coverprofile=$(COVERAGE_DIR)/integration.out -covermode=atomic -coverpkg=./... ./test/integration/...

.PHONY: gosec
gosec: $(GOSEC) ## Run the gosec security scan on the backend.
	cd $(BACKEND_DIR) && GOFLAGS="-buildvcs=false -p=$(GOSEC_CONCURRENCY)" GOMEMLIMIT=$(GOSEC_MEMLIMIT) $(GOSEC) -concurrency=$(GOSEC_CONCURRENCY) -exclude-generated ./...

.PHONY: vuln
vuln: $(GOVULNCHECK) ## Check the backend dependencies for known vulnerabilities.
	cd $(BACKEND_DIR) && $(GOVULNCHECK) ./...

.PHONY: build-backend
build-backend: fmt vet ## Build bin/cowork.
	cd $(BACKEND_DIR) && CGO_ENABLED=0 $(GOCMD) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/cowork ./cmd/cowork

.PHONY: run
run: ## Run the backend from source against the development database of make postgres-up; migrates on start as cowork_owner; a throw-away server key unless COWORK_SESSION_KEY is set.
	cd $(BACKEND_DIR) && COWORK_DATABASE_URL="$${COWORK_DATABASE_URL:-$(DEV_DATABASE_URL)}" COWORK_DATABASE_OWNER_URL="$${COWORK_DATABASE_OWNER_URL:-$(DEV_DATABASE_OWNER_URL)}" COWORK_SESSION_KEY="$${COWORK_SESSION_KEY:-$$(openssl rand -base64 32)}" COWORK_LOG_FORMAT="$${COWORK_LOG_FORMAT:-text}" $(GOCMD) run -ldflags="$(LDFLAGS)" ./cmd/cowork serve

.PHONY: migrate
migrate: ## Apply the pending migrations to the development database as cowork_owner.
	cd $(BACKEND_DIR) && COWORK_DATABASE_URL="$${COWORK_DATABASE_URL:-$(DEV_DATABASE_URL)}" COWORK_DATABASE_OWNER_URL="$${COWORK_DATABASE_OWNER_URL:-$(DEV_DATABASE_OWNER_URL)}" COWORK_LOG_FORMAT="$${COWORK_LOG_FORMAT:-text}" $(GOCMD) run ./cmd/cowork migrate

.PHONY: dev-seed
dev-seed: migrate ## Create a development person, tenant, admin membership and token, and print the token once (docs/adr/0038 D7).
	cd $(BACKEND_DIR) && COWORK_DEV_SEED_DATABASE_URL="$${COWORK_DEV_SEED_DATABASE_URL:-$(DEV_ADMIN_URL)}" $(GOCMD) run ./test/devseed

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
frontend-test-coverage: frontend-install ## Run the frontend unit tests with coverage in frontend/coverage/frontend/.
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
	    docker run -d --name $(POSTGRES_CONTAINER) -e POSTGRES_PASSWORD=postgres -p $(POSTGRES_PORT):5432 $(POSTGRES_IMAGE)
	@echo "Waiting for PostgreSQL..."
	@for i in $$(seq 1 30); do docker exec $(POSTGRES_CONTAINER) pg_isready -U postgres >/dev/null 2>&1 && break; sleep 1; [ $$i -lt 30 ] || { echo "PostgreSQL did not become ready"; exit 1; }; done
	@docker exec $(POSTGRES_CONTAINER) psql -U postgres -q -v ON_ERROR_STOP=1 -c "DO \$$\$$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'cowork_owner') THEN CREATE ROLE cowork_owner LOGIN PASSWORD 'cowork_owner'; END IF; IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'cowork_app') THEN CREATE ROLE cowork_app LOGIN PASSWORD 'cowork_app'; END IF; END \$$\$$;"
	@docker exec $(POSTGRES_CONTAINER) psql -U postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'cowork'" | grep -q 1 || docker exec $(POSTGRES_CONTAINER) psql -U postgres -q -c "CREATE DATABASE cowork OWNER cowork_owner"
	@echo "PostgreSQL is ready on port $(POSTGRES_PORT): database cowork, owner role cowork_owner, runtime role cowork_app"

.PHONY: postgres-down
postgres-down: ## Remove the local PostgreSQL container and its data.
	docker rm -f $(POSTGRES_CONTAINER) >/dev/null 2>&1 || true

.PHONY: verify-phase-2
verify-phase-2: ## Verify phase 2 by hand: both built images against make postgres-up and make minio-up, driven by an agent token from make dev-seed.
	POSTGRES_CONTAINER=$(POSTGRES_CONTAINER) POSTGRES_PORT=$(POSTGRES_PORT) MINIO_PORT=$(MINIO_PORT) \
	MINIO_ACCESS_KEY=$(MINIO_ACCESS_KEY) MINIO_SECRET_KEY=$(MINIO_SECRET_KEY) \
	BACKEND_IMG=$(BACKEND_IMG) FRONTEND_IMG=$(FRONTEND_IMG) hack/verify-phase-2.sh

.PHONY: minio-up
minio-up: ## Start a local S3-compatible server (MinIO) for the attachment tests.
	@docker inspect $(MINIO_CONTAINER) >/dev/null 2>&1 && echo "$(MINIO_CONTAINER) already exists" || \
	    docker run -d --name $(MINIO_CONTAINER) -p $(MINIO_PORT):9000 -e MINIO_ROOT_USER=$(MINIO_ACCESS_KEY) -e MINIO_ROOT_PASSWORD=$(MINIO_SECRET_KEY) $(MINIO_IMAGE) server /data
	@echo "Waiting for MinIO..."
	@for i in $$(seq 1 30); do curl -sf http://localhost:$(MINIO_PORT)/minio/health/live >/dev/null && break; sleep 1; [ $$i -lt 30 ] || { echo "MinIO did not become ready"; exit 1; }; done
	@echo "MinIO is ready on port $(MINIO_PORT): access key $(MINIO_ACCESS_KEY); the tests create their own bucket"

.PHONY: minio-down
minio-down: ## Remove the local MinIO container and its data.
	docker rm -f $(MINIO_CONTAINER) >/dev/null 2>&1 || true

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

$(SQLC): $(LOCALBIN)
	$(call go-install-tool,$(SQLC),github.com/sqlc-dev/sqlc/cmd/sqlc,$(SQLC_VERSION))

$(OAPI_CODEGEN): $(LOCALBIN)
	$(call go-install-tool,$(OAPI_CODEGEN),github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen,$(OAPI_CODEGEN_VERSION))

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
