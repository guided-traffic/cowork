---
id: T57
title: there is no import and no export
state: in-progress
severity: high
security: none
threat:
urgency: release      # rule 2: phase 6 is released with it
effort: L
filed-from: the start of phase 6, 2026-10-06
opened: 2026-10-06
decided: 2026-10-06
done:
---

## Current state

The API of phase 6 is built ([ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md),
[ADR 0063](../adr/0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md),
[ADR 0064](../adr/0064-one-direction-import-and-export-no-synchronisation.md)): the dry run, the
read of a job and its execution, the project's and the tenant's export
([`imports.yaml`](../../backend/api/imports.yaml)); migration 41 with the job and the ticket's
source; the importer ([`internal/importer`](../../backend/internal/importer/)); `cowork-mcp export`;
`COWORK_MAX_IMPORT_BYTES` and `backend.config.maxImportBytes`; the pages
[docs/operations/import-and-export.md](../operations/import-and-export.md),
[docs/security/import-and-export.md](../security/import-and-export.md) and
[docs/developer/import-and-export.md](../developer/import-and-export.md). The integration tier reads
this repository's whole `docs/tickets/`, the archive included, without an error, and its
open tickets after the execution are as many as its open `state:` lines; a project exported,
imported into an empty project and exported again is the same archive up to the keys and the times.

What a person cannot do yet:

- **The UI has no import page and no export button.** Everything goes through the API with a token,
  as the operations page shows. The client is generated (`frontend/src/app/api/fn/imports/`, the
  `Import*` and `Export*` models).
- **Nothing has been imported for real.** Neither the sibling project's tickets nor this
  repository's, so ADR 0064 D4's note in `docs/tickets/README.md` is not written either.

## Required changes

1. **The import page**, for a tenant administrator in a project
   ([ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
   Consequences):
   - the upload: one or more files, a `tar.gz`, a `zip` or Markdown files, to
     `POST …/projects/{project}/imports` as parts named `file`;
   - the report: `summary`, then `files` in their order, each with `outcome`, `reason`, `title`,
     `key`, `type` with `type_reason`, `state`, `block` or `block_candidate`, `columns`, `assignee`,
     `parent`, `note`, `questions`, `links`, `confidential` with `confidential_reason`, and the
     `warnings` and `errors` with their `line`;
   - the corrections per file — exclude, `type`, `state` with its `block`, `assignee` —, sent with
     `POST …/imports/{import}/execution`, and the refusals: `400` at `/corrections/<i>/…`,
     `409 import_conflict` naming each file as `file:<path>`, `409 import_executed`, `404` for a dry
     run past its twenty-four hours;
   - the executed job's report, every `create` now `created`.
   The comment on `project.changed` in
   [`event-stream.service.ts`](../../frontend/src/app/core/event-stream.service.ts) names the sort by
   the score only; an import sends it too, with the kind `imported`, and the client reloads on either.
2. **The export button** on the project — `GET …/projects/{project}/export`, a download — and, for the
   tenant, `GET /api/v1/tenants/{tenant}/export`; the manifest's `confidential_not_included` is worth
   saying beside the button.
3. **The owner imports the sibling project's tickets**, then **this repository's**, by the steps of
   the operations page, and writes ADR 0064 D4's note into `docs/tickets/README.md` the same day.
4. **The checks at scale** (below).

## Open questions

### Q1: Does grammar v1 write the confidential flag, as `confidential: true`?

ADR 0044 D1 listed no key for it, and ADR 0065 D7 has the importer set the flag by the rule of the
source — the `local_` prefix or an unfixed `live` or `boundary` class. Without a key, a ticket an
administrator flagged whose class is `none` or `hardening` comes back from an export readable by
every member.

- (a) **`confidential: true` after `threat` while the flag is set** — recommended and built: the
  round trip stays lossless (ADR 0044 D1, ADR 0051 D5), and only a reader of the ticket gets its
  document, so the key tells nobody more than the ticket does.
- (b) No key; the importer applies the source's rule alone — a restore weakens a flag an
  administrator set.
- (c) A manifest of the confidential keys beside the tickets — the same information in a second
  place, which the import would have to join.

**Answer:** _open_

### Q2: Is a `/context` document an error of the report or a skipped file?

ADR 0063 D5 lists it among the skipped files; ADR 0044 D3 says the importer refuses it, and ADR 0051 D2
lists it among the errors.

- (a) **An error at the line of its `## Links`**, so the execution waits until the person excludes
  it — recommended and built: a context export is never passed over unseen.
- (b) Skipped with its reason — nothing is refused, and nobody has to act on it.

**Answer:** _open_

### Q3: Does a file with an error refuse the execution, or does the execution leave it out?

ADR 0064 D3 refuses an execution while a conflict remains; ADR 0051 D2 says nothing of an error.

- (a) **Both refuse it, `409 import_conflict`, until the file is excluded or fixed** — recommended
  and built: the importer never leaves a file out on its own, and an execution without reading the
  report cannot drop a ticket quietly.
- (b) The execution leaves an erroneous file out and the report says so — one click less; a file
  lost without anybody choosing it.

**Answer:** _open_

### Q4: Is a number that a purged ticket held a conflict?

ADR 0064 D3 names the numbers the project holds; a purged ticket's row is gone, its key stays in the
audit record, and ADR 0007 D4 hands no number out twice.

- (a) **A conflict, with a warning naming the purge** — recommended and built: the key never names
  two tickets, in the audit record and in old references alike.
- (b) Imported — the purged ticket's number comes back with other text, and every old reference to
  it now points at it.

**Answer:** _open_

### Q5: How does the importer recognise a family ticket and its children?

ADR 0008 D5 makes a family ticket a parent with its findings as children. A repository's ticket file
names no parent, and `docs/tickets/README.md` has no rule that says which files are a family's.

- (a) **It does not; the person sets the parents after the import** — recommended and built: nothing
  is guessed, at the cost of one edit per child in the detail page, which chooses a parent already.
- (b) A correction `parent` per file at the execution — one field more in the API and the import
  page, checked as a parent is (same project, no cycle); worth it if the owner's imports hold many
  families.
- (c) A convention in the body — a list of `T<n>` under a heading — read as children: a heuristic
  over prose the rules do not define, which makes wrong parents silently.

**Answer:** _open_

### Q6: Does the purge of an imported ticket reach the report of its import?

The executed report is kept for good and holds every file's title, threat, note and questions; the
purge of ADR 0024 D2 empties the ticket and its audit rows.

- (a) **The purge takes the ticket's file out of its job's report, the summary still counting it**
  — recommended and built (the safer of the two): what the purge removes does not stay readable
  through the API, at the cost of a report with fewer files than its summary counts.
- (b) The report stays as the upload made it — the purged text readable to the tenant's
  administrators for good.

**Answer:** _open_

## Not verified

- **A large import.** Measured only at this repository's 56 files (412 KiB), dry run and execution
  under a second together. An upload of 50 MiB — its memory against the chart's 256 MiB limit, the
  dry run and the execution against the 30-second request timeout — was not tried.
- **The Ingress at `51m`.** The body limit the chart's notes and the operations page now name for
  the default `maxImportBytes` was not run through a controller, and the stand-in
  [`hack/ingress/default.conf`](../../hack/ingress/default.conf), raised to `51m` with it, was not run
  with the images (`make e2e` or the image run of build-test-lint.md).
- **A real repository other than this one**, the sibling project's above all: its frontmatter, its
  archive records without frontmatter and its families are what Q5 and the type detection meet first.
