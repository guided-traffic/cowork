---
id: T36
title: a mention in a comment tells nobody, and the inbox has no end-to-end path
state: in-progress
severity: medium
security: none
threat:
urgency: next         # rule 3: severity medium, live — a person named in a comment is not told
effort: M
blocked-by: decision
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The inbox of [ADR 0020](../adr/0020-notifications-are-an-in-app-inbox-per-person.md) exists: the
notifications written with the act for every event of D2 but one, read and unread, the retention job,
`GET /api/v1/me/inbox` with the marking read, the person-level stream with `inbox.changed`, the bell
and the page `/me/inbox` ([data-access.md](../developer/data-access.md#notifications),
[events.md](../developer/events.md#the-person-level-stream),
[frontend.md](../developer/frontend.md#the-person-level-pages)).

What is missing:

- **"I am mentioned in a comment"** (ADR 0020 D2) tells nobody, and the mentioned person does not
  join the watchers ([ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md)
  D5). D5 writes `@person` and says mentions resolve inside the tenant, but not how a comment's
  text names a person: a username exists only for a local account — a person of the identity
  provider has none ([migration 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)) —,
  an e-mail address is read only by the tenant's administrators, and a display name is not unique.
- **The e2e path** — the second identity sees the assignment in its inbox within the stream's
  latency — belongs to the end-to-end tier (T29), which does not exist.

## Required changes

### Independent of the open questions

1. The e2e path of the inbox in T29's suite: the owner assigns a ticket to the second identity, whose
   bell counts it and whose `/me/inbox` shows it within the stream's latency.

### Depends on the answers

2. A comment's mentions as Q1 decides: written and checked with the comment (each a member who sees
   the ticket, else `400`), a notification `mentioned` for each (a value added to
   `notification_reason` in a migration of its own, the reason in the API document and the UI's
   `happening`), the mentioned persons among the watchers of `ListWatchers` while the comment stands,
   an edit that adds a mention telling the new person; the MCP tool `comment` and the chat able to
   mention; ADR 0015 D5 and ADR 0020 Status updated; integration tests that a mention of a person who
   cannot see the ticket is refused and tells nobody.

## Open questions

### Q1: How does a comment name the person it mentions?

ADR 0015 D5 writes `@person`; change 2 cannot be built without knowing what the API reads as a
mention, and an agent writing through `cowork-mcp` must be able to mention as well.

- **(a) `@<username>` in the text.** Readable as typed; only a person with a local account has a
  username, so a person of the identity provider cannot be mentioned at all.
- **(b) A Markdown link the UI writes into the text, `[@Sam Rivera](person:<id>)`.** One source:
  the text carries the mention, every person can be mentioned, an edit's mentions are the new
  text's; it needs a parser on the comment's path and a form the Markdown export and the rendering
  of T33 must keep — the rendering keeps a link only to an `http`, `https`, `mailto` or relative
  address today, so `person:` would need its own rule —, and a link a person types by hand is a
  mention as well.
- **(c) A list of person ids beside the text, `{"body", "mentions": [...]}`.** The API reads no text:
  each id is checked like `asked_of` (a member who sees the ticket), every person can be mentioned,
  the UI's picker writes the name into the text and the id into the list, and the MCP tool takes the
  ids from the member list; the text and the list can disagree — a name typed without the picker
  tells nobody.

Recommended: **(c)** — it is exact and checkable the way `asked_of` already is, needs no grammar in
the comment's text and no change to the Markdown export, and works for every person whatever their
login; (a) leaves out the persons of the identity provider, the people the owner's clients will be,
and (b) puts a parser and a link scheme on the path of every comment for the same result.

**Answer:** _open_

## Related

- T35 — the person-level pages that reload on the inbox's count
- T29 — the end-to-end tier change 1 belongs to
- T33 — the rendered Markdown a mention would show in
