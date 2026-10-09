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

# The development and test containers publish their ports on this address only.
# Their passwords are development values and the PostgreSQL superuser is
# postgres/postgres, so they are bound to the loopback interface and are not
# reachable from the network the machine is on (docs/adr/0038 D4). A container
# created before this rule keeps its binding until it is removed and made
# again (make postgres-down minio-down dex-down, which drops its data).
CONTAINER_BIND ?= 127.0.0.1

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

# Local OpenID Connect issuer (make dex-up / dex-down) for make dev and the
# login tests: a plain Dex with the static client and users of
# hack/dex/config.yaml (docs/adr/0029 D3), pinned by digest. The file is
# copied into the container, not mounted: CI starts it on a self-hosted
# runner's Docker daemon, which need not see the job's files. Its issuer
# follows DEX_PORT. Every credential in it is a development value.
# renovate: datasource=docker depName=ghcr.io/dexidp/dex
DEX_IMAGE ?= ghcr.io/dexidp/dex:v2.45.1@sha256:8499afd690c437f52301efd2b05b2455da5bd2dfc20332cd697dc9937f808462
DEX_CONTAINER ?= cowork-dex
DEX_PORT ?= 5556
DEX_ISSUER = http://localhost:$(DEX_PORT)/dex
TEST_OIDC_ISSUER ?= $(DEX_ISSUER)

# The stand-in for the Ingress when the two images run together
# (hack/ingress/default.conf, docs/adr/0001 D3): the frontend's base image with
# the chart's path routing copied over its default server.
# renovate: datasource=docker depName=nginxinc/nginx-unprivileged
INGRESS_IMAGE ?= nginxinc/nginx-unprivileged:1.31-alpine

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
KUBECONFORM ?= $(LOCALBIN)/kubeconform-$(KUBECONFORM_VERSION)

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
# renovate: datasource=go depName=github.com/yannh/kubeconform/cmd/kubeconform
KUBECONFORM_VERSION ?= v0.8.0

# The operator releases the example manifests of deploy/examples/ are written
# against (docs/adr/0058 D1, D2): make examples-lint validates them against the
# CustomResourceDefinitions of exactly these releases, and fails while an
# example names another one, so a version Renovate moves is the moment to
# update the example or to say it is stale. The MinIO Operator is archived;
# v7.1.1 is its last release.
# renovate: datasource=github-releases depName=cloudnative-pg/cloudnative-pg
CNPG_VERSION ?= v1.30.1
# renovate: datasource=github-releases depName=minio/operator
MINIO_OPERATOR_VERSION ?= v7.1.1
EXAMPLES_DIR = deploy/examples
EXAMPLES_SCHEMAS = $(LOCALBIN)/examples-schemas

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
generate: $(SQLC) $(OAPI_CODEGEN) ## Regenerate the data layer (sqlc), the API code (oapi-codegen), the files generated from the problem catalogue and the chart's Grafana dashboard.
	cd $(BACKEND_DIR) && $(GOCMD) run ./tools/problemdoc
	cd $(BACKEND_DIR) && $(GOCMD) run ./tools/specbundle
	cd $(BACKEND_DIR) && $(GOCMD) run ./tools/dashboard
	cd $(BACKEND_DIR) && $(OAPI_CODEGEN) -config api/oapi-codegen.yaml api/openapi.gen.json
	cd $(BACKEND_DIR) && $(SQLC) generate

# The chart's Grafana dashboard is generated from backend/internal/metrics
# (docs/adr/0060 D3), so the drift check reads the chart's files too.
DASHBOARD_DIR = $(HELM_CHART)/files

