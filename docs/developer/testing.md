# Testing

The test tiers, what each needs, the fixtures a test builds on, and the environment variables
that steer the suites. **The rules — what belongs in which tier, what may be skipped (nothing),
what a fix has to prove — are [ADR 0003](../adr/0003-test-and-ci-policy.md) and are not restated
here.** The Make targets themselves are listed in [DEVELOPER.md](../../DEVELOPER.md#build-test-and-lint).

Read against the tree on 2026-09-29.

## The tiers

| Tier | Command | Build tag | Needs | What it is for |
|---|---|---|---|---|
| Backend unit | `make test-unit` | none | nothing running | Configuration, handler (health, version, 404, 405, request log), server lifecycle, migration-set well-formedness |
| Backend integration | `make test-integration` | `integration` | PostgreSQL 18 at `COWORK_TEST_DATABASE_URL` (`make postgres-up` provides one) | What only the database decides |
| Frontend unit | `make frontend-test` | — | Node.js and `frontend/node_modules` (`make frontend-install`) | Components and services, vitest on jsdom, no browser |
| Chart | `make helm-lint`, `make helm-template` | — | Helm | Strict lint and a render per `deploy/helm/cowork/ci/*-values.yaml` |
| Release tooling | `make test-release-tooling` | — | Node.js and `npm ci` at the root | The semantic-release plugins still render notes |
| End-to-end | — | — | — | **Not built.** Decided in ADR 0003 D2 |

Per-tier timeouts: the integration target passes `-timeout=10m`; the others use the Go default.

## Backend unit tests

They sit next to the code under `backend/`. Nothing in them opens a socket except
`TestServeShutsDownOnContextCancel`, which listens on `127.0.0.1:0`.

| Fixture | Where | What it gives you |
|---|---|---|
| `envOf(map[string]string)` | [`config_test.go`](../../backend/internal/config/config_test.go), [`main_test.go`](../../backend/cmd/cowork/main_test.go) | A lookup with the contract of `os.LookupEnv` over a map; the way every test sets configuration without touching the process environment |
| `do(t, handler, method, target)`, `decode(t, res)`, `apiErrorOf(t, res)` | [`server_test.go`](../../backend/internal/httpserver/server_test.go) | One request through the handler, one JSON body as a map, the `error` object out of it |
| `newRecordingLogger(&lines)` | [`logger_test.go`](../../backend/internal/httpserver/logger_test.go) | A `slog.Logger` that collects records as maps, for asserting on the request log |
| `Options.Ready` | [`server.go`](../../backend/internal/httpserver/server.go) | Inject a readiness check; `/readyz` reports its error verbatim |
| `MigrationsFS()`, `migrationFilePattern` | [`migrate.go`](../../backend/internal/store/migrate.go) | The embedded migration set and the file-name pattern the well-formedness test enforces |

A `run(ctx, args, lookup, stdout, stderr)` test in `backend/cmd/cowork` covers the command
dispatch and the configuration errors without a database: `migrate` and `serve` with no
`COWORK_DATABASE_URL` exit 1 naming the variable; an unparsable URL exits 1 with "migration
failed".

## Backend integration tests

[`backend/test/integration/`](../../backend/test/integration/), build tag `integration`. `databaseURL(t)` reads
`COWORK_TEST_DATABASE_URL` and **fails** when it is unset, with the command that sets it. There
is no skip.

What runs today: `Migrate` on a fresh database reaches the highest version; a second run
applies nothing; `server_version_num >= 180000`; `tenants` exists; the `schema_migrations` row
matches; an inserted tenant carries a UUIDv7 (`uuid_extract_version(id) = 7`).

The tests do not reset the database. CI gets a fresh service container per job; locally,
`make postgres-down && make postgres-up` gives you the same.

```
make postgres-up                      # postgres:18 on localhost:5432, user/password/db = cowork
make test-integration                 # exports COWORK_TEST_DATABASE_URL to that container
COWORK_TEST_DATABASE_URL=… make test-integration   # any other PostgreSQL 18
make postgres-down
```

`POSTGRES_PORT=55432 make postgres-up test-integration` moves the container off a port that is
taken.

## Frontend unit tests

`ng test` with the `@angular/build:unit-test` builder, vitest, jsdom. `CI=true` and
`--watch=false` make it run once; the Make targets set both. Coverage comes from
`@vitest/coverage-v8` (`make frontend-test-coverage`, reports under `frontend/coverage/`:
`text-summary` on the console, `lcov.info`, `coverage-summary.json` for CI).

| Pattern | Where |
|---|---|
| `provideHttpClient()` + `provideHttpClientTesting()`, `HttpTestingController.expectOne(url).flush(...)`, `http.verify()` in `afterEach` | [`app.spec.ts`](../../frontend/src/app/app.spec.ts), [`version.service.spec.ts`](../../frontend/src/app/core/version.service.spec.ts) |
| `await fixture.whenStable()` after flushing, then read the DOM | `app.spec.ts` — the application is zoneless; `whenStable` settles the signal that `toSignal` feeds |
| `data-testid` attributes for assertions | [`app.html`](../../frontend/src/app/app.html) |

## Container check

Neither image has a unit test; what proves them is building and running them. CI builds each
`Containerfile` from its directory and scans the image (one `container-malware-scan` leg per
image). Locally, `make docker-build` builds both, and the manual run recorded in
[ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
is the recipe: both containers on one Docker network, the frontend with
`--read-only --tmpfs /tmp --tmpfs /etc/nginx/conf.d --user 101:101` and
`BACKEND_URL=http://<backend alias>:8080`, then `curl` through the frontend for `/healthz`,
`/api/v1/version`, a deep link and a hashed asset. Turning that into a scripted target is part
of the end-to-end tier when it is built.

## Chart tests

`helm lint --strict` on the default values and on each `ci/*-values.yaml`; `helm template` on
each `ci/*-values.yaml`. The default values render no database Secret and the helper `fail`s
when neither `database.existingSecret` nor `database.url` is set — `helm lint` reports that
`fail` as `[INFO]`, `helm template` fails on it. That is why the template target runs only
with the `ci/` files, each of which sets one of the two.

## Environment variables the suites read

| Variable | Read by | Meaning |
|---|---|---|
| `COWORK_TEST_DATABASE_URL` | the integration tier | The PostgreSQL 18 to run against; required |
| `CI` | vitest through `ng test` | Non-interactive reporter and no watch |
| `POSTGRES_PORT`, `POSTGRES_IMAGE`, `POSTGRES_CONTAINER` | `make postgres-up` | Where and what to start locally |
