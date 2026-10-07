# ADR 0009: Ticket States Are the Frontmatter States Plus `blocked`, and Every Transition Is a Recorded Act With a Reason

## Status

Accepted, amended 2026-10-03 (D1, D3: the state `review` between `in-progress` and `done`,
and the board's columns as a view over the states; D5: `done` reached by the last progress
stage or by hand, the hand's done withdrawn with a reason; D5 made concrete the same day, when
it was built: the way out of done, the block a ticket done from `blocked` keeps, a parent's
done, which done is by the stages — the tickets closed before the stages among them — and an
open ticket whose stages are full already). Date: 2026-09-29. Decided by the
owner as the answer to the catalog question "workflow states?": the states of the existing
Markdown frontmatter, unchanged, plus a `blocked` state for work that waits on something
outside the team's reach — the owner's example is a firewall rule an external provider has to
set. A `review` state was proposed and not taken.

The transition rules of D3–D6 were put to the owner together with the question and were not
objected to; they are recorded as proposed and stay open to objection until the first
implementation makes them concrete.

The amendment of 2026-10-03 is the owner's answer to four questions put with the board of
[ADR 0018](0018-the-views-of-the-first-release.md) D1, whose columns are Refinement, Ready,
In Progress, Blocked and In Review. The columns are a view over the states with a new state
`review`, over renaming the states to the board's words and over a column per state. The
progress has three stages, one per kind of work
([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D2), and a ticket whose stages are all full is done. The write that fills the last stage is
the done act, with the verification note, the prerequisite refusal and the `close`
capability, over a done without the note and over a done without any guard. A done by hand
leaves the stages as they are, is open to a person from any open state and to an agent from
`in-progress` and `review`, and is withdrawn with a reason to the state before it, over a
reason in place of the note and over an agent's done from any state.

**Built** (phase 2, 2026-10-02): D1–D6 — the states and the block columns (migration 8), the
transition route with the matrix of [`transition.go`](../../backend/internal/domain/transition.go)
(forward one step; `in-progress → decided | analysed` and `decided → analysed` with a reason;
into `blocked` from any open state with a kind and a text and out of it only to its origin;
`dropped` with a reason; `done` from `in-progress` with a note; a reopen to `filed` with a
reason), the `from` precondition, the notes and reasons on the `transitioned` acts, and
`decided_at` and `done_at` set by those acts. **Built in the API** (2026-10-03): the amendment
of D1, D3 and D5 — the state `review` ([migration 18](../../backend/internal/store/migrations/000018_ticket_state_review.up.sql))
and its moves in the matrix of `transition.go`; done by the stages, the `PATCH` that fills the
last of the three ([`tickets.go`](../../backend/internal/api/tickets.go) `planStageMove`), and
done by hand, each with the note, the prerequisite refusal and `close` from `in-progress` or
`review` only; the withdrawal with a reason, the lowering of a stage that reopens, and
`done_from` and `done_by_hand` ([migration 19](../../backend/internal/store/migrations/000019_progress_stages.up.sql)).
The board's columns of D1 are the views' ([ADR 0018](0018-the-views-of-the-first-release.md)).
*(2026-10-06.)* The importer of [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) maps the states one to one and
infers `blocked` from nothing: a ticket is created blocked only where its file or a correction says
so, with the kind, the reason and the state it came from that D2 needs. A ticket it creates as done
is done from `in-progress`, by its stages only while all three are full and it has no children, by
hand otherwise (D5).

## Context

The Markdown tickets carry `state: filed | analysed | decided | in-progress | done | dropped`,
and that sequence encodes the owner's method: an analysis before a decision, a decision
before work. The ticket rules make the frontmatter state the index of the backlog; a board
column is a state. What the sequence lacks is a place for a ticket that cannot move because
someone outside the team has to act — today that is a `blocked-by:` field beside the state,
invisible on any board. A team product ([ADR 0004](0004-cowork-is-a-team-product.md)) also
needs every transition attributable, and an agent's transitions bounded.

## Decision

**D1 — The states are `filed`, `analysed`, `decided`, `in-progress`, *(added 2026-10-03)*
`review`, `blocked`, `done`, `dropped`, for every ticket type alike** ([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md)).
`done` and `dropped` are terminal. ~~A board column is a state, in this order.~~ *(Amended
2026-10-03: the board's columns are a view over the states — Refinement holds `filed` and
`analysed`, Ready `decided`, and `in-progress`, `blocked` and `review` have a column each —
[ADR 0018](0018-the-views-of-the-first-release.md) D1.)* `review` is the check of the work
before it ends: the code read, the result tried, the change accepted.

**D2 — `blocked` remembers where it came from.** A ticket enters `blocked` from any
non-terminal state with a mandatory reason: a kind (`decision`, `human`, `product`,
`release`, `external`, `ticket`), a free text, and optionally the ticket it waits on (a
`blocks` link, the links record) or an external reference. Leaving `blocked` returns the
ticket to the state it came from; the block reason stays in the timeline. A ticket may be
blocked and unblocked any number of times.

**D3 — Forward moves go to the next state; backward moves are limited and reasoned.**
`filed → analysed → decided → in-progress → done` step by step. *(Amended 2026-10-03:
`filed → analysed → decided → in-progress → review` step by step, and `done` as D5 says.)*
Backward: `in-progress → decided` or `→ analysed`, and `decided → analysed`, each with a
reason (the analysis or the decision was wrong); *(added 2026-10-03)* `review → in-progress`,
with a reason (the check found work to do). Any other backward move is a **reopen**: a
terminal ticket goes to `filed`, keeps its key and its history, and the timeline says it was
reopened and why. *(Amended 2026-10-03: the reopen to `filed` is the way out of `dropped`;
`done` is left as D5 says.)*

**D4 — `dropped` is reachable from every non-terminal state and requires a reason.**

**D5 — `done` requires a verification note:** what was run, against what, with what result.
The note is a field of the transition, not of the ticket; the ticket rules' "merged is not
verification" becomes a required input.

*(Added 2026-10-03.)* **`done` is reached in two ways, and each is the done act**: the write
that brings the last of the ticket's three progress stages to 100
([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D2), and **done by hand** without the stages full. Both carry the verification note; both
are refused over open prerequisites unless a person overrides with a reason
([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)); both are an agent's only with
`close` ([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4). A person may act from any open state; an agent only from `in-progress` and `review`, so
that `close` never stands in for `decide`. Done by hand leaves the stages where they are; they
stay editable, and the ticket stays done while they change, until the done by hand is
withdrawn, with a reason, which returns it to the state it was in before `done` — unless all
three stages are full, when it stays done by them. A write that lowers a stage of a ticket
done by its stages reopens it, with a reason, to the state it was in before `done`. A parent,
whose stages its children make, is not done by them: it is done by hand.

*(Made concrete 2026-10-03, when it was built.)* The write that fills the last stage is a
`PATCH` of the stages, and the version rises once for it. Leaving done — the withdrawal, or a
lower stage — ranks the ticket at the bottom of its project, as a reopen does
([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) D2). Any transition out of a
ticket done by its stages is refused (`409 state_conflict`): a lower stage is its way out. A
withdrawal that leaves the ticket done by its full stages is recorded as `updated`,
`done_by_hand` from true to false, with its reason. A ticket done from `blocked` keeps its
block, and the way back to `blocked` takes it back. A done ticket that gains children is done
by hand from then on, and a parent is withdrawn to the state it came from whatever its stages
say. A done ticket is done by its stages only while it has no children, its three stages are
full and no done by hand is on record; any other done is by hand. A ticket a release before
the stages closed counts as done from `in-progress` — the one way that release had — by its
stages when they are full, and by hand when it has children or when its progress fell below
100 after it closed — a parent that release closed takes the last derived value as its own
when its last child leaves. What that release writes over the newer schema when its image is
rolled back to
([ADR 0028](0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D4) is
read by the same rule. An open ticket whose three stages are all full — a parent whose last
child left with its children's stages full
([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D3) — is closed by hand: no write of the stages brings the last of them to 100.

**D6 — Every transition is a recorded act:** actor (and agent, when one acted), from, to,
reason or note, timestamp — in the audit record and the timeline. The dates the frontmatter
carried as fields (`decided:`, `done:`) are derived from these acts, not edited by hand.

## Consequences

- The importer maps the five existing states one to one and never infers `blocked`; the
  `blocked-by:` values of the frontmatter are reported as candidates for a `blocked` state
  and for the reason kind, and a person confirms.
- ~~There is no `review` state: a ticket goes from `in-progress` to `done` in one act. Whether
  an **agent** may perform that act — or must stop at `in-progress` with its verification note
  and leave `done` to a person — is the agent-permission question of the catalog; this record
  does not decide it, and D5 gives that question its lever (the note is required either way).~~
  *(Amended 2026-10-03:)* `review` is the hand-over between the work and its end; an agent
  without `close` stops there, its review stage short of full, and a person closes.
- *(Added 2026-10-03.)* A ticket done by its stages is reopened by lowering one of them; a
  slider moved by mistake would reopen it, which is why the write asks for a reason first.
- The board gets a `blocked` column with the reason kind on the card, so "waiting on the
  provider" is visible at a glance and not hidden in a field.
- The rank of the backlog applies to non-terminal tickets; `blocked` tickets keep their rank
  and are shown as blocked, not moved to the end.
- Reopening produces a longer timeline, never a second ticket for the same key.

## Alternatives Considered

- **The frontmatter states plus `review`** — the recommendation. Would have given a team a
  hand-over state and an agent a natural stopping point. Not taken by the owner; the agent
  boundary moves to the permission question. *(Taken 2026-10-03, with the board of ADR 0018
  D1.)*
- *(Added 2026-10-03.)* **The states renamed to the board's words** (`refinement`, `ready`,
  `in-progress`, `blocked`, `review`, `done`, `dropped`). The board would read the states
  directly, but `analysed` would disappear as a step of its own, renaming enum values is a
  contract over two releases, and the importer would lose a distinction. Lost.
- *(Added 2026-10-03.)* **A board column per state, `review` included.** No mapping, but six
  working columns where the owner wanted five and no Refinement column. Lost.
- *(Added 2026-10-03.)* **`done` derived from the stages without the note**, and **without
  any guard**. The first drops "merged is not verification" as a required input; the second
  would let an agent without `close` close a ticket by filling its stages, and ignore open
  prerequisites. Lost.
- *(Added 2026-10-03.)* **A reason in place of the note for done by hand**, and **an agent's
  done by hand from any state**. The first sets two standards for the same claim; the second
  would let `close` stand in for `decide`. Lost.
- **A generic board** (`backlog, ready, in progress, review, done`). Loses `analysed` and
  `decided`, the two states that distinguish the method from a to-do list. Lost.
- **Configurable states per project.** A workflow editor before a ticket exists; the ticket
  rules' "the state is the index" would mean something different per project. Lost.
- **`blocked` as a flag beside the state** rather than a state. Keeps the underlying state
  visible at the cost of a badge instead of a column; the owner asked for a state one can set,
  and D2's memory of the previous state keeps what the flag would have kept. Lost.

## Residual risks

- D3's backward moves are a judgement call; if a real case needs `analysed → filed` or
  `done → in-progress` without a reopen, this record is amended, not worked around.
- D2 allows `blocked` from `filed`; a ticket blocked before analysis is unusual and may
  deserve a warning in the UI rather than a refusal. Not decided here.

## References

- [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) — the types the states apply to
- [ADR 0004](0004-cowork-is-a-team-product.md) D3 — attribution of every change
- [docs/tickets/README.md](../tickets/README.md) — the frontmatter states and `blocked-by:` the importer reads
