---
id: T36
title: a mention in a comment tells nobody, and the inbox has no end-to-end path
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: decided and built; the e2e path waits for its first run, the owner reviews the result
effort: M
blocked-by: human
filed-from: T26
opened: 2026-10-03
decided: 2026-10-05
done:
---

## Current state

The inbox of [ADR 0020](../adr/0020-notifications-are-an-in-app-inbox-per-person.md) exists for every
event of D2, the mention included since this branch
([data-access.md](../developer/data-access.md#notifications),
[events.md](../developer/events.md#the-person-level-stream),
[frontend.md](../developer/frontend.md#the-person-level-pages)).

The mention, Q1 answered (c) on the recommendation, the owner reviewing the result
([ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D5,
[ADR 0013](../adr/0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D6 and ADR 0020,
amended 2026-10-05):

- A comment carries `mentions`, a list of person ids beside its text, on its creation and on an
  edit (without the list an edit keeps it); each id is checked like `asked_of` — a member who sees
  the ticket — else `400` at `/mentions/<i>`, and nothing is told
  ([`comments.go`](../../backend/internal/api/comments.go) `checkMentions`;
  [migration 36](../../backend/internal/store/migrations/000036_comment_mentions.up.sql): the column
  and the reason `mentioned`).
- A mentioned person is told `mentioned`, and one act tells a person once (`deliver`), so a watcher
  the comment mentions is told that, not also `commented`; an edit tells only the persons it adds.
- "Makes them a watcher" is built as the watcher set of ADR 0013 D6 — the persons a comment that is
  not withdrawn mentions are among `ListWatchers` while it mentions them — and not as a `watch`
  stake in their name: a stake is the person's own (ADR 0013 D1, D2, D4). This is a reading of the
  records the owner may want to revisit.
- The UI: `@` in the comment box and in a comment's editor opens the members who see the ticket
  ([`mention-list.ts`](../../frontend/src/app/shared/mention-list.ts),
  [`mentions.ts`](../../frontend/src/app/shared/mentions.ts)); a pick writes `@<name> ` and mentions
  the person while the text holds the name. Of a restricted project the picker offers every member
  — a member cannot read the access list — and the server refuses one who cannot see the ticket.
  The inbox says "mentioned you in a comment".
- The MCP tool `comment` (and with it the chat) takes `mentions` as `me`, usernames, display names
  or ids, resolved through the member list as `open_question`'s `asked_of`.
- A mention is plain `@Name` text, so the rendered `body_html` shows it as written and links nobody
  (`TestMarkdownRenders`); a deleted ticket tells nobody, its mentions included.
- Tests: `TestAMentionTellsThePersonAndMakesThemAWatcher`,
  `TestAMentionOfAPersonWhoCannotSeeTheTicketIsRefused`, `TestAMentionOnADeletedTicketTellsNobody`,
  the MCP step of
  `TestTheMCPServerRunsTheWorkingDay`, `TestCommentMentions`, the replay of a stored comment without
  `mentions` (`server_test.go`), and the frontend's `mentions.spec.ts`, `mention-list.spec.ts`,
  `conversation-forms.spec.ts`, `comment-item.spec.ts`, `conversation.service.spec.ts`,
  `inbox.spec.ts`.

The README reference, [domain.md](../developer/domain.md#comments-and-the-activity-list),
[data-access.md](../developer/data-access.md#notifications),
[frontend.md](../developer/frontend.md#the-detail-page), [mcp.md](../developer/mcp.md) and
[tenancy.md](../security/tenancy.md) (H-58: the refusal tells the writer whether a member sees the
ticket, as `asked_of` does) carry it.

The e2e path of the inbox — the second identity's bell counting the assignment and its `/me/inbox`
showing it within the stream's latency — is part of T29's `assigned.spec.ts`, which passes (run 37285901009 of commit `65337eb`).

## Required changes

1. The owner's review of the built result: the watcher reading above, and the picker in a browser
   (`make dev`).

## Not verified

The picker has not been used in a browser — `make dev` was not run for this work; its keyboard and
the screen reader's reading of `aria-activedescendant` are verified in jsdom only.

## Related

- T29 — the end-to-end tier the inbox's path belongs to
- T33 — the rendered Markdown a mention shows in
