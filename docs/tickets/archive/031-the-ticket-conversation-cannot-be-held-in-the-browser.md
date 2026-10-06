---
id: T31
title: the conversation's writes and the prerequisite tree have no end-to-end path through a second browser
state: done
severity: medium
security: none
threat:
urgency: later        # rule 4: the fix is decided — the path of T29, and a look in the browser
effort: S
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-04
done: 2026-10-06
shipped: frontend/e2e/conversation.spec.ts, a comment written, edited and withdrawn, a question's text edited and a new blocker seen by a second identity's open page; and a withdrawn comment's earlier texts closed on that page (comment-item.ts, with its unit tests)
---

## Current state

The detail page edits a comment over its version, shows its earlier texts and withdraws it, and the
asker edits an open question's text over its version
([`frontend.md`](../developer/frontend.md#the-detail-page)); the prerequisite tree is shown on the
page and reloads on a link of the ticket. [`conversation.spec.ts`](../../frontend/e2e/conversation.spec.ts)
holds a member's page of a ticket open while the administrator writes a comment, edits it and
withdraws it, edits the question they asked and links another ticket as blocking this one: the
member's page shows each through the event stream — the comment's new text and on request its
earlier one, *withdrawn* without either text, the question's new text, the tree with the blocker —
in Chromium and WebKit, each in both schemes, in three local runs of the whole tier with two workers
on 2026-10-06.

The path found that a page whose comment history was open kept showing the earlier text after the
comment was withdrawn: the earlier texts now belong to the version they were read for, and a newer
version — an edit, a withdrawal — closes them
([`comment-item.ts`](../../frontend/src/app/features/ticket/comment-item.ts), two tests in
`comment-item.spec.ts` that fail without it). The description lives in
[testing.md](../developer/testing.md#end-to-end-tests); what nobody has looked at on a screen, and
how a large tree reads, is [frontend.md](../developer/frontend.md#the-detail-page)'s *Not verified*.

## Required changes

None.
