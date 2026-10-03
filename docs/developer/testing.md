# Testing

The test tiers, what each needs, the fixtures a test builds on, and the environment variables
that steer the suites. **The rules — what belongs in which tier, what may be skipped (nothing),
what a fix has to prove — are [ADR 0003](../adr/0003-test-and-ci-policy.md) and are not restated
here.** The Make targets themselves are listed in [build-test-lint.md](build-test-lint.md).

Read against the tree on 2026-10-03.

## The tiers

| Tier | Command | Build tag | Needs | What it is for |
|---|---|---|---|---|
| Backend unit | `make test-unit` | none | nothing running | Configuration, the domain rules, tokens and authorization, passwords and sessions, the CSRF rule, the client address, the event hub, the Markdown grammar, the outer handler and the server lifecycle, the command dispatch, the API document's completeness (`TestEveryOperationIsDeclaredCompletely`, which holds the session-only operations to a set of six), and the lints over the migration set and the query files |
| Backend integration | `make test-integration` | `integration` | PostgreSQL 18 at `COWORK_TEST_DATABASE_URL` and an S3 server at `COWORK_TEST_S3_*` (`make postgres-up minio-up` provides both) | What only the database and the whole handler decide: migrations, roles, isolation, the wrappers, every API route, the login with its lockout and sessions, the start-up synchronisation |
| Frontend unit | `make frontend-test` | — | Node.js and `frontend/node_modules` (`make frontend-install`) | Components and services, vitest on jsdom, no browser |
| Chart | `make helm-lint`, `make helm-template` | — | Helm | Strict lint and a render per `deploy/helm/cowork/ci/*-values.yaml` |
| Release tooling | `make test-release-tooling` | — | Node.js and `npm ci` at the root | The semantic-release plugins still render notes |
| End-to-end | `make e2e` (planned) | — | the built images, PostgreSQL, MinIO, Dex, a browser | **Not built.** Decided in [ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md): Playwright in `frontend/e2e/`, two identities, both colour schemes |

Per-tier timeouts: the integration target passes `-timeout=10m`; the others use the Go default.

## Backend unit tests

They sit next to the code under `backend/`. Two `httpserver` tests listen on `127.0.0.1:0`
(`TestServeShutsDownOnContextCancel`, `TestListenAndServeReportsBindError`), and
`TestTheBinaryContainsNoTestPackage` runs `go list -deps`; nothing else opens a socket or needs a
tool.

