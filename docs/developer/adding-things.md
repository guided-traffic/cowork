# Adding things

Ordered checklists for the changes that recur. Each names the files to touch and the page
to update in the same change.

## An API operation

The document comes first ([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md);
the mechanics are [api.md](api.md)).

1. **The document.** Add the path to [`backend/api/openapi.yaml`](../../backend/api/openapi.yaml)
   as a `$ref` into its path family's file, or a new file for a new family. Give the operation an
   `operationId`, tags, its parameters from `components/parameters.yaml`, its schemas in
   `components/schemas.yaml` (`additionalProperties: false` on a request body), the `ETag` and
   `Location` headers from `components/headers.yaml`, and `default:
   $ref: './components/responses.yaml#/Problem'`. The default security is the bearer token;
   `security: []` only for what a client reads before it authenticates. A creating `POST` takes
   the `IdempotencyKey` parameter, an overwriting write `IfMatch`, a list `Cursor` and `Limit`.
2. **`make generate`.** The build now fails until `Server` implements the new method of
   `apigen.StrictServerInterface`.
3. **The handler**, a method of `Server` in the family's file under
   [`backend/internal/api/`](../../backend/internal/api/): `tenantFrom(ctx)`; `auth.Authorize`
   with a `Need`; reads in `s.db.InTenant` through `visibleProject` / `visibleTicket`; writes in
   `s.db.Mutate`, every act recorded with `w.Record`; `keyed` and `w.Respond(stored(…))` for a
   creating `POST`; `ifMatch` and `stale` for an overwriting write; `store.ErrNoChange` for a
   write that changes nothing; errors as `problem.*`, never ad-hoc JSON.
