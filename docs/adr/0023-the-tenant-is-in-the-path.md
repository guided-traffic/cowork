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

**Partly built** (phase 2, 2026-10-02): D1–D3 and D5 — every tenant-bound route is under
`/api/v1/tenants/{tenant}/`, `/api/v1/me` and `/api/v1/me/tokens` serve the person, the
resolver answers with the canonical route's body and `ETag`, and an unknown tenant answers
like a missing membership. D2's person-level lists and D4's UI arrive with their phases.

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

**D1 — Every tenant-bound resource lives under `/api/v1/tenants/{slug}/…`.** Projects under
`…/projects/{KEY}`, tickets under `…/projects/{KEY}/tickets/{number}`, and the ticket's
comments, questions, links, interest, attachments, time entries and transitions under the
ticket. The slug is the tenant's slug of ADR 0005 D4; the project key and the number are
those of ADR 0007 D1.

**D2 — The person-level lists live under `/api/v1/me/…`:** `next`, `assigned`, `decisions`,
`inbox`, `search`. These are the only routes whose response spans tenants; they are built as
one iteration per tenant (ADR 0021 D5), and every item names its tenant. An optional
`?tenant=<slug>` narrows a `/me` list to one tenant.

**D3 — One resolver takes a canonical key in one piece:**
`GET /api/v1/tickets/{slug}/{KEY}-{number}` answers with the ticket (no redirect, so an
agent handed a key reaches the ticket in one round trip). It is the only route under
`/api/v1/tickets/`. *(Amended 2026-10-02: the route is `GET /api/v1/tickets/{slug}/{key}`
with `{key}` one segment, `<PROJECT>-<number>`, split at its last hyphen; it answers the same
body and the same `ETag` as the ticket's own route, and a key that names nothing the caller
can see is the same 404.)*

**D4 — The UI mirrors the API:** `/t/{slug}` (the tenant: board, dashboard, members, filters),
`/t/{slug}/p/{KEY}/backlog`, `/t/{slug}/p/{KEY}/board`, `/t/{slug}/tickets/{KEY}-{number}`,
`/me/next`, `/me/assigned`, `/me/decisions`, `/me/inbox`. A person with one membership is
sent to `/t/{slug}` on login and sees no tenant switcher (ADR 0005 D6); the URL still carries
the slug.

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
- The OpenAPI document (its own record) has three top-level path families: `/tenants/{slug}`,
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
  like a permission problem to the person who mistyped it; the UI's tenant switcher lists the
  person's tenants, which is the remedy.
- D3's resolver is a second way to reach a ticket; caches and ETags (the API record) have to
  treat the two routes as one resource.

## References

- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D2, D4, D6 — memberships, the slug, the single-tenant installation
- [ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) — the key the resolver takes
- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D3, D5 — the transaction context and the unions
- [`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go) — the mux the families are registered on
