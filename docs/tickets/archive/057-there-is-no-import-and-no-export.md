---
id: T57
title: there is no import and no export
state: done
severity: high
security: none
threat:
urgency: release      # rule 2: phase 6 is released with it
effort: L
blocked-by:
filed-from: the start of phase 6, 2026-10-06
opened: 2026-10-06
decided: 2026-10-06
done: 2026-10-09
shipped: 0.11.0, and the import as the agent's tool in 0.13.0
---

## Current state

The import and the export of phase 6 are built, in the API and in the UI
([ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md),
[ADR 0063](../adr/0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md),
[ADR 0064](../adr/0064-one-direction-import-and-export-no-synchronisation.md)):

- **The API**: the dry run, the read of a job and its execution, the project's and the tenant's
  export ([`imports.yaml`](../../backend/api/imports.yaml)); migration 43 with the job and the
  ticket's source; the importer ([`internal/importer`](../../backend/internal/importer/));
  `cowork-mcp export`; `COWORK_MAX_IMPORT_BYTES` and `backend.config.maxImportBytes`. The integration
  tier reads this repository's whole `docs/tickets/`, the archive included, without an error, and its
  open tickets after the execution are as many as its open `state:` lines; a project exported,
  imported into an empty project and exported again is the same archive up to the keys and the times.
- **The UI**: a tenant administrator's import page of a project, `/t/<tenant>/p/<KEY>/imports`, its
  job at `…/imports/<id>` — reached from the upload icon of the project's header: the files dropped
  or chosen, the dry run, the report with its summary, what blocks the execution and *Leave them
  out*, every file with its outcome, title, type and why, state, assignee, confidential flag and
  reason, links, questions, warnings and errors with their lines, the corrections per file (left
  out, type, state with its block, assignee), the execution after a question that says it cannot be
  undone as a whole, its refusals on the files they name, the executed job leading to the backlog,
  an expired dry run offering a new one
  ([`project-import.ts`](../../frontend/src/app/features/project/project-import.ts)); the export of a
  project from the download icon of its header for whoever reads it, and of the tenant from its
  settings for its administrators, each saying how many confidential tickets the archive leaves out
  ([`imports.service.ts`](../../frontend/src/app/core/imports.service.ts)). The dashboard and the open
  decisions load again on an executed import. Unit tests beside each part; the end-to-end path
  [`import.spec.ts`](../../frontend/e2e/import.spec.ts).
