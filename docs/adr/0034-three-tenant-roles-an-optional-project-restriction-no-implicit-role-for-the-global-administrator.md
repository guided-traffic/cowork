# ADR 0034: Three Tenant Roles, an Optional Project Restriction Enforced in the Data Layer, and No Implicit Role for the Global Administrator

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"roles?": three tenant roles with an optional per-project restriction, over tenant roles
alone, over a configurable permission matrix, and over an additional project-lead role. The
rules of D6–D8 were put to the owner with the question and not objected to.

**Not built.** No `memberships`, `project_access` or role check exists.

## Context

[ADR 0004](0004-cowork-is-a-team-product.md) made cowork a team product and put the question
of project-level visibility into the first release; D4 made the global administrator an
explicit grant with no bypass in the data layer, which [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D8 restated for policies. Memberships come from mappings and grants
([ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)),
time visibility defaults to "own and administrators" ([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D9), and every query runs through one wrapper that knows the person
([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D2). A client tenant with several people will, sooner or later, have a project not every
member should read; adding that visibility later to queries written without it is the
in-tenant leak this record avoids by deciding it now.

## Decision

**D1 — Three roles per tenant: `viewer`, `member`, `admin`.** Ordered; a check is
`role >= required`.

| Role | May |
|---|---|
| `viewer` | read everything of the tenant it may see (D3); register `watch` interest |
| `member` | everything of the working day: create and edit tickets, transition them, comment, link, register any interest, ask questions and answer those asked of them or open in the tenant, book time, upload and download attachments, drag the rank |
| `admin` | all of `member`, plus: members, mappings and grants, local accounts of the tenant, projects (create, archive, restrict), delete, restore and purge tickets, the time-period lock, the tenant's settings, read every time entry |

**D2 — The global administrator has no implicit role in any tenant.** A global administrator
creates and deletes tenants, reads installation-level audit rows and the allow-list, and in
a tenant's pages sees its administration (members, mappings, settings) — but no tickets,
no time, no attachments — until they grant themselves a role, which is a recorded manual
grant like any other.

**D3 — A project is open to every member of its tenant unless an administrator restricts
it.** A restricted project carries a list of (person, `member` or `viewer`) entries; a
person's effective role in it is the lower of their tenant role and their entry; a person
with no entry does not see the project, its tickets, its board, its tiles, its search hits,
or any notification about it. Tenant administrators always see every project. Restriction
and its list are recorded acts.

**D4 — The restriction is one predicate, carried by the data layer, not by call sites.** The
wrapper of ADR 0027 D2 knows the person and their tenant role; every generated query on
tickets and their children includes `project is unrestricted OR person is on its list OR
person is tenant admin`, and the person-level unions, the search, the dashboard and the
notification fan-out go through the same queries. There is no code path to a ticket that does
not pass the predicate.

**D5 — Time visibility is a tenant setting.** `timeVisibleToMembers` (default off): on, a
`member` sees every time entry of the tenant's projects they may see; off, only their own.
Administrators always see all (ADR 0017 D9 settled).

**D6 — Agents inherit the role of their person minus the agent restrictions.** The agent
permission record names what an agent may never do; the role says what the person may; the
agent gets the intersection.

**D7 — Role changes are recorded acts, and roles are visible.** The member list shows role,
origin (mapping, grant, local account) and project entries; a change of role or of a project
list is an audit row ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)).

**D8 — What a `viewer` cannot do is as fixed as what a `member` can.** A `viewer` answers no
question, books no time, uploads nothing, moves nothing; the UI hides what the role forbids
and the API answers `403` when asked anyway.

## Consequences

- One ordered role column per membership and one small table for restricted projects; every
  check is a comparison and one predicate.
- A restricted project is hidden, not greyed out: a person outside it does not learn it
  exists. That is the intended reading of "restricted".
- D4 depends on ADR 0027's discipline: the predicate lives in the generated queries, and the
  integration tier proves with a restricted project and three persons (on the list, off the
  list, administrator) that no view, union, search or notification leaks.
- D2 means the owner, as global administrator, grants themselves into each client tenant;
  that grant is visible to the client. Intended.
- A project lead role is not here; a tenant that wants delegation below `admin` asks, and
  the amendment is a fourth, project-level role with no change to the predicate's shape.

## Alternatives Considered

- **Tenant roles alone, every member sees every project.** Simplest; a client tenant cannot
  hide an HR or security project from its own members, and retrofitting visibility is the
  leak class. Lost.
- **A configurable permission matrix per tenant.** Administration, a test matrix, and no
  configuration anyone named that D1–D3 do not cover. Lost.
- **A project lead role now.** Plausible delegation; no tenant has asked. Deferred as an
  amendment, not refused.
- **An implicit administrator role in every tenant for the global administrator.** The
  bypass ADR 0004 D4 forbids. Lost.

## Residual risks

- D4's predicate is the second place after tenancy where a forgotten clause leaks — inside a
  tenant rather than across. ADR 0027's wrapper is the enforcement; the three-person test is
  the proof.
- D5's default hides time from members; a tenant that is one team flips the setting once.
- `viewer` with `watch` only (D1) may be too thin or too generous; the first client tenant
  will tell.

## References

- [ADR 0004](0004-cowork-is-a-team-product.md) D2, D4 — project visibility in scope, the administrator as a grant
- [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md) — where roles come from
- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D2 — the wrapper that carries the predicate
- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D8 — no bypass for the administrator
- [ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md) D9 — time visibility, settled here
- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D2 — who answers a question
