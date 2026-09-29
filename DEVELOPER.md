# Developer guide

The contributor entry point: what the repository looks like, how to build, test and lint it,
how CI and the release work, how to add things, and the conventions. How the code works is in
[docs/developer/](docs/developer/README.md); why it works that way is in
[docs/adr/](docs/adr/README.md); this file does not repeat either.

## What has to be in your head first

- **Two containers, one origin.** The Go backend in `backend/` serves the API and migrates
  the schema on start; the nginx frontend in `frontend/` serves the Angular bundle and proxies
  `/api/` to the backend
  ([ADR 0001](docs/adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)).
- **`make` is the entry point**, from the repository root. CI runs Makefile targets; so do
  you ([ADR 0003](docs/adr/0003-test-and-ci-policy.md) D1). Go targets `cd backend`, npm
  targets `cd frontend`. `make help` lists them.
- **Nothing is skipped.** No `-short`, no `testing.Short()`, no "skip when the database is
  missing". The integration tier fails and tells you how to start the database.
- **Newest toolchains.** Go 1.27 and Angular 22 today, moved by Renovate; a lagging version
  is a defect (ADR 0001 D9).
- **A statement has one home.** Decision → ADR; work → ticket; how → `docs/developer/`; run →
  `docs/operations/`; threat → `docs/security/`; reference tables → `README.md`
  ([ADR 0002](docs/adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)).
  Whoever changes behaviour updates the page that describes it in the same change.
- **English only**, in code, comments, commits and documentation.

## Repository layout

```
cowork/
├── backend/                    # Go module github.com/guided-traffic/cowork/backend
│   ├── cmd/cowork/             # the binary: serve, migrate, version
│   ├── internal/
│   │   ├── config/             # COWORK_* environment variables → Config
│   │   ├── httpserver/         # the handler (health, /api/v1, JSON 404/405) and the server lifecycle
│   │   └── store/              # pgx pool, golang-migrate runner
│   │       └── migrations/     # NNNNNN_<name>.{up,down}.sql, embedded
│   ├── test/integration/       # build tag `integration`; needs PostgreSQL 18
│   ├── Containerfile           # golang:1.27-alpine → distroless nonroot
│   └── .golangci.yml
├── frontend/                   # Angular 22 workspace, project "frontend"
│   ├── src/app/                # the shell and core/ services
│   ├── nginx/default.conf.template  # the container's nginx configuration
│   ├── proxy.conf.json         # ng serve → backend :8080, same shape as nginx
│   ├── Containerfile           # node:26-alpine build → nginxinc/nginx-unprivileged
│   └── eslint.config.js
├── deploy/helm/cowork/         # the chart: backend + frontend Deployments and Services; ci/*-values.yaml
├── hack/                       # verify-release-tooling.mjs
├── docs/
│   ├── adr/                    # decisions
│   ├── developer/              # package map, architecture, testing
│   ├── operations/             # installation, runtime
│   ├── security/               # one page per perspective
│   ├── tickets/                # work lists (+ archive/); rules in its README
│   └── planning/               # question catalog, project plan, workflow plan — transitional
├── .github/workflows/          # release.yml (Test and Release), build.yml (Release Docker & Helm), renovate.yml
├── Makefile                    # every target; bin/ and coverage/ land here
├── renovate.json, .releaserc.json, package.json (semantic-release)
├── CLAUDE.md, README.md, SECURITY.md, LICENSE
```

The per-package responsibilities are the table in
[docs/developer/package-map.md](docs/developer/package-map.md).

## Core flows, one fact each

