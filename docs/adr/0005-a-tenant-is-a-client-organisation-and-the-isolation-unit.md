# ADR 0005: A Tenant Is a Client Organisation and the Isolation Unit, a Person May Belong to Many, and One Tenant Is the Common Installation

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the catalog question "what
is a tenant?": the hard-boundary option, with two points the owner added — a person may be in
several tenants, and most installations will run exactly one tenant with every user in it,
while some will create several to keep their tickets apart.

**Partly built** (phase 2, 2026-10-02): D1–D4 and D6 — the tenant as the isolation unit in the
path and in row-level security, memberships (migration 2), nothing crossing the boundary (links
and parents by composite keys), the slug immutable and the name editable.

**Built** (phase 3, 2026-10-03): D5's creation — `POST /api/v1/tenants`
([`api/tenants.go`](../../backend/internal/api/tenants.go) `CreateTenant`), and the bootstrap
tenant of [ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
D6. Until the identity provider exists a global administrator is the one local administrator
the configuration names: `users.global_admin` is set by the start-up synchronisation, by no route,
and no policy lets a tenant's administrator set it. ~~Not built: members joining through the
identity provider's group mapping, and an administrator's grant of an existing person into a
tenant — members come in as the local accounts a tenant's administrators create
([ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D1).~~

**Built** (phase 4, 2026-10-04): D5's other ways in — members joining through the identity
provider's group mapping, and an administrator's grant of an existing person, by address or
username, into a tenant
([ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md));
the members of the provider's administrator group are global administrators beside the local
administrator, the flag set by the provider's login and by no route.

**Partly built** (phase 3, 2026-10-04): D3's person-level lists "assigned to me", "open decisions"
and the inbox, each a union of per-tenant reads under that tenant's rules, a membership that ends
taking its part out at once ([`api/inbox.go`](../../backend/internal/api/inbox.go)
`personTenants`); "next for me" is not built.

## Context

[ADR 0004](0004-cowork-is-a-team-product.md) makes cowork a team product: several people per
tenant, with an agent acting for one of them. The tenant is the line between one client's
tickets and another's, and the founding brief demands that line never blur ("without mixing
the tickets"). What the tenant *is*, whether a person may stand on both sides of a line, and
whether a personal or shared space may cross it, decides the shape of every access check,
every query and every row-level policy that follows. It also decides how an installation
that has one client — the common case, per the owner — feels: multi-tenancy must not be a tax
on the people who have one tenant.

## Decision

**D1 — A tenant is a client or an organisation, and it is the unit of isolation.** Every
project, ticket, comment, link, open question, membership, notification and audit record
belongs to exactly one tenant, by a `tenant_id` that is never null.

**D2 — A person may belong to any number of tenants.** Membership is a row (person, tenant,
role). The owner belongs to every client's tenant; a client's people belong to theirs. The
owner's own work is a tenant like any other, with no special standing in the data model.

**D3 — Nothing crosses the boundary.** No link between tickets of different tenants, no move
of a ticket between tenants, no query that joins two tenants' rows. Exactly one kind of
cross-tenant result exists: the **person-level lists** — "next for me", "assigned to me",
"open decisions", the inbox — which are unions over the tenants the person belongs to, each
part computed under that tenant's own rules. A ticket changes tenant only by export and
import, and the audit record of both ends says so.

**D4 — The slug is the public, immutable identifier of a tenant; the name is editable.** The
slug appears in URLs, in the API path and in the repository binding of the workflow plan; a
rename would break every one of them. The slug rule is the one migration `000001` enforces.

**D5 — Tenants are created by a global administrator only**, which under ADR 0004 D4 is an
explicit grant. Members join a tenant through the identity provider's group mapping or an
administrator's explicit grant; the mechanics are the identity records' to decide. *(Built
2026-10-03: the route takes a browser session only — a token, a global administrator's
included, answers `403 session_required` — and a creator who is not a global administrator
answers `403`. The tenant and its creator's marked `admin` grant are written in one transaction
with both acts, so no tenant exists without an administrator
([ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
D7); a slug that is taken answers `409 tenant_slug_taken`, and a slug is never reused.)*

**D6 — One tenant is the common installation, and it gets no special mode.** The data model
and the API always carry the tenant; there is no flag that turns tenancy off and no second
code path. A person with exactly one membership lands in that tenant and sees no tenant
switcher; an installation grows from one tenant to several by creating them, without a
migration or a reconfiguration. How the first tenant and the first administrator come to
exist on a fresh installation is one decision, taken in the identity block of the catalog.

## Consequences

- The tenancy check is one predicate — `tenant_id = <current tenant>` — everywhere, and a
  row-level-security policy is that predicate written once in the database. Whether the
  policy is used is the tenancy question of the catalog; this record makes the answer easy.
- The API path carries the tenant slug; the alternative (a header) has lost most of its
  appeal, since a person with several memberships would otherwise switch context by header.
- The UI has a tenant switcher that is invisible to the person with one membership, and a
  membership list per tenant. The single-tenant installation sees neither tenancy nor slug
  except in the URL.
- The person-level lists (D3) are the one place where rows of several tenants meet in one
  response. They have to be built as a union of per-tenant queries under the same rules, and
  tested so that a revoked membership drops its part at once.
- A finding that concerns two clients is two tickets, one per tenant, possibly linked by
  nothing. Accepted: that is what "no mixing" means.
- Tests of the boundary need two tenants and two identities in the API tier and in the
  end-to-end tier.

## Alternatives Considered

- **A personal or shared space that crosses tenants** (the owner's cross-client list with
  references into client tickets). Every policy and every query would carry an exception for
  it — the class of mistake that leaks one client's data to another. The need it answers is
  covered by D3's person-level lists without any row crossing a boundary. Lost.
- **Hierarchical tenants** (organisation → departments). Nested membership resolution and
  inherited roles for a structure nobody asked for; projects cover the division inside a
  client. Lost.
- **A single-tenant mode** (a configuration flag that disables tenancy for the common
  installation). Two code paths, and the multi-tenant one would run untested in most
  installations. Lost to D6.

## Residual risks

- D3's person-level lists are the boundary's weakest point by construction; the mitigation
  is the test rule in *Consequences* and building them as unions, never as a single query
  over all tenants.
- Nothing beyond the `tenants` table is built; every claim above is a rule for what will be
  built, not a description of code.

## References

- [ADR 0004](0004-cowork-is-a-team-product.md) — several people per tenant, the global administrator as an explicit grant
- [`backend/internal/store/migrations/000001_tenants.up.sql`](../../backend/internal/store/migrations/000001_tenants.up.sql) — the slug rule and the key
- [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md), [docs/operations/claude-code.md](../operations/claude-code.md) — the repository binding that carries the slug (the workflow plan once linked here is consumed, ADR 0074 D3)
