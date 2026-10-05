---
id: T40
title: the members and tokens pages show no page numbers, and a tenant's administrators cannot see or revoke the members' tokens
state: analysed
severity: low
security: none
threat:
urgency: later        # rule 4: the numbered pages in the browser are decided; the members' tokens wait for Q1
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided:
done:
---

## Current state

Built on this branch, items 1 to 3 of the work list:

- **Numbered pages on the four tables.** The audit view (`listAudit`), the members
  (`listMembers`), the person's tokens (`listMyTokens`) and the projects (`listProjects`) take
  `page` and `per_page` beside the cursor and answer `total`, `page` and `per_page`, counted under
  the same filters and predicates as the page ([`cursor.go`](../../backend/internal/api/cursor.go)
  `tablePage`; `TestTheTablesTakeNumberedPages`;
  [ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D2).
- **The tenant's audit page** at `/t/:tenant/audit`, linked for its administrators after *Group
  mappings*: the filters actor, token, action, entity and period, numbered pages of 25, 50 or 100,
  and *Download CSV* — numbered CSV pages of a hundred with the period ended at the server's clock,
  at most the newest 10 000 rows ([`audit.ts`](../../frontend/src/app/features/tenant/audit.ts),
  [`audit.service.ts`](../../frontend/src/app/core/audit.service.ts);
  [ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D6).
- **Unit tests** for the page, its helpers and the service (`audit.spec.ts`,
  `audit.service.spec.ts`), and the navigation's link (`shell.spec.ts`).

The README reference, [api.md](../developer/api.md), [frontend.md](../developer/frontend.md),
[tokens.md](../security/tokens.md) and the Status of ADR 0026 and ADR 0048 carry it.

Left:

- No route lists a tenant's tokens: the administrator's view and revocation of the members' tokens
  ([ADR 0035](../adr/0035-personal-access-tokens.md) D5) is built neither in the backend — there
  is no `/api/v1/tenants/{tenant}/tokens` — nor in the browser ([tokens.md](../security/tokens.md)
  says so); it waits for Q1.
- The members page and the tokens page of the browser list every row at once
  ([`members.service.ts`](../../frontend/src/app/core/members.service.ts),
  [`tokens.service.ts`](../../frontend/src/app/core/tokens.service.ts)); ADR 0048 D4 has a table show
  page numbers with a choice of the page's size, which the API now serves for both.

## Required changes

### Independent of the open question

1. The members page and the tokens page show numbered pages — 25, 50 or 100 a page — as the audit
   page does (ADR 0048 D4), while the pickers that read every member keep doing so; unit tests per
   page.

### Depends on the answer

2. The administrator's view of the members' tokens — metadata, never plaintext — and their
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

## Not verified

The audit page has not been looked at in a browser — `make dev` was not run for this work — and
the CSV download's blob link has not been tried against the shell's content-security policy in a
real browser; the unit tier stubs `URL.createObjectURL`.
