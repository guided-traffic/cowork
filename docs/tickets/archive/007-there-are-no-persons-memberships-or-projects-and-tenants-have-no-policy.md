---
id: T7
title: there are no persons, memberships or projects, and the tenants table has no policy, no version and no settings
state: done
severity: high
security: hardening
threat: puts forced row-level security with person-aware policies on tenants, users and memberships, the tenant policy on projects, their access lists and the ticket counter, and composite foreign keys under every tenant-bound row — additionally covering a forgotten tenant or person filter, and a row written by a defect that references another tenant's parent, which a plain foreign key accepts (verified)
urgency: later        # rule 4: decided fix (the ADRs)
effort: M
blocked-by: T4
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: persons, tenant settings, memberships, projects and project access with their policies
---

## Current state

Decided by [ADR 0004](../adr/0004-cowork-is-a-team-product.md) D1, D4,
[ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1–D5,
[ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D1, D4, D6,
[ADR 0022](../adr/0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md) D1, D2,
[ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D3, D4, [ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D1, D3, D5, D9, [ADR 0006](../adr/0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md)
D1, D2, D4, [ADR 0019](../adr/0019-no-sprints-and-no-milestones-continuous-flow-with-optional-wip-limits.md)
D3, [ADR 0017](../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D8 and [ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D3, D6. On `main`:

- The schema is [`000001_tenants.up.sql`](../../backend/internal/store/migrations/000001_tenants.up.sql):
  slug, name, timestamps; no policy, no version, no settings.
- A plain foreign key ignores row-level security: under tenant B, a row can reference a
  tenant-A project. A composite key `(tenant_id, x_id) REFERENCES parent (tenant_id, id)`
  refuses it (verified on PostgreSQL 18.6).
- ADR 0021 D6 as written — `memberships` "policy by tenant", `tenants` "a policy on
  membership" — hides a person's own memberships and tenants until a tenant is set, so the
  membership check, which runs before the tenant is set (D3), and the person-level loop (D5)
  would see nothing (verified). The decided shapes: `memberships` readable when the row is the
  request's tenant's or the request's person's; `tenants` when it is the request's tenant or
  the person is one of its members; `users` by itself or when it shares the request's tenant.
  D6 also lists `memberships` among the tables without a `tenant_id`, which ADR 0005 D1 gives
  it.
- `projects` belongs here rather than with its routes (T11): tokens (T8) and the project access
  lists reference it, and a later migration may not narrow what a release reads
  ([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D3).
- No fixture writes past the policies; ADR 0027 D7's repository tests need persons, tenants and
  memberships, and ADR 0038 D6 puts them in a test-only package over the administrative
  connection.

## Required changes

1. **Migrations**, each with `ENABLE` and `FORCE ROW LEVEL SECURITY`, its policies through the
   `NULLIF` guard (T4), composite foreign keys, indexes led by `tenant_id`, and grants naming only
   what the phase's routes use — no `DELETE` on tenants or projects:
   - `users`: a person (ADR 0004 D1) — display name and timestamps, nothing of a login (the
     identity-provider and password columns come with it, ADR 0029, ADR 0033), no
     global-administrator column (no phase-2 rule reads one); the runtime role reads only.
   - `tenants` gains `version` (ADR 0050 D1), `time_visible_to_members` (default off, ADR 0034
     D5), `time_locked_until` (ADR 0017 D8) and `members_create_projects` (default on, ADR 0034
     D9); read and update.
   - `memberships`: tenant, person, role `viewer|member|admin` as an ordered type, source
     `mapping|grant`, version, timestamps; one row per person, tenant and source; the effective
     role is the highest of a person's rows (ADR 0030 D4); read only (no phase-2 route changes a
     membership).
   - `projects` (key `^[A-Z][A-Z0-9]{1,9}$` unique in the tenant, name, description, `restricted`,
     `archived_at`, the optional WIP limits per state of ADR 0019 D3, version, timestamps),
     `project_access` (project, person, role `member|viewer`; ADR 0034 D3) and `ticket_counters`
     (one row per project — a table of its own, so a filed ticket never bumps the project's
     version; ADR 0022 D2).
2. **Policies:** the person-aware shapes above on `users`, `tenants`, `memberships`; the standard
   tenant policy on `projects`, `project_access`, `ticket_counters`. ADR 0021 D6 amended in place
   with those shapes and with `memberships` moved off its list of tables without a `tenant_id`.
3. **Queries:** the tenant by slug with the person's effective role; a person's memberships; the
   projects a person may see in a tenant (unrestricted, on the list with the lower of tenant
   role and entry, or tenant admin; ADR 0034 D3); a tenant's members; the writes T10 and T11 use.
4. **The fixture package** under `backend/test/` (ADR 0038 D6), writing over the administrative
   connection: persons, tenants, memberships, projects and access lists, and the identities of
   ADR 0038 D3 that phase 2 can express — an admin, a member and a viewer of tenant A, a member
   of tenant B, one person in both. A test asserts that `backend/cmd/cowork`'s dependency graph
   does not contain the package.
5. **Tests:** the catalog walk and the unfiltered-query test of T3 cover the new tables; every
   composite key refuses a reference into another tenant; every id is a UUIDv7 (catalog-driven);
   a person reads their own memberships and tenants with no tenant set, and nothing of a tenant
   they do not belong to; the repository tests with two tenants and two identities (ADR 0027 D7).
6. **Docs and records:** new docs/developer/domain.md (tenants and their settings, persons,
   memberships and the effective role, projects as the restriction anchor, the composite-key
   rule) and its row in the developer README; adding-things.md "A migration" (policy or
   exemption marker, composite keys, grants, an enum value usable only from the next file on);
   testing.md (the fixture and its identities); the README's naming (the tenant slug, the project
   key); ADR 0004, 0005, 0021 (D1, D6 built), 0022 Status and index rows.

## Related

- T3 — the roles and the catalog-driven tests.
- T4 — the wrappers and the settings the policies read.
- T8 — tokens and audit rows reference persons, tenants and projects.
- T9 — the tenant boundary reads these queries.
- T11 — the project routes.