| Flow | The fact | Where |
|---|---|---|
| Backend start | Configuration is validated completely before anything else runs; the migration runs before the listener opens | [architecture.md](docs/developer/architecture.md#backend-startup-sequence-cowork-serve) |
| Backend request | Method patterns on the mux; every known path is registered twice so the wrong method is a `405`, not the catch-all's `404` | [architecture.md](docs/developer/architecture.md#backend-request-path) |
| Frontend request | nginx: `/healthz` itself, `/api/` proxied to `BACKEND_URL`, hashed bundles immutable, everything else `index.html` with `no-store` | [architecture.md](docs/developer/architecture.md#frontend-container) |
| Migration | golang-migrate over embedded files, advisory lock across replicas, dirty version refuses to start | [runtime.md](docs/operations/runtime.md#the-migration-run) |

## Build, test and lint

### Prerequisites

| Tool | Version | Why |
|---|---|---|
| Go | 1.27 (`backend/go.mod`: 1.27.1; the toolchain downloads it if yours is older) | the backend |
| Node.js + npm | 26 (`NODE_VERSION` in the workflow; `node:26-alpine` in the Containerfile) | the frontend and the release tooling |
| Docker | any recent | `make postgres-up`, `make docker-build` |
| Helm | 3 or 4 | `make helm-lint`, `make helm-template` |

The Go tools (`golangci-lint`, `gocyclo`, `gosec`, `govulncheck`) install themselves into
`bin/` under versioned names on first use, from the backend module so the same toolchain
builds them.

### Targets

| Area | Target | Needs | Output |
|---|---|---|---|
| Format | `make fmt` | — | rewrites `backend/{cmd,internal,test}` |
| Static analysis | `make lint` | — | vet, gofmt check, golangci-lint (plus the `integration` tag), all inside `backend/` |
| | `make cyclo` | — | fails above complexity 15 |
| | `make gosec`, `make vuln` | — | gosec and govulncheck over the backend |
| Backend tests | `make test-unit` | — | verbose |
| | `make test-unit-coverage` | — | `coverage/unit.out` |
| | `make postgres-up` / `postgres-down` | Docker | `postgres:18` on `localhost:5432` (`POSTGRES_PORT=` to move it) |
| | `make test-integration` | PostgreSQL 18 | `COWORK_TEST_DATABASE_URL` defaults to the container |
| | `make test-integration-coverage` | PostgreSQL 18 | `coverage/integration.out` |
| Frontend | `make frontend-install` | npm | `npm ci` when `frontend/package-lock.json` changed |
| | `make frontend-lint` | | `ng lint` |
| | `make frontend-test` | | vitest on jsdom, once |
| | `make frontend-test-coverage` | | `frontend/coverage/` (`text-summary`, `lcov`, `json-summary`) |
| | `make frontend-build` | | `frontend/dist/frontend/browser/` |
| | `make frontend-serve` | | dev server on `:4200` with the proxy |
| Both | `make test` | | `test-unit` + `frontend-test` |
| Build | `make build-backend` | — | `bin/cowork` |
| | `make build` | npm | `build-backend` + `frontend-build` |
| | `make run`, `make migrate` | PostgreSQL | run the backend from source against the local container |
| | `make docker-build` | Docker | `BACKEND_IMG` and `FRONTEND_IMG` (defaults `guidedtraffic/cowork-backend:latest`, `guidedtraffic/cowork-frontend:latest`); `docker-build-backend` / `docker-build-frontend` for one |
| Chart | `make helm-lint`, `make helm-template` | Helm | strict lint on defaults and each `ci/` file; render per `ci/` file |
| Release | `make test-release-tooling` | `npm ci` at the root | the semantic-release plugins render notes |
| Coverage | `make coverage-merge`, `make coverage-json` | the two profiles | `coverage/combined.*`, `.github/badges/coverage.json` |

`VERSION=`, `GIT_COMMIT=` and `BUILD_TIME=` override what the linker bakes into the binary
and what the images carry as labels.

### Run locally

```bash
make postgres-up
make run                      # backend on :8080, text logs; COWORK_DATABASE_URL overrides the container URL
make frontend-serve           # frontend on :4200 in a second terminal, /api proxied to :8080
```

### Run the images together

`make docker-build`, then both containers on one Docker network: the backend with
`COWORK_DATABASE_URL`, the frontend with `BACKEND_URL=http://<backend alias>:8080` and, to
mirror the chart's read-only root filesystem and `fsGroup: 101`,
`--read-only --tmpfs /tmp:uid=101,gid=101 --tmpfs /etc/nginx/conf.d:gid=101,mode=2775 --user 101:101`.
Start the frontend first: it must come up without the backend and answer `/api/` with `502`.
Then `curl` the frontend for `/healthz`, `/api/v1/version`, a deep link and a hashed asset.
This is the recipe [ADR 0001](docs/adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
was verified with; scripting it is part of the end-to-end tier.

## The toolchain versions

| What | Pinned in | Moved by |
|---|---|---|
| Go | `backend/go.mod`, `backend/Containerfile`, `GO_VERSION` in `release.yml`, the badge in `release-template.hbs` | Renovate, one grouped PR ("Go version") |
| Node.js | `NODE_VERSION` in `release.yml`, `node:26-alpine` in `frontend/Containerfile` | Renovate |
| nginx | `nginxinc/nginx-unprivileged:1.30-alpine` in `frontend/Containerfile` | Renovate (dockerfile manager) |
| Go tools | `*_VERSION` in the `Makefile` with `# renovate:` comments | Renovate (custom regex manager) |
| PostgreSQL test image | `POSTGRES_IMAGE` in the `Makefile`, the service in `release.yml` | Renovate, held on the 18 line |
| Angular | `frontend/package.json` | Renovate, one grouped PR ("Angular"); majors by hand; TypeScript stays in Angular's peer range |
| semantic-release | `package.json` | Renovate; the `release-tooling` job proves the set |

## Continuous integration and the release

**"Test and Release"** ([`release.yml`](.github/workflows/release.yml)) runs on every push
and PR to `main`: `linter`, `gosec`, `govulncheck`, `cyclomatic-complexity`, `malware-scan`,
`unit-tests`, `integration-tests` (a `postgres:18` service container), `frontend`, `helm`,
`container-malware-scan` (one leg per image: builds the `Containerfile` from its directory,
Trivy at CRITICAL/HIGH), `coverage-report` (merges the backend profiles, comments the PR with
the per-package table, the difference to `main` and the frontend lines percentage, writes the
badge on `main`), `release-tooling`. On a push to `main`, `semantic-release` — which `needs:`
every one of them — cuts a release from the conventional commits with a GitHub App token and
commits the badge.

**"Release Docker & Helm"** ([`build.yml`](.github/workflows/build.yml)) runs on the
published release: builds and pushes `guidedtraffic/cowork-backend:<version>` and
`guidedtraffic/cowork-frontend:<version>` with provenance and SBOM, scans them, packages the
chart with the release version and publishes it to the `gh-pages` branch and the release
assets.

**Renovate** ([`renovate.yml`](.github/workflows/renovate.yml), [`renovate.json`](renovate.json))
runs daily on a self-hosted runner: minor and patch updates automerge after CI, majors wait
for a review (except GitHub Actions).

Every job says `runs-on: self-hosted`, inherited from the sibling project and **not yet
verified** for this repository; neither are the secrets `DOCKERHUB_PAT`, `APP_CLIENT_ID`,
`APP_PRIVATE_KEY` or the `gh-pages` branch. Questions Q-G5 to Q-G7 in
[the catalog](docs/planning/questions.md).

## Adding things

### A configuration variable

1. Add the `Env…` constant, the field, the default and the validation in
   [`backend/internal/config/config.go`](backend/internal/config/config.go); the error text
   names the variable and the accepted values.
2. Cover default, override and invalid value in `config_test.go`.
3. Add the row to [README.md, Configuration](README.md#configuration) and, if the chart should
   expose it, the value under `backend.config` in [`values.yaml`](deploy/helm/cowork/values.yaml),
   the `env` entry in [`backend-deployment.yaml`](deploy/helm/cowork/templates/backend-deployment.yaml)
   and the line in the README's values block.
4. If it changes runtime behaviour, say so in [docs/operations/runtime.md](docs/operations/runtime.md).

### A migration

1. Next number, two files: `backend/internal/store/migrations/NNNNNN_<snake_name>.up.sql` and
   `.down.sql`. The unit test refuses a gap, an orphan and an empty file.
2. Run `make postgres-up test-integration`; add an assertion there for what only the database
   proves.
3. If the schema encodes a decision, the ADR is written in the same change.

### An API endpoint

1. Register it in `New` in [`backend/internal/httpserver/server.go`](backend/internal/httpserver/server.go)
   through `handleGet` (or a sibling for other methods) under `/api/v1/`; use `writeJSON` /
   `writeError`.
2. Test it through `New(Options{…})` and `httptest` in `server_test.go`; a handler that touches
   the database gets its test in `backend/test/integration/`.
3. Add the row to [README.md, API](README.md#api-backend). The nginx proxy passes every
   `/api/` path through, so nothing changes in the frontend container. Once the OpenAPI
   document exists (question Q-E1), the document comes first and this list changes.

### A frontend feature

1. Standalone component under `frontend/src/app/<feature>/`, services under `core/`; signals,
   no NgRx.
2. A `.spec.ts` beside it: `provideHttpClient()` + `provideHttpClientTesting()`,
   `await fixture.whenStable()` before reading the DOM, `data-testid` for assertions.
3. `make frontend-lint frontend-test`; `make frontend-build` if the bundle budget in
   `angular.json` might move.

### A path nginx must treat differently

Edit [`frontend/nginx/default.conf.template`](frontend/nginx/default.conf.template); keep
`${BACKEND_URL}` and `${NGINX_LOCAL_RESOLVERS}` the only substituted variables (or extend
`NGINX_ENVSUBST_FILTER` in the Containerfile deliberately). Rebuild the image and run it
read-only as described above, once without a backend present; there is no unit test for
nginx. Record the behaviour in
[docs/operations/runtime.md](docs/operations/runtime.md#the-frontend).

### A chart value

1. `values.yaml` under `backend.` or `frontend.` with a comment, the template, and — when it
   maps to an environment variable — the `env` entry.
2. A `ci/*-values.yaml` if the value opens a new shape worth rendering in CI.
3. The README's values block and, when operators need to understand it,
   [docs/operations/installation.md](docs/operations/installation.md).

### A CI job

Add it to `release.yml` as a Makefile target and add its name to the `needs:` list of
`semantic-release` in the same change (ADR 0003 D4).

## Conventions

- **Commits:** conventional commits (`feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`),
  a scope where one exists (`backend`, `frontend`, `chart`, `ci`, …); **no apostrophe anywhere
  in a commit message**; a `!` or `BREAKING CHANGE:` footer for a breaking change.
  semantic-release reads them.
- **Go:** `gofmt -s`, `goimports` with the module as local prefix, the golangci-lint set in
  [`backend/.golangci.yml`](backend/.golangci.yml); errors wrapped with `%w` and a verb ("parse
  database url: …"); `log/slog` with key-value pairs; no global state beyond the linker
  variables.
- **Tests:** `testify` (`require` for preconditions, `assert` for the claim); table tests as
  `map[string]…` with `t.Run`; a test name states the behaviour
  (`TestKnownPathsRejectOtherMethods`).
- **Angular:** standalone components, signals, `inject()`, OnPush by default in Angular 22,
  SCSS, Prettier as generated by the CLI, ESLint through `ng lint`.
- **Documentation:** every claim verified against the tree; "not verified" is a complete
  sentence; file and line references as relative links; `# default` / `# example` on shown
  values; nothing outside `docs/tickets/` cites a ticket.
- **Security:** a request that weakens auth, secrets, TLS, permissions, isolation, validation
  or exposure is named as such and discussed before it is implemented; an accepted risk is
  written down in the security page it belongs to.
