# ADR 0009: Ticket States Are the Frontmatter States Plus `blocked`, and Every Transition Is a Recorded Act With a Reason

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the catalog question
"workflow states?": the states of the existing Markdown frontmatter, unchanged, plus a
`blocked` state for work that waits on something outside the team's reach — the owner's
example is a firewall rule an external provider has to set. A `review` state was proposed and
not taken.

The transition rules of D3–D6 were put to the owner together with the question and were not
objected to; they are recorded as proposed and stay open to objection until the first
implementation makes them concrete.

**Not built.** No `tickets` table exists.

## Context

The Markdown tickets carry `state: filed | analysed | decided | in-progress | done | dropped`,
and that sequence encodes the owner's method: an analysis before a decision, a decision
before work. The ticket rules make the frontmatter state the index of the backlog; a board
column is a state. What the sequence lacks is a place for a ticket that cannot move because
someone outside the team has to act — today that is a `blocked-by:` field beside the state,
invisible on any board. A team product ([ADR 0004](0004-cowork-is-a-team-product.md)) also
needs every transition attributable, and an agent's transitions bounded.

## Decision

**D1 — The states are `filed`, `analysed`, `decided`, `in-progress`, `blocked`, `done`,
`dropped`, for every ticket type alike** ([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md)).
`done` and `dropped` are terminal. A board column is a state, in this order.

**D2 — `blocked` remembers where it came from.** A ticket enters `blocked` from any
non-terminal state with a mandatory reason: a kind (`decision`, `human`, `product`,
`release`, `external`, `ticket`), a free text, and optionally the ticket it waits on (a
`blocks` link, the links record) or an external reference. Leaving `blocked` returns the
ticket to the state it came from; the block reason stays in the timeline. A ticket may be
blocked and unblocked any number of times.

**D3 — Forward moves go to the next state; backward moves are limited and reasoned.**
`filed → analysed → decided → in-progress → done` step by step. Backward:
`in-progress → decided` or `→ analysed`, and `decided → analysed`, each with a reason (the
analysis or the decision was wrong). Any other backward move is a **reopen**: a terminal
ticket goes to `filed`, keeps its key and its history, and the timeline says it was reopened
and why.

**D4 — `dropped` is reachable from every non-terminal state and requires a reason.**

**D5 — `done` requires a verification note:** what was run, against what, with what result.
The note is a field of the transition, not of the ticket; the ticket rules' "merged is not
verification" becomes a required input.

**D6 — Every transition is a recorded act:** actor (and agent, when one acted), from, to,
reason or note, timestamp — in the audit record and the timeline. The dates the frontmatter
carried as fields (`decided:`, `done:`) are derived from these acts, not edited by hand.

## Consequences

- The importer maps the five existing states one to one and never infers `blocked`; the
  `blocked-by:` values of the frontmatter are reported as candidates for a `blocked` state
  and for the reason kind, and a person confirms.
- There is no `review` state: a ticket goes from `in-progress` to `done` in one act. Whether
  an **agent** may perform that act — or must stop at `in-progress` with its verification note
  and leave `done` to a person — is the agent-permission question of the catalog; this record
  does not decide it, and D5 gives that question its lever (the note is required either way).
- The board gets a `blocked` column with the reason kind on the card, so "waiting on the
  provider" is visible at a glance and not hidden in a field.
- The rank of the backlog applies to non-terminal tickets; `blocked` tickets keep their rank
  and are shown as blocked, not moved to the end.
- Reopening produces a longer timeline, never a second ticket for the same key.

## Alternatives Considered

- **The frontmatter states plus `review`** — the recommendation. Would have given a team a
  hand-over state and an agent a natural stopping point. Not taken by the owner; the agent
  boundary moves to the permission question.
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