| Fixture | Where | What it gives you |
|---|---|---|
| `envOf(map[string]string)` | [`config_test.go`](../../backend/internal/config/config_test.go), [`main_test.go`](../../backend/cmd/cowork/main_test.go) | A lookup with the contract of `os.LookupEnv` over a map; the way every test sets configuration without touching the process environment |
| `do(t, handler, method, target)`, `decode(t, res)`, `problemOf(t, res)` | [`server_test.go`](../../backend/internal/httpserver/server_test.go) | One request through the handler, one JSON body as a map, a problem body with its envelope checked: media type, `type` from `code`, `status`, `request_id` equal to `X-Request-Id` |
| `newRecordingLogger(&lines)` | [`logger_test.go`](../../backend/internal/httpserver/logger_test.go) | A `slog.Logger` that collects records as maps, for asserting on the request log |
| `Options.Ready`, `Options.API` | [`server.go`](../../backend/internal/httpserver/server.go) | Inject a readiness check, or a stand-in for the API handler |
| `newFixtures()`, `note`, `filter`, `Hub.now` | [`hub_test.go`](../../backend/internal/events/hub_test.go) | Notifications and filters without a database; a fixed clock for the replay window |
| `Options.Now` | [`api.go`](../../backend/internal/api/api.go) | The clock every session and login window reads; a test that moves it ages a session or a lock without waiting ([`session_test.go`](../../backend/internal/api/session_test.go) `sessionLive`, the integration tier's `clock`) |
| `auth.Computations()` | [`password.go`](../../backend/internal/auth/password.go) | A counter of the Argon2id computations this process made, to assert that a refusal costs what a success costs and that a throttled attempt computes nothing — exported for the integration tier and read by nothing else |
| the golden files, `-update` | [`markdown_test.go`](../../backend/internal/markdown/markdown_test.go) | `TestRender` compares `Render` with `testdata/*.md`; `cd backend && go test ./internal/markdown -update` rewrites them after a deliberate change ([markdown-grammar.md](markdown-grammar.md#changing-the-grammar)) |

The lints run in this tier, without a database:

| Test | Holds |
|---|---|
| `TestMigrationFilesAreWellFormed` ([`migrate_test.go`](../../backend/internal/store/migrate_test.go)) | every file matches `NNNNNN_<snake_name>.up.sql`, none is empty, the versions are 1..n without a gap |
| `TestEveryTableHasItsPolicyAndGrant` ([`policy_test.go`](../../backend/internal/store/policy_test.go)) | every table has row-level security enabled and forced, a policy and a grant to the runtime role; outside the named list, `tenant_id` and the canonical `tenant_isolation` policy |
| `TestPoliciesReadSettingsGuarded` | every `current_setting('app.…')` in a migration is guarded by `NULLIF` (or is the `app.job` comparison) |
| `TestNothingCascadesIntoTheAuditRecord` | no `ON DELETE` in `audit_events`, and its grant is `SELECT, INSERT` |
| `TestLiftedForceIsRestoredInTheSameMigration` | a migration that lifts the force of row-level security on a table restores it later in the same file |
| `TestEveryReadOfProjectsAndTicketsCarriesTheVisibilityPredicate` ([`queries_test.go`](../../backend/internal/store/queries_test.go)) | the visibility lint ([data-access.md](data-access.md#visibility-in-sql)) |
| `TestTicketListSelectsWhatTheQueriesSelect` | the list builder's columns and joins equal `GetTicketByNumber`'s |
| `TestTheBinaryContainsNoTestPackage` ([`main_test.go`](../../backend/cmd/cowork/main_test.go)) | `cmd/cowork` depends on nothing under `backend/test/` |
| `TestEveryOperationIsDeclaredCompletely` ([`document_test.go`](../../backend/api/document_test.go)) | every operation has an id, a tag, the problem response and a security requirement of one of three shapes: public (empty), the session cookie alone (exactly `logout`, `changeMyPassword`, `createMyToken`, `createTenant`, `createAccount`, `resetAccountPassword`), or the bearer token and the session cookie; a public write carries `x-cowork-origin-check` |

The `run(ctx, args, lookup, stdout, stderr)` tests in `backend/cmd/cowork` cover the command
dispatch and the configuration errors without a database: `migrate` without
`COWORK_DATABASE_URL` or without `COWORK_DATABASE_OWNER_URL`, `serve` without the database URL or
— while it migrates on start — without the owner URL, and with a local administrator's
username but no password or the other way round (`TestServeRefusesAHalfConfiguredLocalAdministrator`),
each exit 1 naming the variable; an unparsable URL exits 1 with "migration failed".

The unit tests of the login's pieces: [`password_test.go`](../../backend/internal/auth/password_test.go)
(the recorded parameters, the salts, verification by the parameters a hash records, the refusal of a
malformed or oversized hash, the context, the dummy hash, the policy),
[`session_test.go`](../../backend/internal/auth/session_test.go) (the cookie value and its shape, the
username rule), [`api/session_test.go`](../../backend/internal/api/session_test.go) (the cookie's
attributes, the CSRF table of headers, the fail-closed rule, both limits of a session, the keyed hash
of an address, the option defaults, the credentials read from the document),
[`api/clientaddr_test.go`](../../backend/internal/api/clientaddr_test.go) (the walk of
`X-Forwarded-For` from the right: no header, one hop, chains of trusted hops, spoofed entries to the
left, malformed entries, IPv6, IPv4-mapped IPv6, several header lines, the empty list, a long
chain; `FuzzClientAddress` holds that an untrusted peer is always the client — its seeds run with
`go test`, `go test ./internal/api -run '^$' -fuzz FuzzClientAddress` explores) and the login cases of
[`config_test.go`](../../backend/internal/config/config_test.go) (all-or-neither, the password floor,
the lockout mode, the lifetimes, the base URL as an origin, the bootstrap tenant, the trusted
proxies as CIDRs and the error that quotes the offending entry only).

The rules of the states and the progress stages without a database:
[`domain_test.go`](../../backend/internal/domain/domain_test.go) (the move matrix, what a write of
the stages does, which done is by the stages) and
[`api/transitions_test.go`](../../backend/internal/api/transitions_test.go) (the transition and
`PATCH` checks for persons and agents over `store.TicketRow` values built by `in`, the stages and
the done by hand a ticket shows — rows the release before the stages left included).

## Backend integration tests

[`backend/test/integration/`](../../backend/test/integration/), build tag `integration`.
`TestMain` ([`main_test.go`](../../backend/test/integration/main_test.go)) prepares one run:

1. `COWORK_TEST_DATABASE_URL` and the three `COWORK_TEST_S3_*` variables are required; a missing
   one ends the run with exit 1 and the command that sets it. There is no skip.
2. A bucket of its own on the S3 server, `cowork-it-<nanoseconds>`.
3. Over the administrative URL: the roles `cowork_it_owner` and `cowork_it_app` (the latter
   `NOSUPERUSER NOBYPASSRLS`) when they are missing, and a database of its own,
   `cowork_it_<nanoseconds>`, owned by the owner role; it is dropped `WITH (FORCE)` at the end.
4. `store.Migrate` as the owner role with `cowork_it_app` as the runtime role — the result is
   `env.Migrated`.

Every store and API test then connects as the runtime role, as `cowork serve` does; the
administrative connection only creates and seeds. The tests share the run's database and keep
apart by unique slugs, so nothing resets between them.

```
make postgres-up minio-up             # postgres:18 on :5432, MinIO on :9000
make test-integration                 # exports COWORK_TEST_DATABASE_URL and COWORK_TEST_S3_* for them
COWORK_TEST_DATABASE_URL=… make test-integration   # any other PostgreSQL 18, as a role that may create roles and databases
make postgres-down minio-down
```

`POSTGRES_PORT=55432 make postgres-up test-integration` moves the container off a port that is
taken; `MINIO_PORT=` does the same for MinIO.

### Fixtures of the integration tier

| Fixture | Where | What it gives you |
|---|---|---|
| `fixture.DB` — `fixtures(t)` | [`test/fixture/fixture.go`](../../backend/test/fixture/fixture.go), [`helpers_test.go`](../../backend/test/integration/helpers_test.go) | Rows written over the administrative connection, past row-level security and without audit rows: `Person`, `Tenant`, `Member` (a marked grant), `Account` (a local account with an Argon2id hash of the password, managed by a tenant, optionally with a temporary password), `GlobalAdmin`, `Project` (with its counter), `Ticket` (a plain task with the next number and no rank, as a release before the rank files one), `Token(TokenSpec)` returning the plaintext once; `Exec`, `Query`, `QueryRow`, `QueryCount` for a state no route reaches. A zero `TokenSpec` is a write token of ninety days; `Agent` without capabilities holds every one, `fixture.AssistedCapabilities` is the "assisted" set |
| `world`, `newWorld(t)` | `helpers_test.go` | Two tenants with unique slugs; an admin, a member and a viewer of A, a member of B, a person in both; the projects `ALPHA` in A and `BETA` in B |
| `seedEveryTenantTable(t, w)`, `tenantBoundTables(t)` | `helpers_test.go` | A row of each tenant in every table with `tenant_id`, and the list of those tables from the catalog. `TestUnfilteredQueryUnderTenantSeesNothingOfAnother` walks the catalog and fails for a table the seed leaves empty |
| `openRuntime(t)`, `openStore(t, url)`, `as(person)` | `helpers_test.go` | The store as the runtime role (or another URL); a context whose store calls act for a person |
| `issueTokens(t, w)` | [`api_helpers_test.go`](../../backend/test/integration/api_helpers_test.go) | A token per person of the world, the administrator's with admin and with write scope, an agent token with every capability and an assisted one |
| `newAPI(t, opts…)` → `apiServer` | `api_helpers_test.go` | The whole handler on `httptest`: health, request id, log and pipeline, the runtime role, a real event listener, the run's bucket (1 MiB files, five per ticket) and response validation on; `opts` change `api.Options` |
| `apiServer.client(t, caller)`, `apiServer.do(…)`, `assertProblem(t, res, status, code)` | `api_helpers_test.go` | The generated Go client acting as a token and agent header; a raw request for what the client cannot express; a problem's status, code and envelope |
| `withLogin`, `testOrigin`, `testPassword` | [`login_helpers_test.go`](../../backend/test/integration/login_helpers_test.go) | What a cookie login needs on `api.Options`: the base origin, the lockout, the password minimum — and the per-address throttle **off**, because every test of the run reaches the server from `127.0.0.1` and one shared limit would make the rest depend on speed; the tests of the throttle switch it on with a server key of their own, and with `Options.TrustedProxies` naming `127.0.0.0/8` the test server plays the proxy and `X-Forwarded-For` names the clients |
| `browser`, `apiServer.browser(t)`, `login`, `mustLogin`, `get`, `request` | `login_helpers_test.go` | A client that holds one session cookie and sends `Origin` and `X-Requested-With` on unsafe methods, as the frontend does; `withHeader`, `without` and `withBearer` take one away or add one to see a refusal |
| `clock`, `newClock()`, `withClock` | `login_helpers_test.go` | A clock a test moves, installed as `api.Options.Now`: hours of a session and minutes of a lock pass without waiting |
| `withAccounts(t, w)` | `login_helpers_test.go` | Local accounts with `testPassword` for the persons of a world, managed by tenant A (B's member by B), and their usernames |
| `isolated`, `newIsolated(t)` | `login_helpers_test.go` | A database of its own, migrated and empty, with the runtime role's store open on it, for what needs an installation that has no tenant and no person: the init state, the start-up synchronisation |
| `recordingLogger` | `login_helpers_test.go` | A logger that collects every record at every level as text, for searching a run for a secret |
| `simultaneously(sends…)`, `times(n, send)` | `api_helpers_test.go` | Starts requests at the same instant and collects their status codes, for the races a conditional write can lose: the request that comes second must answer as a later one would, never `500`. A send runs in its own goroutine, so it builds nothing with `require` |
| `ticketEnv`, `newTicketEnv(t)`, `task(…)`, `file(…)` | [`api_tickets_test.go`](../../backend/test/integration/api_tickets_test.go) | A world with its tokens and a running API; a plain ticket body; filing as a caller, with a key for an agent |
| `openStream`, `next` | [`api_events_test.go`](../../backend/test/integration/api_events_test.go) | An event stream read message by message |

### Response validation

`newAPI` sets `api.Options.ValidateResponses`: `serveValidated` runs the handler into a
recorder, validates status, headers and body against the API document, and answers `500` naming
the mismatch instead of the response. The server's warnings and errors go to the test's log
(`testLog`). A handler that drifts from the document fails the test that exercises it
([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md) D4); the event stream
is not a generated response and is not validated.

### What runs

| File | What it proves |
|---|---|
| [`migrate_test.go`](../../backend/test/integration/migrate_test.go) | A fresh database reaches the embedded version, a second run applies nothing, the runtime role reads the version; PostgreSQL 18 or newer; a schema ahead of the binary is served; tenant ids are UUIDv7; migration 17 ranks every project's open tickets in number order and restores the force it lifts (`TestRankMigrationKeepsNumberOrder`, on a database of its own that `migrateTo` brings to version 16 first); migrations 18 and 19 backfill the three progress stages, `done_from` and `done_by_hand` on the tickets a release before them left, derive the parents' new stages a level at a time, grant the new columns and restore the force (`TestStagesMigrationBackfill`, from version 17) |
| [`store_test.go`](../../backend/test/integration/store_test.go) | The runtime role check; an unfiltered query under tenant A sees nothing of B in any tenant-bound table; the context dies with its transaction; the wrappers; the append-only audit record; `Mutate`'s acts, rollbacks and idempotency, concurrent duplicates included; the expiry job and its lock; the token lookup, refusal bound and last-used date |
| [`api_core_test.go`](../../backend/test/integration/api_core_test.go) | Unauthenticated meta routes, unknown routes and methods, authentication and the agent header, one tenant's token in another, `/me` and tokens, tenant settings, the audit view, the body limit, cursors, validation |
| [`api_boundary_test.go`](../../backend/test/integration/api_boundary_test.go) | Every tenant route refuses another tenant's token exactly like an unknown tenant (`TestEveryTenantRouteRefusesAnotherTenantLikeNoTenant`); the routes come from a walk over the document (`tenantRoutes`), shared with the session test below, so the account routes are covered the day they exist; `POST /tenants` has no tenant in its path and is tested by `TestOnlyAGlobalAdministratorCreatesATenant` |
| [`api_login_test.go`](../../backend/test/integration/api_login_test.go) | The login through the whole handler: the session and its cookie, every failure answering identically at the same cost, the lockout in both modes and its window, the per-address throttle — also behind a trusted proxy, where two clients are throttled apart and a spoofed entry moves nobody, and for a peer that is no proxy, whose header is ignored —, the init state, the options, both session limits on a moved clock, a session surviving a restart, logout, the expiry of the login's state |
| [`api_accounts_test.go`](../../backend/test/integration/api_accounts_test.go) | A temporary password gating the session, a password change counting and ending the other sessions, the account routes per role and across two tenants (what a tenant's administrator manages and does not), a token refused on creating an account and on resetting a password while it still lists, unlocks, deactivates and ends sessions (`TestAccountRoutesAnAdministratorsTokenMayStillCall`), deactivation ending tokens and sessions, a lock not inherited by a new account of the same name, a session's idempotency key scoped to its person |
| [`api_session_routes_test.go`](../../backend/test/integration/api_session_routes_test.go) | The routes only a session calls: a token created and shown once, its lifetime clamped, its idempotency; a tenant created by a global administrator only; the CSRF refusals (the `Referer` fallback, no origin, a second header, a cookie beside a token); the event stream ending with its session; the cross-tenant harness again with a cookie; no password, cookie or token in the log, the answers or the audit record |
| [`policy_login_test.go`](../../backend/test/integration/policy_login_test.go) | The policies of the persons, their accounts and their sessions as the runtime role sees them, with no handler in front; the session lookup finds the presented row only |
| [`bootstrap_test.go`](../../backend/test/integration/bootstrap_test.go) | The start-up synchronisation on an isolated database: created, left alone, re-hashed with the sessions ended, deactivated and reactivated, taken over from a tenant's account of the same name, four replicas at once |
| `api_projects_test.go`, `api_tickets_test.go`, `api_rank_test.go`, `api_links_test.go`, `api_transitions_test.go`, `api_stages_test.go`, `api_questions_test.go`, `api_comments_test.go`, `api_interest_test.go`, `api_progress_test.go`, `api_time_test.go`, `api_attachments_test.go`, `api_events_test.go`, `api_export_test.go` | The rules of [domain.md](domain.md), [storage.md](storage.md), [events.md](events.md) and [markdown-grammar.md](markdown-grammar.md), route by route, across tenants, restricted projects, confidential tickets, roles, scopes and agents; `api_stages_test.go` the state `review`, done by hand and its withdrawal, done by the stages and the reopen, their refusals for persons and agents, a parent's stages, an open ticket whose stages are full, what the release before the stages writes over this schema in a rollback (its statements verbatim), the override that holds, `done_after` and the `review` limit |

## Frontend unit tests

`ng test` with the `@angular/build:unit-test` builder, vitest, jsdom. `CI=true` and
`--watch=false` make it run once; the Make targets set both. Coverage comes from
`@vitest/coverage-v8`, a dev dependency of the frontend (`make frontend-test-coverage`,
reports under `frontend/coverage/frontend/`: `text-summary` on the console, `lcov.info`,
`coverage-summary.json` for CI).

| Pattern | Where |
|---|---|
| `provideHttpClient()` + `provideHttpClientTesting()`, `HttpTestingController.expectOne(url).flush(...)`, `http.verify()` in `afterEach` | [`app.spec.ts`](../../frontend/src/app/app.spec.ts), [`version.service.spec.ts`](../../frontend/src/app/core/version.service.spec.ts) |
| `await fixture.whenStable()` after flushing, then read the DOM | `app.spec.ts` — the application is zoneless; `whenStable` settles the signal that `toSignal` feeds |
| `data-testid` attributes for assertions | [`app.html`](../../frontend/src/app/app.html) |
| A service against the generated client: the real `Api`, `HttpTestingController` answering each URL the client calls | [`tickets.service.spec.ts`](../../frontend/src/app/core/tickets.service.spec.ts), [`auth.service.spec.ts`](../../frontend/src/app/core/auth.service.spec.ts) |
| A component against mocked services: `{ provide: <Service>, useValue: {...} }` with signals and `vi.fn()` | [`ticket-detail.spec.ts`](../../frontend/src/app/features/ticket/ticket-detail.spec.ts) |
| The event stream without a network: a fake `EventSource` through the `EVENT_SOURCE` token, its events pushed by the test | [`event-stream.service.spec.ts`](../../frontend/src/app/core/event-stream.service.spec.ts) |
| Time: `{ provide: Clock, useValue: { now } }` for what a page shows; `vi.useFakeTimers()` and `vi.advanceTimersByTimeAsync` for debounces, the polling fallback and retries | [`time-report.spec.ts`](../../frontend/src/app/features/time/time-report.spec.ts), [`tickets.service.spec.ts`](../../frontend/src/app/core/tickets.service.spec.ts) |
| A PrimeNG overlay (a select's panel) asks `matchMedia`, which jsdom lacks: `vi.stubGlobal('matchMedia', …)` in the test that opens one | [`new-token-dialog.spec.ts`](../../frontend/src/app/features/me/new-token-dialog.spec.ts) |

## Container check

Neither image has a unit test; what proves them is building and running them. CI builds each
`Containerfile` from its directory and scans the image (one `container-malware-scan` leg per
image). Locally, `make docker-build` builds both, and
[build-test-lint.md](build-test-lint.md#run-the-images-together) is the recipe for running them
together read-only; `make verify-phase-2` scripts the API half of that run by hand, and the
nginx checks stay manual until the end-to-end tier exists.

## Chart tests

`helm lint --strict` on the default values and on each `ci/*-values.yaml`; `helm template` on
each `ci/*-values.yaml`. The default values name no database URL, no owner URL and no server-key
Secret, and the helpers `fail` on each — `helm lint` reports them as `[INFO]`, `helm template`
fails on the first. That is why the template target runs only with the `ci/` files, each of
which sets all three.

## Environment variables the suites read

| Variable | Read by | Meaning |
|---|---|---|
| `COWORK_TEST_DATABASE_URL` | the integration tier | An administrative URL to PostgreSQL 18 — a role that may create roles and databases and is not held by row-level security; required |
| `COWORK_TEST_S3_ENDPOINT`, `COWORK_TEST_S3_ACCESS_KEY_ID`, `COWORK_TEST_S3_SECRET_ACCESS_KEY` | the integration tier | The S3 server and its keys; required |
| `CI` | vitest through `ng test` | Non-interactive reporter and no watch |
| `POSTGRES_PORT`, `POSTGRES_IMAGE`, `POSTGRES_CONTAINER` | `make postgres-up` | Where and what to start locally |
| `MINIO_PORT`, `MINIO_IMAGE`, `MINIO_CONTAINER`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY` | `make minio-up`, `make test-integration` | Where and what to start locally, and the keys the tests are given |
| `COWORK_DEV_SEED_DATABASE_URL` | `test/devseed` | The administrative URL `make dev-seed` writes through |
