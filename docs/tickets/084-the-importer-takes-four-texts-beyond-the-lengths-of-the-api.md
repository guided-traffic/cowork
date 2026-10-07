---
id: T84
title: the importer takes a threat, a recommendation, a block's reason and a state's note beyond the lengths the API holds them to
state: filed
severity: low
security: hardening
threat: would additionally cover imported texts longer than any route accepts, and a question that the importer's own key rewriting carries past 2,000 characters, which the database check then refuses as a 500 at the execution instead of an error in the report
urgency: later         # rule 4: a known fix
effort: XS
filed-from: the hardening of the import's lengths, 2026-10-07
opened: 2026-10-07
decided:
done:
shipped:
---

## Current state

The importer holds a body, options and an answer to the API's lengths (ADR 0051 D7 as amended
2026-10-07), the title to 300 and a question to 2,000 characters
([`internal/importer`](../../backend/internal/importer/)). A threat, a recommendation, a block's reason
and a state's note are taken at any length within the upload's bound, while the routes hold each to
2,000 characters (`components/schemas.yaml`). A question within 2,000 characters in its file that the
key rewriting lengthens past them meets the database's check at the execution (read from the code, not
run).

## Required changes

1. The importer checks the four texts, and a question after its rewriting, against the API's lengths, as
   errors of the file in the report; table tests in `internal/importer`.
