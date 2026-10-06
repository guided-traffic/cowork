---
id: T39
title: tickets cannot be deleted, restored or purged
state: done
severity: medium
security: hardening
threat: a leaked administrator token with admin scope can still delete every ticket it sees; each stays restorable in the bin for thirty days, and the purge takes a browser session
urgency: later        # rule 4: decided fix
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done: 2026-10-06
shipped: the deletion, the bin, the restoration and the purge of tickets — explicit in a browser session, and by the job after thirty days — in the API and the browser, with the three decisions of the implementer accepted by the owner
---

## Current state

Built: [ADR 0024](../../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D1–D3 and D7 for tickets, as its Status records — migration 32 with the marker, the restrictive
policies of the purge and the owner's function that empties a purged ticket's audit rows; the
deletion filter beside the visibility predicate in every query, the search's and the person-level
lists' included, held by `TestEveryReadOfTicketsCarriesTheDeletionFilter`; `DELETE …/{number}`, the
bin `GET …/deleted-tickets` with its weak `ETag`, `PUT …/deleted-tickets/{key}/restore` and
`DELETE …/deleted-tickets/{key}` — the purge in a browser session only, Q1's answer
([`api/deletion.go`](../../../backend/internal/api/deletion.go)); the purge job `ticket-purge`
([`store/deletion.go`](../../../backend/internal/store/deletion.go)); in the browser the deletion on the
ticket's page ([`ticket-delete.ts`](../../../frontend/src/app/features/ticket/ticket-delete.ts)), the
deleted tickets page ([`deleted-tickets.ts`](../../../frontend/src/app/features/tenant/deleted-tickets.ts)),
and the inbox, its count and the open decisions read again on a deletion or a restoration in any of
the person's tenants (`changesExistence`); integration tests in
[`api_deletion_test.go`](../../../backend/test/integration/api_deletion_test.go) — among them
`TestPurgingTakesABrowserSession` and `TestADeletedTicketLeavesSearchAndThePersonLevelLists` — and
`TestTheToolsNeverDeleteAndMissADeletedTicket`.

In the browser, [`deletion.spec.ts`](../../../frontend/e2e/deletion.spec.ts) walks the deletion from
the ticket's page after the question, a member's *No such ticket* and backlog without it, the
restoration from the deleted tickets, a second deletion and the purge with its two questions — in
Chromium and WebKit, each in both schemes, in three local runs of the whole tier with two workers on
2026-10-06.

The three decisions of the implementer are accepted by the owner (2026-10-06):
[ADR 0026](../../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D3 as
amended 2026-10-05 — the purge's function runs inside the transaction that records the act instead
of writing the act itself —, and in
[ADR 0024](../../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2 a purged ticket's children become roots, and a block that waited on it waits on its key as an
external reference, an act on that ticket.

## Required changes

None.

## Open questions

### Q1: Should the explicit purge take a browser session only?

A purge is the one irreversible act on a ticket. As first built, `DELETE …/deleted-tickets/{key}`
took an administrator's token with `admin` scope like the deletion, and a leaked `admin` token could
delete and purge every ticket it saw, two requests each.

- **(a) Session only for the purge**: `purgeTicket` joins the session-only operations; scripts
  delete and restore with a token and leave the purge to the browser or the job.
- **(b) Leave it as built**: the rule stays one rule; the gap stays documented, mitigated by not
  handing scripts `admin` tokens.
- **(c) Session only for the deletion and the purge**: a leaked token cannot even hide a ticket;
  every deletion is a browser act.

Recommended: **(a)** — the purge is what cannot be undone, the job purges anyway after thirty days,
and no workflow of a script needs to purge early; the deletion stays reversible for thirty days and
needs no session.

**Answer:** (a) — the purge takes a browser session; deleting and restoring stay open to an
`admin`-scope token. Built on the recommendation, the owner reviewing the result (2026-10-05).
Recorded in ADR 0024 D7 and ADR 0035 D5 as amended 2026-10-05; what a leaked token can still do is
[tenancy.md](../../security/tenancy.md#h-54) H-54.
