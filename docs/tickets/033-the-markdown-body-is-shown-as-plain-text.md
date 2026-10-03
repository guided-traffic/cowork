---
id: T33
title: the Markdown body is shown as plain text — there is no server-side sanitiser and no rendered body
state: decided
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The detail page of T28 shows the body, comments and answers as text with their line breaks
(`white-space: pre-wrap`), never as HTML. [ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D6 decides that rendered Markdown is sanitised on the server — no script, no inline handler, no
raw HTML, `rel="noopener"` on links, images only from the ticket's own raster attachments — and
nothing renders Markdown yet; it is carried over from phase 2.

## Required changes

1. The server renders and sanitises the body (and comments and answers) and returns the HTML
   beside the Markdown, or on a route of its own; the API document says which.
2. The detail page shows the rendered HTML; editing the body stays Markdown, written with
   `If-Match` (`replaceTicketBody`).
3. Tests with hostile input: script tags, event handlers, `javascript:` and `data:` links, raw
   HTML, images from elsewhere; the security page that names the sanitiser and what it does not
   cover.
