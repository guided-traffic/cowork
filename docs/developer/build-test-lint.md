# Build, test and lint

Every target `make` offers, what each needs and what it produces; the generated code; how to run
the backend and the frontend locally and the two images together. The tiers and their rules are
[testing.md](testing.md) and [ADR 0003](../adr/0003-test-and-ci-policy.md).

## Prerequisites

| Tool | Version | Why |
|---|---|---|
| Go | 1.27 (`backend/go.mod`: 1.27.1; the toolchain downloads it if yours is older) | the backend, its generators and tools |
| Node.js + npm | 26 (`NODE_VERSION` in the workflow's frontend job; `node:26-alpine` in the Containerfile); the workflow's release jobs use the current LTS | the frontend and the release tooling |
| Docker | any recent | `make postgres-up`, `make minio-up`, `make dex-up`, `make docker-build` |
| Helm | 3 or 4 | `make helm-lint`, `make helm-template` |
| `openssl`, `curl` | any | `make run` draws a throw-away server key with `openssl rand`; `make minio-up` and `make dex-up` wait for their servers with `curl` |
| `python3` | 3 | `make coverage-json`, `make verify-phase-2` |

The Go tools (`golangci-lint`, `gocyclo`, `gosec`, `govulncheck`, `sqlc`, `oapi-codegen`)
install themselves into `bin/` under versioned names on first use, from the backend module so the
same toolchain builds them.

## Targets

| Area | Target | Needs | Output |
|---|---|---|---|
| Help | `make help` | — | every target with its one-line description |
| Generate | `make generate` | — | the problem-code enum and README table, `api/openapi.gen.json`, `internal/api/apigen/`, `internal/store/readq/` and `writeq/` (below) |
| | `make generate-check` | git | runs `generate`, then fails on a diff under `backend/` or in `README.md`, or on an untracked file under `backend/` |
| Format | `make fmt` | — | rewrites `backend/{api,cmd,internal,test,tools}` |
| Static analysis | `make lint` | — | vet, gofmt check, golangci-lint (plus the `integration` tag), `sqlc compile`, all inside `backend/` |
| | `make vet` | — | `go vet`, the integration tests included |
| | `make lint-fix` | — | golangci-lint with the fixes it offers applied |
| | `make cyclo` | — | fails above complexity 15; generated code and tests are not measured |
| | `make cyclo-report` | — | the 20 most complex functions, tests included; CI prints it |
| | `make gosec`, `make vuln` | — | gosec (generated files excluded) and govulncheck over the backend |
| Backend tests | `make test-unit` | — | verbose, no database |
| | `make test-unit-coverage` | — | `coverage/unit.out` |
| | `make postgres-up` / `postgres-down` | Docker | `postgres:18` on `localhost:5432` (`POSTGRES_PORT=` to move it) with the development database `cowork` and its roles `cowork_owner` and `cowork_app` |
| | `make minio-up` / `minio-down` | Docker | MinIO on `localhost:9000` (`MINIO_PORT=`), the attachment tests' S3 server |
| | `make dex-up` / `dex-down` | Docker | Dex on `localhost:5556` (`DEX_PORT=`, which moves the issuer with it), configured from [`hack/dex/config.yaml`](../../hack/dex/config.yaml): the identity provider of `make dev` and of the login tests ([testing.md](testing.md#the-identity-provider-in-the-tests)); it keeps nothing, so `dex-down` loses nothing, and `dex-down dex-up` loads a changed configuration |
| | `make dev-up` | Docker | `postgres-up`, `minio-up` and `dex-up` together; each `-up` starts its container again when it exists but stopped — after a restart of Docker or of the machine |
| | `make test-integration` | PostgreSQL 18, S3 and Dex | `COWORK_TEST_DATABASE_URL`, `COWORK_TEST_S3_*` and `COWORK_TEST_OIDC_ISSUER` default to the three containers ([testing.md](testing.md#environment-variables-the-suites-read)) |
| | `make test-integration-coverage` | the same | `coverage/integration.out` |
| Frontend | `make frontend-install` | npm | `npm ci` when `frontend/package-lock.json` changed |
| | `make frontend-lint` | | `ng lint` |
| | `make frontend-test` | | vitest on jsdom, once |
| | `make frontend-test-coverage` | | `frontend/coverage/frontend/` (`text-summary`, `lcovonly`, `json-summary`), through `@vitest/coverage-v8` |
| | `make frontend-build` | | `frontend/dist/frontend/browser/` (with the PrimeUI key, when there is one); warns while the initial bundle is above `angular.json`'s budget of 1,000,000 bytes, which it is — 1,016.52 kB on 2026-10-04, an open point of [ADR 0052](../adr/0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) |
| | `make frontend-serve` | | dev server on `:4200` with the proxy (and the PrimeUI key, when there is one) |
| | `make frontend-generate` | npm | the Angular client in `frontend/src/app/api/` from `backend/api/openapi.gen.json` |
| | `make frontend-generate-check` | | fails when the committed client differs from a fresh generation |
| | `make frontend-clean` | | removes the build output |
| Both | `make test` | | `test-unit` + `frontend-test` |
| Build | `make build-backend` | — | `bin/cowork`, after `fmt` (which rewrites sources) and `vet` |
| | `make build-mcp` | — | `bin/cowork-mcp`, the MCP server and hooks for Claude Code ([mcp.md](mcp.md)); `GOOS=` and `GOARCH=` cross-compile, `MCP_OUT=` names the file — a `.exe` by default for `GOOS=windows` |
| | `make build` | npm | `build-backend` + `frontend-build` |
| | `make run`, `make migrate` | `make postgres-up` | run the backend from source against the development database (below) |
| | `make dev-seed` | `make postgres-up` | migrates, then creates a person, a tenant, an admin membership and a token, and prints the token once |
| | `make docker-build` | Docker | `BACKEND_IMG` and `FRONTEND_IMG` (defaults `guidedtraffic/cowork-backend:latest`, `guidedtraffic/cowork-frontend:latest`); `docker-build-backend` / `docker-build-frontend` for one |
| | `make docker-push` | Docker, a registry login | pushes both images |
| | `make verify-phase-2` | Docker, `python3`, the two images, `make postgres-up minio-up` | runs both images read-only and drives the API as a `make dev-seed` agent ([`hack/verify-phase-2.sh`](../../hack/verify-phase-2.sh)) |
| Chart | `make helm-lint`, `make helm-template` | Helm | strict lint on defaults and each `ci/` file; render per `ci/` file |
| Release | `make test-release-tooling` | `npm ci` at the root | the semantic-release plugins render notes |
| Coverage | `make coverage-merge`, `make coverage-json` | the two profiles | `coverage/combined.*`, `.github/badges/coverage.json` |

`VERSION=`, `GIT_COMMIT=` and `BUILD_TIME=` override what the linker bakes into the binary
and what the images carry as labels. `CONTAINER_BIND=` (`127.0.0.1` `# default`) is the address
the PostgreSQL, MinIO and Dex containers publish their ports on: their credentials are development
values and PostgreSQL's superuser is `postgres`/`postgres`, so they are not reachable from the
network the machine is on
([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D4). A container made before the rule keeps its binding until it is removed and made again.

## Generated code

`make generate` runs, from `backend/`: `tools/problemdoc` (the `ProblemCode` enum in
`api/components/problem-codes.yaml` and the code table in the root `README.md`),
`tools/specbundle` (`api/openapi.gen.json`), oapi-codegen (`internal/api/apigen/api.gen.go`) and
`sqlc generate` (`internal/store/readq/`, `writeq/`). Run it after changing the API document, a
query file, a migration that a query reads, or the problem catalogue, and commit what it writes.
Never edit a generated file: the next run overwrites it, and `make generate-check` — a step of the
Code Linting job — fails on the difference. The pipeline is [api.md](api.md#the-document) and
[data-access.md](data-access.md#the-wrappers).

## Run locally

**The whole stack, to watch the UI while it is built:**

```bash
make dev                      # PostgreSQL, MinIO, Dex, the backend, demo data, the UI on https://localhost:4200 — Ctrl-C stops it
make dev-reset                # empties the development database; the next make dev seeds it again
```

`make dev` ([`hack/dev.sh`](../../hack/dev.sh)) refuses to start when `:8080` or `:4200` is in
use, starts the three containers (`make dev-up`), runs the backend on `127.0.0.1:8080` with the
local administrator `dev` and Dex as its identity provider — `cowork-users` allowed,
`cowork-admins` the administrator group, the button *Sign in with Dex* — logs it to
`.dev/backend.log`, maps the group `team-red` to `member` in the tenant `dev` through a session of
the local administrator, and keeps the demo data's token in `.dev/token` and a stable server key in
`.dev/session-key` (all untracked). Two ways in: the form as `dev` with the development-only
password `dev-only-cowork`, or *Sign in with Dex* as `ada@example.com`, `bob@example.com`,
`cyd@example.com` or `dan@example.com` with `dev-only-dex` (what each is:
[testing.md](testing.md#the-identity-provider-in-the-tests)). When LM Studio answers on
`localhost:1234` and lists the model `COWORK_DEV_CHAT_MODEL` (`qwen/qwen3-30b-a3b-2507` `# default`),
the backend gets it as the chat's provider, declared inside, and the UI has its assistant; load the
model with a context of 16k tokens or more first ([chat.md](../operations/chat.md#lm-studio-on-the-operators-machine)).
Without it `make dev` runs without the chat and says so. The browser asks once about the dev
server's self-signed certificate (HTTPS, because Safari stores no `Secure` cookie from
`http://localhost`, and Dex knows `https://localhost:4200/auth/callback` as the redirect URI). A
saved frontend file reloads the page; a backend change needs a restart. The PrimeUI license key
goes into `.dev/primeui-license` ([frontend.md](frontend.md#the-primeui-license-key)). How it
works: [frontend.md](frontend.md#the-development-loop).

**The parts by hand:**

```bash
make postgres-up              # PostgreSQL 18 on :5432: database cowork, roles cowork_owner and cowork_app
make dev-seed                 # migrates, prints a person, a tenant and a token — once
make run                      # backend on :8080: migrates as cowork_owner, serves as cowork_app, text logs
make frontend-serve           # frontend on :4200 in a second terminal, /api and /auth proxied to :8080 (unsigned: the API answers 401)
curl -s -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/me   # TOKEN: the one dev-seed printed
```

`make run` takes `COWORK_DATABASE_URL`, `COWORK_DATABASE_OWNER_URL` and `COWORK_SESSION_KEY`
from the environment when they are set, else the development values; a throw-away key
invalidates the list cursors at every restart — and fails a login through Dex begun before it. It
sets no `COWORK_S3_*`, so uploads answer `501 uploads_disabled`
([storage.md](storage.md#the-test-server)). Every other `COWORK_*` variable of the shell reaches
the backend as it is, which is how the real logins are tried by hand: with
`COWORK_LOCAL_ADMIN_USERNAME`, `COWORK_LOCAL_ADMIN_PASSWORD` and
`COWORK_BASE_URL=http://localhost:4200` set, the backend creates the administrator at start and
`/auth/local` logs in; with `make dex-up` and the `COWORK_OIDC_*` variables `hack/dev.sh` sets —
the base URL then `https://localhost:4200`, the redirect URI Dex knows, and the UI from
`make frontend-serve NG_SERVE_FLAGS=--ssl` — the browser logs in through Dex
([README, run it locally](../../README.md#run-it-locally)). `make run` sets none of them; `make dev`
sets all of them.

`make dev-seed` reuses the person `dev` and the tenant `dev` and mints a fresh token on every
run. The token is an agent's — write scope, every capability — so its `POST`s need an
`Idempotency-Key`. A plain admin-scope token instead:
`cd backend && COWORK_DEV_SEED_DATABASE_URL='postgres://postgres:postgres@localhost:5432/cowork?sslmode=disable' go run ./test/devseed -agent=false`
(the `# default` administrative URL of `make postgres-up`; `-username` and `-tenant` choose
other names).

## Run the images together

`make docker-build`, then both containers on one Docker network with read-only root filesystems,
as the chart runs them:

- **The backend** with `--read-only`, `COWORK_DATABASE_URL` (the runtime role),
  `COWORK_DATABASE_OWNER_URL` (the owner role; the image migrates on start unless
  `COWORK_MIGRATE_ON_START=false`) and `COWORK_SESSION_KEY` (`openssl rand -base64 32`);
  `COWORK_S3_*` for uploads.
- **The frontend** with `BACKEND_URL=http://<backend alias>:8080` and, to mirror the chart's
  `fsGroup: 101`,
  `--read-only --tmpfs /tmp:uid=101,gid=101 --tmpfs /etc/nginx/conf.d:gid=101,mode=2775 --user 101:101`.
  The size and timeout variables keep their image defaults unless set
  ([architecture.md](architecture.md#frontend-container)).

Start the frontend first: it must come up without the backend and answer `/api/` with the `502`
problem body. Then `curl` through the frontend: `/healthz`, `/api/v1/version`, a deep link, a
hashed asset; with a token, an authenticated route; a JSON body above `COWORK_MAX_JSON_BODY` (the
backend's `413`, with a `request_id`) and one above `NGINX_CLIENT_MAX_BODY_SIZE` (nginx's `413`,
without); the event stream, which must arrive unbuffered; and `SIGTERM` to the backend with a
stream open, which must end the stream at once. A change to the shell's content-security policy, or
to the build under it, is checked in a browser against the image: the UI's pages — the chat's panel
among them — with the console showing no violation in Chromium and WebKit. There is no unit test for
nginx: this run is the check,
[ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
records the one of 2026-10-02. `make verify-phase-2` scripts the API half of it — both images
read-only, driven as a `make dev-seed` agent — by hand, not as a CI job; the nginx checks above
stay manual.

## The toolchain versions

| What | Pinned in | Moved by |
|---|---|---|
| Go | `backend/go.mod`, `backend/Containerfile`, `GO_VERSION` in `release.yml`, the badge in `release-template.hbs` | Renovate, one grouped PR ("Go version") |
| Node.js | `NODE_VERSION` in `release.yml`, `node:26-alpine` in `frontend/Containerfile` | Renovate |
| nginx | `nginxinc/nginx-unprivileged:1.31-alpine` in `frontend/Containerfile`; its Alpine packages are upgraded at build time (`apk upgrade`), so a published Alpine fix does not wait for the upstream rebuild | Renovate (dockerfile manager) |
| Go tools, sqlc, oapi-codegen | `*_VERSION` in the `Makefile` with `# renovate:` comments | Renovate (custom regex manager) |
| PostgreSQL test image | `POSTGRES_IMAGE` in the `Makefile`, the service in `release.yml` | Renovate, held on the 18 line: the Makefile manager captures the tag without the image name, so the hold rule sees `18` |
| MinIO test image | `MINIO_IMAGE` in the `Makefile`, pinned as `tag@digest`, with a `# renovate:` comment | Renovate, through the regex manager for `tag@digest` lines |
| Dex test image | `DEX_IMAGE` in the `Makefile`, `ghcr.io/dexidp/dex:v2.45.1` pinned as `tag@digest`, with a `# renovate:` comment | Renovate, through the same regex manager — its pattern matches the line; no Renovate run has confirmed it |

The two Makefile managers in `renovate.json` were matched against the `Makefile` locally; no
Renovate run has confirmed them yet.
| Angular | `frontend/package.json` | Renovate, one grouped PR ("Angular"); majors by hand; TypeScript stays in Angular's peer range |
| semantic-release | `package.json` | Renovate; the `release-tooling` job proves the set |
