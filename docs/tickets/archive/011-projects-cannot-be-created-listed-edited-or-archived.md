---
id: T11
title: projects cannot be created, listed, read, edited or archived
state: done
severity: high
security: none
threat:
urgency: later        # rule 4: decided fix (the ADRs)
effort: S
blocked-by: T9
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: projects created per the tenant setting, listed, edited and archived
---

## Current state

Decided by [ADR 0006](../adr/0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md)
D1, D2, D4, [ADR 0007](../adr/0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md)
D1, D5, [ADR 0019](../adr/0019-no-sprints-and-no-milestones-continuous-flow-with-optional-wip-limits.md)
D3, [ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D1, D3, D9, [ADR 0035](../adr/0035-personal-access-tokens.md) D3,
[ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D3, D4 and [ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D5.

- `projects` and `ticket_counters` exist from T7; no route.
- Creating a project is a member act while the tenant's `members_create_projects` is on (the
  default) and an admin act when it is off; it is a `write`-scope act either way, and an
  agent-marked request also needs `create-project` (ADR 0034 D1, D9; ADR 0035 D3; ADR 0043 D4;
  ADR 0066 D5). Archiving, restricting and deleting stay admin acts on the agent hard-off list
  (ADR 0043 D3).
- No record says who edits a project's name, description or WIP limits: ADR 0034 D1's admin row
  names create, archive and restrict, ADR 0043 D3's hard-off list archive, restrict and delete.
- No record decides unarchiving (ADR 0006 D4: archived, never deleted). Restricting a project and
  its access list are phase 4's administration; binding a repository is phase 5's (ADR 0066).

## Required changes

1. `POST /api/v1/tenants/{slug}/projects` (`{key, name, description?, wip?}`): the role the
   tenant setting asks for, `write` scope, `create-project` when agent-marked; the key per ADR
   0007 D1, unique in the tenant (`409` naming the cause) and immutable; the counter row in the
   same `Mutate`; an `Idempotency-Key` accepted, and required when agent-marked
   ([ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
   D3); the act `created`.
2. `GET …/projects` (table mode; archived projects only with `include_archived=true`) and
   `GET …/projects/{KEY}` with its `ETag`; a restricted project exists only for the people ADR
   0034 D3 lets see it.
3. `PATCH …/projects/{KEY}` (`If-Match`; name, description, the WIP limits — advisory, they never
   refuse a transition, ADR 0019 D3): role `member`, `write` scope, agents allowed — the open
   variant of a detail no record decides, by ADR 0043's default of everything reversible and
   attributable; the key and `restricted` are not writable here.
4. `PUT …/projects/{KEY}/archive`: admin role, `admin` scope, refused to an agent-marked request
   (hard-off); idempotent; the act `archived`. An archived project refuses new tickets (T12). No
   unarchive route.
5. **Tests:** the same key in two tenants, unique in one; the key immutable; creation by a member
   with the setting on and off and by an admin either way; `create-project` allowed and refused
   for an agent; `If-Match` on `PATCH`; archive idempotent, hidden by default, refused to a member
   and to an agent; the restriction rows; the cross-tenant rows.
6. **Docs and records:** domain.md (projects, the counter, WIP limits as information); the
   README's naming (the project key) and API section; ADR 0006 (D1, D2, D4 built; D3 phase 5),
   ADR 0019 D3 (stored; the board shows them in phase 3) and ADR 0034 D9 Status and index rows.

## Related

- T7 — the tables.
- T10 — the tenant setting this route reads.
- T12 — tickets refuse an archived project.
