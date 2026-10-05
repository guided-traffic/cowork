---
id: T33
title: the rendered Markdown has no end-to-end check in a browser under the shell's content-security policy
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: S
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The server renders and sanitises the body, a comment, a question's options and its answer
([ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6,
[`internal/richtext`](../../backend/internal/richtext/richtext.go)): the body on
`GET …/tickets/{number}/body`, the rest beside the Markdown; the detail page shows it through
[`RenderedText`](../../frontend/src/app/shared/rendered-text.ts) and Angular's sanitiser. The unit
tests feed both server lines hostile input and hold every output to the allow-list; the integration
tier does the same through the API; the frontend's tests run on jsdom. No test has shown a rendered
text — its images, its links, its styles — in a real browser under the shell's content-security
policy, and [trust-boundaries.md](../security/trust-boundaries.md#the-shells-content-security-policy)
says so.

## Required changes

1. An end-to-end test ([`frontend/e2e/`](../../frontend/e2e/), ADR 0056) on the built images: a
   ticket whose body holds headings, a table, code, a link and an image of the ticket's own PNG
   attachment, and a hostile line (`<img src=x onerror=…>`, a `javascript:` link): the page shows
   the rendering, the image loads from the attachment's path, the hostile line is text, no
   content-security violation is reported, in Chromium and WebKit and both colour schemes; then the
   sentence of trust-boundaries.md that names the rendered Markdown as not verified goes.

## Not verified

- The look of the rendered text in both colour schemes on a real screen — `make dev`, by the owner.

## Related

- T37 — the search, whose results page is not walked end to end either
