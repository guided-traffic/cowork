---
id: T3
title: one database role migrates and serves — a superuser in development and CI —, serve starts on any schema, and an image rollback over a newer schema exits 1
state: done
severity: high
security: hardening
threat: separates the owner role that migrates from a runtime role that owns nothing and cannot bypass the policies, and refuses to serve a stale schema — additionally covering a compromised or defective serving process that switches row-level security off, gives itself back a revoked privilege or rewrites the audit trail, and a pod serving a schema older than its binary
urgency: later        # rule 4: decided fix (the ADRs)
effort: M
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: two database roles — migrations as the owner in an init container, serving as the runtime role — with the start-up refusals and the schema-ahead start
---

## Current state

Decided by [ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D2 and its residual risks, [ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)
D1, D3, D7, [ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
D3–D5, [ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D5, D7, D8, [ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)
D2, D4, D5 and [ADR 0003](../adr/0003-test-and-ci-policy.md) D2, D3, D10. On `main`:

- `make postgres-up` ([`Makefile`](../../Makefile)) starts `postgres:18` with
  `POSTGRES_USER=cowork`, a role the image makes `SUPERUSER` with `BYPASSRLS` (verified on
  PostgreSQL 18.6). The integration tests, `make run`, `make migrate` and the service container
  of the job "Integration Tests (PostgreSQL)" ([`release.yml`](../../.github/workflows/release.yml))
  all connect as it, so no test can prove ADR 0021's second line, and ADR 0021 D2's role
  assertion would fail by construction.
- One credential does both jobs: `cowork serve` migrates with `COWORK_DATABASE_URL` before it
  listens ([`main.go`](../../backend/cmd/cowork/main.go)), and the chart hands that one Secret to
  the serving container ([`values.yaml`](../../deploy/helm/cowork/values.yaml) `database.*`,
  [`backend-deployment.yaml`](../../deploy/helm/cowork/templates/backend-deployment.yaml)). The
  amended records require an owner role that migrates, a runtime role that owns nothing, and
  the owner credential only in a migration init container.
- `serve` checks no role attribute. With `COWORK_MIGRATE_ON_START=false` it logs "migrations
  skipped on start" and serves whatever schema it finds; ADR 0057 D3's refusal is not built.
- **Defect, verified by running:** against a database one version ahead of the binary,
  `store.Migrate` ([`migrate.go`](../../backend/internal/store/migrate.go)) fails with "no
  migration found for version N", because it accepts only `migrate.ErrNoChange`, and `serve`
  exits 1. "Rolling back is rolling the image back" (ADR 0028 D4,
  [installation.md, "Upgrade"](../operations/installation.md#upgrade)) is therefore false in the
  default mode. It stays dormant until a release ships a second migration, which every phase-2
  child does.
- ADR 0028's residual risks say "a test asserts the command surface has no `down`";
  [`main_test.go`](../../backend/cmd/cowork/main_test.go) tests only the unknown command
  "dance".
- The migration unit test ([`migrate_test.go`](../../backend/internal/store/migrate_test.go))
  checks names, emptiness and the gap-free sequence, not the policies ADR 0021's Consequences
  and [ADR 0027](../adr/0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
  D7 ask it to check.
- Pages that describe the one-role shape: installation.md, "The database credential" ("A
  split into a migration role and a runtime role is not supported today"); the README's
  configuration row of `COWORK_DATABASE_URL` ("The role owns the schema: migrations run under
  it"); trust-boundaries.md's database row and "Where the credential lives". runtime.md's
  backend steps say the start log reports "whether a UI is embedded"; it reports the address,
  the version and the commit.

## Required changes

1. **Configuration:** `COWORK_DATABASE_OWNER_URL`, the owner role, required by `cowork migrate`
   and by `cowork serve` while `COWORK_MIGRATE_ON_START=true` (a development convenience) and
   never echoed; `COWORK_DATABASE_URL` is the runtime role (ADR 0001 D7, ADR 0058 D4).
2. **Grants:** the migration runner sets a session setting naming the runtime role — the user
   of `COWORK_DATABASE_URL` — and every migration from the next one on grants that role exactly
   what the phase's routes use, through `format('%I')`. The next migration grants what `tenants`
   needs (`000001` is never edited, ADR 0028 D5) and `SELECT` on `schema_migrations` for the
   version check of `serve`.
3. **Role refusal:** `serve` and `migrate` exit 1 when the runtime role is a superuser, has
   `BYPASSRLS`, owns a relation of the schema or is a member of the owner role (ADR 0021 D2),
   naming the cause and never the URL.
4. **Schema state** at every start of `serve` (ADR 0057 D3, D7): a dirty version exits 1 and a
   person repairs it; pending migrations with `COWORK_MIGRATE_ON_START=false` exit 1 with
   `pending migrations: N; run the migration job (or set COWORK_MIGRATE_ON_START=true)`; a
   schema **ahead** of the newest embedded migration is logged and served, and `cowork migrate`
   exits 0 on it without calling `Up` (ADR 0028 D4).
5. **Development:** `make postgres-up` creates an owner role, a runtime role (`LOGIN
   NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE`) and the development database owned by the
   owner, with development-only passwords; `make run` and `make migrate` use both URLs; the
   image's superuser stays the administrative connection (`COWORK_TEST_DATABASE_URL` and the
   fixture of ADR 0038 D6).
6. **Integration harness:** creates the same roles idempotently and a fresh database per run
   owned by the owner role, migrates as the owner, runs every test as the runtime role, and
   writes past the policies only through the administrative connection — the same code locally
   and in CI, no step that only the workflow knows (ADR 0003 D1, D3).
7. **Chart:** `database.owner.existingSecret` with `database.owner.existingSecretKey`, and the
   throw-away `database.owner.url` rendered into a release Secret with the warning `database.url`
   carries in the notes; an init container that runs `cowork migrate` and alone holds the owner
   credential (it reads the runtime URL too, for the grants and the role check); the serving
   container gets the runtime credential and `COWORK_MIGRATE_ON_START=false` (ADR 0057 D1).
   `backend.config.migrateOnStart` switches the init container; ADR 0057's `migrations.mode` and
   its hook Job (D2) are not part of this ticket. The template fails when neither owner source
   is set; the `ci/` values files set one each, and one of them renders the owner Secret.
8. **Tests.** Unit: each cause of the role refusal through an injected check;
   `run(…, []string{"down"}, …)` exits 2 with "unknown command" (ADR 0028's residual risk); the
   migration-set test requires `ENABLE` and `FORCE ROW LEVEL SECURITY` and a policy — or a
   written exemption marker — for every table with a `tenant_id`, a policy for every named table
   without one (ADR 0021 D6), and the `NULLIF` guard (T4) in policy text. Integration: the runtime
   role's attributes (neither superuser nor `BYPASSRLS`, owns nothing, no member of the owner
   role); `serve` refuses a superuser and a member of the owner role; each schema-state branch,
   the schema-ahead start failing on `main` and passing after the fix (ADR 0003 D10); a catalog
   walk (`relrowsecurity`, `relforcerowsecurity`, `pg_policies`) over every table with a
   `tenant_id`; the unfiltered-query test of T2's verification — vacuous now, guarding from T7
   on; `TestTenantIdsAreUUIDv7` inserts through the administrative connection.
9. **Docs and records:** the README's configuration rows (`COWORK_DATABASE_URL` as the runtime
   role, `COWORK_DATABASE_OWNER_URL`) and its Helm values block; installation.md — the two roles
   and their attributes (ADR 0058 D5), the extensions the owner creates or the installation
   creates beforehand, the refusal messages, the corrected rollback statement; runtime.md — the
   init container, the refusals, the schema-ahead start, the start log line;
   trust-boundaries.md — the two credentials and where each one lives; testing.md and
   build-test-lint.md — the administrative URL, the roles, the harness; adding-things.md "A
   migration" — the grant step; ADR 0057 (D3 built), ADR 0021 (D2 built) and ADR 0058 (D3's owner
   row built) Status and index rows; ADR 0028's residual risk (the test exists); ADR 0003 D2's
   integration row (the roles it creates and what it proves).

## Not verified

- The attributes CloudNativePG gives the role of its `-app` Secret, which installation.md
  suggests as the credential; the role refusal names the cause if they do not fit.

## Related

- T2 — the role assertions are one of the phase's four proofs.
- T4 — the wrappers connect as the runtime role.
- T7 — the first tenant tables the catalog walk and the unfiltered query guard.
