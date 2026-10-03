---
id: T10
title: a person cannot see who they are or list and revoke their tokens, and a tenant's settings, members and audit record have no route
state: done
severity: high
security: hardening
threat: lets a person list and revoke their tokens at once and a tenant administrator read what each token did, filtered and exported — the answer to a leaked or looping token ADR 0039 D4 names — additionally covering a leaked token nobody can see or stop, and an exported CSV whose cells run as spreadsheet formulas
urgency: later        # rule 4: decided fix (the ADRs)
effort: M
blocked-by: T9
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: the person's tokens, the tenant's settings, members and audit view as JSON and CSV
---

## Current state

Decided by [ADR 0035](../adr/0035-personal-access-tokens.md) D2, D3, D6,
[ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D6,
[ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D4, [ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D1, D3, D5, D7, D9, [ADR 0017](../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D8, [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D3 and [ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D2.
On `main` none of these routes exists.

- Phase 2's token routes read and revoke ([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
  D6, ADR 0035's Status); creating a token arrives with the sessions, and with it
  `COWORK_TOKEN_DEFAULT_LIFETIME` and `COWORK_TOKEN_MAX_LIFETIME`.
- ADR 0035 D3: `read` reads what the person may read, and revoking is not a read. ADR 0043 D3
  puts token administration on the hard-off list without saying whether ending the token a
  request carries is administration; that detail stays open, the variant the owner chose for
  undecided agent permissions.
- A tenant administrator's view and revocation of members' tokens (ADR 0035 D5, second half)
  needs a widened policy ([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
  D7) and arrives with phase 4's administration.
- ADR 0034 D7's member list shows "project entries"; the entries of a restricted project would
  reveal it to someone outside it (ADR 0034 D3).

## Required changes

1. `GET /api/v1/me`: the person (id, display name) and their memberships (tenant slug and name,
   effective role); a token restricted to a tenant sees that tenant only. Any scope.
2. `GET /api/v1/me/tokens` (table mode, ADR 0048 D2; `read`): the person's tokens with name,
   scope, restrictions, agent flag, capabilities, created, expires, last used, revoked, and a
   state — active, expired or revoked (ADR 0035 D6).
3. `DELETE /api/v1/me/tokens/{id}`: immediate revocation, the row kept with `revoked_at` and the
   revoking person, the act `revoked`; `204`, idempotent. A token may always revoke itself, at
   any scope and agent-marked or not; revoking another of the person's tokens needs `write` and
   is refused to an agent-marked request (`403 agent_forbidden`, hard-off "tokens"). The next
   request with a revoked token answers `token_revoked`.
4. `GET /api/v1/tenants/{slug}` (any role, `read`) and `PATCH` (admin role, `admin` scope,
   `If-Match`; agent hard-off — tenant settings and time locks, ADR 0043 D3): `name`,
   `members_create_projects` (ADR 0034 D9), `time_visible_to_members` (D5) and
   `time_locked_until` (ADR 0017 D8). Moving the lock either way is the act `locked`; every other
   change is `updated`.
5. `GET /api/v1/tenants/{slug}/members` (table mode; any role, `read`): each person with their
   role, the origin of each membership (`grant`, `mapping`) and their project entries, the
   entries shown only for projects the caller can see.
6. `GET /api/v1/tenants/{slug}/audit` (admin role, `read`; table and cursor modes): ADR 0026 D6's
   filters — actor, token, action, entity type, period; `Accept: text/csv` exports the same rows
   as CSV with a header row, RFC 3339 timestamps, and every cell that begins with `=`, `+`, `-`,
   `@`, a tab or a carriage return prefixed with `'`. An agent-marked request may read it — no
   hard-off rule names a read. The CSV writer is shared with T18.
7. **Tests:** a listed token's state for active, expired and revoked; self-revocation with a
   `read` token and by an agent; another token's revocation refused to `read` and to an
   agent-marked request; `token_revoked` on the next request; `PATCH` without `If-Match` → `428`,
   stale → `412`; each setting recorded, the lock moved both ways; the member list hides the
   entries of a restricted project from a person outside it; the audit view's filters with two
   tenants; the CSV neutralisation; the scope × role matrix and the agent rules on every route;
   the cross-tenant rows.
8. **Docs and records:** the README's API section; docs/security/tokens.md (listing and
   revocation, the tenant audit view as the answer to abuse, the CSV neutralisation); runtime.md
   (a script sends `If-Match`); ADR 0026 D6 (the tenant view built; the per-token and
   installation views not), ADR 0034 D5, D7, D9 and ADR 0035 D6 Status and index rows.

## Related

- T9 — the resolver, the boundary and the cross-tenant harness these routes join first.
- T11 — reads `members_create_projects`.
- T18 — the time lock's readers and the shared CSV writer.
