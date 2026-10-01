# ADR 0028: Migrations Only Go Forward — No Down Files, and a Migration Never Removes What the Previous Release Still Reads

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "down
migrations?": no down files at all, with expand-before-contract as the operating rule — over
mandatory, tested down files that production never runs (the first recommendation), over
optional down files, and over a production `migrate down` command. The owner asked whether
downgrades were likely or important; they are not, and the image rollback that *is* likely
needs forward compatibility, not down files.

**Implemented** in the working tree on 2026-10-01: `000001_tenants.down.sql` is removed, the
file pattern in [`migrate.go`](../../backend/internal/store/migrate.go) accepts `.up.sql`
only, and the unit test asserts an up-only, gap-free set. The forward-compatibility rule
(D3) binds every migration from the second one on; nothing checks it mechanically.

## Context

golang-migrate supports paired up and down files, and the skeleton started with the pair and
a test that enforced it. A down file has two uses: rolling the schema back in production, and
undoing a migration while iterating locally. The first is neither likely nor safe — with
row-level policies, generated columns, `SECURITY DEFINER` functions and data that new columns
fill, a production `down` almost always loses data, and a command for it invites use under
pressure. The second is served by a fresh local database in two seconds. What production
does need is to roll an **image** back after a bad release, and that works only if the old
binary still runs against the new schema.

## Decision

**D1 — A migration is one `NNNNNN_<snake_name>.up.sql` file; there are no down files.** The
set is numbered `1..n` without a gap; the unit test enforces the pattern, the sequence and
that no file is empty.

**D2 — `cowork migrate` and the start-up run only move forward.** There is no `down`
subcommand and no `--to` target below the current version. A dirty version is repaired by a
person ([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D5, the operations page), never rolled back by the tool.

**D3 — Expand before contract.** A migration shipped with release N must leave the schema
usable by the binary of release N-1: it may add tables, columns, indexes, policies and
functions, and may widen constraints; it may **not** drop, rename or narrow anything that
release N-1 reads or writes. Removal happens in release N+1 or later, in a migration of its
own, once no supported binary touches the object. Renames are an add, a backfill and a later
drop.

**D4 — Rolling back is rolling the image back.** An operator who meets a bad release deploys
the previous image tag; the schema stays at the newer version, and D3 is what makes that
safe. The operations page states this as the only rollback.

**D5 — Local iteration is a fresh database.** `make postgres-down postgres-up` (or dropping
the schema) and `make migrate`; a migration file is edited freely until it is committed, and
never after.

## Consequences

- One file per migration, no fiction in a `down` that could not restore data anyway.
- Every destructive change takes two releases. That is the cost of a safe image rollback and
  it is paid rarely.
- Nothing enforces D3 mechanically; a review question ("does release N-1 still read this?")
  and, later, a CI job that runs the previous release's integration tests against the new
  schema are the checks. The second is an amendment when a release exists.
- The unit test on the migration set is simpler; the integration tier still proves that the
  set applies to a fresh database and is idempotent.

## Alternatives Considered

- **Mandatory down files, tested by an up–down–up round trip, never run in production.**
  Honest down files at the price of thought per migration; the round trip would have
  exercised code nobody deploys. Lost once the owner asked what it was for.
- **Optional down files.** Down files nobody tests are down files nobody can trust. Lost.
- **A production `migrate down --to N`.** A tool for the worst moment that loses data in
  most schemas this project will have. Lost.

## Residual risks

- D3 unenforced: a contract migration that slips into the same release as its expand breaks
  the image rollback for that release. The review question is the only guard until the CI
  job exists.
- golang-migrate's `Down` and `Steps` APIs still exist in the library; nothing in the binary
  calls them, and a test asserts the command surface has no `down`.

## References

- [`backend/internal/store/migrate.go`](../../backend/internal/store/migrate.go) — the up-only pattern and the forward-only run
- [`backend/internal/store/migrate_test.go`](../../backend/internal/store/migrate_test.go) — the up-only, gap-free assertion
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D5 — migrations on start, dirty versions
- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D1 — sqlc reads these files as the schema
- [docs/operations/installation.md](../operations/installation.md) — the upgrade and rollback story
