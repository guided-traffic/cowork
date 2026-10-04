---
id: T32
title: the tenant has no attachment quota
state: in-progress
severity: medium
security: none
threat:
urgency: icebox       # rule 5: the quota needs the owner's call on Q1
effort: M
blocked-by: decision
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The browser uploads to a comment of the person's own, shows a raster attachment as a preview —
each load a recorded download — and corrects a time entry over its version, with its earlier
values on request ([`frontend.md`](../developer/frontend.md#the-detail-page)). Missing:

- **The per-tenant attachment quota** of ADR 0016 D6, carried over from phase 2: limits per file
  (`COWORK_ATTACHMENT_MAX_BYTES`) and per ticket (`COWORK_ATTACHMENT_MAX_PER_TICKET`) exist,
  nothing counts a tenant's bytes. Whether the quota refuses an upload is open (Q1).

## Required changes

1. The per-tenant quota: its setting, the usage in the tenant's administration and, by Q1's
   answer, the refusal before bytes are stored with its problem code; integration tests across
   two tenants; the README reference for the quota's variable and code; ADR 0016 amended where
   the answer departs from it.

## Open questions

### Q1: Does the tenant's attachment quota refuse an upload, or is it only reported?

ADR 0016 D6 lists "a per-tenant quota reported in the tenant's administration" among the limits
and says "uploads beyond a limit are refused before bytes are stored"; its Residual risks say the
quota "is reported, not enforced against a hard storage limit; a runaway upload loop is bounded by
the per-file and per-ticket limits only". The two readings disagree, and nothing of the quota is
built.

- **(a) Enforce:** a quota per tenant (a variable, `0` for none, as
  [ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
  D2 has every limit); the tenant's stored bytes summed and checked under a per-tenant lock
  before the bytes are stored, as the per-ticket count is checked under its ticket's lock; a
  refusal with a problem code of its own; the usage in the administration. It costs a lock that
  orders one tenant's uploads, a sum per upload, a code and a variable; the residual risk is
  amended.
- **(b) Report only:** the usage in the administration, no refusal; D6's refusal is amended to
  the per-file and per-ticket limits. One tenant — or an agent with `upload` in a loop, which
  files tickets without bound and attaches up to the per-ticket count to each — can fill the
  storage every tenant shares, and the operator's bucket quota, where there is one, stops every
  tenant at once.

Recommended: **(a)** — the tenant is the isolation unit
([ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)), and only a
refusal per tenant keeps one tenant from using up what all of them share; the per-ticket count
does not bound a loop that files new tickets, which is the gap the record's own residual risk
names; and D6 already counts the quota among the limits that refuse, with the per-ticket count
under its lock as the mechanism to copy.

**Answer:** _open_
