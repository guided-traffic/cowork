# ADR 0004: cowork Is a Team Product — Several People per Tenant Work in It, and an Agent Acts for One of Them

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the first question of the
planning catalog ("who works in cowork?"); the recommendation put to the owner was the middle
option (owner, agent, occasional guests per tenant) and the owner chose the widest one. The
record states the decision; no rationale beyond the choice was given, and none is invented
here.

**Partly built** (phase 2, 2026-10-02): D1, D3 and D4 — people with identities of their own,
memberships, assignment, every act attributed to a person (and agent) in the audit record, the
tenant boundary equal for everyone. D2's views and D5's priority participation arrive with the
UI and the score. D4's global administrator exists since phase 3 (2026-10-03) as
`users.global_admin`: set by the start-up synchronisation for the one local administrator, and
revoked by emptying its variables; the data layer gives it no bypass.

## Context

The founding brief names several users, tenants that keep clients apart, OIDC login gated by
groups, and users who link tickets to take part in prioritisation. That leaves three sizes of
product: a private tool for the owner and the agent, the same with guests in their own
tenant, or a team product in which several people per tenant work, assign, and are notified.

The size decides how much of the domain is real: whether a role model is one line or a table,
whether an assignee exists, whether the tenant boundary is ever crossed by a hostile eye,
whether notifications are a feature or a footnote. Building the small size and growing later
means re-deciding the data model under load; building the large size means building features
the owner is, today, the only person to use.

## Decision

**D1 — cowork's users are people, several per tenant, each with an identity of their own.**
A person belongs to one or more tenants through a membership that carries a role. An LLM
agent is never a user of its own: it acts through a token bound to a person, and every write
it makes is that person's, marked as made by an agent (the token design and the attribution
are decided in their own records).

**D2 — Collaboration is first-class in the first release, not a later layer.** A ticket has
an assignee. A person has views across every tenant they belong to. Roles exist per tenant and
the question of roles per project is a design question of the first release, not deferred.
Notifications are in scope of the first release cycle; the open question is the channel, not
whether.

**D3 — Every change is attributable to a person, and shown.** Because several people share a
tenant, the timeline of a ticket names who did what, and when an agent did it, whose agent.
An audit record is therefore not optional.

**D4 — The tenant boundary holds for every person equally.** There is no owner-sees-everything
path in the data layer. A global administrator is an explicit, revocable grant, and even that
role reads a tenant's data through the same boundary the tenant's members do.

**D5 — The priority-participation feature of the brief is a multi-user feature.** Several
people register interest in a ticket and that interest feeds the priority; its exact shape is
decided in its own record, but it is built for many voices, not for one weight.

## Consequences

- The first release carries assignments, per-user cross-tenant views, per-tenant roles with a
  decision on project roles, notifications, an audit trail and an interest relation. Phases 3
  and 4 of the plan grow accordingly; the plan and the catalog were adjusted in the same
  change.
- Row-level security (the tenancy question) moves from "recommended" to "hard to argue
  against": with several parties per installation, a forgotten filter is a data leak between
  clients.
- Every team feature has to be tested with at least two identities, in the API tier and in
  the end-to-end tier, because the owner is, today, the only person who will use them.
- The UI needs people: a member list per tenant, an assignee picker, a way to invite or map a
  person into a tenant. That is work the smaller sizes would not have had.

## Alternatives Considered

- **The owner and the agent only.** Cheapest; the tenant boundary would never meet a hostile
  eye and stay untested, and the brief's multi-user sentences would be dropped. Lost by the
  owner's decision.
- **The owner, the agent, and occasional guests per tenant** — the recommendation. Covers the
  brief with a minimal role model and no team features. Lost by the owner's decision; what it
  would have saved is listed under *Consequences*.

## Residual risks

- Features built for a team that has one member. The mitigation is D3 and the two-identity
  test rule above: what cannot be exercised with two identities is not done.
- Scope: "team product" invites feature growth (mentions, watchers, presence, chat). Nothing
  of that is decided here; each is a question of its own and the default answer is no until a
  person asks for it.

## References

- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) — the shape the product runs in
- [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D10 — how a catalog question becomes a record
- [`backend/internal/store/migrations/000001_tenants.up.sql`](../../backend/internal/store/migrations/000001_tenants.up.sql) — the only domain object today
