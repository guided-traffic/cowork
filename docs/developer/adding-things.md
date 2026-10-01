# Adding things

Ordered checklists for the changes that recur. Each names the files to touch and the page
to update in the same change.

## A configuration variable

1. Add the `Env…` constant, the field, the default and the validation in
   [`backend/internal/config/config.go`](../../backend/internal/config/config.go); the error text
   names the variable and the accepted values.
2. Cover default, override and invalid value in `config_test.go`.
3. Add the row to [README.md, Configuration](../../README.md#configuration) and, if the chart should
   expose it, the value under `backend.config` in [`values.yaml`](../../deploy/helm/cowork/values.yaml),
   the `env` entry in [`backend-deployment.yaml`](../../deploy/helm/cowork/templates/backend-deployment.yaml)
   and the line in the README's values block.
4. If it changes runtime behaviour, say so in [docs/operations/runtime.md](../operations/runtime.md).

## A migration

1. Next number, one file: `backend/internal/store/migrations/NNNNNN_<snake_name>.up.sql`.
   There is no down file. The unit test refuses a gap, a stray file and an empty file. The
   migration may add and widen; it may not drop, rename or narrow anything the previous
   release still reads — removal is a later release's migration
   ([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)).
2. Run `make postgres-up test-integration`; add an assertion there for what only the database
   proves.
3. If the schema encodes a decision, the ADR is written in the same change.

## An API endpoint

1. Register it in `New` in [`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go)
   through `handleGet` (or a sibling for other methods) under `/api/v1/`; use `writeJSON` /
   `writeError`.
2. Test it through `New(Options{…})` and `httptest` in `server_test.go`; a handler that touches
   the database gets its test in `backend/test/integration/`.
3. Add the row to [README.md, API](../../README.md#api-backend). The nginx proxy passes every
   `/api/` path through, so nothing changes in the frontend container. Once the OpenAPI
   document exists (question Q-E1), the document comes first and this list changes.

## A frontend feature

1. Standalone component under `frontend/src/app/<feature>/`, services under `core/`; signals,
   no NgRx.
2. A `.spec.ts` beside it: `provideHttpClient()` + `provideHttpClientTesting()`,
   `await fixture.whenStable()` before reading the DOM, `data-testid` for assertions.
3. `make frontend-lint frontend-test`; `make frontend-build` if the bundle budget in
   `angular.json` might move.

## A path nginx must treat differently

Edit [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template); keep
`${BACKEND_URL}` and `${NGINX_LOCAL_RESOLVERS}` the only substituted variables (or extend
`NGINX_ENVSUBST_FILTER` in the Containerfile deliberately). Rebuild the image and run it
read-only as described above, once without a backend present; there is no unit test for
nginx. Record the behaviour in
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
