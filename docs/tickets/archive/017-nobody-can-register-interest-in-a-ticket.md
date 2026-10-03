---
id: T17
title: nobody can register interest in a ticket, so the stakes that feed the priority are not recorded
state: done
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix (the ADRs)
effort: S
blocked-by: T12
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: stakes per person and ticket, settled after the work, the interest filter
---

## Current state

Decided by [ADR 0013](../adr/0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md)
D1–D5, [ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D1, [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D2, D4, [ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D1 and [ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md)
D1.

- No interest table, no route.
- ADR 0013 D1: one row per person and ticket — a weight `watch`, `need` or `urgent`, a note
  (expected for `need` and `urgent`), since when. A viewer registers `watch` only (ADR 0034 D1);
  an agent `watch`, and `need` or `urgent` with `interest` (ADR 0043 D2, D4). Rows outlive the
  work and show as settled (ADR 0013 D5).
- No record says whether an agent may remove its person's interest; the open variant applies.
- The score that reads interest ([ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md))
  and the watcher set notifications address (ADR 0013 D6) are phase 3's.

## Required changes

1. **Migration:** the weight enum; `ticket_interest` (person, weight, note, timestamps; one row
   per person and ticket; a composite key to the ticket); an index by person; the tenant policy;
   grants with `DELETE`.
2. **Routes:** `PUT …/tickets/{number}/interest` (`{weight, note?}`; the caller's own row; `201`
   new, `200` changed, nothing recorded when unchanged; the act `interest`); `DELETE` (`204`,
   idempotent; the act `interest` with nothing after); `GET` (person, weight, note, since, last
   change, `settled` when the ticket is terminal). A viewer `watch` only; an agent `watch` at
   baseline, `need` and `urgent` with `interest`, and removal allowed (the open variant).
3. **Filter:** `interest=me|any` on the ticket lists (ADR 0049 D1).
4. **Tests:** one row per person and ticket; a weight change keeps one row; a viewer's `watch`
   passes and `need` is refused; an agent without `interest` refused for `need`, with it allowed;
   rows survive `done` and come back settled; the filter; the restriction and confidential rows;
   the cross-tenant rows.
5. **Docs and records:** domain.md (interest; the watcher set is phase 3's); ADR 0013 Status and
   index row.

## Related

- T12 — tickets, the predicates and the list filters.
