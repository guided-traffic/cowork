---
id: T35
title: the person-level pages do not follow the changes of every tenant of the person
state: analysed
severity: medium
security: none
threat:
urgency: next         # rule 3: severity medium, live — a change in another tenant shows on the start page only at the next reload
effort: M
blocked-by: decision
filed-from: T26
opened: 2026-10-03
decided:
done:
---

## Current state

The four person-level pages exist, each a union of per-tenant reads with the tenant beside each key
([api.md](../developer/api.md#the-person-level-routes),
[frontend.md](../developer/frontend.md#the-person-level-pages)): "next for me" — the person's open
tickets and the unassigned open tickets of the projects they see, as
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D3 as amended records the owner's answer —
at `/me/next` and as the start page `/`
([ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D4 as amended), "assigned to me", "open
decisions" and the inbox. The lists of tickets follow the score with the place in the project's
rank beside each ticket ([ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D5),
and `session_start` offers the top of "next for me" in the bound project
([ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D1).
`TestNextForMeAcrossTenants`, `TestAssignedToMeAcrossTenants` and `TestOpenDecisionsAcrossTenants`
cover two tenants, a restricted project and confidential tickets.

What is missing:

- **The pages follow one tenant.** The browser's one stream is the person-level stream of
  [ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
  D1 on one tenant: it carries the person's own events across their tenants — the inbox count, the
  questions asked of them — and the events of that tenant. A change in another tenant that tells the
  person nothing — a ticket filed unassigned there, which "next for me" lists, a ticket assigned to
  them there and unassigned by somebody else, a question open in that tenant answered — shows on
  the person-level pages only at the next reload.
- **The end-to-end path** of "assigned to me" with two identities (T29), and one of the start page.

## Required changes

### Independent of the open question

1. The e2e path's "assigned to me" (T29), and a path through the start page: a person with one
   tenant lands on "next for me" and reaches the tenant by its name in the top bar.

### Depends on the answer

2. The four pages follow the changes of every tenant the person belongs to as Q1 decides.

## Open questions

### Q1: How do the person-level pages follow every tenant's changes?

Change 2 wants the lists to change without a reload, as a tenant's pages do. ADR 0054 D1 gives a
stream per tenant, and its `?me=true` adds only the person's own events; a person holds at most
`COWORK_SSE_MAX_STREAMS_PER_PERSON` streams per replica, ten by default, and one more closes the
oldest with `event: unavailable` ([README](../../README.md#configuration)).

- **(a) One stream per tenant on the person-level pages.** No backend change. A person in more
  than ten tenants — fewer, with tenant pages open in other tabs — closes their own streams,
  which fall back to polling, and over HTTP/1.1 the browser's six connections per origin go to
  streams before the page's own requests (ADR 0054 Consequences).
- **(b) The person-level stream carries every event of the person's tenants** that each tenant's
  filter admits (ADR 0054 D3, a token's restriction included); D1 and D3 are amended — the
  stream spans the person's tenants. One connection whatever the number of tenants, and all
  four lists within the stream's latency; every payload already holds what the filter reads —
  tenant, project, confidential flag, assignee and reporter
  ([`notify.go`](../../backend/internal/store/notify.go)). It costs the hub's subscription across
  tenants — the person-level stream already receives the questions asked of its person from every
  tenant and judges them in their tenant (`writeStreamed` in
  [`events.go`](../../backend/internal/api/events.go)), and would judge every event so or keep a
  filter per tenant refreshed at the heartbeat —, a replay that merges the tenants' buffers or
  answers `resync` (D5), and integration tests that an event of a tenant the person left or of a
  project hidden from them never arrives, which `TestThePersonLevelStream` already has the shape of.
- **(c) Reload on `inbox.changed`, and poll.** The pages reload on the person-level stream's
  `inbox.changed` — which they do today — and every fifteen seconds with `If-None-Match` (ADR 0054
  D7); the stream carries no more than it does. An assignment, a state change and a comment on a
  ticket the person watches arrive within the stream's latency; everything else, "next for me"
  above all, within fifteen seconds.

Recommended: **(b)** — the only option that keeps the owner's sub-second requirement on these
pages whatever the number of tenants; it widens what the stream D1 already addresses to the
person across their tenants instead of adding a second mechanism, and the filter needs no new
input. (a) breaks at the default limit for the person with many tenants, the one cowork is for;
(c) is the fifteen-second latency the owner turned down when ADR 0054 chose streams over
polling.

**Answer:** _open_

## Related

- T36 — the inbox and the person-level stream these pages reload on
- T29 — the end-to-end tier the paths of change 1 belong to
