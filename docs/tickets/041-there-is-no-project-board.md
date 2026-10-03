---
id: T41
title: the project board has no end-to-end path, and what its cards count as prerequisites is open
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The board is built at `/t/{slug}/p/{KEY}/board`
([`board.ts`](../../frontend/src/app/features/project/board.ts),
[`board-card.ts`](../../frontend/src/app/features/project/board-card.ts)) as
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1 amended on 2026-10-03 has it: the
column `next` with its Now button, the open leaves of urgency `now` and `release` in Refinement,
Ready, In Progress, Blocked and Review with their WIP counts, the count of the tickets done in the
last fourteen days; cards with the size, the bar of the current stage, the block and the count of
the open tickets that block a card directly; a drag between the columns is a transition, offered
only where the matrix allows it, with the dialogs of T30; a card being dragged is not moved by an
event. Missing: the e2e path, and the answer to Q1.

## Required changes

1. The e2e path of T29 drags a card in both schemes
   ([ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md) D3).
2. Depends on Q1: the answer recorded in
   [ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) — D6's count on the card
   and its Consequences amended in place for A; for B, `open_prerequisites` made transitive in
   [`queries/read/tickets.sql`](../../backend/internal/store/queries/read/tickets.sql),
   [`queries/write/tickets.sql`](../../backend/internal/store/queries/write/tickets.sql)
   (`GetWrittenTicket`) and [`store/tickets.go`](../../backend/internal/store/tickets.go), with its
   integration test.

## Open questions

### Q1: Does a card count the open tickets that block it directly, or its open prerequisites transitively?

[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1 puts the count of open prerequisites
on every card and cites [ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D6,
which defines a ticket's prerequisites as the transitive closure of the `blocks` edges into it
and caches the count on the ticket. The API built `open_prerequisites` as the open tickets that
block the ticket directly and that the caller can see, computed per read — the set D7 refuses
done over.

- **A — direct, per read, what the caller can see (built; recommended).** One subquery per row
  over the index `ticket_links_by_target`; the card's number is exactly what would refuse
  closing the ticket, and the transitive picture is the detail page's prerequisite tree (D2).
  Cost: in a chain A blocks B blocks C, C shows 1 while A is open behind B.
- **B — the transitive closure, per read, over what the caller can see.** The number of
  everything still to do, at the price of a recursive walk per card on every board and list
  read. A walk that passes through a ticket the caller cannot see counts what lies behind it
  and so tells that a hidden link exists; a walk that stops there undercounts.
- **C — D6 as written, cached on the ticket.** Cheap to read, but one number for every caller,
  counting prerequisites some of them cannot see
  ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
  D4, D5). Not viable as written.

A is recommended: it is the only option that stays inside the visibility predicate at the cost
of one indexed lookup, and it names the same tickets the done refusal names.

**Answer:** _open_
