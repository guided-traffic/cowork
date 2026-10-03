# ADR 0021: Row-Level Security Is the Second Line of Tenant Isolation — a Policy per Table, a Tenant per Transaction, and a Policy Is Widened by Migration When the Product Needs It

## Status

Accepted, amended 2026-10-02 (D2: a separate owner role is mandatory; D1, D3, D6 made
concrete by the first implementation: the guarded setting functions, the settings besides the
tenant, the policy of every named table). Date: 2026-09-30.
Decided by the owner as the answer to the catalog question "how
is tenant isolation enforced?": application filtering **and** PostgreSQL row-level security,
over application filtering alone, over a schema per tenant, and over a database per tenant.
The owner first chose application filtering alone in order to keep the freedom to open the
tenant boundary later, and took row-level security once it was clear that a policy is
changed by the same migration that changes a schema and restricts nothing but the forgotten
filter. D7 records that freedom explicitly.

The amendment of D2 is the owner's answer of 2026-10-02 to the question of how
`audit_events` stays append-only when one role migrates and owns every table: a mandatory
separate owner role from phase 2 of the plan on, over the single owning role revoking its own
`UPDATE`, `DELETE` and `TRUNCATE` (the recommendation, which stops a defect but not a
compromised application, because an owner can give itself the privileges back and switch
`FORCE` off — verified on PostgreSQL 18.6), and over a split that stays optional.

The amendment of D1, D3 and D6 records what the first implementation (2026-10-02) found:
`current_setting('app.tenant_id', true)::uuid` raises on the empty string a pooled
connection is left with after its transaction, so the policies read the settings through
functions that turn an empty value into `NULL`; the visibility predicate and the token lookup
need settings besides the tenant; and D6's tables needed policies the original sentence only
sketched.

**Built** (phase 2, 2026-10-02): every rule. Every tenant-bound table of migrations 2–14 is
forced with a `tenant_isolation` policy, a unit test holds the migration set to it
(`backend/internal/store/policy_test.go`), and the integration tier proves for every such
table that an unfiltered query under one tenant sees nothing of another. D5's unions arrive
with the person-level lists; D7 has not been used.

## Context

[ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) made the
tenant the isolation unit with one `tenant_id` per row and no result that crosses it except
the person-level unions; [ADR 0004](0004-cowork-is-a-team-product.md) D4 forbids an
owner-sees-everything path in the data layer. The views of [ADR 0018](0018-the-views-of-the-first-release.md)
add nine dashboard queries, a full-text search and several unions to the places where a
`WHERE tenant_id = …` can be forgotten. A forgotten clause in a team product is one client's
ticket in another client's browser — the one failure the product cannot afford. Application
filtering catches it by review and test; row-level security catches it in the engine, on
every query, whether or not anyone remembered.

## Decision

**D1 — Every table that carries a `tenant_id` has row-level security enabled and forced.**
`ALTER TABLE … ENABLE ROW LEVEL SECURITY; ALTER TABLE … FORCE ROW LEVEL SECURITY;` and one
policy:

~~`USING (tenant_id = current_setting('app.tenant_id', true)::uuid)`~~ *(amended
2026-10-02: the setting is read through a function that maps an unset or empty value to
`NULL`, so an empty context matches no row instead of raising)*:

```sql
CREATE FUNCTION app_tenant_id() RETURNS uuid LANGUAGE sql STABLE
  AS $$ SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid $$;

CREATE POLICY tenant_isolation ON <table>
  USING (tenant_id = app_tenant_id())
  WITH CHECK (tenant_id = app_tenant_id());
```

`FORCE` makes the policy apply to the table's owner as well; only a superuser or a role with
`BYPASSRLS` sees past it.

