---
id: T33
title: the rendered Markdown has no end-to-end check in a browser under the shell's content-security policy
state: done
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: S
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done: 2026-10-06
shipped: frontend/e2e/rendered.spec.ts, a body's headings, table, code, link and own PNG rendered and its hostile lines kept as text, with no content-security violation, in Chromium and WebKit and both colour schemes
---

## Current state

The server renders and sanitises the body, a comment, a question's options and its answer
([ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6,
[`internal/richtext`](../../backend/internal/richtext/richtext.go)), and the detail page shows it
through [`RenderedText`](../../frontend/src/app/shared/rendered-text.ts) and Angular's sanitiser.
[`rendered.spec.ts`](../../frontend/e2e/rendered.spec.ts) shows, on the built images behind the
Ingress stand-in and the shell's content-security policy, a body with a heading, a table, code, a
link and the ticket's own PNG attachment as an image, and two hostile lines — a raw `<img>` with a
handler and a `javascript:` link: the markup shows, the image loads from its attachment's path, the
hostile lines are text and nothing of them runs, and no violation is reported (`policyViolations`,
which catches one in both engines, checked against a deliberate refusal) — in Chromium and WebKit,
each in both schemes, in three local runs of the whole tier with two workers on 2026-10-06.
[trust-boundaries.md](../security/trust-boundaries.md#the-shells-content-security-policy) no longer
names the rendered Markdown as not verified, and
[rendered-markdown.md](../security/rendered-markdown.md) names the check; the look of the rendered
text on a real screen is [the developer page](../developer/rendered-markdown.md#tests)'s *Not
verified*.

## Required changes

None.

## Related

- T37 — the search, whose path watches the same policy