.PHONY: generate-check
generate-check: generate ## Fail when a generated file differs from what make generate writes (docs/adr/0027 D1, docs/adr/0046 D2).
	@git diff --exit-code -- $(BACKEND_DIR) README.md $(DASHBOARD_DIR) || { echo "generated files are out of date: run make generate and commit the result"; exit 1; }
	@untracked=$$(git ls-files --others --exclude-standard -- $(BACKEND_DIR) $(DASHBOARD_DIR)); if [ -n "$$untracked" ]; then echo "make generate wrote untracked files:"; echo "$$untracked"; exit 1; fi

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

# The integration tests need PostgreSQL 18 at COWORK_TEST_DATABASE_URL, an
# S3-compatible server at COWORK_TEST_S3_* and an OpenID Connect issuer at
# COWORK_TEST_OIDC_ISSUER; the variables default to the containers
# `make postgres-up`, `make minio-up` and `make dex-up` start.
TEST_ENV = COWORK_TEST_DATABASE_URL="$${COWORK_TEST_DATABASE_URL:-$(TEST_DATABASE_URL)}" \
	COWORK_TEST_S3_ENDPOINT="$${COWORK_TEST_S3_ENDPOINT:-$(TEST_S3_ENDPOINT)}" \
	COWORK_TEST_S3_ACCESS_KEY_ID="$${COWORK_TEST_S3_ACCESS_KEY_ID:-$(MINIO_ACCESS_KEY)}" \
	COWORK_TEST_S3_SECRET_ACCESS_KEY="$${COWORK_TEST_S3_SECRET_ACCESS_KEY:-$(MINIO_SECRET_KEY)}" \
	COWORK_TEST_OIDC_ISSUER="$${COWORK_TEST_OIDC_ISSUER:-$(TEST_OIDC_ISSUER)}"

.PHONY: test-integration
test-integration: ## Run the backend integration tests against PostgreSQL, S3 and Dex (make dev-up first).
	@echo "Running integration tests against $${COWORK_TEST_DATABASE_URL:-$(TEST_DATABASE_URL)}, $${COWORK_TEST_S3_ENDPOINT:-$(TEST_S3_ENDPOINT)} and $${COWORK_TEST_OIDC_ISSUER:-$(TEST_OIDC_ISSUER)}..."
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

# The MCP server for Claude Code (docs/adr/0041): a static binary per
# platform. GOOS= and GOARCH= cross-compile, MCP_OUT= names the file; the
# release workflow builds every platform with them.
MCP_OUT ?= $(BIN_DIR)/cowork-mcp$(if $(filter windows,$(GOOS)),.exe,)

.PHONY: build-mcp
build-mcp: ## Build bin/cowork-mcp, the MCP server and hooks for Claude Code; GOOS= GOARCH= cross-compile, MCP_OUT= names the file.
	cd $(BACKEND_DIR) && CGO_ENABLED=0 $(if $(GOOS),GOOS=$(GOOS)) $(if $(GOARCH),GOARCH=$(GOARCH)) $(GOCMD) build -trimpath -ldflags="$(LDFLAGS)" -o $(MCP_OUT) ./cmd/cowork-mcp

.PHONY: run
run: ## Run the backend from source against the development database of make postgres-up; migrates on start as cowork_owner; a throw-away server key unless COWORK_SESSION_KEY is set.
	cd $(BACKEND_DIR) && COWORK_DATABASE_URL="$${COWORK_DATABASE_URL:-$(DEV_DATABASE_URL)}" COWORK_DATABASE_OWNER_URL="$${COWORK_DATABASE_OWNER_URL:-$(DEV_DATABASE_OWNER_URL)}" COWORK_SESSION_KEY="$${COWORK_SESSION_KEY:-$$(openssl rand -base64 32)}" COWORK_LOG_FORMAT="$${COWORK_LOG_FORMAT:-text}" $(GOCMD) run -ldflags="$(LDFLAGS)" ./cmd/cowork serve

