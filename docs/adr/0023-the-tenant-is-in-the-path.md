# ADR 0023: The Tenant Is in the Path — `/api/v1/tenants/{slug}/…`, `/api/v1/me/…` for the Person-Level Lists, and One Resolver for a Canonical Key

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "how
does a request name its tenant?": the tenant slug in the path, over a header, over a
subdomain per tenant, and over a default tenant for the single-tenant case. The rules of D5
were put to the owner with the question and not objected to.

Amended 2026-10-02 (D3: the key is one path segment). The router takes a wildcard only as a
whole path segment, so `{KEY}-{number}` cannot be two parameters; the first implementation
reads the segment as one key and splits it at its last hyphen, which a project key never
contains ([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D1).

Amended 2026-10-05 (D4: the start page `/` is "next for me" for every person with a membership,
as the work list of the person-level lists named it once "next for me" had its members
([ADR 0018](0018-the-views-of-the-first-release.md) D3 as amended the same day); this replaces the
redirect of a person with one membership to their tenant, and that tenant's name in the top bar
leads there instead).

**Partly built** (phase 2, 2026-10-02): D1–D3 and D5 — every tenant-bound route is under
`/api/v1/tenants/{tenant}/`, `/api/v1/me` and `/api/v1/me/tokens` serve the person, the
resolver answers with the canonical route's body and `ETag`, and an unknown tenant answers
like a missing membership. D2's person-level lists arrive with their phase. *(Phase 3,
2026-10-03:)* D4's routes `/t/{slug}`, `/t/{slug}/p/{KEY}/backlog`,
`/t/{slug}/tickets/{KEY}-{number}` and `/t/{slug}/members`, and ~~the single membership's
redirect to `/t/{slug}`~~ *(gone 2026-10-05 with D4's amendment)* without a tenant switcher —
[`app.routes.ts`](../../frontend/src/app/app.routes.ts). *(2026-10-04:)* D2's `inbox` — with
`/me/inbox/read` and `/me/inbox/{notification}/read` to mark it read —, `assigned` and `decisions`,
each one read per tenant of the person, every item naming its tenant, `?tenant=<slug>` narrowing to
one and answering a slug that names none of the person's tenants like D5's unknown slug
([`api/inbox.go`](../../backend/internal/api/inbox.go), [`api/mylists.go`](../../backend/internal/api/mylists.go));
D4's `/me/inbox`, `/me/assigned` and `/me/decisions`. ~~Not built: D2's `next` and `search`, D4's
`/me/next`.~~ *(2026-10-05:)* D2's `search`, `GET /api/v1/me/search`, read per tenant of the person,
each hit naming its tenant, `?tenant=<slug>` narrowing to one as on the other lists
([`api/search.go`](../../backend/internal/api/search.go)); the UI mirrors it and the tenant's own
`GET /api/v1/tenants/{slug}/search` with `/me/search` and `/t/{slug}/search`, two routes D4 did not
list. *(2026-10-05:)* D1's tenant-bound resources gain the bin, `…/deleted-tickets` with
`…/deleted-tickets/{key}` and its `restore` — `{key}` one segment, `<PROJECT>-<number>`, as D3's —,
and the saved filters, `…/filters` and `…/filters/{filter}`; D4's UI mirrors the bin as
`/t/{slug}/deleted-tickets`. *(2026-10-05:)* D4's UI mirrors the tenant's tickets, `GET …/tickets`,
as `/t/{slug}/tickets`, a route D4 did not list, its filters the page's query parameters; a ticket's
own page stays `/t/{slug}/tickets/{KEY}-{number}`. *(2026-10-05:)* D2's `next`, which also takes `?project=<KEY>` with
`?tenant=`, and D4's `/me/next` and the start page as amended
([`features/home/home.ts`](../../frontend/src/app/features/home/home.ts)). *(2026-10-07:)* D4's UI
mirrors a project's import, `POST …/projects/{KEY}/imports` and `GET …/imports/{import}`, as
`/t/{slug}/p/{KEY}/imports` and `/t/{slug}/p/{KEY}/imports/{import}`, two routes D4 did not list
([`app.routes.ts`](../../frontend/src/app/app.routes.ts)).

**Amended 2026-10-10 by the owner (not built)**, after a walk through the UI before use, and with
[ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1's tenant that is a team: D1 — the family is `/api/v1/teams/{slug}/…`, the old one served
beside it for a release; D2 — `projects` joins the person-level lists; D4 — the sidebar lists every
team of the person with its projects on every page, a team's configuration is one dialog behind a
gear, and the top bar loses the tenant switcher and the team's name to the sidebar and to "All
teams". The rules this replaces are marked in place. *(2026-10-10:)* D1's team family with the old
family's deprecated twins is built, and D2's `?team=` beside the deprecated `?tenant=` (below); D2's
`projects` and D4 are not.

**Built** (2026-10-10): D1 — the source names the team family only, `/api/v1/teams/{team}/…` and
`/api/v1/teams`; [`tools/specbundle`](../../backend/tools/specbundle/main.go) writes for every path of
it a deprecated twin under `/api/v1/tenants/{tenant}/…`, tagged `tenants`, which neither generated
client carries, and the server answers a twin by reading its path as the team path before it routes
it (`asTeamPath` in [`api/api.go`](../../backend/internal/api/api.go)), so a twin meets D5's
boundary, the security and the handler of its team path and answers as it does, while the request
log keeps the path as it was sent — the way
[ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) D7 states, made concrete by the
implementer, open to the owner's objection; D3's resolver is
`/api/v1/tickets/{team}/{key}`, its parameter renamed and its path the same. D2 — the lists take
`?team=<slug>`, and `?tenant=` in its place for one release, both together `400 validation_failed`
at `query:tenant` whatever the values ([`api/inbox.go`](../../backend/internal/api/inbox.go)
`teamQuery`), and every item names its team as `team` and, beside it, as `tenant`. D4's routes keep
`/t/{slug}`. Not built: the removal of the twins and of `?tenant=`, a later release's.

## Context

The tenant has to reach three places on every request: the membership check, the
`SET LOCAL app.tenant_id` of [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D3, and the person's eye in a bookmark, a chat link or a commit trailer. A person may belong
to several tenants ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
D2), a token may be valid in several, and a key is canonical in its full form
([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D2).
The single-tenant installation must not need a second URL form that breaks when a second
tenant arrives (ADR 0005 D6).

## Decision

**D1 — Every tenant-bound resource lives under `/api/v1/tenants/{slug}/…`** *(amended
2026-10-10 by the owner, ~~not built~~ built 2026-10-10 with the old family's twins, their removal
outstanding: under `/api/v1/teams/{slug}/…`, the tenant being a team, [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
D1; the family `/api/v1/tenants/{slug}/…` stays served beside it for one release, deprecated, and
goes in the next, as `urgency` went beside `horizon`
([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md),
[ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) D7))*. Projects under
`…/projects/{KEY}`, tickets under `…/projects/{KEY}/tickets/{number}`, and the ticket's
comments, questions, links, interest, attachments, time entries and transitions under the
ticket. The slug is the tenant's slug of ADR 0005 D4; the project key and the number are
those of ADR 0007 D1.

**D2 — The person-level lists live under `/api/v1/me/…`:** `next`, `assigned`, `decisions`,
`inbox`, `search` *(amended 2026-10-10 by the owner, not built: and `projects`, every project of
every team of the person, each naming its team, which the sidebar of D4 lists; the current team's
stay live through its event stream, the others are read again when the tab regains focus, when the
person enters a team and when their memberships change)*. These are the only routes whose response
spans tenants; they are built as
one iteration per tenant (ADR 0021 D5), and every item names its tenant. An optional
~~`?tenant=<slug>`~~ `?team=<slug>` *(2026-10-10; `?tenant=` taken in its place for one release,
deprecated)* narrows a `/me` list to one tenant.

**D3 — One resolver takes a canonical key in one piece:**
`GET /api/v1/tickets/{slug}/{KEY}-{number}` answers with the ticket (no redirect, so an
agent handed a key reaches the ticket in one round trip). It is the only route under
`/api/v1/tickets/`. *(Amended 2026-10-02: the route is `GET /api/v1/tickets/{slug}/{key}`
with `{key}` one segment, `<PROJECT>-<number>`, split at its last hyphen; it answers the same
body and the same `ETag` as the ticket's own route, and a key that names nothing the caller
can see is the same 404.)*

**D4 — The UI mirrors the API:** `/t/{slug}` (the tenant: board, dashboard, members, filters),
`/t/{slug}/p/{KEY}/backlog`, `/t/{slug}/p/{KEY}/board`, `/t/{slug}/tickets/{KEY}-{number}`,
`/me/next`, `/me/assigned`, `/me/decisions`, `/me/inbox`. A person with one membership ~~is
sent to `/t/{slug}` on login and~~ sees no tenant switcher (ADR 0005 D6); the URL still carries
the slug. *(Amended 2026-10-05:)* **The start page `/` is "next for me"** for every person with a
membership, whether they belong to one tenant or to many: what they could take up next across
their tenants ([ADR 0018](0018-the-views-of-the-first-release.md) D3). For a person with one
membership ~~the top bar names the tenant and leads to `/t/{slug}`~~; a person with none chooses
among the tenants they may open, as before. *(Amended 2026-10-10 by the owner, not built:)*
**The sidebar is for the daily work.** Below the person-level lists it shows every team the person
is a member of, one below the other, on every page — the person-level pages included —, each as its
name, a small gear beside it, and its projects, each project opening its board; the team's name
opens `/t/{slug}`, the dashboard that sums up the team, which carries the team's board, its ticket
list and its time report as tabs ([ADR 0018](0018-the-views-of-the-first-release.md)). A team in
which a global administrator holds no role
([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2) gets no group. **The gear opens one dialog of the team's configuration**, large, with a tab for
each page the person's role shows today — members, accounts, group mappings, tokens, settings, the
audit record, the deleted tickets; each tab keeps its address (`/t/{slug}/members`, `…/accounts`,
`…/group-mappings`, `…/tokens`, `…/settings`, `…/audit`, `…/deleted-tickets`), which shows the dialog
on that tab over the team's dashboard, so a reload, the back button and a bookmark keep working and
the pages' services keep taking their team from the route; closing returns to the page before, or
to the dashboard. **The top bar carries no tenant switcher and no team name**: a global
administrator reaches every team of the installation through "All teams" in the person menu, a page
of its own, which also makes a new team ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D5). `/t/{slug}` keeps its path — its `t` reads as
team.

**D5 — The membership check runs before any handler under `/tenants/{slug}/`, and an unknown
slug and a missing membership answer identically with `404`.** A tenant's existence is not
revealed to someone outside it, and a ticket that exists in another tenant is "not found", not
"forbidden". Inside a tenant, a role that forbids an action answers `403`.

**D6 — There is no default tenant and no second URL form.** A path without a slug is not a
tenant-bound route; the single-tenant installation uses the same paths as every other.

## Consequences

- One middleware, one place: slug → tenant row → membership → `SET LOCAL` → handler.
- Every log line, bookmark, chat link and commit trailer carries the tenant; a token valid in
  several tenants is never ambiguous.
- Paths are long. The owner accepted the same for the key (ADR 0007).
- The OpenAPI document (its own record) has three top-level path families: `/tenants/{slug}`
  *(2026-10-10, ~~not built~~ built the same day: `/teams/{slug}`, the old family deprecated for a
  release)*,
  `/me`, `/tickets/{slug}/{key}`.
- The frontend's route table is fixed by D4; the catalog's URL-scheme question is answered by
  this record.

## Alternatives Considered

- **A header (`X-Cowork-Tenant`) and tenant-less paths.** Shorter URLs; bookmarks and links
  lose their context, logs must record the header, a browser tab per tenant needs client
  state, and an agent handed a link must reconstruct the header. Lost.
- **A subdomain per tenant.** Clean cookie scopes, attractive for the session design; a
  wildcard certificate and a wildcard Ingress as an installation requirement, and the
  single-tenant installation would still need a subdomain or a special case. Lost.
- **Path with an optional slug that defaults to the person's only tenant.** Two URL forms for
  one resource; caches and bookmarks break the day a second tenant appears. Lost to D6.

## Residual risks

- D5's identical `404` for "no such tenant" and "not a member" makes a mistyped slug look
  like a permission problem to the person who mistyped it; ~~the UI's tenant switcher lists the
  person's tenants, which is the remedy~~ *(2026-10-10: the sidebar lists the person's teams)*,
  which is the remedy.
- D3's resolver is a second way to reach a ticket; caches and ETags (the API record) have to
  treat the two routes as one resource.

## References

- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D2, D4, D6 — memberships, the slug, the single-tenant installation
- [ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) — the key the resolver takes
- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D3, D5 — the transaction context and the unions
- [`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go) — the mux the families are registered on
