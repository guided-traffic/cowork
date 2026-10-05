---
id: T39
title: tickets cannot be deleted, restored or purged
state: in-progress
severity: medium
security: hardening
threat: closing Q1 by a session-only purge would additionally cover a leaked administrator token with admin scope deleting and purging every ticket of the tenant it sees, irreversibly, without the browser's second question
urgency: later        # rule 4: decided fix; Q1 is a hardening question
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

Built: [ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D1–D3 and D7 for tickets, as its Status records — migration 35 with the marker, the restrictive
policies of the purge and the owner's function that empties a purged ticket's audit rows; the
deletion filter beside the visibility predicate in every query, held by
`TestEveryReadOfTicketsCarriesTheDeletionFilter`; `DELETE …/{number}`, the bin
`GET …/deleted-tickets`, `PUT …/deleted-tickets/{key}/restore` and `DELETE …/deleted-tickets/{key}`
([`api/deletion.go`](../../backend/internal/api/deletion.go)); the purge job `ticket-purge`
([`store/deletion.go`](../../backend/internal/store/deletion.go)); in the browser the deletion on the
ticket's page ([`ticket-delete.ts`](../../frontend/src/app/features/ticket/ticket-delete.ts)) and the
deleted tickets page ([`deleted-tickets.ts`](../../frontend/src/app/features/tenant/deleted-tickets.ts));
integration tests in [`api_deletion_test.go`](../../backend/test/integration/api_deletion_test.go)
and `TestTheToolsNeverDeleteAndMissADeletedTicket`.

Outstanding:

- **No end-to-end path** walks the deletion, the bin, the restoration and the purge: the images
  could not be built where the feature was made, so a spec in `frontend/e2e/` would have run
  nowhere.
- **Three decisions of the implementer** wait for the owner's objection or acceptance: ADR 0026
  D3 as amended 2026-10-05 (the purge's function runs inside the transaction that records the act
  instead of writing the act itself), and two consequences ADR 0024's Status names — a purged
  ticket's children become roots, and a block that waited on it waits on its key as an external
  reference, an act on that ticket.
- **Q1** below.

## Required changes

1. An end-to-end spec in `frontend/e2e/`: an administrator deletes a ticket from its page after the
   question, a member gets *No such ticket* at its address and does not find it in the backlog, the
   administrator restores it from the deleted tickets, deletes it again and purges it with the two
   questions; `make docker-build e2e`.
2. The owner's word on the three decisions; an objection amends ADR 0026 or ADR 0024 in place and
   changes `store/deletion.go` and migration 35's function in a new migration.
3. Q1's answer, as an amendment of ADR 0035 D5 and of [tokens.md](../security/tokens.md#what-only-a-session-does)
   when the purge becomes session-only.

## Open questions

### Q1: Should the explicit purge take a browser session only?

A purge is the one irreversible act on a ticket. Today `DELETE …/deleted-tickets/{key}` takes an
administrator's token with `admin` scope like the deletion, by the rule that only an act that gives
access or outlives a token's revocation takes a session; the browser asks twice, the API not at all.
A leaked `admin` token can delete and purge every ticket it sees, two requests each
([tenancy.md](../security/tenancy.md#h-51) H-51).

- **(a) Session only for the purge**: `purgeTicket` joins the session-only operations; scripts
  delete and restore with a token and leave the purge to the browser or the job. The session rule
  gains a second reason — irreversibility — beside access, which ADR 0035 D5 has to say.
- **(b) Leave it as built**: the rule stays one rule; H-51 stays a documented gap, mitigated by not
  handing scripts `admin` tokens.
- **(c) Session only for the deletion and the purge**: a leaked token cannot even hide a ticket;
  every deletion is a browser act, which is how D7's confirmation is meant anyway.

Recommended: **(a)** — the purge is what cannot be undone, the job purges anyway after thirty days,
and no workflow of a script needs to purge early; the deletion stays reversible for thirty days and
needs no session.

**Answer:** _open_
