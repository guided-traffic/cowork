# Build, test and lint

Every target `make` offers, what each needs and what it produces; how to run the backend and
the frontend locally and the two images together. The tiers and their rules are
[testing.md](testing.md) and [ADR 0003](../adr/0003-test-and-ci-policy.md).

## Prerequisites

| Tool | Version | Why |
|---|---|---|
| Go | 1.27 (`backend/go.mod`: 1.27.1; the toolchain downloads it if yours is older) | the backend |
| Node.js + npm | 26 (`NODE_VERSION` in the workflow; `node:26-alpine` in the Containerfile) | the frontend and the release tooling |
| Docker | any recent | `make postgres-up`, `make docker-build` |
| Helm | 3 or 4 | `make helm-lint`, `make helm-template` |

The Go tools (`golangci-lint`, `gocyclo`, `gosec`, `govulncheck`) install themselves into
`bin/` under versioned names on first use, from the backend module so the same toolchain
builds them.

## Targets

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

## Run locally

```bash
make postgres-up
make run                      # backend on :8080, text logs; COWORK_DATABASE_URL overrides the container URL
make frontend-serve           # frontend on :4200 in a second terminal, /api proxied to :8080
```

## Run the images together

`make docker-build`, then both containers on one Docker network: the backend with
`COWORK_DATABASE_URL`, the frontend with `BACKEND_URL=http://<backend alias>:8080` and, to
mirror the chart's read-only root filesystem and `fsGroup: 101`,
`--read-only --tmpfs /tmp:uid=101,gid=101 --tmpfs /etc/nginx/conf.d:gid=101,mode=2775 --user 101:101`.
Start the frontend first: it must come up without the backend and answer `/api/` with `502`.
Then `curl` the frontend for `/healthz`, `/api/v1/version`, a deep link and a hashed asset.
This is the recipe [ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
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
