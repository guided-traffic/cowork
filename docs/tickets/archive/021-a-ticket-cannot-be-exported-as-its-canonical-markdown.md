---
id: T21
title: a ticket cannot be exported as its canonical Markdown document, and the key list the importer will read is not written down
state: done
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix (the ADRs)
effort: M
blocked-by: T19
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: GET …/markdown with grammar v1 and golden files, every call recorded
---

## Current state

Decided by [ADR 0044](../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D1, D5, D6, [ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D4, [ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D5,
[ADR 0050](../adr/0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
D2 and [ADR 0055](../adr/0055-english-only-browser-locale-for-dates-and-numbers.md) D3, D4.

- No export. The plan named `GET …/tickets/{key}/markdown`; ADR 0044 D1 and
  [ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D1 place it at
  `…/projects/{KEY}/tickets/{number}/markdown`, and a key reaches it through the resolver.
- ADR 0044 D1 lists the frontmatter keys — `key`, `title`, `type`, `state`, `severity`,
  `security`, `threat`, `urgency`, `effort`, `progress`, `assignee`, `parent`, `opened`,
  `decided`, `done`, "and the transition note or reason where the state has one" — then the body
  and `## Open questions` with `**Answer:**` lines in ADR 0011 D4's order; D6 adds the
  attachments' names. It names no key for the note or reason, the block data or the attachments,
  and leaves links to `/context` (ADR 0044's Context, D2). The owner's choice for phase 2: a v1
  that follows ADR 0044 as written and spells keys as the [tickets page](README.md#frontmatter)
  does, reviewed after experience (T23).
- The transition notes and reasons live on the acts ([ADR 0009](../adr/0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)
  D5, T14), not on the ticket.
- The document contains entities with versions of their own; its `ETag` serves `If-Match` (ADR
  0050 D2), not a cache.

## Required changes

1. `GET …/tickets/{number}/markdown`: `text/markdown; charset=utf-8`, `ETag` the ticket's
   version, `Cache-Control: no-store`, never `304`; the restriction and confidential `404`; any
   role, `read`; every call writes the act `exported` through an audited read — in phase 2 every
   caller is a token (ADR 0044 D5, ADR 0026 D5).
2. **The v1 grammar**, deterministic: frontmatter keys in this order, absent keys omitted, values
   as the API spells them (ADR 0055 D4), dates as UTC dates (ADR 0055 D3), strings YAML-quoted
   where needed —
   - `key`, `title`, `type`, `state`, `severity`, `security`, `threat` (when `security` is not
     `none`), `urgency` (the effective value), `effort`, `progress`, `assignee` (the display
     name), `parent` (the full key), `opened`, `decided`, `done`;
   - the state's note under the tickets page's key: `shipped` for `done` (the verification note
     of the `done` act), `dropped-reason` for `dropped`; for `blocked` the tickets page's
     `blocked-by` with the block kind, and `blocked-reason` and `blocked-from` for the text and
     the origin, which the tickets page has no key for;
   - `attachments` (names, ADR 0044 D6);

   then the body; then `## Open questions`, always written and the last heading of that name,
   with `### Q<n>: <question>` in number order, the options verbatim, `**Recommendation:** …`
   when there is one, and `**Answer:**` with the answer, `_open_` or `_withdrawn_`.
3. **Tests:** golden files — every key, a blocked ticket, a done ticket with its note, a dropped
   one, questions open, answered and withdrawn, a body that contains its own `## Open questions`
   heading, non-ASCII text, a ticket with attachments; one `exported` act per call; the
   restriction and confidential rows; the cross-tenant rows.
4. **Docs and records:** new docs/developer/markdown-grammar.md and its row in the developer
   README; tokens.md (an export through a token is recorded); the README's API section; ADR 0044
   D1 amended in place with the v1 key list (and D6 with the key name); ADR 0044 (D1, D5, D6
   built; D2 and D4 phase 5; D3 phase 6) and ADR 0011 D4 (the export half) Status and index rows.

## Related

- T14 — the notes and reasons on the transition acts.
- T15 — the questions it renders.
- T19 — the attachment names.
- T23 — the grammar's review after experience.