.PHONY: migrate
migrate: ## Apply the pending migrations to the development database as cowork_owner.
	cd $(BACKEND_DIR) && COWORK_DATABASE_URL="$${COWORK_DATABASE_URL:-$(DEV_DATABASE_URL)}" COWORK_DATABASE_OWNER_URL="$${COWORK_DATABASE_OWNER_URL:-$(DEV_DATABASE_OWNER_URL)}" COWORK_LOG_FORMAT="$${COWORK_LOG_FORMAT:-text}" $(GOCMD) run ./cmd/cowork migrate

.PHONY: dev-seed
dev-seed: migrate ## Create a development person, tenant, admin membership and token, and print the token once (docs/adr/0038 D7).
	cd $(BACKEND_DIR) && COWORK_DEV_SEED_DATABASE_URL="$${COWORK_DEV_SEED_DATABASE_URL:-$(DEV_ADMIN_URL)}" $(GOCMD) run ./test/devseed

.PHONY: dev
dev: ## Run the whole development stack in this terminal to watch the UI: PostgreSQL, MinIO, Dex, the backend, demo data and the Angular dev server on https://localhost:4200 (hack/dev.sh).
	./hack/dev.sh

.PHONY: dev-reset
dev-reset: ## Drop the development database of make dev and its token; the next make dev seeds a fresh one with demo data.
	@docker exec $(POSTGRES_CONTAINER) psql -U postgres -q -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS cowork WITH (FORCE)" -c "CREATE DATABASE cowork OWNER cowork_owner"
	rm -f .dev/token
	@echo "the development database is empty; make dev seeds it again"

##@ Frontend

# The PrimeUI license key (docs/adr/0052 D9), from PRIMEUI_LICENSE or the untracked file
# .dev/primeui-license; without one the build works and shows PrimeNG's license notice.
PRIMEUI_DEFINE = $$(node scripts/primeui-define.mjs ../.dev/primeui-license)
PRIMEUI_SECRET = $$([ -f .dev/primeui-license ] && echo --secret id=primeui_license,src=.dev/primeui-license)

# npm ci runs only when the lockfile changed; npm writes this marker on success.
$(FRONTEND_DIR)/node_modules/.package-lock.json: $(FRONTEND_DIR)/package-lock.json
	cd $(FRONTEND_DIR) && npm ci --no-audit --no-fund

.PHONY: frontend-install
frontend-install: $(FRONTEND_DIR)/node_modules/.package-lock.json ## Install the frontend dependencies (npm ci, when the lockfile changed).

.PHONY: frontend-generate
frontend-generate: frontend-install ## Regenerate the Angular API client in frontend/src/app/api from backend/api/openapi.gen.json (docs/adr/0046 D3).
	cd $(FRONTEND_DIR) && npx ng-openapi-gen -c ng-openapi-gen.json

.PHONY: frontend-generate-check
frontend-generate-check: frontend-generate ## Fail when the generated Angular API client differs from what make frontend-generate writes.
	@git diff --exit-code -- $(FRONTEND_DIR)/src/app/api || { echo "frontend/src/app/api is stale: run make frontend-generate and commit the result"; exit 1; }
	@test -z "$$(git ls-files --others --exclude-standard -- $(FRONTEND_DIR)/src/app/api)" || { echo "untracked files in frontend/src/app/api: run make frontend-generate and commit the result"; exit 1; }

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
	cd $(FRONTEND_DIR) && npx ng build --configuration production $(PRIMEUI_DEFINE)

.PHONY: frontend-serve
frontend-serve: frontend-install ## Run the Angular dev server on :4200, proxying /api and /auth to the backend on :8080; NG_SERVE_FLAGS=--ssl serves HTTPS.
	cd $(FRONTEND_DIR) && npx ng serve $(PRIMEUI_DEFINE) $(NG_SERVE_FLAGS)

.PHONY: frontend-clean
frontend-clean: ## Remove the frontend build output.
	rm -rf $(FRONTEND_DIR)/dist

##@ Test

.PHONY: test
test: test-unit frontend-test ## Run the backend unit tests and the frontend tests.

