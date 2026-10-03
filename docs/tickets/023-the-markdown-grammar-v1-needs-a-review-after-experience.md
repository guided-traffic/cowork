---
id: T23
title: the /markdown grammar v1 leaves out links, writes people as display names and fixes a question format nobody has round-tripped yet — to be reviewed after experience
state: filed
severity: medium
security: none
threat:
urgency: icebox       # rule 5: needs a product call (after experience with the export)
effort: S
blocked-by: product
filed-from: the owner's choice of a v1 grammar, phase-2 conversion
opened: 2026-10-02
decided:
done:
---

## Current state

Phase 2 built the v1 grammar ([`internal/markdown`](../../backend/internal/markdown/), golden
files in its `testdata/`): [ADR 0044](../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D1's keys as written, spelled as the tickets page spells them, the attachment names of D6, and
the question form of [ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D4. The owner chose to gain experience with it before fixing more.

- **Links are not in `/markdown`** (ADR 0044's Context and D2 put them into `/context`).
  [ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
  D4 makes the project export a mirror archive of tickets as `/markdown`, and
  [ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
  D2 makes that export the backup's second line: a backup restored through the importer loses
  the link graph.
- **People are display names** (ADR 0044 D1's `assignee`). Display names are not unique, so an
  import resolves them by name and reports what it cannot (ADR 0044's residual risks); stable
  identities arrive with the login
  ([ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
  D5, [ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)
  D2).
- **The question form and the state notes** — `### Q<n>:`, the options verbatim,
  `**Recommendation:**`, `**Answer:**` with `_open_` or `_withdrawn_`, and `shipped` carrying the
  `done` act's verification note — are v1's reading of ADR 0011 D4 and the tickets page; nothing
  has parsed them back yet.

## Required changes

### Depends on the answers

1. Each answer amends ADR 0044 in place (and ADR 0051 D4 for Q1 (b)), changes the renderer of
   `internal/markdown` and its golden files, and becomes the grammar the importer of phase 6
   reads.

## Open questions

### Q1: Do links enter the export?

- **(a) Into `/markdown`'s frontmatter** (`blocked-by`, `found-in`, `relates-to`, `duplicates`),
  the tickets page's shape: amends ADR 0044's Context and D1; the importer must not create a
  link twice from its two ends.
- **(b) Into the project export only, as a links manifest beside the tickets** (ADR 0051 D4):
  `/markdown` stays the single ticket; the backup carries the graph once.
- **(c) Neither:** the backup loses the links, and the backup page says so.

Recommended: **(b)** — it closes the backup's gap without changing what one ticket's document is,
and writes each link once instead of on both ends.

**Answer:** _open_

### Q2: How does the export write a person?

- **(a) The display name,** resolved by name on import (v1).
- **(b) The stable identity** (`local:<username>`, or the identity provider's): resolvable, less
  readable.
- **(c) Both, as `Name <identity>`,** the way git writes an author.

Recommended: **(c)** once identities exist (phase 4) — readable for a person and unambiguous for
the importer.

**Answer:** _open_

### Q3: Does the question and note form stay as v1 writes it?

- **(a) Keep v1** until the importer has parsed this repository's tickets with it.
- **(b) The recommendation inside the options text,** as the tickets page writes it, without a
  line of its own.
- **(c) A key of cowork's own for the verification note** (`verification`) instead of
  `shipped`, which the tickets page uses for "what shipped".

Recommended: **(a)** — the importer of phase 6 is the first real reader; what it cannot map is
the evidence for (b) or (c).

**Answer:** _open_
