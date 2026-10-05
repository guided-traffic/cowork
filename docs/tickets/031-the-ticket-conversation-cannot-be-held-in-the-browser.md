---
id: T31
title: the conversation's writes and the prerequisite tree have no end-to-end path through a second browser
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: the fix is decided — the path of T29, and a look in the browser
effort: S
blocked-by: T29
filed-from: T26
opened: 2026-10-03
decided: 2026-10-04
done:
---

## Current state

The detail page edits a comment over its version, shows its earlier texts and withdraws it — its
author, or a tenant administrator after a confirmation
([`comment-item.ts`](../../frontend/src/app/features/ticket/comment-item.ts)) —, and the asker
edits an open question's text over its version
([`conversation-forms.ts`](../../frontend/src/app/features/ticket/conversation-forms.ts)
`EditQuestion`). The prerequisite tree is `GET …/tickets/{number}/prerequisites`, its upward
reading `direction=up`, walked in SQL under the visibility predicate a link once per depth
([`domain.md`](../developer/domain.md#the-prerequisite-tree)), shown on the detail page
([`prerequisite-tree.ts`](../../frontend/src/app/features/ticket/prerequisite-tree.ts)); the
integration tier holds a confidential ticket and a restricted project out of it, with what lies
behind them (`TestPrerequisiteTreeLeavesOutWhatTheCallerCannotSee`). The context document reads
the same tree. Missing:

- **A second browser.** That each of these writes reaches another open page through the event
  stream is proven by unit tests of the reloads only, not end to end — the tier is T29.

## Required changes

1. The e2e path of T29: a comment edited and withdrawn, a question's text edited and a link that
   changes the tree, each seen by a second identity's open page.

## Not verified

- The comment's editor, its history and its withdrawal, the question's editor and the tree card
  were built against unit tests and the page's patterns, with the preset's tokens only; nobody has
  looked at them in a browser, in either scheme.
- The tree on a large graph in the browser: the route answers a graph of forty tickets and a
  hundred and eighty links in milliseconds in the integration tier; the card draws at most 200
  nodes and says when the tree goes on. How a tree of hundreds of nodes reads is not judged.
