---
id: T22
title: six agent acts are built open by the owner's choice — removing a blocks link before done, reassigning a confidential ticket, moving back and reopening, removing a stake, editing a question, editing a project — and wait for a review after experience
state: done
severity: medium
security: hardening
threat: closing a gate would additionally cover an agent session, or a prompt injected into one, closing a ticket over open prerequisites by removing the blocks links first, admitting a person to a confidential ticket by reassigning it, undoing a decision or a close its person had reserved with an assisted token, withdrawing its person's stake, rewording a question its person is asked to decide, and renaming a project or changing its WIP limits
urgency: later        # rule 4: the owner decided the fix
effort: S
blocked-by:
filed-from: the owner's guidance for undecided agent permissions, phase-2 conversion
opened: 2026-10-02
decided: 2026-10-06
done: 2026-10-06
shipped: an agent assigns a confidential ticket only to its own person or to nobody (hard-off, ADR 0043 D3, ADR 0065 D9); the other five acts stay an agent's at the baseline by the owner's decision, each risk accepted (ADR 0043 D2, ADR 0012 D7)
---

## Current state

[ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
gives an agent token a baseline (D2), nine capabilities (D4) and a hard-off list (D3). Phase 2
built six acts on none of them as allowed, by the owner's guidance to gain experience first; the
owner reviewed them on 2026-10-06.

- **Built:** assigning a confidential ticket is hard-off for agents unless the assignee is the
  agent's own person, or nobody, or the assignee the ticket already had — `mayAssign` in
  [`tickets.go`](../../backend/internal/api/tickets.go), called by the filing (`newTicket`) and by
  the `PATCH` (`applyRelations`), with the confidential flag as the write leaves it, so a change
  to `live` or `boundary` in the same write counts; `403 agent_forbidden`, `hard-off: assigning a
  confidential ticket to anyone but the agent's person`. Those are the only two writes of
  `assignee_id` (`InsertTicket`, `UpdateTicketFields`). The `api` tool's limits name the rule.
- **Kept, by the owner's decision:** removing a `blocks` link before `done`, the backward moves
  and reopens, removing or lowering its person's stake, editing an open question its person asked,
  editing a project — the baseline of ADR 0043 D2 as amended, each risk named in its Residual
  risks and in docs/security/tokens.md H-6, the `blocks` link's also in
  [ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D7.
- **Verified:** unit `TestAnAgentAssignsAConfidentialTicketOnlyToItsPerson` (`api/tickets_test.go`);
  integration `TestConfidentialTickets` (an agent's reassignment to another person refused, to its
  own person allowed) and `TestAnAgentAssignsAConfidentialTicketOnlyToItsPerson` (a filing, a
  change, the change that makes a ticket confidential, nobody, the assignee as it was, a ticket
  that is not confidential); both fail with the rule taken out. The five kept acts stay asserted
  as allowed in `api_links_test.go`, `api_transitions_test.go`, `api_stages_test.go`,
  `api_interest_test.go`, `api_questions_test.go` and `api_projects_test.go`.
- **Not decided here:** saving, changing, sharing and deleting a saved filter, an act no record
  lists and open to agents until its own review (ADR 0043).

## Required changes

None.

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

**Answer:** (a) — keep it allowed. The owner accepts that an agent with `close` steps around the
prerequisite override of ADR 0012 D7 by removing the open `blocks` links first; every removal is
recorded on both tickets as the agent's. Recorded in ADR 0043 D2 and ADR 0012 D7.

### Q2: May an agent change the assignee of a confidential ticket?

- **(a) Keep it allowed.**
- **(b) Hard-off for agents.**
- **(c) Allowed only to its own person,** who already sees the ticket, so nobody new is admitted.

Recommended: **(c)** — it keeps "I am taking this" for an agent and removes the one agent path
that admits a new person to a confidential finding.

**Answer:** (c) — only to its own person; assigning to nobody admits nobody and stays allowed;
anything else is `403 agent_forbidden` naming the rule, on a filing and on a change. Recorded in
ADR 0043 D3 and ADR 0065 D9.

### Q3: Do backward moves and reopens need the capability of the transition they undo?

- **(a) Keep them allowed** (ADR 0043's title: everything reversible and attributable).
- **(b) Undoing needs the gate's capability:** `in-progress → analysed` and `decided → analysed`
  need `decide`; withdrawing a done by hand and lowering a stage of a ticket done by its stages
  need `close`; reopening `dropped` needs `drop`; `in-progress → decided` and
  `review → in-progress` stay baseline.
- **(c) Every backward move and reopen is hard-off for agents.**

Recommended: **(b)** — an "assisted" token's gate then holds in both directions, a "full" token
loses nothing, and the refusal keeps ADR 0043 D5's vocabulary, the missing capability.

**Answer:** (a) — keep them allowed. Recorded in ADR 0043 D2, the risk in its Residual risks.

### Q4: Does removing a `need` or `urgent` stake need the `interest` capability?

- **(a) Keep it allowed** — a stake is the person's, and removing it is reversible.
- **(b) Removing a stake needs the capability that registering it needs** (`watch`: none;
  `need` and `urgent`: `interest`).

Recommended: **(b)** — the capability then guards the weight in both directions, as Q3 (b) does
for the transitions.

**Answer:** (a) — keep it allowed. Recorded in ADR 0043 D2, the risk in its Residual risks.

### Q5: May an agent reword a question that is asked of a person?

- **(a) Keep it allowed while the question is open.**
- **(b) Only a question an agent asked**, like the withdrawal of ADR 0011 D2.

Recommended: **(b)** — it matches the withdrawal rule the record already has.

**Answer:** (a) — keep it allowed while the question is open. Recorded in ADR 0043 D2, the risk
in its Residual risks.

### Q6: Does editing a project need a capability?

- **(a) Keep it allowed** — name, description and WIP limits are reversible and every edit is
  recorded.
- **(b) Editing needs `create-project`:** whoever may shape a new project may reshape one.
- **(c) Hard-off for agents,** like the other administration of a project (archiving).

Recommended: **(b)** — an assisted token then leaves the project as its person set it, a full
token loses nothing, and ADR 0043 D4 gains no new name.

**Answer:** (a) — keep it allowed. Recorded in ADR 0043 D2, the risk in its Residual risks.
