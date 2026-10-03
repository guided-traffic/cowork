# ADR 0017: Effort Is a Size, Progress Is a Five-Step Percentage, and Time Is Booked by People — Correctable, Voidable, Lockable, Never Deleted

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the catalog question
"estimates and time tracking?": time bookings in the first release (over effort only, over
hour estimates with burndown, and over deferring bookings), and — added by the owner — a
progress percentage on the ticket, set with a slider in steps of five. The rules on the
parent's progress (D3), on agents (D4, D6), on voiding and locking (D7–D8) and on visibility
(D9) are this record's proposal for implementing the choice; they stay open to the owner's
objection until the first implementation makes them concrete.

Amended 2026-10-02 (D3 made concrete by the first implementation: the rounding, a parent whose
children are all dropped, a done ticket's own value; D9 settled by
[ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D5). An effort-weighted mean is rarely a multiple of five, which D2 requires of every progress.

**Built** (phase 2, 2026-10-02): D1–D10 — the progress columns and the derivation
(`ticket_derived_progress`, migration 8), `time_entries` with their revisions (migration 13),
booking, correcting, voiding, the lock, the visibility function `app_time_visible`, the
tenant's list and the report as JSON and CSV.

## Context

`effort` is already a column with the sizes `XS`, `S`, `M`, `L`
([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md)); it is a planning
signal and deliberately not a priority input ([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)).
The owner works for several clients, each a tenant ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)),
and wants the time spent to be bookable against tickets — the basis of invoicing — and the
state of work on a ticket to be visible as a number between "not started" and "finished",
finer than the state machine of [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)
gives. A parent ticket's progress was already promised to be derived from its children
([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D3).

## Decision

**D1 — `effort` stays a size and nothing else.** No hour estimate, no burndown, no velocity.

**D2 — Every ticket has a `progress` of 0 to 100 in steps of five.** Stored as an integer
with that constraint; edited with a slider that shows the value; default 0. It may be set in
every non-terminal state.

**D3 — A ticket with children shows the progress derived from them and does not take a
manual value.** The derivation is the mean of the children's progress weighted by their
effort (`XS` 1, `S` 2, `M` 3, `L` 5); a child in `dropped` is excluded; a child in `done`
counts as 100. When the last child is removed the parent's own value becomes editable again
and starts at the last derived value. *(Made concrete 2026-10-02: the mean is rounded to the
nearest multiple of five, halves up; a parent whose children are all dropped shows 0; a child
that has children counts with its own derived value; the derivation is maintained in the
transaction of every change to its inputs, up the ancestors, without changing their versions
([ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
D1); a `done` ticket shows 100 whatever its children say.)*

**D4 — Progress is a work statement, not a decision, so an agent may set it** on a ticket it
is working on. Every change is a recorded act; the activity list shows it as one line.

**D5 — Transitions and progress.** `done` sets `progress` to 100 as part of the act;
`dropped` leaves it; a reopen keeps it. No state is derived from the percentage: 100 does
not close a ticket.

**D6 — Time is booked by people, against a ticket, in minutes.** A time entry is (person,
ticket, minutes, the day worked, note). An agent never books time: a session's duration is
not a person's working time, and invoicing is a person's statement.

**D7 — An entry is corrected or voided, never deleted.** Its author may edit it; every edit
keeps the previous values; an entry that should not have existed is **voided** — kept,
excluded from every sum, shown struck through to those who may see it. Both are recorded
acts.

**D8 — A tenant administrator may close a period.** A tenant carries a `time_locked_until`
date; entries on or before it can neither be edited nor voided nor added. Moving the date is
a recorded act; moving it backwards is allowed and recorded, so a wrongly closed period can
be reopened, but never silently.

**D9 — Visibility of time follows roles, narrowly.** A person sees their own entries;
a tenant administrator sees every entry of the tenant. Whether a `member` may see others'
entries, and whether a client's people may see the booked time at all, is decided in the
roles record; the default until then is "own and administrators". *(Settled 2026-10-02 by
[ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D5: a member also sees everyone's entries of the projects they may see while the tenant's
`time_visible_to_members` is on; a confidential ticket's entries follow the ticket's
visibility ([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md) D1).)*

**D10 — Sums and export.** Minutes summed per ticket, per project, per tenant and per person
over a date range, in the UI and as CSV. No rounding, no rates, no currency: cowork records
time, it does not invoice.

## Consequences

- Two new columns on the ticket (`progress`, and effort already there) and one new table
  with an edit history and a void flag.
- The board card can show the percentage as a bar; the parent's bar is honest about size
  because of D3's weighting.
- D8 gives invoicing a stable base: once a month is closed its sums do not move.
- D9 leaves a decision for the roles record, and until then a client's `member` cannot see
  what was booked on their tenant. That is the safe default.
- The export of [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
  D4 gains `progress` in the frontmatter; time entries are not part of the ticket's Markdown
  (they are the person's, not the ticket's) and are exported through D10.

## Alternatives Considered

- **Effort only** — the recommendation, with bookings as a later record. Lost by the owner's
  decision; D6–D10 are the minimum that makes bookings usable for invoicing without a
  second round.
- **Hour estimates with burndown.** Sprint thinking the iterations question is about to
  refuse; estimate accuracy as a new topic. Lost.
- **Progress derived from the state alone** (`filed` 0, `in-progress` 50, `done` 100). Too
  coarse for the owner's purpose. Lost to D2.
- **An unweighted mean for the parent.** Three finished small children and one open large one
  would read 75 %. Lost to D3.
- **Deletable time entries.** Sums that change after an invoice. Lost to D7 and D8.

## Residual risks

- D3's effort weights are a first guess and the same numbers other records may want for
  other purposes; if they diverge, this record names its own.
- D8 locks by date, not by invoice; an installation that invoices per project will want a
  per-project lock. Amendment when asked for.
- D9's default may surprise a client who expects to see hours; the roles record settles it.

## References

- [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) — `effort`
- [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D3 — the parent's derived progress
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) — the transitions D5 hooks into
- [ADR 0004](0004-cowork-is-a-team-product.md) D3 — every change attributable