.PHONY: test-release-tooling
test-release-tooling: ## Verify the semantic-release dependency set renders release notes (needs node+npm, run npm ci first).
	node hack/verify-release-tooling.mjs

# The end-to-end tier (docs/adr/0056, hack/e2e.sh): BACKEND_IMG and FRONTEND_IMG of one commit
# behind the Ingress stand-in with TLS on E2E_PORT, with a PostgreSQL, a MinIO and a Dex of its
# own (Dex on E2E_DEX_PORT), and the Playwright suite of frontend/e2e/ in Chromium and WebKit.
# E2E_ARGS reaches `playwright test`, e.g. E2E_ARGS="--project=chromium-dark board.spec.ts".
E2E_PORT ?= 18443
E2E_DEX_PORT ?= 5557
E2E_ENV = E2E_PORT=$(E2E_PORT) E2E_DEX_PORT=$(E2E_DEX_PORT) BACKEND_IMG=$(BACKEND_IMG) FRONTEND_IMG=$(FRONTEND_IMG) \
	INGRESS_IMAGE=$(INGRESS_IMAGE) POSTGRES_IMAGE=$(POSTGRES_IMAGE) MINIO_IMAGE=$(MINIO_IMAGE) DEX_IMAGE=$(DEX_IMAGE) \
	CONTAINER_BIND=$(CONTAINER_BIND)

.PHONY: e2e
e2e: frontend-install ## Run the end-to-end suite against the built images (make docker-build first) and remove its stack afterwards.
	$(E2E_ENV) hack/e2e.sh run $(E2E_ARGS)

.PHONY: e2e-up
e2e-up: ## Start the end-to-end stack and keep it, to run the suite against it while it is written (cd frontend && npx playwright test -c e2e).
	$(E2E_ENV) hack/e2e.sh up

.PHONY: e2e-down
e2e-down: ## Remove the end-to-end stack: its containers and its network.
	hack/e2e.sh down

.PHONY: e2e-browsers
e2e-browsers: frontend-install ## Install the browsers of the end-to-end suite, Chromium and WebKit; PLAYWRIGHT_INSTALL_FLAGS=--with-deps adds their system packages.
	cd $(FRONTEND_DIR) && npx playwright install $(PLAYWRIGHT_INSTALL_FLAGS) chromium webkit

.PHONY: postgres-up
postgres-up: ## Start a local PostgreSQL 18 container for the integration tests.
	@if docker inspect $(POSTGRES_CONTAINER) >/dev/null 2>&1; then echo "$(POSTGRES_CONTAINER) already exists" && docker start $(POSTGRES_CONTAINER) >/dev/null; else \
	    docker run -d --name $(POSTGRES_CONTAINER) -e POSTGRES_PASSWORD=postgres -p $(CONTAINER_BIND):$(POSTGRES_PORT):5432 $(POSTGRES_IMAGE); fi
	@echo "Waiting for PostgreSQL..."
	@for i in $$(seq 1 30); do docker exec $(POSTGRES_CONTAINER) pg_isready -U postgres >/dev/null 2>&1 && break; sleep 1; [ $$i -lt 30 ] || { echo "PostgreSQL did not become ready"; exit 1; }; done
	@docker exec $(POSTGRES_CONTAINER) psql -U postgres -q -v ON_ERROR_STOP=1 -c "DO \$$\$$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'cowork_owner') THEN CREATE ROLE cowork_owner LOGIN PASSWORD 'cowork_owner'; END IF; IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'cowork_app') THEN CREATE ROLE cowork_app LOGIN PASSWORD 'cowork_app'; END IF; END \$$\$$;"
	@docker exec $(POSTGRES_CONTAINER) psql -U postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'cowork'" | grep -q 1 || docker exec $(POSTGRES_CONTAINER) psql -U postgres -q -c "CREATE DATABASE cowork OWNER cowork_owner"
	@echo "PostgreSQL is ready on port $(POSTGRES_PORT): database cowork, owner role cowork_owner, runtime role cowork_app"

