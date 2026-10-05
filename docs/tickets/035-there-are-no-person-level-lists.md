---
id: T35
title: there is no "next for me", and the lists wait for the score's order
state: analysed
severity: high
security: none
threat:
urgency: next         # rule 3: severity high, live — "next for me" is the person's starting point and session_start reads it
effort: M
blocked-by: decision
filed-from: T26
opened: 2026-10-03
decided:
done:
---

## Current state

`GET /api/v1/me/assigned` and `GET /api/v1/me/decisions` exist with `?tenant=` narrowing, each a
union of per-tenant reads ([api.md](../developer/api.md#the-person-level-routes)), and so do the
pages `/me/assigned` and `/me/decisions` with the tenant beside each key, in the navigation for every
person ([frontend.md](../developer/frontend.md#the-person-level-pages)). Their order is the interim
one written in [ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md)'s Status — the
tenant's slug, the project's key, the project's rank — because the score of D5 is T34's. The pages
follow the changes of every tenant of the person over the one person-level stream, which carries
every event of the person's tenants that each tenant's filter admits
([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D1, D3, D5; `reloadOn` in [`person-list.ts`](../../frontend/src/app/features/me/person-list.ts)).

What is missing:

- **"Next for me."** No record says which tickets it lists: ADR 0005 D3 gives it its scope, the
  person's tenants, and ADR 0014 D5 its order, the score, and `session_start` reads it for the top
  candidates when the person has no ticket in progress
  ([ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D1). No route
  `GET /api/v1/me/next`, no page `/me/next`, and `/` still lists the tenants or goes to the only one
  ([ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D2, D4).
- **The score's order.** When T34 builds the score, both lists, and "next for me", take its order
  (ADR 0014 D5) and their cursors `(score, id)`
  ([ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D1).

## Required changes

### Independent of the open questions

1. When the score exists: `ListMyAssigned` and `ListMyDecisions` in
   [`mylists.go`](../../backend/internal/api/mylists.go) order by it, with the project's rank as the
   secondary indicator of ADR 0014 D5; the interim order leaves ADR 0014's and ADR 0018's Status;
   `TestAssignedToMeAcrossTenants` and `TestOpenDecisionsAcrossTenants` assert the score's order.
2. The e2e path's "assigned to me" (T29).

### Depends on the answers

3. `GET /api/v1/me/next` with what Q2 decides it holds, ordered and narrowed as the others, its page
   `/me/next` in the navigation — following every tenant through `reloadOn`, as the other
   person-level pages do —, and `/` as "next for me" (ADR 0023 D4).

## Open questions

### Q1: How do the person-level pages follow every tenant's changes?

The person-level pages were to change without a reload, as a tenant's pages do, when ADR 0054 D1
gave a stream per tenant and its `?me=true` added only the person's own events; a person holds at
most `COWORK_SSE_MAX_STREAMS_PER_PERSON` streams per replica, ten by default, and one more closes
the oldest with `event: unavailable` ([README](../../README.md#configuration)).

- **(a) One stream per tenant on the person-level pages.** No backend change. A person in more
  than ten tenants — fewer, with tenant pages open in other tabs — closes their own streams,
  which fall back to polling, and over HTTP/1.1 the browser's six connections per origin go to
  streams before the page's own requests (ADR 0054 Consequences).
- **(b) The person-level stream carries every event of the person's tenants** (built) that each
  tenant's filter admits (ADR 0054 D3, a token's restriction included); D1, D3 and D5 are amended —
  the stream spans the person's tenants, with a filter per tenant, and a reconnect replays them
  merged. One connection whatever the number of tenants, and all three lists within the stream's
  latency.
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

**Answer:** (b) — the owner, 2026-10-05: the person-level stream carries every event of every
tenant the person belongs to, as far as that tenant's filter admits it, as built. Recorded in ADR 0054
(D1, D3 and D5 amended).

### Q2: Which tickets does "next for me" list?

The records give the list its scope and its order but not its members (Current state); change 3
cannot be built without them, and `session_start` offers its top to an agent as the work to take
up.

- **(a) The person's open tickets:** the same set as "assigned to me", in the same order — the
  two lists would be one.
- **(b) The person's open tickets and the unassigned open tickets of the projects they see:**
  what the person, or their agent, could take up next; another person's ticket is not "for me".
- **(c) Every open ticket the person can see:** the whole open backlog of their tenants,
  colleagues' assigned work included.

Recommended: **(b)** — it is the set a person picks from, and the one `session_start` needs
when it offers an agent the top candidates: (c) would offer an agent a ticket a colleague holds,
and (a) adds nothing to "assigned to me". The list is cursor-paged
([ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D3), so a long
unassigned backlog costs only its top page.

**Answer:** _open_

## Related

- T34 — the score that orders the person-level lists
- T36 — the inbox and the person-level stream these pages reload on
- T29 — the end-to-end tier the e2e path of change 2 belongs to
