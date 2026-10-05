# ADR 0014: Rank Is the Decision, Score Is the Warning — a Manual Rank per Project Wins, a Versioned Score Suggests and Orders the Person-Level Lists

## Status

Accepted, amended 2026-10-03 (D2: what a key is, where a move puts it and over which tickets
it is computed, where a filed and a reopened ticket start, what a release before the rank
leaves behind — open tickets without a key, done and dropped ones with one — written when the
rank was built; that a key is never shown and whether a move writes is decided over the
tickets the mover sees — written the same day, when a review found that a shown key tells
where hidden tickets sit and whether they are still open) and 2026-10-04 (D2: a filing may
name its place in its horizon, the owner's answer when an agent could neither file a ticket into
a horizon nor re-sort the backlog). Date: 2026-09-29. Decided by the
owner as the answer to the catalog question "priority model?": both a manual rank and a
computed score, the rank winning inside a project, over rank only, score only, and
score-with-pins. The formula of D4 was proposed with the question; the owner chose the option
without objecting to it, and it stands as version 1 until amended. D3, D4 and D5 were made
concrete on 2026-10-05, when the score was built (below).

**Built** (D1 and D2 2026-10-03; D3–D5 and the rebalancing 2026-10-05): D1 and D2 — the `rank` column ([migration 17](../../backend/internal/store/migrations/000017_ticket_rank.up.sql),
which ranked every project's open tickets in number order), [`domain.RankBetween`](../../backend/internal/domain/rank.go),
filings and reopens at the bottom — or, since 2026-10-04, a filing beside a ticket of its
horizon (`rankBeside` in [`rank.go`](../../backend/internal/api/rank.go)) —, done and dropped without a key, the move
`PUT …/tickets/{number}/rank` recorded as `ranked` ([`rank.go`](../../backend/internal/api/rank.go)),
and a project's list in its rank; the drag in the backlog
([ADR 0018](0018-the-views-of-the-first-release.md) D1). ~~**Not built:** the score of D3–D5;
the rebalancing the Consequences name.~~ *(Built 2026-10-05, below.)* *(2026-10-04:)* "assigned to me" and the open decisions of
[ADR 0018](0018-the-views-of-the-first-release.md) D3 are built before the score, and ~~until it
exists D5's order stands in as the interim order of the person-level lists: by the tenant's slug,
the project's key and the project's rank — the open decisions by their ticket's place in it, `done`
and `dropped` tickets after the ranked ones, then the question's number
([`api/mylists.go`](../../backend/internal/api/mylists.go), `ByProjectRank` in
[`store/tickets.go`](../../backend/internal/store/tickets.go)); the rank stays sealed in their
cursors (D2). The score replaces it when it is built.~~ *(2026-10-05: the interim order is gone; the
score's order of D5 replaced it.)*

*(2026-10-05:)* **D3–D5 built.** The score is version 1 of [`domain.ScoreKey`](../../backend/internal/domain/score.go),
stored on the ticket as its key with the version that computed it (`score_key`, `score_version`,
[migration 33](../../backend/internal/store/migrations/000033_ticket_score.up.sql), which scored every
ticket); the write that changes an input scores the ticket again in its own transaction — a filing,
a severity, a horizon set or withdrawn, a stake set or removed (`refreshScore` in
[`api/score.go`](../../backend/internal/api/score.go)) — and a ticket shows `score` and
`score_version`, none while it is done or dropped. The backlog marks where the two orders disagree
and sorts by the score on request, `PUT …/projects/{project}/rank` with `{"by": "score"}`
(`SortProjectRank`); `GET /api/v1/me/next`, `/me/assigned` and `/me/decisions` follow the score
([`api/mylists.go`](../../backend/internal/api/mylists.go)). The rebalancing of the Consequences is
built: a place whose key would be longer than 32 characters, or for which none fits, spreads the
project's keys again first (`rebalanceRank` in [`rank.go`](../../backend/internal/api/rank.go),
[`domain.RankSpread`](../../backend/internal/domain/rank.go)); the integration tier moves 800 tickets
into one gap (`TestEightHundredMovesIntoOneGap`).

## Context

A backlog of two hundred tickets across ten projects cannot be kept in order by hand alone:
a new `security: live` finding with `urgency: now` lands at the bottom until somebody drags
it, and the rank goes stale without anyone noticing. A backlog ordered by a formula alone
has the opposite defect: the order stops being a decision, and every deviation has to be
forced by bending the inputs — which [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md)
D3 ruled out for urgency. The inputs of any formula are already fixed by earlier records:
severity and urgency (ADR 0010), the `need` and `urgent` stakes of
[ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md), and age.
The person-level lists of [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
D3 span projects and tenants and therefore cannot have a shared rank at all.

## Decision

**D1 — Every project has a manual rank over its non-terminal tickets, and the rank wins.**
The backlog of a project is ordered by rank. A `blocked` ticket keeps its rank
([ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)); a terminal ticket
has none. Children are ranked among their siblings under their parent
([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D4).

**D2 — The rank is a sortable string, not a position number.** A move writes one row (the
moved ticket's key between its new neighbours' keys) and never renumbers the list. A move is
a recorded act. *(Added 2026-10-03:)* A key is a base-62 fraction over `0-9A-Za-z`, compared
byte by byte as the `C` collation compares, of at most 128 characters and never ending in `0`.
A move names the ticket it goes directly after or before; the new key lies strictly between
that ticket's key and the next key on that side, over every ticket of the project — those the
mover cannot see included — so no key is handed out twice and a hidden ticket keeps its key
and its place. Because it is computed over tickets the mover may not see, a key is never
shown: not on a ticket, not in the act, which names the neighbour, not in a list's cursor,
which carries its position sealed. The list's order is what a client reads of the rank. Whether
a move writes is decided over the tickets the mover can see: a ticket that already sits there
among them is answered unchanged, without an act, whatever sits between unseen. Between
two keys a move takes the middle; at an open end it moves by no more than the square of the
distance to that end, so filings at the bottom and moves to the top keep the keys short — ten
thousand stay within five characters. A filed ticket and a reopened one join the rank at the
bottom; done and dropped take the key away (D1). *(Amended 2026-10-04:)* A filing may name the
ticket it goes directly after or before — an open ticket of the project in the horizon it is
filed into ([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3 as amended
2026-10-04) — and takes its key as a move there would; without one it joins at the bottom, which
is the end of its horizon, since a horizon's group in the backlog is the rank read over that
horizon. An agent names a place with `rank`
([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4). The writes that hand out keys in a project
are ordered by one lock. A ticket filed or reopened by a release before the rank has no key;
the project's list shows it after the ranked tickets, by number, and the next write that hands
out a key in its project ranks it there first — no move, so no act and no version
([ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md) D1).
That release's done and dropped leave the key in the column; the list and every move read a
key on a done or dropped ticket as none.

**D3 — Every non-terminal ticket has a computed score, shown beside the rank.** Where the
score order and the rank order disagree, the backlog marks the ticket ("score says higher" /
"lower"). A "sort by score" action reorders a project's backlog to the score in one recorded
act; it is never automatic. *(Made concrete 2026-10-05: the backlog is grouped by horizon and a
drag places a ticket among its siblings ([ADR 0018](0018-the-views-of-the-first-release.md) D1), so
the two orders are compared among the siblings of each horizon's group: the longest run of
siblings whose scores do not rise from the top down agrees with the score, and every other sibling
is marked — "higher" where a ticket of that run above it scores less, "lower" otherwise — so one
ticket out of its place marks one ticket, not every ticket it pushed aside; equal scores agree. The
sort gives the open tickets the sorter sees the keys they hold among themselves in the score's
order, each scored anew with the function as it stands, an equal score keeping the rank's order:
every group and every set of siblings, which read the rank over a part of the project, then read
in the score's order, and a ticket the sorter cannot see keeps its key and its place. It is one
act of the project, `ranked` with `{"by": "score", "score_version", "moved"}`, naming the tickets
it moved, whose activity shows it ([ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md)
D1), published as `project.changed` ([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D2); every ticket it moved gets a new version, as a move gives one
([ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
D1), without an act or an event of its own; a rank that follows the score already records
nothing. A member sorts, an agent with `rank`
([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4).)*

**D4 — The score is a versioned function in the code, not a setting.** Version 1:

```
score = severity_weight + urgency_weight + Σ interest_weight + age_days / 30

severity_weight:  critical 8, high 5, medium 3, low 1, cosmetic 0
urgency_weight:   now 8, release 5, next 3, later 1, icebox -5
interest_weight:  need 1, urgent 2   (per person; watch 0)
age_days:         days since opened_at
```

The score is recomputed when an input changes and stored with the version of the function
that produced it. A change to the function is an amendment of this record and a migration
that recomputes. *(Made concrete 2026-10-05: `urgency_weight` reads the horizon the ticket shows
([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3 as amended 2026-10-04),
`interest_weight` every stake of `need` and `urgent`. `age_days` is the time elapsed since
`opened_at`, in days, not a count of whole days: time then adds the same to every ticket's score,
and the order of two scores changes only when an input does — never at midnight. So the score is
stored as its key, the weights less the age the ticket will gain (`opened_at`, in units of thirty
days since the Unix epoch), whose order is the score's at every moment and which a list orders
and a cursor resumes by; a ticket shows the key plus the age at the moment of the read, to one
decimal. A done or dropped ticket shows none; one filed by a release before the score shows none
until an input of it changes. The function is [`domain.ScoreKey`](../../backend/internal/domain/score.go);
the migration that recomputes writes it out once more in SQL, and an integration test holds the
two equal, as [migration 33](../../backend/internal/store/migrations/000033_ticket_score.up.sql) does
for version 1.)*

**D5 — The person-level lists are ordered by score.** "Next for me" and "assigned to me"
have no shared rank across projects and tenants; they show the score order, with the ticket's
project rank as a secondary indicator. *(Made concrete 2026-10-05: highest first, an equal score
by the ticket's id; the open decisions by the score of their ticket, a done or dropped ticket's
last. The secondary indicator is the ticket's place in its project's rank among the open tickets
of its horizon that the reader sees — where the backlog's group shows it; a ticket the reader
cannot see is never counted.)*

## Consequences

- The rank says what the project decided; the score says where the facts disagree with
  that decision; the owner reads the disagreement, not the number.
- `/me/next` has a defined order for the first time; it is the score's, and the score's
  weights are therefore the one place where a person's cross-project priorities are tuned.
- D2 avoids the classic renumbering write storm on every drag; the cost is that the string
  occasionally needs rebalancing, which is a maintenance task, not a user-visible event.
  *(Built 2026-10-05: a place whose key would be longer than 32 characters, or for which none
  fits, first spreads the keys of every ticket of the project evenly again in their order — those
  the mover cannot see included, each keeping its place — with no act and no version, and takes its
  key in the gap as it then is.)*
- Two numbers per card. The UI shows the score as a marker, not a figure, unless asked.
  *(Built 2026-10-05: the backlog's marker says higher or lower and gives the figure in its
  tooltip; the person-level lists show the place in the backlog beside each ticket and the score in
  its tooltip.)*

## Alternatives Considered

- **Manual rank only.** No formula to defend; the rank goes stale silently and the
  person-level lists have no order. Lost.
- **Score only.** Never stale; the rank as a decision disappears and deviations must be
  forced through the inputs. Lost.
- **Score by default, rank as a pin.** Fewer drags; a list with six pinned tickets and a
  computed rest reads worse than a ranked list with markers, and a pin is a rank under another
  name. Lost.
- **A configurable formula per tenant.** A settings page for a function nobody has used yet;
  D4's versioning gives a change a home without one. Lost.

## Residual risks

- Version 1's weights are a first guess; the first months of use will show whether age or
  interest dominates in the wrong way. The amendment path is cheap by design.
- D3's marker can nag: a project that deliberately ranks against the score sees the marker on
  every card. If that happens, a per-project "acknowledge score" is the amendment, not a
  softer formula.
- *(Added 2026-10-03.)* ~~No rebalancing is built. Moves into one and the same gap halve it:
  about 630 to 760 in a row there exhaust the 128 characters — 635 when each lands directly
  before the same ticket, 762 when each lands directly after it — and the next such move fails
  as an internal error until a rebalancing exists. Filings and moves to either end do not wear
  a gap down.~~ *(2026-10-05: the rebalancing is built and no move fails for a worn gap; see the
  Consequences.)*
- *(Added 2026-10-03.)* A key is computed over tickets the mover may not see, so a shown key
  would tell where they sit, how many were open when the rank was introduced, and whether one
  is still open; D2 therefore never shows it. Two signals remain, neither of them a hidden
  ticket's content ([docs/security/tenancy.md](../security/tenancy.md#h-3), H-3): ~~a move into a
  gap that moves of hidden tickets wore down fails as any exhausted gap does,~~ *(2026-10-05: gone
  with the rebalancing; what is left of it is the time a move takes when it spreads the project's
  keys first)* and the one write
  that ranks the tickets an earlier release left without a key — a hidden ticket's filing
  included — changes how the list shows them, with no act the caller sees.
- *(Added 2026-10-05.)* A release before the score, run over this schema in a rollback
  ([ADR 0028](0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D4), files
  tickets without a score and changes severities, horizons and stakes without scoring again: such
  a ticket shows no score, or the one it had, and stands there in the person-level lists, until
  its next input change or a sort of its project, which scores every ticket it orders anew.
- *(Added 2026-10-05.)* A cursor into a project's list handed out before a rebalancing resumes at
  its old key's place among the new keys, so the next page may repeat or skip tickets — once, for a
  client paging through the list while the keys are spread; the backlog loads its pages again on
  the next event.

## References

- [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) — severity and urgency
- [ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) — the interest weights
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) — rank and `blocked`
- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3 — the person-level lists
- [ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md) D1, D4 — a move raises the version and takes no `If-Match`
- [`backend/internal/domain/score.go`](../../backend/internal/domain/score.go), [`backend/internal/api/score.go`](../../backend/internal/api/score.go), [migration 33](../../backend/internal/store/migrations/000033_ticket_score.up.sql) — the score and the sort
- [`backend/internal/domain/rank.go`](../../backend/internal/domain/rank.go), [`backend/internal/api/rank.go`](../../backend/internal/api/rank.go), [migration 17](../../backend/internal/store/migrations/000017_ticket_rank.up.sql) — the implementation
