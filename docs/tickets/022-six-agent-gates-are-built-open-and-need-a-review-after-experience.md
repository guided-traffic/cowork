---
id: T22
title: six agent acts are built open by the owner's choice — removing a blocks link before done, reassigning a confidential ticket, moving back and reopening, removing a stake, editing a question, editing a project — and wait for a review after experience
state: filed
severity: medium
security: hardening
threat: closing a gate would additionally cover an agent session, or a prompt injected into one, closing a ticket over open prerequisites by removing the blocks links first, admitting a person to a confidential ticket by reassigning it, undoing a decision or a close its person had reserved with an assisted token, withdrawing its person's stake, rewording a question its person is asked to decide, and renaming a project or changing its WIP limits
urgency: icebox       # rule 5: needs a product call (after experience with the tool)
effort: S
blocked-by: product
filed-from: the owner's guidance for undecided agent permissions, phase-2 conversion
opened: 2026-10-02
decided:
done:
---

## Current state

[ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
gives an agent token a baseline (D2), nine capabilities (D4) and a hard-off list (D3); six
acts are on none of them. The owner's guidance for phase 2: leave an undecided gate open, gain
experience with the tool first, decide afterwards. Phase 2 built the six as allowed, asserts
each of them in the integration tests so a change is deliberate, and docs/security/tokens.md
names them as a gap.

- **Removing a `blocks` link before `done`** (`UnlinkTickets`,
  [`links.go`](../../backend/internal/api/links.go)). An agent with `close` cannot override the
  prerequisite refusal (ADR 0043 D3,
  [ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D7), but it can remove
  the open `blocks` links into its ticket and then close it: two calls around a hard-off rule.
  Both are audit rows marked as the agent's; nothing refuses them.
- **Reassigning a confidential ticket** (`UpdateTicket`,
  [`tickets.go`](../../backend/internal/api/tickets.go)). Assignment admits the new assignee
  ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
  D9). An agent of a person who sees the ticket can assign it to anyone in the tenant; the
  disclosure cannot be undone, which puts it outside the "reversible" of ADR 0043's own default.
- **Backward moves and reopens** (`checkTransition`,
  [`transitions.go`](../../backend/internal/api/transitions.go); the stage writes in
  [`tickets.go`](../../backend/internal/api/tickets.go)). `decided → analysed`,
  `in-progress → analysed` and the reopen of `dropped` need no capability, and neither do the two
  ways out of `done` ([ADR 0009](../adr/0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)
  D5): withdrawing a done by hand, and lowering a stage of a ticket done by its stages, each with
  a reason. An "assisted" token — whose person kept `decide` and `close` — can undo its person's
  decision or close.
- **Removing its person's stake** (`RemoveInterest`, [`interest.go`](../../backend/internal/api/interest.go)).
  Registering `need` or `urgent` takes the `interest` capability (ADR 0043 D4); removing any
  stake takes none, so an agent without `interest` can withdraw what its person registered.
- **Editing an open question** (`UpdateQuestion`, [`questions.go`](../../backend/internal/api/questions.go)).
  The asker edits the question's text while it is open, an agent of the asker included; a
  question asked of a person can be reworded by an agent before that person answers.
- **Editing a project** (`UpdateProject`, [`projects.go`](../../backend/internal/api/projects.go)).
  A member edits a project's name, description and WIP limits with a `write` token; creating
  a project takes `create-project` (ADR 0043 D4), editing one takes nothing, so an assisted
  token can rename a project or change the WIP limits its person set.

## Required changes

### Depends on the answers

1. Each answer that closes a gate amends ADR 0043 in place (D3 or D4) and changes the route
   named above with its refusal (`403 agent_forbidden` naming the rule or the capability) and
   its tests; tokens.md's gap loses the gate.

## Open questions

### Q1: Is removing an open `blocks` prerequisite an agent act?

- **(a) Keep it allowed:** reversible and attributable; ADR 0012 D7's human override stays a
  convention an agent can step around by unlinking first.
- **(b) Hard-off for agents while the link's source is open;** other link types and settled
  prerequisites stay removable: closes the bypass at D7's strength; a link an agent set by
  mistake needs a person to remove it.
- **(c) A capability of its own:** ADR 0043 D4 gains a tenth switch, the token page one more
  choice.

Recommended: **(b)** — the narrowest rule that makes the human override real again, at the cost
of a rare manual unlink; decide it once the audit record shows how agents use links.

**Answer:** _open_

### Q2: May an agent change the assignee of a confidential ticket?

- **(a) Keep it allowed.**
- **(b) Hard-off for agents.**
- **(c) Allowed only to its own person,** who already sees the ticket, so nobody new is admitted.

Recommended: **(c)** — it keeps "I am taking this" for an agent and removes the one agent path
that admits a new person to a confidential finding.

**Answer:** _open_

### Q3: Do backward moves and reopens need the capability of the transition they undo?

- **(a) Keep them allowed** (ADR 0043's title: everything reversible and attributable).
- **(b) Undoing needs the gate's capability:** `in-progress → analysed` and `decided → analysed`
  need `decide`; withdrawing a done by hand and lowering a stage of a ticket done by its stages
  need `close`; reopening `dropped` needs `drop`; `in-progress → decided` and
  `review → in-progress` stay baseline.
- **(c) Every backward move and reopen is hard-off for agents.**

Recommended: **(b)** — an "assisted" token's gate then holds in both directions, a "full" token
loses nothing, and the refusal keeps ADR 0043 D5's vocabulary, the missing capability.

**Answer:** _open_

### Q4: Does removing a `need` or `urgent` stake need the `interest` capability?

- **(a) Keep it allowed** — a stake is the person's, and removing it is reversible.
- **(b) Removing a stake needs the capability that registering it needs** (`watch`: none;
  `need` and `urgent`: `interest`).

Recommended: **(b)** — the capability then guards the weight in both directions, as Q3 (b) does
for the transitions.

**Answer:** _open_

### Q5: May an agent reword a question that is asked of a person?

- **(a) Keep it allowed while the question is open.**
- **(b) Only a question an agent asked**, like the withdrawal of ADR 0011 D2.

Recommended: **(b)** — it matches the withdrawal rule the record already has.

**Answer:** _open_

### Q6: Does editing a project need a capability?

- **(a) Keep it allowed** — name, description and WIP limits are reversible and every edit is
  recorded.
- **(b) Editing needs `create-project`:** whoever may shape a new project may reshape one.
- **(c) Hard-off for agents,** like the other administration of a project (archiving).

Recommended: **(b)** — an assisted token then leaves the project as its person set it, a full
token loses nothing, and ADR 0043 D4 gains no new name.

**Answer:** _open_
