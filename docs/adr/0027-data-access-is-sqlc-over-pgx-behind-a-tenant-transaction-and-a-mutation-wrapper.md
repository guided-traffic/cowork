# ADR 0027: Data Access Is sqlc Over pgx Behind Two Wrappers — a Tenant Transaction Is the Only Way to Query, a Mutation Is the Only Way to Write

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "data
access layer?": `sqlc` with `pgx` behind a tenant-transaction wrapper and a mutation wrapper,
over hand-written `pgx`, over an ORM, and over a query builder for the list endpoints. The
rules of D6–D8 were put to the owner with the question and not objected to.

**Partly built.** [`backend/internal/store/`](../../backend/internal/store/) has the `pgxpool`
and the golang-migrate run; no `sqlc` configuration, no wrapper, no generated code.

## Context

Two earlier records cannot be kept by discipline. [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D3 wants every query inside a transaction that has set `app.tenant_id`, and says "which
transaction, which tenant" must be a type; [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)
D2 wants no mutation committed without its audit row. The SQL the records need is also
SQL that must stay visible: policies and tenant-led indexes (ADR 0021), generated `tsvector`
columns and `ts_headline` ([ADR 0025](0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md)),
a recursive walk for the prerequisite tree ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)
D6), a counter row for the ticket number ([ADR 0022](0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md)
D2). An ORM hides or fights each of these; hand-written scanning pays in runtime errors.
The schema has one source, the golang-migrate files ([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D5), and that must stay so.

## Decision

**D1 — SQL lives in files and `sqlc` generates the typed Go that runs it; `pgx` v5 is the
driver.** `sqlc` reads the migration files as the schema, so there is one source. The
generated code is committed, and a CI job regenerates and fails on a diff.

**D2 — `store.InTenant(ctx, tenantID, fn)` is the only way to obtain a `Queries` value.** It
opens a transaction on the pool, runs `SET LOCAL app.tenant_id`, hands `fn` the generated
queries bound to that transaction, and commits or rolls back. The pool is unexported; no
connection is reachable outside the wrapper. A request without a tenant (the `/me` routes,
installation-level administration) uses `store.InTenant` once per tenant of the person
(ADR 0021 D5) or `store.Installation(ctx, fn)`, which sets no tenant and reaches only the
tables whose policies allow that.

**D3 — `store.Mutate(ctx, act, fn)` is the only way to commit a write.** It runs inside D2's
transaction, executes `fn` against a `Writer` type that carries the mutating queries, and
writes the audit row described by `act` (ADR 0026 D1) before commit. The read-only queries
are generated into a separate `Reader` type with no mutating methods; a handler that only
reads never holds a `Writer`.

**D4 — Dynamic list filters are the one place sqlc is not enough, and they get a small,
typed builder of their own,** used only by the list endpoints, producing SQL that runs inside
the same transaction. Everything else is a named query in a `.sql` file.

**D5 — Background work runs through the same wrappers with a system actor.** The purge of
[ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2 and any later job acquire `pg_try_advisory_lock` on a job-specific key, so exactly one
backend replica runs each job at a time; the lock is released with the job's connection.

**D6 — `golang-migrate` stays for migrations; `database/sql` appears nowhere else.** The
`pgx` stdlib adapter is used by the migration run only ([`migrate.go`](../../backend/internal/store/migrate.go)).

**D7 — Tests.** Every repository (the queries of one aggregate) has integration tests against
a real PostgreSQL with two tenants and two identities; `sqlc vet` runs in `make lint`; the
unit test on the migration set is extended to assert that every tenant-bound table has its
policy (ADR 0021).

**D8 — Observability hooks are prepared, not filled.** The pool is created with a tracer
slot for query logging and, later, OpenTelemetry; the first release logs slow queries above
a threshold and nothing more.

## Consequences

- An un-scoped query cannot be written: there is no `Queries` outside `InTenant`. An
  unaudited write cannot be committed: there is no `Writer` outside `Mutate`. The two records
  that asked for this are satisfied by the type system.
- `sqlc generate` is a build step; `make generate` and a CI drift check join the Makefile,
  the way the sibling project guards generated manifests.
- JSONB columns (audit diffs) and the enums (types, states, severities) need `sqlc`
  overrides to Go types; that is configuration, once.
- D4 admits a second way to build SQL; it is confined to one package and the list endpoints,
  and its output runs under the same policies.
- The first background job brings a scheduler into the backend: a ticker per job, a lock per
  job, no second component.

## Alternatives Considered

- **Hand-written `pgx` behind the same wrappers.** Maximum freedom, no generation; every
  `Scan` by hand, column-order mistakes at runtime, drift between migration and struct
  unnoticed. Lost.
- **An ORM** (bun, ent, gorm). Fast CRUD; `SET LOCAL` and policies fight hooks and connection
  handling, the recursive and full-text SQL become raw strings inside the ORM, `ent` wants to
  own the schema against golang-migrate, `gorm` hides N+1. Lost.
- **A query builder for everything.** Dynamic filters elegant; two dialects everywhere and no
  generation-time check. Lost to D4's confinement.

## Residual risks

- `sqlc`'s handling of optional parameters is clumsy; D4 exists because of it, and the line
  between "named query" and "builder" has to be held in review.
- D5's advisory lock keeps two replicas from running one job at once; it does not survive a
  crashed job's connection being reused before the lock is released. The pool releases the
  connection on error, which releases the lock; a test proves it.

## References

- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D3, D5 — the transaction context and the unions
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D2 — the audit row in the mutation's transaction
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D5 — golang-migrate and the one schema source
- [ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md) D2 — the first background job
- [`backend/internal/store/store.go`](../../backend/internal/store/store.go), [`migrate.go`](../../backend/internal/store/migrate.go) — the pool and the migration run that exist
