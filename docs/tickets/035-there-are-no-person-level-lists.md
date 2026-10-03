---
id: T35
title: there are no person-level lists — next for me, assigned to me and open decisions across the person's tenants
state: decided
severity: high
security: none
threat:
urgency: next         # rule 3: severity high, live — the phase's verification goes through "assigned to me"
effort: M
blocked-by: T34
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

No route under `/api/v1/me` lists tickets; [ADR 0023](../adr/0023-the-tenant-is-in-the-path.md)
D2 puts `next`, `assigned` and `decisions` there, each one iteration per tenant with the tenant
named on every item (ADR 0021 D5). The shell of T28 has no person-level pages; `/` lists the
tenants or goes to the only one.

## Required changes

1. `GET /api/v1/me/next`, `/assigned`, `/decisions` with `?tenant=` narrowing, ordered by score
   ([ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D5); `decisions` holds
   the open questions asked of the person and those open in their tenants
   ([ADR 0018](../adr/0018-the-views-of-the-first-release.md) D3).
2. Pages `/me/next`, `/me/assigned`, `/me/decisions` ([ADR 0023](../adr/0023-the-tenant-is-in-the-path.md)
   D4) with the tenant beside each key, in the navigation for every person; `/` becomes "next for
   me".
3. They follow the events of every tenant the person belongs to (with T36's person-level stream).
4. Integration tests across two tenants and a restricted project; the e2e path's "assigned to me".
