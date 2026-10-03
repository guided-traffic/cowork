---
id: T15
title: open questions cannot be asked, answered or withdrawn
state: done
severity: high
security: none
threat:
urgency: later        # rule 4: decided fix (the ADRs)
effort: M
blocked-by: T12
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: open questions asked, edited, answered — recorded by agents with record-answer — and withdrawn; has_open_questions
---

## Current state

Decided by [ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D2–D5, [ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D8, [ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D1, D8, [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D2, D4 and [ADR 0050](../adr/0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
D3.

- No question table, no route.
- ADR 0011 D2's fields: the question, the context and the options (Markdown), the
  recommendation, the answer (Markdown), who asked, whom it is asked of, who answered and when,
  `recorded_by_agent`, a status `open`, `answered` or `withdrawn`. Only a person decides an
  answer; an agent may ask and may withdraw its own question. ADR 0066 D8: with `record-answer`
  an agent records and updates the answer its person gave; the actor stays the person; an answer
  an agent recorded may be updated by an agent of the same person, and a person may always edit
  their own.
- ADR 0034 D1, D8: a member answers what is asked of them or open in the tenant; a viewer
  answers nothing.
- ADR 0011 D4 renders `### Q<n>:`, so a question needs a number that stays when others are
  withdrawn.
- ADR 0011 D3's "open decisions" is a person-level list (`/me/decisions`, phase 3); it reads the
  asked-of person and the status.
- No record says whether an agent may edit a question's text; the open variant applies.

## Required changes

1. **Migration:** the status enum; `questions` (the number within the ticket, question, options,
   recommendation, answer, the asker and the asking request's agent mark, the person asked —
   NULL meaning open in the tenant —, answered by and at, `recorded_by_agent`, withdrawn at and
   by, the generated `search` column over question, options, recommendation and answer — ADR 0025
   D2 —, version, timestamps); a composite key to the ticket; status CHECKs; an index on the
   person asked and the status; the tenant policy.
2. **Routes:**
   - `POST …/tickets/{number}/questions` (member, `write`, agent baseline; the person asked must
     be a member of the tenant who can see the ticket; an `Idempotency-Key`, required when
     agent-marked; the act `asked`). The number is the ticket's next, taken under a lock on the
     ticket's row.
   - `GET` list (by number) and one.
   - `PATCH …/questions/{id}` (the asker; `If-Match`; only while open; agents allowed — the open
     variant; the act `edited`).
   - `PUT …/questions/{id}/answer` (the person asked, or any member when it is open in the
     tenant; a viewer `403`; an agent-marked request needs `record-answer` and sets
     `recorded_by_agent`, the actor staying the person; an agent updates only an answer an agent
     of the same person recorded; `If-Match` once answered; a withdrawn question → `409`; the act
     `answered`).
   - `PUT …/questions/{id}/withdrawal` (the asker; an agent only a question an agent of the same
     person asked; only while open; idempotent; the act `withdrawn`).
3. **Filter:** `has_open_questions` on the ticket lists (ADR 0049 D1).
4. **Tests:** the lifecycle (open → answered, open → withdrawn, an answer edited by its person);
   ADR 0066 D8's rules with two identities and a plain and a flagged token each; a viewer refused;
   numbers stable after a withdrawal; the filter; the restriction and confidential rows; the
   cross-tenant rows.
5. **Docs and records:** domain.md (questions, who answers, the agent rules); ADR 0011 (D1, D2, D5
   built; D3's list and D6 phase 3; D4 with T21) and ADR 0066 D8 Status and index rows.

## Related

- T12 — tickets and the predicates.
- T21 — the export renders the questions.
