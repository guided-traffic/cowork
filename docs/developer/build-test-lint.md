# Build, test and lint

Every target `make` offers, what each needs and what it produces; the generated code; how to run
the backend and the frontend locally and the two images together behind a stand-in for the Ingress. The tiers and their rules are
[testing.md](testing.md) and [ADR 0003](../adr/0003-test-and-ci-policy.md).

## Prerequisites

| Tool | Version | Why |
|---|---|---|
| Go | 1.27 (`backend/go.mod`: 1.27.2; the toolchain downloads it if yours is older) | the backend, its generators and tools |
| Node.js + npm | 26 (`NODE_VERSION` in the workflow's frontend job; `node:26-alpine` in the Containerfile); the workflow's release jobs use the current LTS | the frontend and the release tooling |
| Docker | any recent | `make postgres-up`, `make postgres-tls-up`, `make minio-up`, `make dex-up`, `make docker-build`, `make e2e` |
| Helm | 3 or 4 (CI installs 4.3.0) | `make helm-lint`, `make helm-template` |
| `openssl`, `curl` | any; `curl` with `--aws-sigv4` (7.75 or newer) for `make e2e` | `make run` draws a throw-away server key with `openssl rand`; `make minio-up` and `make dex-up` wait for their servers with `curl`; `make e2e` makes its server key and TLS certificate with `openssl` and its bucket and readiness checks with `curl`; `make examples-lint` fetches CloudNativePG's CustomResourceDefinition and Silo's source archive with `curl` |
| Chromium and WebKit of Playwright | the version of `@playwright/test` in `frontend/package.json` | `make e2e`; `make e2e-browsers` installs them |
| `python3` | 3 | `make coverage-json`, `make verify-phase-2` |

The Go tools (`golangci-lint`, `gocyclo`, `gosec`, `govulncheck`, `sqlc`, `oapi-codegen`,
`kubeconform`) install themselves into `bin/` under versioned names on first use, from the backend
module so the same toolchain builds them. gosec is the exception: its latest release cannot read the
export data of Go 1.27.2, so the Makefile builds it in a module of its own with `golang.org/x/tools`
pinned to `GOSEC_XTOOLS_VERSION`, on the toolchain `backend/go.mod` selects, and names the binary
after both versions.

## Targets

| Area | Target | Needs | Output |
|---|---|---|---|
| Help | `make help` | — | every target with its one-line description |
| Generate | `make generate` | — | the problem-code enum and README table, `api/openapi.gen.json`, the chart's Grafana dashboard `deploy/helm/cowork/files/grafana-dashboard.json`, `internal/api/apigen/`, `internal/store/readq/` and `writeq/` (below) |
| | `make generate-check` | git | runs `generate`, then fails on a diff under `backend/`, in `README.md` or under `deploy/helm/cowork/files/`, or on an untracked file under `backend/` or `deploy/helm/cowork/files/` |
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
| | `make postgres-tls-up` / `postgres-tls-down` | Docker | `postgres:18` on `localhost:5433` (`POSTGRES_TLS_PORT=`) that serves TLS under a private authority, which [`hack/postgres-tls/entrypoint.sh`](../../hack/postgres-tls/entrypoint.sh) makes in the container at its first start with a server certificate for `localhost` and `127.0.0.1`; the target copies the authority's certificate to `bin/<container>-ca.crt` (`POSTGRES_TLS_CA`), the test of `COWORK_DATABASE_CA` trusts it; `postgres-tls-down` removes the container and the copy |
| | `make minio-up` / `minio-down` | Docker | PGSTY Silo, the maintained MinIO fork, on `localhost:9000` (`MINIO_PORT=`), the attachment tests' S3 server; the names stay MinIO's, as Silo keeps MinIO's interface, and a `cowork-minio` made before keeps its MinIO image until `minio-down minio-up` |
| | `make dex-up` / `dex-down` | Docker | Dex on `localhost:5556` (`DEX_PORT=`, which moves the issuer with it), configured from [`hack/dex/config.yaml`](../../hack/dex/config.yaml): the identity provider of `make dev` and of the login tests ([testing.md](testing.md#the-identity-provider-in-the-tests)); it keeps nothing, so `dex-down` loses nothing, and `dex-down dex-up` loads a changed configuration |
| | `make dev-up` | Docker | `postgres-up`, `minio-up` and `dex-up` together; each `-up` starts its container again when it exists but stopped — after a restart of Docker or of the machine |
| | `make test-integration` | PostgreSQL 18, one that serves TLS, S3 and Dex | `COWORK_TEST_DATABASE_URL`, `COWORK_TEST_DATABASE_TLS_URL` with `COWORK_TEST_DATABASE_TLS_CA`, `COWORK_TEST_S3_*` and `COWORK_TEST_OIDC_ISSUER` default to the four containers ([testing.md](testing.md#environment-variables-the-suites-read)) |
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
| | `make verify-phase-2` | Docker, `python3`, the two images, `make postgres-up minio-up` | runs both images read-only behind the Ingress stand-in and drives the API through it as a `make dev-seed` agent ([`hack/verify-phase-2.sh`](../../hack/verify-phase-2.sh)) |
| End-to-end | `make e2e` | Docker, the two images of one commit (`make docker-build`), the browsers | both images behind the Ingress stand-in with TLS, with a PostgreSQL, a Silo and a Dex of its own, the Playwright suite in Chromium and WebKit, then the stack removed ([`hack/e2e.sh`](../../hack/e2e.sh), [testing.md](testing.md#end-to-end-tests)); `E2E_ARGS=` reaches `playwright test`; a failed run leaves `frontend/e2e/test-results/` and `frontend/e2e/playwright-report/` |
| | `make e2e-up` / `e2e-down` | the same | the stack alone, kept for `cd frontend && npx playwright test -c e2e`, and its removal |
| | `make e2e-browsers` | npm | Playwright's Chromium and WebKit; `PLAYWRIGHT_INSTALL_FLAGS=--with-deps` adds their system packages |
| Chart | `make helm-lint`, `make helm-template` | Helm | strict lint on defaults and each `ci/` file; render per `ci/` file, and the inline credentials' backend pod template held to the release's revision and no checksum |
| | `make examples-lint` | Go, Helm, `curl`, `tar`, the network | validates `deploy/examples/` with kubeconform: the CustomResourceDefinition of `CNPG_VERSION`'s `Cluster` is fetched at its tag into `bin/examples-schemas/` and turned into a JSON schema by `backend/tools/crdschema`; Silo's chart, `helm/silo` of its repository's archive at `SILO_VERSION`, is unpacked into `bin/silo-chart-<version>/` and rendered with `silo-values.yaml`; the built-in kinds of both are checked against kubeconform's default schemas, all in strict mode; fails while an example names another release than the `Makefile`; reads the shell example with `sh -n`. Syntax only ([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D2) |
| Release | `make test-release-tooling` | `npm ci` at the root | the semantic-release plugins render notes |
| Coverage | `make coverage-merge`, `make coverage-json` | the two profiles | `coverage/combined.*`, `.github/badges/coverage.json` |

`VERSION=`, `GIT_COMMIT=` and `BUILD_TIME=` override what the linker bakes into the binary
and what the images carry as labels. `CONTAINER_BIND=` (`127.0.0.1` `# default`) is the address
the PostgreSQL, Silo and Dex containers publish their ports on: their credentials are development
values and PostgreSQL's superuser is `postgres`/`postgres`, so they are not reachable from the
network the machine is on
([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D4). A container made before the rule keeps its binding until it is removed and made again.

## Generated code

`make generate` runs, from `backend/`: `tools/problemdoc` (the `ProblemCode` enum in
`api/components/problem-codes.yaml` and the code table in the root `README.md`),
`tools/specbundle` (`api/openapi.gen.json`), `tools/dashboard` (the chart's
`files/grafana-dashboard.json` from `internal/metrics/dashboard.go`, [metrics.md](metrics.md#the-dashboard)),
oapi-codegen (`internal/api/apigen/api.gen.go`) and
`sqlc generate` (`internal/store/readq/`, `writeq/`). Run it after changing the API document, a
query file, a migration that a query reads, the problem catalogue or the dashboard, and commit what
it writes.
Never edit a generated file: the next run overwrites it, and `make generate-check` — a step of the
Code Linting job — fails on the difference. The pipeline is [api.md](api.md#the-document) and
[data-access.md](data-access.md#the-wrappers).

## Run locally

**The whole stack, to watch the UI while it is built:**

```bash
make dev                      # PostgreSQL, Silo, Dex, the backend, demo data, the UI on https://localhost:4200 — Ctrl-C stops it
make dev-reset                # empties the development database; the next make dev seeds it again
```

`make dev` ([`hack/dev.sh`](../../hack/dev.sh)) refuses to start when `:8080` or `:4200` is in
use, starts the three containers (`make dev-up`), runs the backend on `127.0.0.1:8080` with the
local administrator `dev` and Dex as its identity provider — `cowork-users` allowed,
`cowork-admins` the administrator group, the button *Sign in with Dex* — logs it to
`.dev/backend.log`, maps the group `team-red` to `member` in the tenant `dev` through a session of
the local administrator, and keeps the demo data's token in `.dev/token` and a stable server key in
`.dev/session-key` (all untracked). The dev server's proxy
([`frontend/proxy.conf.mjs`](../../frontend/proxy.conf.mjs)) is the developer's stand-in for the
Ingress: it sends `/api` and `/auth` to the backend on `:8080` as the Ingress routes `/api/` and
`/auth/` on an installation, and the dev server serves the rest. Two ways in, the form as the local
administrator or *Sign in with Dex* as one of four users — who they are and their development-only
passwords: [development-credentials.md](development-credentials.md#signing-in-to-the-ui-under-make-dev). When LM Studio answers on
`localhost:1234` and lists the model `COWORK_DEV_CHAT_MODEL` (`qwen/qwen3-30b-a3b-2507` `# default`),
the backend gets it as the chat's one provider, `lmstudio`, and the UI has its assistant; load the
model with a context of 16k tokens or more and one prediction first — `make dev` never loads it, and
prints the `lms load` command when the `lms` CLI shows the model is not loaded
([chat.md](../operations/chat.md#lm-studio-on-the-operators-machine)).
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

`make docker-build`, then three containers on one Docker network, as the chart and its Ingress run
them ([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3):

- **The backend** with `--network-alias backend`, `--read-only`, `COWORK_DATABASE_URL` (the runtime
  role), `COWORK_DATABASE_OWNER_URL` (the owner role; the image migrates on start unless
  `COWORK_MIGRATE_ON_START=false`) and `COWORK_SESSION_KEY` (`openssl rand -base64 32`);
  `COWORK_S3_*` for uploads; for a login `COWORK_LOCAL_ADMIN_USERNAME`,
  `COWORK_LOCAL_ADMIN_PASSWORD` and `COWORK_BASE_URL` set to the stand-in's origin.
- **The frontend** with `--network-alias frontend` and, to mirror the chart's `fsGroup: 101` and its
  one volume, `--read-only --tmpfs /tmp:uid=101,gid=101 --user 101:101`. It takes no variable and
  needs no backend to start.
- **The Ingress stand-in** — [`hack/ingress/default.conf`](../../hack/ingress/default.conf) over the
  default server of `INGRESS_IMAGE` (`nginxinc/nginx-unprivileged:1.31-alpine` `# default`, the
  `Makefile`), published on the loopback address. It routes `/api/` and `/auth/` to `backend:8080`
  and everything else to `frontend:8080`, as the chart's Ingress does; writes the address it saw
  into `X-Forwarded-For`, as ingress-nginx does by default; and has the body limit (`51m`) and read
  timeout (`40s`) the operations page names for the controller. It leaves response buffering on, so
  the two streams pass unbuffered by the backend's `X-Accel-Buffering: no` alone. It looks both
  names up per request (cached a second), so the three may start in any order. Its configuration
  is copied in, not mounted, as `make dex-up` does with Dex's:

```bash
docker network create cowork-run                                                    # example names and port
docker run -d --name cowork-run-backend --network cowork-run --network-alias backend --read-only \
  --user 65532:65532 -e COWORK_DATABASE_URL=… -e COWORK_DATABASE_OWNER_URL=… -e COWORK_SESSION_KEY=… \
  guidedtraffic/cowork-backend:latest serve
docker run -d --name cowork-run-frontend --network cowork-run --network-alias frontend --read-only \
  --tmpfs /tmp:uid=101,gid=101 --user 101:101 guidedtraffic/cowork-frontend:latest
docker create --name cowork-run-ingress --network cowork-run -p 127.0.0.1:18090:8080 \
  nginxinc/nginx-unprivileged:1.31-alpine
docker cp hack/ingress/default.conf cowork-run-ingress:/etc/nginx/conf.d/default.conf
docker start cowork-run-ingress                                                     # the UI on http://localhost:18090
```

`make verify-phase-2` ([`hack/verify-phase-2.sh`](../../hack/verify-phase-2.sh)) does that against
`make postgres-up minio-up`, with a database and a bucket of its own, and drives the API through the
stand-in as a `make dev-seed` agent — by hand, not as a CI job. `make e2e` puts the same stand-in,
listening with TLS, in front of the images and walks the UI through it in a browser
([ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md) D1,
[testing.md](testing.md#end-to-end-tests)).

Then check through the stand-in: `/healthz` (the frontend's), `/api/v1/version` (the backend's,
with `X-Request-Id`), a deep link and a hashed asset (`no-store`, `immutable`, the shell's
policy); with a token or a session, an authenticated route; a JSON body above
`COWORK_MAX_JSON_BODY` (the backend's `413`, with a `request_id`) and one above `51m` (the
stand-in's own `413` page); the event stream, which must deliver an event at once and stay open
past the stand-in's read timeout on its heartbeats; and `SIGTERM` to the backend with a stream
open, which must end the stream at once. Then the frontend alone — published, or reached from a
container on the network — for `/api/` and `/auth/`, which must answer the `404` problem, a body
above 1 MiB included. A change to the shell's content-security policy, or to the build under it,
is checked in a browser through the stand-in: the UI's pages — the chat's panel among them — with
the console showing no violation. Chromium keeps the `Secure` session cookie on
`http://localhost`, WebKit does not, so a WebKit run needs TLS in front of the stand-in, as `make e2e`
has it. There is
no unit test for nginx: this run is the check, and ADR 0001 records the one of 2026-10-04, which
also ran the chart behind ingress-nginx in a kind cluster.

## The toolchain versions

| What | Pinned in | Moved by |
|---|---|---|
| Go | `backend/go.mod`, `backend/Containerfile`, `GO_VERSION` in `release.yml`, the badge in `release-template.hbs` | Renovate, one grouped PR ("Go version") |
| Node.js | `NODE_VERSION` in `release.yml`, `node:26-alpine` in `frontend/Containerfile` | Renovate |
| nginx | `nginxinc/nginx-unprivileged:1.31-alpine` in `frontend/Containerfile`; its Alpine packages are upgraded at build time (`apk upgrade`), so a published Alpine fix does not wait for the upstream rebuild. The same image is `INGRESS_IMAGE` in the `Makefile`, the Ingress stand-in's, with a `# renovate:` comment | Renovate (dockerfile manager; the regex manager for the `Makefile`, whose pattern matches the line — no Renovate run has confirmed it) |
| Go tools, sqlc, oapi-codegen, kubeconform | `*_VERSION` in the `Makefile` with `# renovate:` comments | Renovate (custom regex manager) |
| The releases of the examples | `CNPG_VERSION` and `SILO_VERSION` in the `Makefile`, each with a `# renovate:` comment (`github-releases`); each example names its release in its first lines | Renovate, through the same regex manager — its pattern matches the lines, no Renovate run has confirmed it; `make examples-lint` then fails until the example is brought to the new release. Silo's tags, `RELEASE.<date>T<time>Z`, are read by a regex versioning of `renovate.json`'s package rules, not by semver — not confirmed by a Renovate run either |
| PostgreSQL test image | `POSTGRES_IMAGE` in the `Makefile`, the service in `release.yml` | Renovate, held on the 18 line: the Makefile manager captures the tag without the image name, so the hold rule sees `18` |
| S3 test image | `MINIO_IMAGE` in the `Makefile`, PGSTY Silo `docker.io/pgsty/silo` pinned as `tag@digest` (multi-arch), with a `# renovate:` comment; `hack/e2e.sh` names the same as its default | Renovate, through the regex manager for `tag@digest` lines and the same regex versioning of Silo's tags — no Renovate run has confirmed it |
| Dex test image | `DEX_IMAGE` in the `Makefile`, `ghcr.io/dexidp/dex:v2.45.1` pinned as `tag@digest`, with a `# renovate:` comment | Renovate, through the same regex manager — its pattern matches the line; no Renovate run has confirmed it |
| Playwright and its browsers | `@playwright/test` in `frontend/package.json`; the browsers are the ones that version names (`make e2e-browsers`); the image that makes the screenshots' pictures, `mcr.microsoft.com/playwright:v<version>-noble`, is named in [testing.md](testing.md#the-screenshots) only | Renovate (npm); the image's tag follows by hand |
| The nginx exporter | `frontend.metrics.exporter.image` in the chart's `values.yaml`, `nginx/nginx-prometheus-exporter` `1.5.3` | Renovate's `helm-values` manager, on by default — `renovate.json` names no `enabledManagers` — and made for a `repository` and `tag` pair in a `values.yaml`; no Renovate run has confirmed it |

The two Makefile managers in `renovate.json` were matched against the `Makefile` locally; no
Renovate run has confirmed them yet.
| Angular | `frontend/package.json` | Renovate, one grouped PR ("Angular"); majors by hand; TypeScript stays in Angular's peer range |
| semantic-release | `package.json` | Renovate; the `release-tooling` job proves the set |
