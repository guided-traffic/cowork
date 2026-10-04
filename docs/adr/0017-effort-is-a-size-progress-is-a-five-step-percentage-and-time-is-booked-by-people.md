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

Amended 2026-10-03 (D2: three progress stages; D3: each derived from the children's same
stage; D4, D5: the stages close a ticket). The owner's answer, with the board of
[ADR 0018](0018-the-views-of-the-first-release.md) D1: a bar per stage of work — refinement,
implementation, review — over a bar per board column and over a bar per state, and a ticket
done when all three are full, by the done act of
[ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D5.

Amended 2026-10-04 by the owner's decision that every act made through a token is shown as such
([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6): D6, an entry and each correction of it record the token they came through. A plain
token's person books time, no agent does, and the ticket's activity leaves time out
([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D6), so the entry is
where a booking through a token shows. Built the same day
([migration 27](../../backend/internal/store/migrations/000027_acts_through_a_token.up.sql)).

**Built** (phase 2, 2026-10-02): D1–D10 — the progress columns and the derivation
(`ticket_derived_progress`, migration 8), `time_entries` with their revisions (migration 13),
booking, correcting, voiding, the lock, the visibility function `app_time_visible`, the
tenant's list and the report as JSON and CSV. **Built in the API** (2026-10-03): the amendment
of D2–D5 — `progress_refinement` and `progress_review` beside `progress` and their backfill
([migration 19](../../backend/internal/store/migrations/000019_progress_stages.up.sql)), each
stage derived from the children's same stage (`ticket_derived_stage`), settable in every state
but `dropped`, the done act and the reopen of
[ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D5, and the three
stages in the export. The bars on the board's cards and the detail's three sliders are the
views' ([ADR 0018](0018-the-views-of-the-first-release.md)). *(2026-10-04.)* In the browser the
author corrects an entry over its version and reads its earlier values (D7).

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

*(Amended 2026-10-03.)* **The progress has three stages, one per kind of work**, each 0 to 100
in steps of five with its own slider: **refinement**, the work of `filed` and `analysed` until
`decided`; **implementation**, the work of `in-progress` and the `progress` of the first
release, which keeps that name; and **review**, the work of `review`. `decided` and `blocked`
are waiting, not working, and have no stage of their own. A stage may be set in every state
but `dropped`. A ticket that existed before the stages keeps its `progress` as
implementation, has refinement full from `decided` on, and review full when it is `done`.

**D3 — A ticket with children shows the progress derived from them and does not take a
manual value.** The derivation is the mean of the children's progress weighted by their
effort (`XS` 1, `S` 2, `M` 3, `L` 5); a child in `dropped` is excluded; a child in `done`
counts as 100. When the last child is removed the parent's own value becomes editable again
and starts at the last derived value. *(Made concrete 2026-10-02: the mean is rounded to the
nearest multiple of five, halves up; a parent whose children are all dropped shows 0; a child
that has children counts with its own derived value; the derivation is maintained in the
transaction of every change to its inputs, up the ancestors, without changing their versions
([ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
D1); ~~a `done` ticket shows 100 whatever its children say~~ *(superseded 2026-10-03 by D5:
done leaves the stages as they are)*.)* *(Amended 2026-10-03: each stage
is derived from the same stage of the children, by this rule; a done child counts as 100 in
each. A parent whose three stages are full is not done by them — it is done by hand, ADR 0009
D5 — and a parent done by hand shows its stages as its children make them.)*

**D4 — Progress is a work statement, not a decision, so an agent may set it** on a ticket it
is working on. Every change is a recorded act; the activity list shows it as one line.
*(Amended 2026-10-03: except two writes that are decisions. The write that fills the last
stage is the done act of ADR 0009 D5 — the verification note, the refusal over open
prerequisites, and for an agent the `close` capability and a state of `in-progress` or
`review`. The write that lowers a stage of a ticket done by its stages reopens it, with a
reason.)*

**D5 — Transitions and progress.** ~~`done` sets `progress` to 100 as part of the act;
`dropped` leaves it; a reopen keeps it. No state is derived from the percentage: 100 does
not close a ticket.~~ *(Amended 2026-10-03:)* The stages close a ticket: the write that fills
the last of them is the done act, and the write that lowers one of a ticket done by them
reopens it, to the state it was in before `done` (ADR 0009 D5). Done by hand and `dropped`
leave the stages as they are; a reopen keeps them.

**D6 — Time is booked by people, against a ticket, in minutes.** A time entry is (person,
ticket, minutes, the day worked, note). *(Amended 2026-10-04, [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6: and the token it was
booked through, its id and name, none for a browser session; each correction's previous values
keep the token the correction came through.)* An agent never books time: a session's duration is
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
  (they are the person's, not the ticket's) and are exported through D10. *(Amended
  2026-10-03: the export carries the three stages.)*
- *(Added 2026-10-03.)* The board card shows the bar of the stage its column works on
  (ADR 0018 D1); the detail shows all three.

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
- *(Added 2026-10-03.)* **A bar per board column**, `decided` and `blocked` included. Done
  would ask for bars to be filled that no work moves. Lost.
- *(Added 2026-10-03.)* **A bar per state** (`filed`, `analysed`, `decided`, `in-progress`,
  `review`). Finer, but a Refinement card would carry two bars. Lost.
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
