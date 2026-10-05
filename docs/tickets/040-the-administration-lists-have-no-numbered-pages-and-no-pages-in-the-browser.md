---
id: T40
title: the members and tokens pages show no page numbers, and a tenant's administrators cannot see or revoke the members' tokens
state: in-progress
severity: low
security: none
threat:
urgency: later        # rule 4: decided and built; the owner reviews the result
effort: M
blocked-by: human
filed-from: T26
opened: 2026-10-03
decided: 2026-10-05
done:
---

## Current state

Built on this branch:

- **Numbered pages on the four tables** of the API (`listAudit`, `listMembers`, `listMyTokens`,
  `listProjects`) and the tenant's audit page at `/t/:tenant/audit`
  ([ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D2, D4;
  [ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D6).
- **The members page and the tokens page show numbered pages** — 25, 50 or 100 a page — through
  [`table-pages.ts`](../../frontend/src/app/core/table-pages.ts) `tablePages`; the pickers keep
  reading every member (`MembersService.members`). Unit tests: `members.spec.ts`,
  `members.service.spec.ts`, `tokens.spec.ts`, `tokens.service.spec.ts`.
- **The tokens that can act in the tenant**, Q1 answered (b) on the recommendation, the owner
  reviewing the result ([ADR 0035](../adr/0035-personal-access-tokens.md) D5 as amended
  2026-10-05): `GET` and `DELETE /api/v1/tenants/{tenant}/tokens` in
  [`members.yaml`](../../backend/api/members.yaml) and
  [`tenanttokens.go`](../../backend/internal/api/tenanttokens.go), numbered pages, the tokens policy
  of [migration 39](../../backend/internal/store/migrations/000039_tenant_tokens.up.sql), and the
  tenant's page *Tokens* ([`tenant-tokens.ts`](../../frontend/src/app/features/tenant/tenant-tokens.ts)),
  whose revocation of an unrestricted token asks first and says that it ends the token in every
  tenant of its person. Integration tests across two tenants:
  `TestTenantAdministratorsSeeAndRevokeTheTokensThatCanActInTheTenant` (exactly the members'
  unrestricted tokens and those restricted to the tenant; nothing of a token restricted to another
  tenant or of a non-member, not its name, not its id; the revocation a recorded act of the tenant,
  once, that ends an unrestricted token in the other tenant too) and the policy subtest of
  `TestPoliciesOfThePersonsAndTheirAccounts`; unit tests `tenant-tokens.spec.ts`,
  `tenant-tokens.service.spec.ts`, `shell.spec.ts`, `app.routes.spec.ts`.

The README reference, [api.md](../developer/api.md), [data-access.md](../developer/data-access.md),
[frontend.md](../developer/frontend.md), [package-map.md](../developer/package-map.md),
[tokens.md](../security/tokens.md) (H-51) and [tenancy.md](../security/tenancy.md) carry it.

## Required changes

1. The owner's review of the built result: the tenant's *Tokens* page, the confirmation of a
   revocation, and the numbered pages of the members and the tokens in a browser (`make dev`).

## Not verified

None of the three pages has been looked at in a browser — `make dev` was not run for this work —
and the audit page's CSV download has not been tried against the shell's content-security policy in
a real browser; the unit tier stubs `URL.createObjectURL`.
