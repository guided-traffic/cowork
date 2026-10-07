---
id: T4
title: there is no data-access layer — the pool is exported, no query runs in a transaction that sets its tenant and person, and nothing generates or checks the SQL
state: done
severity: high
security: hardening
threat: makes a query outside a transaction that has set its tenant and person unwritable, and a write without its recorded act uncommittable — additionally covering a forgotten tenant filter or person predicate in application code and an unaudited write
urgency: later        # rule 4: decided fix (the ADRs)
effort: L
blocked-by: T3
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: sqlc over pgx (readq, writeq), InTenant and Installation, Mutate with the audit row, jobs under a transaction-level advisory lock
---

## Current state

Decided by [ADR 0027](../adr/0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D1–D8, [ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D1, D3, D4, [ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D4, [ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D4 and [ADR 0055](../adr/0055-english-only-browser-locale-for-dates-and-numbers.md) D3. On `main`:

- `store.Connect` ([`store.go`](../../backend/internal/store/store.go)) returns an exported
  `*pgxpool.Pool`; `main.go` uses its `Ping` and `Close`. No sqlc, no `InTenant`,
  `Installation` or `Mutate`, no read and write types, no `make generate`.
- ADR 0021 D1's policy text `current_setting('app.tenant_id', true)::uuid` raises
  `invalid input syntax for type uuid: ""` on a reused pooled connection: after a committed
  transaction-local setting, `current_setting` returns `''`, not NULL (verified on PostgreSQL
  18.6), and pgx's pool resets nothing. `NULLIF(…, '')` evaluates to NULL and matches no row,
  which is what D3 intends.
- `SET LOCAL app.tenant_id = $1` takes no bind parameter; `SELECT set_config('app.tenant_id',
  $1, true)` does (verified).
- ADR 0027 D2 names only the tenant; ADR 0034 D4 and ADR 0065 D4 say the same wrapper carries
  the person, which the person-aware policies (T7, T8) and the visibility predicates read.
- ADR 0027 D5 and its residual risk rely on `pg_try_advisory_lock` being released "with the
  job's connection". pgx's pool returns an idle connection to the pool with a session-level
  advisory lock still held; `pg_try_advisory_xact_lock` is released at commit or rollback
  (verified from the pgx source and in PostgreSQL).
- ADR 0027 D3 makes `Mutate` the only write path, each write with its audit row;
  [ADR 0035](../adr/0035-personal-access-tokens.md) D2, D9 need a token's last-used date,
  which is not an act.
- pgx decodes `timestamptz` into `time.Local` unless the codec's scan location is set
  (verified from the source), against ADR 0055 D3.
- [`.golangci.yml`](../../backend/.golangci.yml) has no rule that keeps `database/sql` and
  `pgxpool` inside `store`.

## Required changes

1. **sqlc** pinned in the Makefile with a `# renovate:` comment, reading the migration files as
   the schema; queries in `.sql` files under `store`, generated into **two packages** — one whose
   type has no mutating method, one with the writes — with the enum and JSONB overrides once in
   `sqlc.yaml`; generated code committed. `make generate` runs it (T5 adds its generators);
   `make generate-check` (generate, then fail on a diff or an untracked file) runs as a step of
   the existing "Code Linting" job, so ADR 0073 D1's list of required checks stays as it is;
   `make lint` runs `sqlc vet` with rules that need no database (ADR 0027 D7). `gocyclo` and
   `gosec` skip generated files by name. A Renovate bump of a generator turns its pull request
   red until somebody runs `make generate`; ci-and-release.md says so.
2. **`store.DB`** replaces the exported pool: `Open`, `Ping`, `Close`, and nothing that yields a
   connection (ADR 0027 D2).
3. **The wrappers:** `InTenant(ctx, tenantID, fn)` and `Installation(ctx, fn)` open a
   transaction, set `app.tenant_id` (`InTenant` only) and `app.user_id` from the principal in
   `ctx` with `set_config(…, true)`, hand `fn` the read type, and commit or roll back. A lookup
   helper sets `app.token_hash` for the resolver (T9). Every policy reads its setting through
   `NULLIF(current_setting(…, true), '')`.
4. **`Mutate`** runs `fn` with the write type inside the tenant's (or the installation's)
   transaction; `fn` records one or more acts — a link records one per ticket, an act and its
   explaining comment are one request — and a compare-and-bump helper answers a stale version
   with the current one (ADR 0050 D3, D5). A request that changes nothing records no act and
   commits nothing (an existing link, an unchanged interest); a write without a recorded act is
   refused at commit as a defect. T8 adds the audit row and the idempotency key, T20 the
   `NOTIFY`.
5. **The bookkeeping writer:** a type with a closed list of writes that are not acts and get
   no audit row; the token's last-used date (T9) is its one entry in phase 2.
6. **Background jobs:** a ticker per job; each run is one transaction that takes
   `pg_try_advisory_xact_lock(<namespace>, <job>)` (two `int4` keys, apart from golang-migrate's
   single `bigint`), sets `app.job` to the job's name for the job clauses of its tables'
   policies, and acts as the system actor `system:<name>` (T8).
7. **Around the pool:** `timestamptz` scans into UTC; a tracer logs a query slower than a fixed
   threshold at `warn` with the query's name, never its arguments (ADR 0027 D8); a `depguard`
   rule forbids `database/sql` and `pgxpool` outside `backend/internal/store`, `backend/test/`
   exempt for the fixture.
8. **Tests.** Integration: `InTenant` on a pooled connection that served another tenant sees
   only its own rows, and `Installation` sees no tenant-bound row; a job's lock is free for a
   second replica right after a normal and after a failing run (ADR 0027's residual risk,
   corrected); timestamps read back in UTC under `TZ=Europe/Berlin`; a write without an act is
   refused; a request that changes nothing commits nothing. Unit: the read type has no mutating
   method.
9. **Docs and records:** new docs/developer/data-access.md (the wrappers and their settings, the
   read and write types, `Mutate`, the bookkeeping writer, jobs, sqlc) and its row in the
   developer README's page table; package-map.md, architecture.md (start-up and the request's
   transaction), repository-layout.md, adding-things.md ("A query", "A background job"),
   build-test-lint.md (`make generate`, `make generate-check`), ci-and-release.md (the drift
   step); in place: ADR 0021 D1 (the `NULLIF` guard, `set_config`) and D3 (the settings
   `app.user_id`, `app.token_hash` and `app.job` beside `app.tenant_id`), ADR 0027 D2 (the
   wrapper carries the person), D3 (the bookkeeping writer) and D5 with its residual risk
   (transaction-level locks); ADR 0027 Status and index row.

## Not verified

- sqlc's parser on the migrations' policy DDL, `FORCE`, the `DO` blocks that grant, the
  text-search configuration and generated columns; the first migration it reads proves it.

## Related

- T3 — the runtime role the wrappers connect as.
- T7 — the first policies that read the settings.
- T8 — the audit row and the idempotency key in `Mutate`.
