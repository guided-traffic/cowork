# ADR 0021: Row-Level Security Is the Second Line of Tenant Isolation — a Policy per Table, a Tenant per Transaction, and a Policy Is Widened by Migration When the Product Needs It

## Status

Accepted, amended 2026-10-02 (D2: a separate owner role is mandatory; D1, D3, D6 made
concrete by the first implementation: the guarded setting functions, the settings besides the
tenant, the policy of every named table) and 2026-10-03 (D3, D6: the settings and the policies
of the sessions and of the local login; D1: a migration that rewrites rows lifts the force for
its own transaction only, written when the rank's migration needed it) and 2026-10-04 (D3: the
job `identity-provider` and the setting `app.person_lookup`; D6: the identity provider's policies
and the restrictive policies of the administration, and — after the security review — the trigger
that holds a project's restriction to the tenant's administrators), and again on 2026-10-04 by the
owner's answer recorded in [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D7 (D6: the restrictive policies hold a group mapping's insert and update to an administrator of the
tenant who is a global administrator as well), and for the global administrator's grant to
themselves of [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2 (D6: a global administrator reads every tenant, inserts their own grant in any role and changes
its role), and for the chat's capabilities of
[ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D5 (D6: `chat_capabilities`, a named table that only its person reads and writes; built 2026-10-04),
and on 2026-10-06 for the owner's answer recorded in
[ADR 0018](0018-the-views-of-the-first-release.md) D5 that a tenant administrator unshares or
deletes another person's shared saved filter (D3: an eighth setting, `app.saved_filter_id`; built
the same day, migration 39).
Date: 2026-09-30.
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
table that an unfiltered query under one tenant sees nothing of another. ~~D5's unions arrive
with the person-level lists~~ *(built 2026-10-04: the inbox, "assigned to me" and "open decisions"
read each tenant of the person in a transaction of its own and merge the parts in Go; the cross-tenant
search the same way on 2026-10-05)*; D7 has not been used. Migrations 15 and 16 (phase 3, 2026-10-03)
add the policies of `sessions`, `local_accounts`, `login_attempts` and `login_locks`, widen those
of `users`, `tenants`, `memberships` and `tokens`, and the unit test's list of named tables holds
them. Migration 17 (2026-10-03) is the first that rewrites rows: it lifts and restores the force
on `tickets` for its backfill, a unit test holds every lifted force to its restore in the same
file, and the integration tier reads the force back after the run. Migrations 20 to 22 (phase 4,
2026-10-04) widen the policies of `users`, `tenants` and `memberships` for the identity provider,
add `group_mappings` with the canonical policy and three more, and hold the writes of
`group_mappings` and `project_access` to a tenant's administrators with restrictive policies.
Migration 25 (2026-10-04) narrows a mapping's insert and update to an administrator of the tenant
who is a global administrator as well. Migration 26 (2026-10-04) widens `tenants` to a global
administrator's reading of every row and `memberships` to their own grant in any role and the
change of its role. Migration 30 (2026-10-04) adds `notifications` with the canonical policy and
restrictive ones that hold reading and marking to the notification's own person and deleting to the
job `notification-expiry`, which a permissive policy admits past the tenant: the writer of an act
inserts notifications for others, so the canonical policy alone would show one person another's
inbox to a query that forgot its person.
Migration 32 (2026-10-05) holds every delete of a ticket and of what belongs only to it to the purge
of a deleted ticket with restrictive policies, admits the purge job — named in `app.job`, with no
tenant set — to read the deleted tickets of every tenant, the one cross-tenant read it needs
(D7's reason: the purge of
[ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2, ids only), and adds `audit_purge`, the owner role's update of a purged ticket's audit rows.
Migration 33 (2026-10-05) adds `saved_filters` with the canonical policy and restrictive ones that
hold a person to their own filters and the shared ones. Migration 39 (2026-10-06) widens them for
ADR 0018 D5 as amended that day: an administrator of the current tenant changes another person's
shared filter only into one that is not shared, deletes it, and reads it back unshared only while
the transaction names it in D3's `app.saved_filter_id`
(`TestTheSavedFilterPoliciesAdmitAnAdministratorToASharedFilter`).

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
`BYPASSRLS` sees past it. *(Added 2026-10-03:)* A migration that rewrites the rows of a forced
table — no tenant is set in a migration, so the policy hides every row from the owner — lifts
the force for itself and restores it before the file ends: the file runs as one transaction,
which holds the table exclusively from its first `ALTER` on, so no committed state and no other
transaction sees the table unforced. The runtime role is held by the policy either way; the
force concerns the owner alone.

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
*(Amended 2026-10-03:)* two settings more. `app.session_hash` carries the SHA-256 of the session
cookie a request presented, and the policy of `sessions` admits exactly that row for it, as
`app.token_hash` finds a token; it is read through `app_session_hash()`. `app.job` names four
actors more — `login` (the login's own transaction, where no person is known yet),
`login-expiry`, `session-expiry` and `bootstrap` (the start-up synchronisation of
[ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md))
— and the policies of the tables they write admit them by that name. A policy that asks whether
the person is a global administrator reads the flag through `app_is_global_admin()`, of the
person in `app.user_id`. *(Amended 2026-10-04:)* `app.job` names one actor more,
`identity-provider`: the identity provider's own transactions — a login through it, a session's
groups refresh *(amended after the security review: the transaction that applies its answer; the
short one that claims it runs as the person and names no job)*, a token's gate check — and, inside an administrator's transaction, the derivation
that follows a change of a group mapping, which names the job for that part only and clears it
after. A seventh setting, `app.person_lookup`, carries the e-mail address or username a tenant's
administrator adds a member by, in that lookup's transaction alone, read through
`app_person_lookup()`. *(Amended 2026-10-06:)* An eighth setting, `app.saved_filter_id`, names the
one saved filter a tenant administrator unshares — another person's,
[ADR 0018](0018-the-views-of-the-first-release.md) D5 as amended that day — for that statement
alone, read through `app_saved_filter_id()`: PostgreSQL holds an update's new row to the read
policy, and an unshared filter of another person is one nobody but its owner reads, so the read
policy of `saved_filters` admits the named filter to an administrator of the current tenant.

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
*(Made concrete 2026-10-03:)* `sessions` — a person's own rows, the one row of the cookie
presented, the administrators of a managed account, a global administrator for reading, and the
jobs `session-expiry` and `bootstrap` for deleting; `local_accounts` — the person's own row, the
administrators of the managing tenant, the login and the start-up synchronisation by their job
names; `login_attempts` and `login_locks` carry neither a tenant nor a person, so only the login's
transaction writes them, the expiry job removes them, and the administrators of a managed
account's tenant read and clear those of its username. Of the earlier tables, `users` gains an
insert policy (a tenant's administrator inserts a person who is no global administrator; the
bootstrap job anything) and an update policy (what a tenant manages, never a global
administrator), `tenants` an insert policy (a global administrator, or the bootstrap job) and a
read by the login's transaction for the init state, `memberships` a marked-grant insert (an
administrator into their tenant, the creator of a tenant into it, the bootstrap job), and `tokens`
an insert by the person and a read and update extended to the administrators of a managed account
and the bootstrap job, which revoke tokens when they deactivate an account. What a tenant's
administrators manage is decided in one place, `local_accounts.managing_tenant_id` — the tenant
that created the account — through `app_manages_account()` and `app_manages_username()`. ~~A
global administrator still reads only the tenants they are a member of; the reading of all
tenants is not built.~~ *(Built 2026-10-04 for
[ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2, [migration 26](../../backend/internal/store/migrations/000026_global_admin_self_grant.up.sql):
`tenants` admits every row to a global administrator, `app_is_global_admin()` — the list of every
tenant and the request layer's admission to a tenant without a role read it in a transaction that
names no tenant —, and `memberships` admits a global administrator's own marked grant in any role,
not only as `admin`, and the change of its role inside the tenant's transaction, by which one who
holds a role below `admin` raises it. No other policy changes: inside the tenant's transaction the tenant-bound
tables admit whomever D3's request layer admitted, and which operations it admits a global
administrator without a role to is the request layer's list, as membership is for everyone else.)*
*(Made concrete 2026-10-04:)* the identity provider reads every person —
the derivation of a mapping finds the persons whose groups hold its group — and inserts and updates
only the persons of the provider (`oidc_issuer` set, no username), never a local account; it reads
whether any tenant exists; it reads every tenant's `group_mappings`; and it alone inserts, changes
and removes a `mapping` membership, while a `grant` membership is changed and removed by an
administrator of its tenant alone. A tenant's administrator reads, besides the persons who share the
tenant, the persons the lookup in `app.person_lookup` names. `group_mappings` carries `tenant_id`
and the canonical policy; the bootstrap inserts the administrator group's mapping. On
`group_mappings` and `project_access` every write must also pass an `AS RESTRICTIVE` policy that
names an administrator of the current tenant (the bootstrap's insert excepted): a restrictive policy
is ANDed with the permissive ones, so no later permissive policy widens who writes them. *(Amended
2026-10-04, the owner's answer recorded in ADR 0030 D7: the insert and the update of a group mapping
name an administrator of the current tenant who is a global administrator as well,
`app_is_tenant_admin() AND app_is_global_admin()`; its delete stays any administrator's of the tenant
([migration 25](../../backend/internal/store/migrations/000025_group_mappings_global_admin.up.sql)).
Every tenant shares the identity provider's groups, and the second line holds the rule the handler
holds.)* *(Added
after the security review, 2026-10-04:)* a rule on one column, which a policy cannot state because
it sees rows, is a `BEFORE UPDATE OF` trigger: `projects_restriction_guard`
([migration 22](../../backend/internal/store/migrations/000022_membership_administration.up.sql))
refuses a change of `projects.restricted` unless the caller is an administrator of the tenant, while
a member still changes the project's other settings; a superuser, whom no policy binds either, is
left to it. *(Added 2026-10-04 for the chat's capabilities,
[migration 24](../../backend/internal/store/migrations/000024_chat_capabilities.up.sql):)*
`chat_capabilities` — one row per person, the set the person gave the chat in the UI — carries no
tenant and is read, inserted and updated by its person alone (`user_id = app_user_id()` in every
policy), never deleted (no grant), and holds only the nine capabilities (a `CHECK`). It is a table of
its own rather than a column of `users` because `users`' update policies are permissive and admit an
administrator of the account, the start-up synchronisation and the identity provider: a policy that
let a person update their own row would admit every column the runtime role may update there,
`global_admin` and `deactivated_at` among them, and a policy sees rows, not columns.

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
- [migrations 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)–[22](../../backend/internal/store/migrations/000022_membership_administration.up.sql) — the identity provider's and the administration's policies
