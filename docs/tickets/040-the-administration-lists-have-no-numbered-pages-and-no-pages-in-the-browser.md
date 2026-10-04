---
id: T40
title: the audit view, members, tokens and projects have no numbered pages, the tenant has no audit page, and its administrators cannot see or revoke the members' tokens
state: analysed
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix for the paging and the audit page; the members' tokens wait for Q1
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided:
done:
---

## Current state

The ticket lists and the tenant's time take `page` and `per_page`; the audit view (`listAudit`),
the member list (`listMembers`), the person's token list (`listMyTokens`) and the project list
(`listProjects`) page by cursor only.
[ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D2 (numbered
pages on tables) is carried over from phase 2.

The browser has the tenant's administration pages for its settings (`updateTenant` with
`If-Match`), a project's settings with its WIP limits, access list and archive, the new project,
the members with their roles and where each comes from, the local accounts and the group
mappings ([`app.routes.ts`](../../frontend/src/app/app.routes.ts#L48-L79)). It has no audit page.
And no route lists a tenant's tokens: the administrator's view and revocation of the members'
tokens ([ADR 0035](../adr/0035-personal-access-tokens.md) D5) is built neither in the backend —
there is no `/api/v1/tenants/{tenant}/tokens` — nor in the browser
([tokens.md](../security/tokens.md) says so).

## Required changes

### Independent of the open question

1. `page`/`per_page` with `total` on the audit view, members, tokens and projects, in the API
   document first; integration tests for the paging.
2. The tenant's audit page with its filters — actor, token, action, entity, period — and its CSV
   (`listAudit`, [ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D6).
3. Unit tests per page.

### Depends on the answer

4. The administrator's view of the members' tokens — metadata, never plaintext — and their
   revocation (ADR 0035 D5): a route under `/api/v1/tenants/{tenant}/` in the API document
   first, the page, and integration tests across two tenants that it shows the tokens Q1 decides
   and nothing else.

## Open questions

### Q1: Which of a member's tokens does a tenant's administrator see and revoke?

ADR 0035 D5 gives a tenant's administrators "the tokens of their tenant's members" and their
revocation, without saying which: a member's token may be unrestricted, reaching every tenant
they belong to, or restricted to one tenant — possibly another. A token's name is the person's
free text, and nothing of one tenant crosses to another
([ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3).

- **(a) Every token of every member,** as the sentence reads: an administrator of one tenant
  reads the names, scopes and dates of tokens restricted to the member's other tenants, and can
  revoke them.
- **(b) The tokens that can act in the tenant** — the unrestricted ones and those restricted to
  it. What the administrator sees is what can touch the tenant, and the name of such a token
  already shows there beside every act made through it
  ([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
  D6). Revoking an unrestricted token ends it in the member's other tenants too, which the page
  says before it acts.
- **(c) Only the tokens restricted to the tenant:** nothing of another tenant and no revocation
  that reaches beyond it; an unrestricted token that acts in the tenant stays out of its
  administrators' reach, findable only through the audit view's `token` filter.

Recommended: **(b)** — it covers every token that can act in the tenant, which is what its
administrator answers for, and shows nothing of the member's other tenants; that revoking an
unrestricted token ends it everywhere is what an unrestricted token is, and the person makes a
new one in a session. (a) puts another tenant's metadata in front of this one's administrators;
(c) leaves out exactly the tokens that most need the view.

**Answer:** _open_