4. **New SQL** is a named query in `backend/internal/store/queries/read/` or `write/`, carrying
   the visibility predicate or naming its exemption ([data-access.md](data-access.md#visibility-in-sql));
   `make generate` again.
5. A route under `{tenant}` but outside `{project}` is refused to a project-restricted token,
   unless its operation is in `tenantWideForProjectTokens` in
   [`tenant.go`](../../backend/internal/api/tenant.go) — and then the data layer must narrow it to
   the token's project.
6. **Tests** in `backend/test/integration/`, through `newAPI` (responses are validated) and the
   generated client: both tenants, a restricted project and a confidential ticket (the same
   `404` as a missing one), each role and scope, an agent with and without the capability or on
   the hard-off list, `428`/`412` for an overwriting write, replay and mismatch for a keyed one.
7. The row in [README.md, API](../../README.md#api-backend); `make generate-check` clean, the
   generated files committed.

## A table

1. **A migration** (below) that creates it with `tenant_id uuid NOT NULL`; a composite foreign
   key `(tenant_id, …)` to each parent — a plain foreign key ignores row-level security and would
   let a row reference another tenant's parent; `UNIQUE (tenant_id, id)` when children
   reference it; indexes that lead with `tenant_id`; `version integer NOT NULL DEFAULT 1` when
   the entity is mutable ([ADR 0050](../adr/0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md) D1).
2. `ALTER TABLE … ENABLE ROW LEVEL SECURITY;`, `… FORCE ROW LEVEL SECURITY;` and the
   `tenant_isolation` policy exactly as the existing migrations write it — `policy_test.go`
   matches the text. A table without a tenant goes on the named list there and in
   [ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D6, with a
   policy of its own that reads settings only through `app_tenant_id()`, `app_user_id()` or a
   `NULLIF`.
3. The grants in a `DO` block to `current_setting('cowork.runtime_role')`: `SELECT`, `INSERT`,
   `UPDATE` on the columns a route changes, `DELETE` only where rows are really removed.
4. Every query that reads it from a ticket or a project joins that ticket and calls
   `app_ticket_visible`. The visibility lint enforces the predicate once the ticket is joined;
   the join itself is the author's to remember ([data-access.md](data-access.md#visibility-in-sql)).
5. A row of each tenant in `seedEveryTenantTable` in
   [`helpers_test.go`](../../backend/test/integration/helpers_test.go), or
   `TestUnfilteredQueryUnderTenantSeesNothingOfAnother` fails for the new table.
6. A database enum the Go code reads gets its `domain` type in
   [`sqlc.yaml`](../../backend/sqlc.yaml); `make generate`, `make test-unit`,
   `make postgres-up minio-up test-integration`.

## A migration

1. Next number, one file: `backend/internal/store/migrations/NNNNNN_<snake_name>.up.sql`.
   There is no down file. The unit test refuses a gap, a stray file and an empty file. The
   migration may add and widen; it may not drop, rename or narrow anything the previous
   release still reads — removal is a later release's migration
   ([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)).
   It runs as the owner role; whatever the runtime role needs is granted in the same file.
2. Run `make postgres-up minio-up test-integration`; add an assertion there for what only the
   database proves.
3. If the schema encodes a decision, the ADR is written in the same change.

## A problem code

1. A `Code{…}` variable in [`backend/internal/problem/problem.go`](../../backend/internal/problem/problem.go)
   — snake_case code, status, title, the meaning with the record it comes from — and its place
   in `Catalogue`, which is the order of the README table.
2. `make generate`: the `ProblemCode` enum in `api/components/problem-codes.yaml` and the table in
   [README.md, Problem codes](../../README.md#problem-codes) follow.
3. Answer it with `problem.New` or a `&problem.Error{…}`; an integration test asserts status and
   code (`assertProblem`). A code nginx answers itself also needs its static body in the
   template (as `backend_unreachable`).

## A configuration variable

1. The `Env…` constant, the field, the default and the validation in
   [`backend/internal/config/config.go`](../../backend/internal/config/config.go); the error text
   names the variable and the accepted values. **A secret is never echoed**: its error names the
   variable only, and a test asserts that the value is not in the message (as
   `TestLoadRejectsBadLimitsAndNeverEchoesTheKey` and `TestStorageIsAllOrNone` do). A variable
   only `serve` requires goes into `requireForServe` in
   [`main.go`](../../backend/cmd/cowork/main.go).
2. Cover default, override and invalid value in `config_test.go`; wire it in `runServe`.
3. The row in [README.md, Configuration](../../README.md#configuration). If the chart should
   expose it: the value under `backend.config` in [`values.yaml`](../../deploy/helm/cowork/values.yaml),
   the `env` entry in [`backend-deployment.yaml`](../../deploy/helm/cowork/templates/backend-deployment.yaml),
   and the line in the README's values block. A secret comes from an existing Secret with a
   configurable key (`existingSecret` and `keys.…`, as `session` and `storage` do), never from an
   inline value ([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D3).
4. A limit nginx must stay above also moves the computation in
   [`_helpers.tpl`](../../deploy/helm/cowork/templates/_helpers.tpl).
5. If it changes runtime behaviour, say so in [docs/operations/runtime.md](../operations/runtime.md).

## A frontend feature

1. Standalone component under `frontend/src/app/<feature>/`, services under `core/`; signals,
   no NgRx.
2. A `.spec.ts` beside it: `provideHttpClient()` + `provideHttpClientTesting()`,
   `await fixture.whenStable()` before reading the DOM, `data-testid` for assertions.
3. `make frontend-lint frontend-test`; `make frontend-build` if the bundle budget in
   `angular.json` might move.

## A path nginx must treat differently

1. Edit [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template). An
   API path gets a location nested inside `location ^~ /api/`, as the event stream's does, which
   repeats `set $backend ${BACKEND_URL}` and `proxy_pass $backend`.
2. Keep the four substituted variables — `BACKEND_URL`, `NGINX_LOCAL_RESOLVERS`,
   `NGINX_CLIENT_MAX_BODY_SIZE`, `NGINX_PROXY_READ_TIMEOUT` — the only ones. A fifth is deliberate:
   `NGINX_ENVSUBST_FILTER` and the image default in
   [`frontend/Containerfile`](../../frontend/Containerfile), the `env` entry in
   [`frontend-deployment.yaml`](../../deploy/helm/cowork/templates/frontend-deployment.yaml).
3. A status nginx answers itself gets a static problem body ([ADR 0047](../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D6).
4. Rebuild the image and run it read-only as in
   [build-test-lint.md](build-test-lint.md#run-the-images-together), once without a backend;
   there is no unit test for nginx. Record the behaviour in
   [docs/operations/runtime.md](../operations/runtime.md#the-frontend).

## A chart value

1. `values.yaml` under `backend.` or `frontend.` with a comment, the template, and — when it
   maps to an environment variable — the `env` entry.
2. A `ci/*-values.yaml` if the value opens a new shape worth rendering in CI.
3. The README's values block and, when operators need to understand it,
   [docs/operations/installation.md](../operations/installation.md).

## A CI job

Add it to `release.yml` as a Makefile target, add its name to the `needs:` list of
`semantic-release` in the same change (ADR 0003 D4), and add the job's exact name to the
required status checks of the `main` ruleset
([ADR 0073](../adr/0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md)
D6) — a renamed job is the same change.
