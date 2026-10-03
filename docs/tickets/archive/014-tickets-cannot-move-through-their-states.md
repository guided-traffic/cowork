---
id: T14
title: tickets cannot move through their states, and done is guarded neither by a verification note nor by open prerequisites
state: done
severity: high
security: none
threat:
urgency: later        # rule 4: decided fix (the ADRs)
effort: M
blocked-by: T13
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: the transition matrix with from, notes and reasons on the acts, the prerequisite refusal and a person's override, the agent capabilities
---

## Current state

Decided by [ADR 0009](../adr/0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)
D1–D6, [ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D5, D7,
[ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D2, D3, D7, [ADR 0017](../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D5, [ADR 0019](../adr/0019-no-sprints-and-no-milestones-continuous-flow-with-optional-wip-limits.md)
D3, [ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D1,
[ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3 and
[ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D2–D5.

- No transition route.
- The matrix ADR 0009 D2–D4 imply: forward one step at a time (`filed → analysed → decided →
  in-progress → done`); backward `in-progress → decided`, `in-progress → analysed` and
  `decided → analysed`, each with a reason; into `blocked` from any non-terminal state with a
  kind, a text and optionally the ticket it waits on (a `blocks` link) or an external reference,
  leaving it only to the remembered state; `dropped` from any non-terminal state with a reason;
  `done` only from `in-progress`, with a verification note; a terminal ticket reopens to `filed`
  with a reason.
- ADR 0009 D5: the verification note "is a field of the transition, not of the ticket"; it and
  the reasons live on the act (`note`, `reason`).
- ADR 0043 D2–D4: the baseline (`filed → analysed`, `decided → in-progress`, into `blocked` and
  back) and the capabilities `decide`, `close`, `drop`; overriding the prerequisite refusal is
  hard-off (D3, ADR 0012 D7). Backward moves and reopens are on no list; by the owner's choice
  agents may make them (ADR 0043's title: everything reversible and attributable) until T22
  decides.
- ADR 0045 D2: a transition carries `from`; D3 requires no key on it.

## Required changes

1. `POST …/tickets/{number}/transitions` (`{from, to, reason?, note?, block?: {kind, ticket?,
   external_ref?}, override_prerequisites?}`; member, `write`): a stale `from` →
   `409 state_conflict` with the current state in `errors[]` (ADR 0045 D2); a pair outside the
   matrix → `409`; a missing reason, note or block → `400 validation_failed`; an unsolicited
   `Idempotency-Key` recorded on the act (ADR 0045 D7), not stored.
2. **Effects in the same `Mutate`:** the act `transitioned` with from, to and the reason or note;
   `done` sets `progress` to 100 (ADR 0017 D5), clears the rank and sets `done_at`; `dropped`
   clears the rank; a reopen puts the rank at the end of the sibling group; reaching `decided`
   sets `decided_at`; `blocked` remembers its origin, and kind `ticket` with a ticket creates the
   `blocks` link from it when absent, with its acts; urgency re-derived (ADR 0010 D3) for the
   ticket and — when a `decision` ticket opens or settles — for the tickets it blocks, without
   bumping their versions; a WIP limit never refuses (ADR 0019 D3).
3. **ADR 0012 D7:** `done` over open direct `blocks` sources the caller can see → `409` listing
   them; a person repeats the transition with `override_prerequisites` and a reason, which the
   activity records as "closed over open prerequisites"; an agent's override is refused
   (hard-off); `dropped` is never refused by a prerequisite.
4. **Agent gates:** the baseline of ADR 0043 D2; `analysed → decided` needs `decide`, `→ done`
   `close`, `→ dropped` `drop`; backward moves and reopens allowed (the gate T22 reviews).
5. **Tests:** every allowed pair succeeds and every other pair is refused; reasons and notes
   required where the matrix says; `blocked` refuses an exit other than to its origin; a repeated
   transition answers `409` and writes no second act; `done` sets 100; the D7 matrix (refused,
   allowed once settled, a person's override recorded, an agent's override refused, `dropped`
   never refused, a prerequisite the caller cannot see neither listed nor refusing); per
   capability an allowed and a refused test; an agent's backward move and reopen succeed (the open
   gate, asserted); a WIP limit at its value refuses nothing; the cross-tenant rows.
6. **Docs and records:** domain.md (the matrix, the block data, D7, where the notes live);
   tenancy.md's integrity-walk gap gains the `done` refusal: a ticket can be closed over a
   prerequisite its closer cannot see; tokens.md's gap of open agent gates gains backward moves
   and reopens; ADR 0009 and ADR 0012 D7 Status and index rows.

## Related

- T13 — the prerequisites the refusal reads.
- T16 — the transition's optional explaining comment.
- T18 — `done` and the parents' derived progress.
- T21 — the export reads the notes from the acts.
- T22 — backward moves and reopens by agents, reviewed after experience.
