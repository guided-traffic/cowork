---
id: T35
title: there are no person-level lists — next for me, assigned to me and open decisions across the person's tenants
state: analysed
severity: high
security: none
threat:
urgency: next         # rule 3: severity high, live — the phase's verification goes through "assigned to me"
effort: M
blocked-by: T34
filed-from: T26
opened: 2026-10-03
decided:
done:
---

## Current state

No route under `/api/v1/me` lists tickets; [ADR 0023](../adr/0023-the-tenant-is-in-the-path.md)
D2 puts `next`, `assigned` and `decisions` there, each one iteration per tenant with the tenant
named on every item (ADR 0021 D5). The shell of T28 has no person-level pages; `/` lists the
tenants or goes to the only one.

No record says which tickets "next for me" lists: ADR 0005 D3 gives it its scope, the person's
tenants, and [ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D5 its order,
the score, and `session_start` reads it for the top candidates when the person has no ticket in
progress ([ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D1). And the event
stream follows one tenant: the person-level `?me=true` of
[ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D1 adds only the person's own events — the inbox and the questions asked of them.

## Required changes

### Independent of the open questions

1. `GET /api/v1/me/assigned` and `/decisions` with `?tenant=` narrowing, ordered by score
   (ADR 0014 D5); `decisions` holds the open questions asked of the person and those open in
   their tenants ([ADR 0018](../adr/0018-the-views-of-the-first-release.md) D3).
2. Pages `/me/next`, `/me/assigned`, `/me/decisions` ([ADR 0023](../adr/0023-the-tenant-is-in-the-path.md)
   D4) with the tenant beside each key, in the navigation for every person; `/` becomes "next for
   me".
3. Integration tests across two tenants and a restricted project; the e2e path's "assigned to me".

### Depends on the answers

4. `GET /api/v1/me/next` with what Q2 decides it holds, ordered and narrowed as the others.
5. The three pages follow the changes of every tenant the person belongs to as Q1 decides, with
   T36's person-level stream.

## Open questions

### Q1: How do the person-level pages follow every tenant's changes?

Change 5 wants the three lists to change without a reload, as a tenant's pages do. ADR 0054 D1
gives a stream per tenant, and its `?me=true` adds only the person's own events; a person holds
at most `COWORK_SSE_MAX_STREAMS_PER_PERSON` streams per replica, ten by default, and one more
closes the oldest with `event: unavailable` ([README](../../README.md#configuration)).

- **(a) One stream per tenant on the person-level pages.** No backend change. A person in more
  than ten tenants — fewer, with tenant pages open in other tabs — closes their own streams,
  which fall back to polling, and over HTTP/1.1 the browser's six connections per origin go to
  streams before the page's own requests (ADR 0054 Consequences).
- **(b) The person-level stream carries every event of the person's tenants** that each tenant's
  filter admits (ADR 0054 D3, a token's restriction included); D1 and D3 are amended — the
  stream spans the person's tenants. One connection whatever the number of tenants, and all
  three lists within the stream's latency; every payload already holds what the filter reads —
  tenant, project, confidential flag, assignee and reporter
  ([`notify.go`](../../backend/internal/store/notify.go#L22-L33)). It costs the hub's
  subscription across tenants, a replay that merges the tenants' buffers or answers `resync`
  (D5), the heartbeat's check of every membership, and integration tests that an event of a
  tenant the person left or of a project hidden from them never arrives.
- **(c) Reload on `inbox.changed`, and poll.** The pages reload on the person-level stream's
  `inbox.changed` and every fifteen seconds with `If-None-Match` (ADR 0054 D7); the stream
  carries no more than T36 builds. An assignment, a state change and a comment on a ticket the
  person watches — the assignee among the watchers
  ([ADR 0013](../adr/0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D6,
  [ADR 0020](../adr/0020-notifications-are-an-in-app-inbox-per-person.md) D2) — arrive within the
  stream's latency; everything else, "next for me" above all, within fifteen seconds.

Recommended: **(b)** — the only option that keeps the owner's sub-second requirement on these
pages whatever the number of tenants; it widens what the stream D1 already addresses to the
person across their tenants instead of adding a second mechanism, and the filter needs no new
input. (a) breaks at the default limit for the person with many tenants, the one cowork is for;
(c) is the fifteen-second latency the owner turned down when ADR 0054 chose streams over
polling.

**Answer:** _open_

### Q2: Which tickets does "next for me" list?

The records give the list its scope and its order but not its members (Current state); change 4
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
