---
id: T35
title: the person-level pages and the start page have no end-to-end path
state: done
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix
effort: S
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-05
done: 2026-10-06
shipped: frontend/e2e/start.spec.ts, a person of one tenant landing on next for me at the start page and reaching the tenant by its name in the top bar, in Chromium and WebKit and both colour schemes
---

## Current state

The four person-level pages exist, each a union of per-tenant reads with the tenant beside each key
([api.md](../developer/api.md#the-person-level-routes),
[frontend.md](../developer/frontend.md#the-person-level-pages)): "next for me" — the person's open
tickets and the unassigned open tickets of the projects they see, the answer to Q2 that
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D3 as amended records — at `/me/next`
and as the start page `/` ([ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D4 as amended),
"assigned to me", "open decisions" and the inbox. The lists of tickets follow the score with the
place in the project's rank beside each ticket
([ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D5), and `session_start`
offers the top of "next for me" in the bound project
([ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D1). The pages follow the
changes of every tenant of the person over the one person-level stream, which carries every event
of the person's tenants that each tenant's filter admits — the answer to Q1
([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D1, D3, D5; `reloadOn` in [`person-list.ts`](../../frontend/src/app/features/me/person-list.ts)) —,
and a reload that finds a page unchanged is a `304` (D7). `TestNextForMeAcrossTenants`,
`TestAssignedToMeAcrossTenants` and `TestOpenDecisionsAcrossTenants` cover two tenants, a restricted
project and confidential tickets, `TestThePersonLevelStream` the stream across the tenants, and
`TestThePolledListsAnswerNotModified` the `304`s.

"Assigned to me" is walked with two identities by
[`assigned.spec.ts`](../../frontend/e2e/assigned.spec.ts), and the start page by
[`start.spec.ts`](../../frontend/e2e/start.spec.ts): a member of one tenant lands at `/` on "next for
me", which shows the ticket assigned to them beside its tenant, and reaches the tenant's front page by
its name in the top bar, which offers no switcher — in Chromium and WebKit, each in both schemes, in
three local runs of the whole tier with two workers on 2026-10-06. The description lives in
[testing.md](../developer/testing.md#end-to-end-tests).

## Required changes

None.

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
  merged. One connection whatever the number of tenants, and every person-level list within the
  stream's latency.
- **(c) Reload on `inbox.changed`, and poll.** The pages reload on the person-level stream's
  `inbox.changed` and every fifteen seconds with `If-None-Match` (ADR 0054 D7); the stream carries
  no more than the person's own events. An assignment, a state change and a comment on a ticket the
  person watches arrive within the stream's latency; everything else, "next for me" above all,
  within fifteen seconds.

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

The records gave the list its scope, the person's tenants
([ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3), and its
order, the score (ADR 0014 D5), but not its members; `session_start` offers its top to an agent as
the work to take up.

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

**Answer:** (b) — the owner, 2026-10-05, as recommended. Recorded in ADR 0018 (D3 amended) and
built: `GET /api/v1/me/next`, the page `/me/next` and the start page.

## Related

- T36 — the inbox and the person-level stream these pages reload on
- T29 — the end-to-end tier the paths of change 1 belong to
