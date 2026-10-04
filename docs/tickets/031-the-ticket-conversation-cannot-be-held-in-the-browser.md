---
id: T31
title: comments cannot be edited or withdrawn in the browser, an open question's text cannot be changed there, and there is no prerequisite tree
state: in-progress
severity: medium
security: none
threat:
urgency: next         # rule 3: severity medium, live
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-04
done:
---

## Current state

Released: the detail page writes comments, asks and answers questions (a changed answer with
`If-Match`), withdraws open questions, sets and removes the person's stake and adds and removes
links ([`conversation-forms.ts`](../../frontend/src/app/features/ticket/conversation-forms.ts),
[`interest-control.ts`](../../frontend/src/app/features/ticket/interest-control.ts),
[`conversation.service.ts`](../../frontend/src/app/core/conversation.service.ts)); each part
reloads on its own event. Missing:

- **A comment cannot be edited, its revisions read or the comment withdrawn in the browser.** The
  API has all three — `editComment` with `If-Match`, `listCommentRevisions`, `withdrawComment`
  ([`comments.yaml`](../../backend/api/comments.yaml#L73-L147)) — and nothing in the frontend
  calls them.
- **An open question's text cannot be changed in the browser:** `updateQuestion` with `If-Match`
  ([`questions.yaml`](../../backend/api/questions.yaml#L76-L81)) has no caller.
- **The prerequisite tree has no route.**
  [ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D6 serves it at
  `…/tickets/{number}/prerequisites` and its mirror, the dependents, at `…/prerequisites` read
  upward; [ADR 0018](../adr/0018-the-views-of-the-first-release.md) D2 puts it on the detail page.
  The downward walk exists for the context export: `ContextPrerequisites`
  ([`context.sql`](../../backend/internal/store/queries/read/context.sql#L24-L51)) follows the
  `blocks` edges into a ticket to a depth of eight under the visibility predicate, stops at a
  ticket the caller cannot see, and serves `/context`
  ([`context.go`](../../backend/internal/api/context.go#L118)). Nothing walks upward.

## Required changes

1. Comments: edit with `If-Match`, the revisions, withdraw
   ([ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md)).
2. Questions: edit an open question's text with `If-Match`
   ([ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)).
3. The prerequisite tree: the route and its upward mirror in the API document first
   ([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md)), the tree from
   `ContextPrerequisites` and the mirror from a walk of its own, both under the visibility
   predicate; on the detail page each node with key, title, state, assignee and progress, the
   settled ones marked, and the count of the open ones (ADR 0012 D6). Integration tests across a
   restricted project and a confidential ticket that a node the caller cannot see, and what lies
   behind it, is absent.
4. Unit tests; each write reaches a second open browser through the event stream (e2e, T29).

## Open questions

### Q1: Where is the prerequisite tree computed?

ADR 0012 D6 makes it a first-class view. **A route** (`GET …/tickets/{n}/prerequisites`): one
recursive query under the visibility predicate, one round trip, and the same answer for the MCP
server and the context export
([ADR 0040](../adr/0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md),
[ADR 0044](../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md));
backend work and an API document change. **The browser walks the links**: no backend work; one
request per level, and an agent would have to repeat the walk. Recommended: the route — the
tree is a decision aid for people and agents alike, and visibility belongs in SQL
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md)), not in a
client that silently skips a node it cannot see.

**Answer:** the route — ADR 0012 D6 decides it: the tree at `…/tickets/{number}/prerequisites`,
and its mirror, the dependents, at `…/prerequisites` read upward.
