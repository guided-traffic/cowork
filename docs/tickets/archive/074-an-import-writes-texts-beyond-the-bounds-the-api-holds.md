---
id: T74
title: an import writes a body, options and an answer beyond the bounds the API holds every write to, and every read renders them unbounded
state: done
severity: medium
security: boundary
threat: a tenant's administrator (a session or an admin token) imports a ticket whose body, or a question's options or answer, runs far past the API's 200,000 and 100,000 characters — up to COWORK_MAX_IMPORT_BYTES, 50 MiB by default —, for example megabytes of nested quotes; every read that renders the text then spends the shared replica's memory and CPU without a bound, and the replica is killed and restarts for every tenant on it, again at the next read
urgency: now           # rule 1: fixed in the change that found it
effort: S
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-07
shipped: the importer holds a body, options and an answer to the API's lengths, migration 44 makes them checks of the database, and the renderer holds a text to its length, its nesting and its markers (0.12.0)
---

## Current state

The importer bounds the title and the question text only (internal/importer/fields.go:315-316,
questions.go:21, 123-124) and inserts body, options and answer as parsed (api/importwrite.go:150-151,
222-226); no CHECK holds `tickets.body` or `questions.options`/`answer` (migrations 8, 10), unlike
comments (migration 11). Every read renders (api/rendered.go:87, questions.go:38, 55); `richtext.HTML`
has no length or depth bound, and the request timeout does not stop a rendering. Not measured: the size
at which a replica falls.

## What the fix found beside it

Within the API's own lengths, a text whose every read took tens of seconds of a replica's CPU could be
written by any member with write scope (a 200,000-character body of `*a_ ` took 49 s, `a**b` + `c* `
80 s); the renderer now holds a text to 2,000 emphasis runs, 1,000 link openers with their brackets
within 4,096 bytes, 250 HTML comments and declarations and 1,000 lines of reference definitions, and
shows a text over 200,000 characters escaped in a `<pre>`; the worst of more than fifty input families
takes 0.09 s, and 14,674 real Markdown files render as before (ADR 0011 D6).

## Required changes

1. The importer refuses a body over 200,000 characters and options or an answer over 100,000 as errors of
   the file in the report.
2. `richtext.HTML` renders nothing beyond those bounds (the text as plain text instead, or a notice) and
   caps the nesting depth.
3. Migration 44 adds the length checks to `tickets.body`, `questions.options` and `questions.answer`
   (`NOT VALID`, then validated — what no supported path wrote passes).
4. `TestPathologicalInputRendersQuickly` runs at 200,000 characters with a memory bound.
