---
id: T31
title: the ticket's conversation cannot be held in the browser — comments, questions, interest, links and the prerequisite tree
state: in-progress
severity: medium
security: none
threat:
urgency: next         # rule 3: severity medium, live
effort: M
blocked-by: decision
filed-from: T26
opened: 2026-10-03
decided:
done:
---

## Current state

On the branch of phase 3, the detail page writes comments, asks and answers questions (a
changed answer with `If-Match`), withdraws open questions, sets and removes the person's stake
and adds and removes links ([`conversation-forms.ts`](../../frontend/src/app/features/ticket/conversation-forms.ts),
[`interest-control.ts`](../../frontend/src/app/features/ticket/interest-control.ts),
[`conversation.service.ts`](../../frontend/src/app/core/conversation.service.ts)); each part
reloads on its own event. Missing: editing and withdrawing a comment and its revisions, editing
an open question's text, the prerequisite tree — no route returns it
([ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D6,
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D2).

## Required changes

### Independent of the open question

1. Comments: edit with `If-Match`, the revisions, withdraw
   ([ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md)).
2. Questions: edit an open question's text with `If-Match`
   ([ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)).
3. Unit tests; each write reaches a second open browser through the event stream (e2e, T29).

### Depends on the answer

4. The prerequisite tree on the detail page: each node with key, title, state, assignee and
   progress.

## Open questions

### Q1: Where is the prerequisite tree computed?

ADR 0012 D6 makes it a first-class view but names no route. **A route**
(`GET …/tickets/{n}/prerequisites`): one recursive query under the visibility predicate, one
round trip, and the same answer for the MCP server and the context export
([ADR 0040](../adr/0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md),
[ADR 0044](../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md));
backend work and an API document change. **The browser walks the links**: no backend work; one
request per level, and an agent would have to repeat the walk. Recommended: the route — the
tree is a decision aid for people and agents alike, and visibility belongs in SQL
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md)), not in a
client that silently skips a node it cannot see.

**Answer:** _open_