**D2 — The application connects as a role that is neither a superuser nor `BYPASSRLS`.**
~~The migrations may run under the same role (it may own the tables; D1's `FORCE` covers that)
or under a separate owner role; [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D5 is unchanged.~~ *(Amended 2026-10-02: the migrations always run under a separate owner
role that owns every object of the schema. The application role — the runtime role — owns
nothing, is not a member of the owner role, and holds only the privileges the migrations
grant it; on `audit_events` that is `INSERT` and `SELECT`
([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D3). An owner
can switch `FORCE` off and give itself back a revoked privilege, so a role that owns the
tables is no second line against a compromised application; the split is.)* The integration
tier asserts the role's attributes at start and fails when the role could bypass the
policies; `cowork serve` and `cowork migrate` refuse to start when the runtime role is a
superuser, has `BYPASSRLS`, owns a relation of the schema or is a member of the owner role.

**D3 — The tenant is set once per transaction, by the request layer, after the membership
check.** Every request that names a tenant (the path, the API record) opens a transaction and
runs `SET LOCAL app.tenant_id = '<uuid>'` before the first query; `SET LOCAL` dies with the
transaction, so a pooled connection carries nothing over. An empty or missing setting makes
every policy evaluate to false: there is no default tenant and no "all tenants" value.
*(Amended 2026-10-02:)* the same transaction sets, through `set_config(…, true)`, the person
(`app.user_id`), a token's project restriction (`app.restricted_project_id`, read by the
visibility predicate of [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D4) and, for a background job, its name (`app.job`); each is read through a guarded function
like `app_tenant_id()`. A transaction that names no tenant — the token lookup, the tenant
boundary, the person's own routes — sets the person only. The store opens both kinds
(`InTenant`, `Installation`) and nothing else opens a transaction
([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D2).

**D4 — Application queries still filter by tenant.** The policy is the second line, not the
only one: every query on a tenant-bound table names `tenant_id` explicitly, both for the
planner (an index led by `tenant_id`) and so that a reader of the query sees the boundary.

**D5 — The person-level unions run one iteration per tenant.** "Next for me", "assigned to
me", "open decisions", the inbox, the cross-tenant search: a loop over the person's
memberships, each iteration in its own transaction with its own `SET LOCAL`, the results
merged in the application. No single query with `tenant_id IN (…)` crosses the boundary.

**D6 — Tables without a `tenant_id` are named and handled one by one.** `tenants` (readable
by the members of the row's tenant, all rows by a global administrator — a policy on
membership), `users` (a row visible to the people who share a tenant with it, and to itself),
`memberships` (policy by tenant), personal access tokens (by owning user), `schema_migrations`
(no policy; owned by the migration run). Any new table without `tenant_id` gets its policy or
its written exemption in the migration that creates it. *(Made concrete 2026-10-02:)* `tenants`
is read by its members and in its own transaction and updated only there (the global
administrator's reading arrives with that role); `memberships` is read in its tenant and by the
member; `users` by itself and by those who share the current tenant; `tokens` by its person, and
by the token lookup through the hash of the presented token in `app.token_hash`, updated by its
person; `idempotency_keys` (which carries an optional `tenant_id`) by the person who stored the
response, and by the expiry job named in `app.job`; `audit_events` — a tenant's rows in that
tenant, an installation-level row (no tenant) by the person it names, and every row inserted
only into the context it belongs to. A unit test holds each named table to having a policy.

**D7 — Widening the boundary is a migration, and this record says how.** When the product
needs a cross-tenant view, the policy of the tables concerned is amended
(`OR tenant_id = ANY (current_setting('app.shared_tenants')::uuid[])`, or a dedicated
policy for a dedicated role), in the same migration that changes the schema, with this record
amended to name the view and its reason. Row-level security constrains the forgotten filter,
never the deliberate one.

**D8 — A global administrator works inside one tenant at a time.** The administrator's
requests set `app.tenant_id` like anyone's; there is no context in which one query sees two
tenants. Administration across tenants (the tenant list, creating a tenant) is D6's `tenants`
policy, not a bypass.

## Consequences

- A forgotten `WHERE` yields zero rows, not foreign rows; the integration tier proves it
  with a query that omits the filter under tenant A and asserts it sees nothing of tenant B.
- One round trip per transaction for `SET LOCAL`; policies add a predicate the planner
  pushes down when the index leads with `tenant_id` (D4). Full-text indexes are composite
  with `tenant_id` first.
- Every migration that creates a table carries its policy; the unit test on the migration
  set checks that every new table either has a policy or is on D6's list.
- The data access layer (its own record) must make "which transaction, which tenant" a type,
  not a convention, or D3 is one missed middleware away from being nothing.
- D7 gives the owner's stated need a defined path: opening the boundary is a reviewed,
  recorded migration.

## Alternatives Considered

- **Application filtering only** — chosen first by the owner, for the freedom to open the
  boundary later. That freedom exists with D7; what application filtering alone would have
  cost is the retrofit: the same plumbing added to a codebase of fifteen tables, dashboard
  queries and search, when a second client is already on the instance. Lost.
- **A schema per tenant.** Strong separation, per-client backups; migrations run once per
  tenant, the unions become generated `UNION ALL` over schemas, and the single-tenant
  installation of ADR 0005 D6 carries the whole machinery. Lost.
- **A database per tenant.** Everything of the schema variant plus a pool per database and
  operations per tenant. The wrong size for this product. Lost.

## Residual risks

- D2 is a deployment property: an installation that connects as a superuser has no second
  line and does not know it. The integration test of D2 and a start-up check in `serve` that
  logs a warning — or refuses, the operations record decides — are the mitigation.
  *(Amended 2026-10-02: the start-up check refuses, D2.)*
- *(Added 2026-10-02.)* The split protects against a compromised serving process only while
  that process does not hold the owner credential. The chart runs the migration in an init
  container and gives the serving container the runtime credential alone
  ([ADR 0057](0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)
  D1); an installation that hands the owner URL to `cowork serve` keeps the split against
  defects but not against a compromise, and the tenancy security page says so.
- D5 costs one transaction per tenant of the person; a person in fifty tenants pays fifty
  round trips for "next for me". Acceptable at the expected sizes; a cached union is the
  amendment.
- `current_setting(…, true)` returns null when unset, and null compared to a uuid is false —
  the desired outcome — but a policy that is edited to use a different expression could
  change that; the unit test on the migration set checks the policy text.

## References

- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1, D3, D6 — the boundary, the unions, the single-tenant installation
- [ADR 0004](0004-cowork-is-a-team-product.md) D4 — no bypass for the administrator
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D5 — the migration run this record leaves as it is
- [`backend/internal/store/migrations/000001_tenants.up.sql`](../../backend/internal/store/migrations/000001_tenants.up.sql) — the first table that gets its policy
