---
id: T16
title: tickets have no comment thread and no activity list, and an act cannot point at the comment that explains it
state: done
severity: high
security: none
threat:
urgency: later        # rule 4: decided fix (the ADRs)
effort: M
blocked-by: T14
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: the comment thread with revisions and withdrawal, explaining comments on acts, the activity list with withheld payloads
---

## Current state

Decided by [ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md)
D1–D4, D6, [ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md)
D1, D3, D5, [ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D6 and [ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D3,
D6.

- No comment table, no route, no activity projection.
- ADR 0015 D2 binds an act's explaining comment to "a comment in the same request".
  [ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D1's
  `comment(…, explains_act?)` maps to `POST …/comments`, a second request, and an append-only
  audit row cannot gain the pointer later (ADR 0026 D3): the act routes carry the comment, and
  ADR 0042's mapping is phase 5's.
- The activity list (ADR 0015 D1, D6) is a projection of `audit_events`. Time entries have a
  narrower visibility ([ADR 0017](../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
  D9, [ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
  D5); downloads and exports are recorded as data leaving the system, not as work (ADR 0026 D5);
  a withdrawn comment's text must not come back through the projection.
- Mentions (ADR 0015 D5) notify and make watchers — phase 3, with the inbox.

## Required changes

1. **Migration:** `comments` (author, agent mark, text, withdrawn at and by, the generated
   `search` column, version, timestamps; a unique `(tenant, ticket, id)` key for attachments to
   reference); `comment_revisions` (the previous text, editor, agent mark, time); tenant policies.
2. **Routes:** `POST …/comments` (member, `write`, agent baseline; an `Idempotency-Key`,
   required when agent-marked; the act `commented`); `GET` thread (oldest first, `order=asc|desc`,
   ADR 0048 D6; a withdrawn comment as `withdrawn: true` without its text) and one;
   `PATCH …/comments/{id}` (`If-Match`; a revision kept; the act `edited`; a withdrawn comment →
   `409`); `GET …/comments/{id}/revisions` (none once withdrawn); `PUT …/comments/{id}/withdrawal`
   (idempotent; the act `withdrawn`). ADR 0015 D3, D4: a person edits and withdraws their own
   comments and their agents'; an agent only those an agent of the same person wrote
   (`403 agent_forbidden` naming the rule); a tenant administrator withdraws, never edits.
3. **The explaining comment** (ADR 0015 D2): an optional `comment` on `POST …/transitions`,
   `PATCH …/tickets/{n}` and `PUT …/tickets/{n}/body`, created in the same `Mutate`, its id on the
   act's `explained_by_comment_id`; a comment shows the act it explains through the audit index.
4. `GET …/tickets/{number}/activity` (a stream, `order`): the ticket's acts from `audit_events`
   without time-entry acts, without `downloaded` and `exported`, and without comment texts,
   behind the same predicates as the ticket.
5. **Tests:** order and its reversal; an edit writes a revision; withdrawal hides the text in the
   thread, the revisions and the activity; ADR 0015 D4's matrix with two identities and a plain
   and a flagged token each; the explaining pointer on a transition, a field change and a body
   change; the activity's exclusions; an idempotent creation; the restriction and confidential
   rows; the cross-tenant rows.
6. **Docs and records:** domain.md (thread, revisions, withdrawal, D4, the activity projection);
   ADR 0015 (D1–D4, D6 built; D5 phase 3) Status and index row.

## Related

- T12 — the field and body routes gain `comment`.
- T14 — the transition route gains `comment`.
- T19 — attachments on comments.