- **The pages**: [docs/operations/import-and-export.md](../operations/import-and-export.md),
  [docs/security/import-and-export.md](../security/import-and-export.md),
  [docs/developer/import-and-export.md](../developer/import-and-export.md) and
  [docs/developer/frontend.md](../developer/frontend.md#the-import-and-the-export).

What has not happened:

- **Nothing has been imported for real.** Neither the sibling project's tickets nor this repository's,
  so ADR 0064 D4's note in `docs/tickets/README.md` is not written either.
- **Nobody has looked at the pages** in a browser.

## Required changes

None. The owner's look at the import page and his imports are T55; the checks at scale stay under Not verified.

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

**Answer:** (a) — the owner, 2026-10-09. `confidential: true` after `threat`; ADR 0044 D1 records it as confirmed.

### Q2: Is a `/context` document an error of the report or a skipped file?

ADR 0063 D5 lists it among the skipped files; ADR 0044 D3 says the importer refuses it, and ADR 0051 D2
lists it among the errors.

- (a) **An error at the line of its `## Links`**, so the execution waits until the person excludes
  it — recommended and built: a context export is never passed over unseen.
- (b) Skipped with its reason — nothing is refused, and nobody has to act on it.

**Answer:** (b), reversed — the owner, 2026-10-09: "Das LLM hat freie Hand … Hör auf hier alles abzusichern". A `/context` document is skipped with its reason; nothing in an import refuses. To build (required change, below).

### Q3: Does a file with an error refuse the execution, or does the execution leave it out?

ADR 0064 D3 refuses an execution while a conflict remains; ADR 0051 D2 says nothing of an error.

- (a) **Both refuse it, `409 import_conflict`, until the file is excluded or fixed** — recommended
  and built: the importer never leaves a file out on its own, and an execution without reading the
  report cannot drop a ticket quietly.
- (b) The execution leaves an erroneous file out and the report says so — one click less; a file
  lost without anybody choosing it.

**Answer:** (b), reversed — the owner, 2026-10-09: the execution imports what it can and leaves out each file with an error or a conflict, the report naming each; the agent reads the report and acts. To build.

### Q4: Is a number that a purged ticket held a conflict?

ADR 0064 D3 names the numbers the project holds; a purged ticket's row is gone, its key stays in the
audit record, and ADR 0007 D4 hands no number out twice.

- (a) **A conflict, with a warning naming the purge** — recommended and built: the key never names
  two tickets, in the audit record and in old references alike.
- (b) Imported — the purged ticket's number comes back with other text, and every old reference to
  it now points at it.

**Answer:** (b), reversed — the owner, 2026-10-09: "es darf Tickets egal welcher Nummer importieren". A number a purged ticket held is imported; a number a live ticket holds stays a conflict, left out by Q3's rule. To build.

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

**Answer:** (a), by the agent — the owner, 2026-10-09: "Das LLM sorgt dafür, dass Family-Tickets zusammenhängen oder eben nicht". The importer guesses no parent; the person's agent sets the parents after the import.

### Q6: Does the purge of an imported ticket reach the report of its import?

The executed report is kept for good and holds every file's title, threat, note and questions; the
purge of ADR 0024 D2 empties the ticket and its audit rows.

- (a) **The purge takes the ticket's file out of its job's report, the summary still counting it**
  — recommended and built (the safer of the two): what the purge removes does not stay readable
  through the API, at the cost of a report with fewer files than its summary counts.
- (b) The report stays as the upload made it — the purged text readable to the tenant's
  administrators for good.

**Answer:** not asked further — the owner ended the round on 2026-10-09; the built recommendation (a) stands, open to his objection.

### Q7: Does the UI offer the tenant's export to every member, as the API does?

The API serves `GET /api/v1/tenants/{tenant}/export` to every reader of the tenant, each archive
holding what its reader reads (ADR 0051 D6). The UI offers it in the tenant's settings to the
tenant's administrators only; every member exports each project they read from its header.

- (a) **The administrators only, in the settings** — recommended and built: the tenant's archive is
  the backup's second line (ADR 0059 D2), an administrator's concern, and the settings are the
  administrators' page; a member loses nothing they cannot reach project by project, and a member's
  scheduled export keeps the API.
- (b) Every member, in the settings or the navigation — the UI as wide as the API, for an act a
  member rarely needs.

**Answer:** not asked further — the owner ended the round on 2026-10-09; the built recommendation (a) stands, open to his objection.

## Not verified

- **The end-to-end path of the import** — written and type-checked, not run here: it needs the built
  images (`make docker-build e2e`). It is what would show that the shell's content-security policy
  lets the export's download — an object URL clicked as a link — through in Chromium and WebKit.
- **How the pages look and respond in a browser**, in either scheme, and how a report of hundreds of
  files renders: every file to create holds three selects. The unit tests render the page in jsdom
  only.
- **A large import.** Measured only at this repository's 56 files (412 KiB), dry run and execution
  under a second together. An upload of 50 MiB — its memory against the chart's 256 MiB limit, the
  dry run and the execution against the 30-second request timeout — was not tried, nor its upload
  from the browser.
- **The Ingress at `51m`.** The body limit the chart's notes and the operations page now name for
  the default `maxImportBytes` was not run through a controller, and the stand-in
  [`hack/ingress/default.conf`](../../hack/ingress/default.conf), raised to `51m` with it, was not run
  with the images (`make e2e` or the image run of build-test-lint.md).
- **A real repository other than this one**, the sibling project's above all: its frontmatter, its
  archive records without frontmatter and its families are what Q5 and the type detection meet first.