.PHONY: postgres-down
postgres-down: ## Remove the local PostgreSQL container and its data.
	docker rm -f -v $(POSTGRES_CONTAINER) >/dev/null 2>&1 || true

.PHONY: verify-phase-2
verify-phase-2: ## Verify phase 2 by hand: both built images behind the Ingress stand-in, against make postgres-up and make minio-up, driven by an agent token from make dev-seed.
	POSTGRES_CONTAINER=$(POSTGRES_CONTAINER) POSTGRES_PORT=$(POSTGRES_PORT) MINIO_PORT=$(MINIO_PORT) \
	MINIO_ACCESS_KEY=$(MINIO_ACCESS_KEY) MINIO_SECRET_KEY=$(MINIO_SECRET_KEY) \
	BACKEND_IMG=$(BACKEND_IMG) FRONTEND_IMG=$(FRONTEND_IMG) INGRESS_IMAGE=$(INGRESS_IMAGE) hack/verify-phase-2.sh

.PHONY: minio-up
minio-up: ## Start a local S3-compatible server (MinIO) for the attachment tests.
	@if docker inspect $(MINIO_CONTAINER) >/dev/null 2>&1; then echo "$(MINIO_CONTAINER) already exists" && docker start $(MINIO_CONTAINER) >/dev/null; else \
	    docker run -d --name $(MINIO_CONTAINER) -p $(CONTAINER_BIND):$(MINIO_PORT):9000 -e MINIO_ROOT_USER=$(MINIO_ACCESS_KEY) -e MINIO_ROOT_PASSWORD=$(MINIO_SECRET_KEY) $(MINIO_IMAGE) server /data; fi
	@echo "Waiting for MinIO..."
	@for i in $$(seq 1 30); do curl -sf http://localhost:$(MINIO_PORT)/minio/health/live >/dev/null && break; sleep 1; [ $$i -lt 30 ] || { echo "MinIO did not become ready"; exit 1; }; done
	@echo "MinIO is ready on port $(MINIO_PORT): access key $(MINIO_ACCESS_KEY); the tests create their own bucket"

.PHONY: minio-down
minio-down: ## Remove the local MinIO container and its data.
	docker rm -f $(MINIO_CONTAINER) >/dev/null 2>&1 || true

# The configuration goes in between docker create and docker start, through a
# copy readable by Dex's own user with the issuer moved to DEX_PORT.
.PHONY: dex-up
dex-up: ## Start a local Dex with the static client and users of hack/dex/config.yaml, the identity provider of make dev and the login tests.
	@if docker inspect $(DEX_CONTAINER) >/dev/null 2>&1; then echo "$(DEX_CONTAINER) already exists; make dex-down dex-up loads a changed hack/dex/config.yaml" && docker start $(DEX_CONTAINER) >/dev/null; else \
	    config=$$(mktemp) && trap 'rm -f "$$config"' EXIT && \
	    sed 's#http://localhost:5556/dex#$(DEX_ISSUER)#' hack/dex/config.yaml >"$$config" && chmod 644 "$$config" && \
	    docker create --name $(DEX_CONTAINER) -p $(CONTAINER_BIND):$(DEX_PORT):5556 $(DEX_IMAGE) dex serve /etc/dex/cowork.yaml >/dev/null && \
	    docker cp "$$config" $(DEX_CONTAINER):/etc/dex/cowork.yaml && \
	    docker start $(DEX_CONTAINER) >/dev/null; fi
	@echo "Waiting for Dex..."
	@for i in $$(seq 1 30); do curl -sf $(DEX_ISSUER)/.well-known/openid-configuration >/dev/null && break; sleep 1; [ $$i -lt 30 ] || { docker logs --tail 20 $(DEX_CONTAINER); echo "Dex did not become ready"; exit 1; }; done
	@echo "Dex is ready: issuer $(DEX_ISSUER), client cowork and the users of hack/dex/config.yaml, every credential development-only"

