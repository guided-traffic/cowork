# Package map

Where things live and what each part is responsible for. Read against the tree on
2026-09-29.

## Backend (`backend/`, Go module `github.com/guided-traffic/cowork/backend`)

| Package | Responsibility | Key symbols |
|---|---|---|
| [`backend/cmd/cowork/`](../../backend/cmd/cowork/) | The binary. Parses the command (`serve`, `migrate`, `version`, `help`), loads the configuration, builds the logger, wires store and server. `run` is separated from `main` so a test drives it with its own environment and writers | `run`, `runServe`, `runMigrate`, `version`/`commit`/`buildTime` (set by the linker) |
| [`backend/internal/config/`](../../backend/internal/config/) | The configuration surface: every `COWORK_*` variable, its default and its validation. `Load` takes a lookup function with the contract of `os.LookupEnv` and reports every invalid value at once | `Config`, `Load`, `Env*` constants, `Default*` constants |
| [`backend/internal/httpserver/`](../../backend/internal/httpserver/) | The HTTP handler and the server lifecycle. `New` assembles the mux: health endpoints, `/api/v1/version`, a JSON 404 for every other path, a 405 for a known path with the wrong method, a request log around all of it. `Serve` runs an `http.Server` until the context is cancelled and shuts it down within the timeout | `Options`, `New`, `ListenAndServe`, `Serve`, `handleGet`, `writeJSON`, `writeError` |
| [`backend/internal/store/`](../../backend/internal/store/) | PostgreSQL: the connection pool and the schema migrations. `Migrate` opens `database/sql` through the pgx stdlib adapter, applies the embedded migrations with golang-migrate and reports the version; `Connect` returns a `pgxpool.Pool` after a ping | `Connect`, `Migrate`, `MigrateResult`, `MigrationsFS`, `MigrationsTable` |
| [`backend/internal/store/migrations/`](../../backend/internal/store/migrations/) | The schema, as `NNNNNN_<name>.up.sql` / `.down.sql` pairs, embedded. The unit test enforces the naming, the pairing and the gap-free sequence | `000001_tenants` |
| [`backend/test/integration/`](../../backend/test/integration/) | The integration tier (build tag `integration`): the tests that need a real PostgreSQL 18 at `COWORK_TEST_DATABASE_URL` | `TestMigrateBringsFreshDatabaseToCurrentVersion`, `TestTenantIdsAreUUIDv7` |
| [`backend/Containerfile`](../../backend/Containerfile) | Two stages: `golang:1.27-alpine` builds the static binary, distroless `nonroot` runs it | — |
| [`backend/.golangci.yml`](../../backend/.golangci.yml) | The linter set; golangci-lint runs from `backend/` | — |

## Frontend (`frontend/`, Angular project `frontend`)

| Path | Responsibility |
|---|---|
| [`frontend/src/app/app.ts`](../../frontend/src/app/app.ts) | The shell: header, router outlet, footer with the backend version (or "backend unreachable") |
| [`frontend/src/app/app.config.ts`](../../frontend/src/app/app.config.ts) | Application providers: router, `HttpClient` with fetch, the global error listeners |
| [`frontend/src/app/app.routes.ts`](../../frontend/src/app/app.routes.ts) | The routes; empty |
| [`frontend/src/app/core/version.service.ts`](../../frontend/src/app/core/version.service.ts) | `GET /api/v1/version` and the `VersionInfo` type that mirrors the Go handler |
| [`frontend/proxy.conf.json`](../../frontend/proxy.conf.json) | `ng serve` forwards `/api`, `/healthz`, `/readyz` to the backend on `:8080` — the same shape nginx has in the container |
| [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template) | The container's nginx configuration: the bundle with `index.html` fallback, immutable hashed assets, its own `/healthz`, `/api/` proxied to `${BACKEND_URL}` |
| [`frontend/Containerfile`](../../frontend/Containerfile) | Two stages: `node:26-alpine` builds the bundle, `nginxinc/nginx-unprivileged` serves it; `NGINX_ENVSUBST_FILTER` limits the template substitution to `BACKEND_URL` |
| [`frontend/eslint.config.js`](../../frontend/eslint.config.js) | The angular-eslint configuration `ng lint` runs |
| [`frontend/angular.json`](../../frontend/angular.json) | The build (`@angular/build:application`, output `dist/frontend/browser`), the test runner (`@angular/build:unit-test`, vitest on jsdom), the dev server with the proxy |

## Delivery

| Path | Responsibility |
|---|---|
| [`Makefile`](../../Makefile) | Every build, test, lint and tooling target; the entry point for CI and developers. Go targets `cd backend`, npm targets `cd frontend`; tools, `bin/` and `coverage/` land at the root |
| [`deploy/helm/cowork/`](../../deploy/helm/cowork/) | The chart: backend and frontend Deployments and Services, optional Ingress on the frontend, optional database Secret, one ServiceAccount without a token; `ci/*-values.yaml` are the shapes CI lints and renders |
| [`.github/workflows/release.yml`](../../.github/workflows/release.yml) | "Test and Release": every gate on push and PR, one container-scan leg per image, semantic-release on `main` |
| [`.github/workflows/build.yml`](../../.github/workflows/build.yml) | "Release Docker & Helm": both images with SBOM and scan, the chart to `gh-pages`, on a published release |
| [`.github/workflows/renovate.yml`](../../.github/workflows/renovate.yml) | Self-hosted Renovate, daily |
| [`hack/verify-release-tooling.mjs`](../../hack/verify-release-tooling.mjs) | Drives the semantic-release plugins against a synthetic commit set, so a broken preset/writer pair fails a PR, not a release |
| [`package.json`](../../package.json) | The semantic-release dependency set (root; the UI has its own `frontend/package.json`) |
