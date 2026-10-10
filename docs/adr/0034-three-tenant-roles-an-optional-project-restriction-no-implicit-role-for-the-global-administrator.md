# ADR 0034: Three Tenant Roles, an Optional Project Restriction Enforced in the Data Layer, and No Implicit Role for the Global Administrator

## Status

Accepted, amended 2026-10-02 (D1, D9: creating a project is a member act unless the tenant
reserves it to administrators) and 2026-10-04 (D1: no change leaves a tenant without an
administrator; D3, D7: the administration built, and a restricted project's entries on its own
access list, not in the member list), and again on 2026-10-04 after the security review (D1: only
an administrator who can log in counts, and the changes take the tenant's lock; D3: a trigger holds
the restriction to the tenant's administrators; D7: the address shown to administrators only),
and a third time on 2026-10-04 (D1: the deactivation of a local account is held to the rule in the
tenant that manages the account), and a fourth time on 2026-10-04 (D1: making a group mapping and
changing its role take a global administrator besides the `admin` role — the owner's answer, recorded
in [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D7), and a fifth time on 2026-10-04 (D2: built — the owner's choice of the global administrator's
grant to themselves as the recovery of a tenant without an administrator, in any tenant in which
they do not hold `admin` — one without a role, or one with a lower role, whose own grant they raise;
the view of a tenant without a role is a browser session's). Date: 2026-10-01. Decided by the owner as the answer to the
catalog question "roles?": three tenant roles with an optional per-project restriction, over tenant roles
alone, over a configurable permission matrix, and over an additional project-lead role. The
rules of D6–D8 were put to the owner with the question and not objected to. D9 is the
owner's answer of 2026-10-02 to the question whether an agent's `write` token may create a
project when its person may: a tenant setting, because people who create projects in their
Git forge want to feed them with work through cowork, and a tenant that wants exceptions
switches it off.

**Partly built** (phase 2, 2026-10-02): D1, D3–D6, D8, D9 and D7's member list — `memberships` and `project_access`
(migrations 2 and 3), the role check of every act through
[`auth.Authorize`](../../backend/internal/auth/authorize.go), the effective role of a restricted
project, the visibility predicate in every ticket query, the time visibility of D5, and the
tenant settings `timeVisibleToMembers` and `membersCreateProjects`, and agents bounded by their
person's role minus [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)'s
restrictions. Writing a restricted project's list and changing roles (D7's acts) arrive with
the administration.

**Partly built** (phase 3, 2026-10-03): D2's global administrator — `users.global_admin`, which
the start-up synchronisation sets for the local administrator, and `POST /api/v1/tenants`; a
global administrator who creates a tenant is its first administrator by a marked grant
([ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
D7), and in any other tenant has no role ~~and meets the tenant boundary like a stranger~~. Not built:
~~the view of a tenant's administration without a role,~~ the reading of installation-level audit
rows, the deletion of a tenant ~~and the route by which a global administrator grants themselves a
role in an existing tenant~~ *(built 2026-10-04, below)*; the `memberships` policy admits a global
administrator's own grant as `admin`, which the creation of a tenant uses *(in any role since
migration 26)*.

**Built** (phase 4, 2026-10-04): D7's acts — a member's grant added by address or username, its
role changed and removed, the group mappings of
[ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md),
a project's restriction and its access list — in
[`api/members.go`](../../backend/internal/api/members.go) and the UI, every one recorded and
announced as `membership.changed`; the writes of the restriction's list held to the tenant's
administrators in the data layer by restrictive policies
([migration 22](../../backend/internal/store/migrations/000022_membership_administration.up.sql));
D1's refusal to leave a tenant without an administrator (`409 last_admin`), and — built the same
day — the deactivation of a local account held to it in the managing tenant under that tenant's
lock ([`api/accounts.go`](../../backend/internal/api/accounts.go) `DeactivateAccount`). The members of the
identity provider's administrator group are global administrators too (ADR 0030 D1). Still not
built: D2's parts named above.

**Built** (2026-10-04): D2's view of a tenant without a role, the list of every tenant and the
grant to themselves — the request layer's admission (`oversees` and `overseen` in
[`api/tenant.go`](../../backend/internal/api/tenant.go)), `GET /api/v1/tenants`
([`api/tenants.go`](../../backend/internal/api/tenants.go) ~~`ListTenants`~~ `ListTeams` *(2026-10-10)*), the grant
([`api/members.go`](../../backend/internal/api/members.go) `grantSelf`), the policies of
[migration 26](../../backend/internal/store/migrations/000026_global_admin_self_grant.up.sql), and
the UI's offer above a tenant's pages — and, the same day, the raise of a global administrator's own
grant where they hold a role below `admin` (`ownGrant`, `setOwnGrant`; the UI's offer on the members
page). Still not built: the reading of installation-level audit rows
and the deletion of a tenant.

Amended 2026-10-10 by the owner's rename of a tenant to a team ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1): the Status and D2 name the
handler and the operation the rename renamed, `ListTeams` and `getTeam`; the request layer's
admission names `getTeam`, and a request to the deprecated twin of `getTenant` is answered as it.
No rule changes.

*(2026-10-10.)* D4's head at the other end of a relation is built
([migration 47](../../backend/internal/store/migrations/000047_relations_across_teams.up.sql), `ticket_sight`): a member to whom a project is restricted reads a
ticket of it that a parent, a child or a link of a ticket they see names by its head, and that
ticket counts among the open prerequisites of the ticket it blocks and refuses its `done`
([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D6, D7), since its state is in its
head; every other surface keeps the predicate.

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
| `member` | everything of the working day: create and edit tickets, transition them, comment, link, register any interest, ask questions and answer those asked of them or open in the tenant, book time, upload and download attachments, drag the rank; *(added 2026-10-02)* create projects while the tenant allows it (D9) |
| `admin` | all of `member`, plus: members, mappings *(amended 2026-10-04: making one and changing its role take a global administrator besides, ADR 0030 D7; reading and removing one do not)* and grants, local accounts of the tenant, projects (~~create,~~ archive, restrict; *(amended 2026-10-02)* create always, D9), delete, restore and purge tickets, the time-period lock, the tenant's settings, read every time entry |

*(Added 2026-10-04:)* A tenant always has an administrator. A change of a grant or of a group
mapping that would leave no ~~active~~ person holding `admin` in the tenant, mapped or granted, is
refused with `409 last_admin` and changes nothing — the administrator's own grant and the mapping
that makes them administrator included. A derivation from the identity provider's groups at a
login, a refresh or a token's gate check is the issuer's word and is not refused
([docs/security/identity-provider.md](../security/identity-provider.md) H-29). *(Amended after the
security review, 2026-10-04: only a person who can log in counts — active, and a local account or a
person of the configured issuer whom the gate admitted at their last login, refresh or check — and
each such change takes the tenant's lock before any person's, so two administrators who take each
other's role away at once are decided one after the other. ~~Not held to the rule: the deactivation of
a local account ([docs/security/local-accounts.md](../security/local-accounts.md) H-32).~~)*
*(Amended 2026-10-04: the deactivation of a local account is such a change in the tenant that
manages the account — a deactivated person is no administrator who can log in. It takes that
tenant's lock first and is refused with `409 last_admin`, changing nothing, when no administrator
who can log in would remain, so two administrators who deactivate each other's accounts at once are
decided one after the other. The other tenants in which the person holds `admin` are not asked: the
deactivation runs in the managing tenant's transaction, and row-level security keeps their rows out
of it ([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D3, D8;
[docs/security/local-accounts.md](../security/local-accounts.md) H-32).)*

**D2 — The global administrator has no implicit role in any tenant.** A global administrator
creates and deletes tenants, reads installation-level audit rows and the allow-list, and in
a tenant's pages sees its administration (members, mappings, settings) — but no tickets,
no time, no attachments — until they grant themselves a role, which is a recorded manual
grant like any other. *(Built 2026-10-04, chosen by the owner as the recovery of a tenant that lost
its last administrator who can log in, whatever took it — a derivation from the issuer's groups, a
mapping's change, the deactivation of an account another tenant manages
([docs/security/identity-provider.md](../security/identity-provider.md) H-29,
[docs/security/local-accounts.md](../security/local-accounts.md) H-32). The reach is a browser
session's that no agent header marks: the request layer admits a global administrator without a
role in the tenant to ~~`getTenant`~~ `getTeam` *(2026-10-10)*, `listMembers` — without the addresses, which are the tenant's
administrators' (D7) — and `listGroupMappings`, and to `setMemberGrant` on their own person, and
answers every other route of the tenant the `404` an unknown tenant gets; a token of theirs keeps
the reach of the person's memberships, so a leaked one gains nothing by it, and an agent — the chat
in the UI among them — gets none. They find such a tenant through `GET /api/v1/tenants`, every
tenant of the installation with the role they hold in it or none, which takes a browser session as
well ([ADR 0035](0035-personal-access-tokens.md) D5). The grant is `PUT …/members/{person_id}/grant`
with their own id, in any role — the UI offers `admin` first —: made under the tenant's lock,
recorded in the tenant with the administrator as its actor and announced to its members as
`membership.changed`; it takes no administrator away, so it is never `409 last_admin`. A global
administrator who holds a role below `admin` in the tenant — mapped or granted — sets their own grant
the same way, made or its role changed, which raises them (the owner's answer of 2026-10-04: "a
global administrator grants themselves `admin` in any tenant"). Once they hold `admin` they are an
administrator of the tenant like any other, and their grant is changed or removed as anyone's, a
lowering of their own held to `last_admin`. Row-level security
shows a global administrator every row of `tenants`, admits their own grant in any role and the
change of their own grant's role in the tenant (migration 26); inside the tenant's transaction the tenant-bound tables admit whomever the request
layer admitted, as they do for a member ([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D3), so which operations a global administrator without a role reaches is the request layer's
list.)*

**D3 — A project is open to every member of its tenant unless an administrator restricts
it.** A restricted project carries a list of (person, `member` or `viewer`) entries; a
person's effective role in it is the lower of their tenant role and their entry; a person
with no entry does not see the project, its tickets, its board, its tiles, its search hits,
or any notification about it. Tenant administrators always see every project. Restriction
and its list are recorded acts. *(Built 2026-10-04: `PUT …/projects/{project}/restriction` with
the project's `If-Match`, and `PUT`/`DELETE …/projects/{project}/access/{person_id}` for a member of
the tenant, an entry `member` or `viewer`; restricting, opening and putting a person on the list take
a browser session, taking a person off it an administrator's token as well
([ADR 0035](0035-personal-access-tokens.md) D5). The list may be written before the project is
restricted, so nobody on it loses the project in between.)* *(Amended after the security review,
2026-10-04: the restriction is the tenant's administrators' in the data layer too — a trigger,
`projects_restriction_guard`, refuses a change of `restricted` by anyone else, because a member may
change the project's other settings and a policy cannot tell the columns apart.)*

**D4 — The restriction is one predicate, carried by the data layer, not by call sites.** The
wrapper of ADR 0027 D2 knows the person and their tenant role; every generated query on
tickets and their children includes `project is unrestricted OR person is on its list OR
person is tenant admin`, and the person-level unions, the search, the dashboard and the
notification fan-out go through the same queries. There is no code path to a ticket that does
not pass the predicate. *(Made concrete 2026-10-10 on the recommendation, open to the owner's
objection, built the same day:)* at the other end of a parent, a child or a link of a ticket the person sees,
a ticket of a project restricted from them is shown by its head, as to a person outside the team
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3) — a member is never shown less than an outsider; every other surface keeps the
predicate.

**D5 — Time visibility is a tenant setting.** `timeVisibleToMembers` (default off): on, a
`member` sees every time entry of the tenant's projects they may see; off, only their own.
Administrators always see all (ADR 0017 D9 settled).

**D6 — Agents inherit the role of their person minus the agent restrictions.** The agent
permission record names what an agent may never do; the role says what the person may; the
agent gets the intersection.

**D7 — Role changes are recorded acts, and roles are visible.** The member list shows role,
origin (mapping, grant, local account) ~~and project entries~~; a change of role or of a project
list is an audit row ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)).
*(Amended 2026-10-04: a restricted project's entries are on the project's own access list,
`GET …/projects/{project}/access`, which the tenant's administrators read, and not in the member
list: every member, a viewer included, reads the member list, and an entry there would name a
restricted project to members who must not learn it exists (D3). The member list shows the effective
role, every origin with its own role — the mapping, the grant — and whether the person has a local
account.)* *(Amended after the security review, 2026-10-04: and, to the tenant's administrators only,
the person's e-mail address, which tells two persons of one name apart; everyone else reads `null`.
The access list shows it too, being the administrators' to read.)*

**D8 — What a `viewer` cannot do is as fixed as what a `member` can.** A `viewer` answers no
question, books no time, uploads nothing, moves nothing; the UI hides what the role forbids
and the API answers `403` when asked anyway.

**D9 — Who creates projects is a tenant setting.** *(Added 2026-10-02.)*
`membersCreateProjects` (default on): on, a `member` creates projects in the tenant; off,
only its administrators do. A tenant administrator changes it, and the change is a recorded
act ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)). Creating a
project is a `write`-scope act either way ([ADR 0035](0035-personal-access-tokens.md) D3), so
an agent token creates one where its person may and its `create-project` capability is set
([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4). Archiving, restricting and deleting a project stay administration acts.

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
- *(Added 2026-10-04.)* D1 holds a deactivation to the managing tenant alone. A local account
  that another tenant manages and that is a tenant's only administrator who can log in is
  deactivated without that tenant being asked, and leaves it without one
  ([docs/security/local-accounts.md](../security/local-accounts.md) H-32); D2's self-grant ~~, which
  would recover such a tenant, is not built~~ *(built 2026-10-04)* recovers such a tenant once a
  global administrator acts.
- *(Added 2026-10-04.)* D2's reach of a global administrator without a role is held by one list in
  the request layer: inside the tenant's transaction row-level security admits its tickets, time and
  attachments to whomever the request layer admitted (ADR 0021 D3), so an operation added to that
  list by mistake would show the tenant's work to a global administrator who holds no role in it.
  The integration tier walks every route of the document as such a global administrator, so a route
  that answers more than an unknown tenant's `404` fails the day it is added.
- ~~*(Added 2026-10-04.)* A global administrator who holds a role below `admin` in a tenant does not
  grant themselves one: they are no longer without a role, and changing their own grant is an
  administrator's act. In a tenant without an administrator who can log in they cannot recover it;
  another global administrator who holds no role there can
  ([docs/security/identity-provider.md](../security/identity-provider.md) H-29).~~ *(Closed
  2026-10-04 by the owner's answer recorded in D2: they raise their own grant.)*

## References

- [ADR 0004](0004-cowork-is-a-team-product.md) D2, D4 — project visibility in scope, the administrator as a grant
- [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md) — where roles come from
- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D2 — the wrapper that carries the predicate
- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D8 — no bypass for the administrator
- [ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md) D9 — time visibility, settled here
- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D2 — who answers a question