.PHONY: dex-down
dex-down: ## Remove the local Dex container; it keeps nothing, so the next one starts empty.
	docker rm -f $(DEX_CONTAINER) >/dev/null 2>&1 || true

.PHONY: dev-up
dev-up: postgres-up minio-up dex-up ## Start the containers make dev and the integration tests need: PostgreSQL, MinIO and Dex.

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
	docker build -f $(FRONTEND_DIR)/Containerfile $(PRIMEUI_SECRET) --build-arg BUILD_NUMBER=$(VERSION) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t $(FRONTEND_IMG) $(FRONTEND_DIR)

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
	@echo "helm template with the inline credentials: the release revision on the backend pods, no checksum of a credential"
	@out=$$(helm template cowork $(HELM_CHART) -f $(HELM_CHART)/ci/inline-url-values.yaml --show-only templates/backend-deployment.yaml) && \
	  echo "$$out" | grep -q 'cowork/inline-credentials-revision: "1"' && \
	  ! echo "$$out" | grep -q 'checksum/' || \
	  { echo "the backend pod template of the inline values must carry the release revision and no checksum of a credential (docs/adr/0058 D3)"; exit 1; }

# The CustomResourceDefinitions of the two releases are fetched at their tags
# and turned into the JSON schemas kubeconform reads (backend/tools/crdschema);
# the built-in kinds are checked against kubeconform's default schemas. Both
# need the network. It proves the examples parse, nothing more (docs/adr/0058 D2).
.PHONY: examples-lint
examples-lint: $(KUBECONFORM) ## Validate deploy/examples/ against the CRD schemas of the operator releases they name (syntax only).
	@grep -q "CloudNativePG $(CNPG_VERSION:v%=%)" $(EXAMPLES_DIR)/cloudnative-pg-cluster.yaml || { echo "$(EXAMPLES_DIR)/cloudnative-pg-cluster.yaml does not name CloudNativePG $(CNPG_VERSION:v%=%): update the example to that release"; exit 1; }
	@grep -q "MinIO Operator $(MINIO_OPERATOR_VERSION)" $(EXAMPLES_DIR)/minio-tenant.yaml || { echo "$(EXAMPLES_DIR)/minio-tenant.yaml does not name the MinIO Operator $(MINIO_OPERATOR_VERSION): update the example to that release"; exit 1; }
	@rm -rf $(EXAMPLES_SCHEMAS) && mkdir -p $(EXAMPLES_SCHEMAS)/crds
	curl -fsSL -o $(EXAMPLES_SCHEMAS)/crds/clusters.yaml https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/$(CNPG_VERSION)/config/crd/bases/postgresql.cnpg.io_clusters.yaml
	curl -fsSL -o $(EXAMPLES_SCHEMAS)/crds/tenants.yaml https://raw.githubusercontent.com/minio/operator/$(MINIO_OPERATOR_VERSION)/resources/base/crds/minio.min.io_tenants.yaml
	cd $(BACKEND_DIR) && $(GOCMD) run ./tools/crdschema -out $(EXAMPLES_SCHEMAS) $(EXAMPLES_SCHEMAS)/crds/clusters.yaml $(EXAMPLES_SCHEMAS)/crds/tenants.yaml
	$(KUBECONFORM) -strict -summary -schema-location default -schema-location '$(EXAMPLES_SCHEMAS)/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' $(EXAMPLES_DIR)/*.yaml
	@for f in $(EXAMPLES_DIR)/*.sh; do echo "sh -n $$f"; sh -n $$f; done

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

$(KUBECONFORM): $(LOCALBIN)
	$(call go-install-tool,$(KUBECONFORM),github.com/yannh/kubeconform/cmd/kubeconform,$(KUBECONFORM_VERSION))

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
