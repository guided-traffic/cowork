---
id: T23
title: the /markdown grammar v1 leaves out links, writes people as display names and fixes a question format nobody has round-tripped yet — to be reviewed after experience
state: done
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix (the owner's answers)
effort: S
blocked-by:
filed-from: the owner's choice of a v1 grammar, phase-2 conversion
opened: 2026-10-02
decided: 2026-10-06
done: 2026-10-06
shipped: /markdown writes the assignee as Name <identity> (local:<username>, oidc:<issuer>#<subject>) with golden files, unit and integration tests; ADR 0044 D1 and its Context and ADR 0051 D4 amended
---

## Current state

Decided by the owner's three answers below and extracted into
[ADR 0044](../../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
(D1, the Context, the Residual risks) and
[ADR 0051](../../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
D4; the grammar page is [markdown-grammar.md](../../developer/markdown-grammar.md).

- **Links** stay out of `/markdown`. The project export writes them once each in a links manifest
  beside the tickets (ADR 0051 D4), built with the export itself in phase 6. What the manifest
  holds and how the importer reads it is the implementer's, open to the owner's objection; a
  restore through the importer keeps the links within a project only (ADR 0051 Residual risks).
  [ADR 0059](../../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
  D2 never said the backup loses the links and is unchanged.
- **A person** is written `Name <identity>`
  ([`person`](../../../backend/internal/markdown/markdown.go)): `local:<username>` for a local
  account, `oidc:<issuer>#<subject>` for a person of the identity provider, read by the query
  `ExportPerson` in [`exportAssignee`](../../../backend/internal/api/export.go); a person who
  leaves the reader's sight between the two reads is written by neither. The identity strings,
  the issuer written out among them, and the rest of ADR 0044 D1's rules on persons are the
  implementer's, open to the owner's objection. `assignee` is the only key of the grammar that
  writes a person. Nothing in `internal/tools`, `cowork-mcp` or the UI parses the field back: the
  tools hand the context document on as text.
- **The question form and the state notes** stay as v1 writes them until the importer of phase 6
  has read this repository's tickets with them (ADR 0044 D1).

Verified: `TestPerson`, `TestRender` and `TestRenderContext` with the golden files
`every-key.md` (a person of the provider) and `context-full.md` (a local account);
`TestExportAssignee` (a person out of sight); `TestMarkdownExport` and
`TestMarkdownExportWritesAPersonOfTheIdentityProvider` against PostgreSQL.

## Required changes

None left.

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

**Answer:** (b) — into the project export only, as a links manifest beside the tickets, each
link once; `/markdown` stays the single ticket. ADR 0051 D4 and ADR 0044's Context say so; the
manifest is built with the export in phase 6, nothing is built now.

### Q2: How does the export write a person?

- **(a) The display name,** resolved by name on import (v1).
- **(b) The stable identity** (`local:<username>`, or the identity provider's): resolvable, less
  readable.
- **(c) Both, as `Name <identity>`,** the way git writes an author.

Recommended: **(c)** once identities exist (phase 4) — readable for a person and unambiguous for
the importer.

**Answer:** (c) — `Name <identity>`, the way git writes an author; the identity string chosen
from what the code holds, for example `local:<username>` for a local account and, for a person of
the identity provider, the stable key that already exists (one issuer per installation, ADR 0029);
the choice and its reason stated in ADR 0044 D1.

### Q3: Does the question and note form stay as v1 writes it?

- **(a) Keep v1** until the importer has parsed this repository's tickets with it.
- **(b) The recommendation inside the options text,** as the tickets page writes it, without a
  line of its own.
- **(c) A key of cowork's own for the verification note** (`verification`) instead of
  `shipped`, which the tickets page uses for "what shipped".

Recommended: **(a)** — the importer of phase 6 is the first real reader; what it cannot map is
the evidence for (b) or (c).

**Answer:** (a) — keep v1 until the importer of phase 6 has read this repository's tickets with
it; recorded in ADR 0044 D1, nothing to build.
