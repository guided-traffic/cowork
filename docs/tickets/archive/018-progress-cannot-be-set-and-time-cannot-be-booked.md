---
id: T18
title: progress cannot be set or derived, and time cannot be booked, corrected, voided, locked, summed or exported
state: done
severity: high
security: none
threat:
urgency: later        # rule 4: decided fix (the ADRs)
effort: L
blocked-by: T14
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: derived progress up the ancestors, time entries with revisions, voiding, the lock and their visibility, the tenant list and the report as CSV
---

## Current state

Decided by [ADR 0017](../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D1–D10, [ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D1, D5, D8, [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D2, D3, [ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D1, [ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D2 and
[ADR 0055](../adr/0055-english-only-browser-locale-for-dates-and-numbers.md) D3.

- Nothing computes progress or books time. T12 creates the progress columns, T7 the tenant's
  `time_visible_to_members` and `time_locked_until`, T10 the route that moves the lock.
- ADR 0017's Status leaves D3–D9 "open to the owner's objection until the first implementation
  makes them concrete". D3's effort-weighted mean (XS 1, S 2, M 3, L 5) is rarely a multiple of
  five, which D2's constraint requires.
- ADR 0017 D6: "An agent never books time … invoicing is a person's statement" — by that reason
  every time-entry write, booking, correcting and voiding, is hard-off for agents.
- ADR 0034 D5 settles ADR 0017 D9: a person sees their own entries, an administrator all, a
  member all of the projects they may see while `time_visible_to_members` is on. ADR 0065 D1: a
  confidential ticket's entries follow the ticket's visibility.

## Required changes

1. **Progress** on `PATCH …/tickets/{number}` (`If-Match`): 0–100 in fives, in every
   non-terminal state (ADR 0017 D2); refused with `409` on a terminal ticket and on a ticket with
   children; agents set it (baseline, ADR 0043 D2).
2. **Derived progress** (ADR 0017 D3, made concrete and recorded there): the effort-weighted mean
   of the children, `dropped` children excluded, `done` children counted as 100, deleted children
   excluded, rounded to the nearest five with halves up, 0 when every child is dropped;
   maintained in the same `Mutate` as every change of its inputs, up the ancestors, without
   bumping their versions (T12's version scope); when the last child leaves, the parent's own
   value becomes editable and starts at the last derived value. The representation shows the
   effective value and whether it is derived.
3. **Migration:** `time_entries` (person and author, minutes 1–1440, the day worked, note, voided
   at and by, version, timestamps) and `time_entry_revisions` (the values before each edit,
   editor, time); composite keys; indexes by ticket, by day and by person; tenant policies;
   grants without `DELETE`.
4. **Routes:** `POST …/tickets/{number}/time-entries` (member, `write`; a viewer `403`; an
   `Idempotency-Key`; the act `booked`); `GET` (the visible entries and their sum) and one;
   `PATCH …/time-entries/{id}` (the author; `If-Match`; a revision; the act `edited`; a voided
   entry refused with `409`); `PUT …/time-entries/{id}/void` (the author; idempotent; the act
   `voided`); `GET …/time-entries/{id}/revisions`; `GET /api/v1/tenants/{slug}/time-entries`
   (table mode; period, project, ticket, person, `include_voided`; CSV) and `GET …/time-report`
   (minutes summed per ticket, project, person or tenant over a period; CSV; ADR 0017 D10). Every
   write reads `time_locked_until` `FOR SHARE` and refuses an old or a new day on or before it
   with `409` (ADR 0017 D8). Every write by an agent-marked request is refused
   (`403 agent_forbidden`, hard-off "booking time").
5. **Visibility** as ADR 0034 D5 and ADR 0065 D1 above, in the queries.
6. **CSV** through T10's writer: a header row, `YYYY-MM-DD` days, RFC 3339 timestamps, minutes as
   integers, formula cells neutralised.
7. **Tests:** the derivation table (weights, dropped, done, rounding, every child dropped); a
   parent refuses a manual value and shows the derived one; the last child leaving restores
   editing; `done` sets 100 in the transition's act; a derived change bumps no version; the time
   rules (positive minutes, revisions, voided entries out of every sum, no delete route, agents
   refused on every write, the lock checked on the old and the new day, a moved lock recorded);
   visibility with two identities and the setting on and off; sums per ticket, project, person
   and tenant; the CSV; the activity never shows time acts; the restriction and confidential
   rows; the cross-tenant rows.
8. **Docs and records:** domain.md (the progress derivation, the time rules); ADR 0017 (D3 made
   concrete; D9 settled by ADR 0034 D5) and ADR 0034 D5 Status and index rows.

## Related

- T10 — the time lock's route and the CSV writer.
- T12 — the progress columns and the version scope.
- T14 — `done` sets 100 and triggers the parents' derivation.
