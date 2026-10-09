---
id: T40
title: the members and tokens pages show no page numbers, and a tenant's administrators cannot see or revoke the members' tokens
state: done
severity: low
security: none
threat:
urgency: later        # rule 4: decided and built; the owner reviews the result
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-05
done: 2026-10-09
shipped: built and released in phase 3, before 0.8.0
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
  2026-10-05): `GET` (numbered pages, a weak `ETag` and `304`) and `DELETE /api/v1/tenants/{tenant}/tokens` in
  [`members.yaml`](../../backend/api/members.yaml) and
  [`tenanttokens.go`](../../backend/internal/api/tenanttokens.go), numbered pages, the tokens policy
  of [migration 35](../../backend/internal/store/migrations/000035_tenant_tokens.up.sql), and the
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
[tokens.md](../security/tokens.md) (H-57) and [tenancy.md](../security/tenancy.md) carry it.

## Required changes

None. The owner reviews it in use ("Lass mich doch erstmal anfangen das Tool zu verwenden", 2026-10-09): T55 holds the review of the built pages as one item, and what he wants changed becomes a ticket of its own.

## Not verified

None of the three pages has been looked at in a browser — `make dev` was not run for this work —
and the audit page's CSV download has not been tried against the shell's content-security policy in
a real browser; the unit tier stubs `URL.createObjectURL`.
