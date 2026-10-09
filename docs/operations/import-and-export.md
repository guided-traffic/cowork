# Importing tickets and exporting them

How a tenant's administrator brings a repository's Markdown tickets — or an earlier export — into a
project, and how anyone who reads a project takes it out again as files: for a move, and as the
second line of a backup. The routes, their fields and the variable are in the
[README's reference](../../README.md#api-backend); what an import lets in and an export lets out is
[docs/security/import-and-export.md](../security/import-and-export.md); why it works this way is
[ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md),
[ADR 0063](../adr/0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md)
and [ADR 0064](../adr/0064-one-direction-import-and-export-no-synchronisation.md). In the browser, a
tenant's administrator imports on the project's import page and anyone who reads a project exports
it from the project's header ([below](#in-the-browser)); the steps after that are the same through
the API, with a token.

**After the import, cowork is the source.** The files in the repository are history or are
removed — the repository decides. cowork reads them once, never watches them, and never writes
to a repository; importing the same files again is a conflict, never an update (ADR 0064 D1, D3).

## Before you import

- **Who.** The role `admin` in the tenant. Through a token, an `admin`-scoped personal access token
  of yours that is not an agent's, sent without `X-Cowork-Agent`: an agent never imports
  ([ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D6).
- **Where.** The project exists, is not archived, and is the one the tickets belong to: an import
  goes into one project, and keeps the numbers of its files — `117-….md` becomes `<tenant>/<PROJECT>-117`.
  A number the project already holds is a conflict, so a repository's tickets go into a project of
  their own or into one whose numbers do not overlap.
- **How much.** `COWORK_MAX_IMPORT_BYTES` (`backend.config.maxImportBytes`, 50 MiB `# default`)
  bounds the upload and what its files hold unpacked; at most 10,000 files. The Ingress controller
  must pass a body that large ([installation.md, expose it](installation.md#expose-it): `51m` with
  the defaults), and the dry run and the execution must each finish within `COWORK_REQUEST_TIMEOUT`
  (30 seconds `# default`). An upload is held in memory, one import at a time per replica: keep
  the backend's memory limit well above the variable. A large repository is imported directory by
  directory.

## In the browser

**Import.** On the project's board or backlog, the upload icon of its header, *Import tickets*, opens
`/t/<tenant>/p/<KEY>/imports` — a tenant's administrator sees it, for a project that is not
archived. Drop the files onto the page or choose them — a `tar.gz` or a `zip` of the directory, or
the Markdown files themselves —, and *Start the dry run*. Its report opens at an address of its own,
`/t/<tenant>/p/<KEY>/imports/<id>`, which a reload or a bookmark keeps for the dry run's twenty-four
hours:

- the summary counts the files by outcome, and says how many tickets would be open and confidential
  and the highest number;
- a panel names every file that blocks the execution — a conflict, or a file with an error — with
  what blocks it, and *Leave them out*;
- the table lists every file with its outcome, title, detected type and why, state, assignee,
  confidential flag, questions, and a line for each warning, error with its line, link, note or
  reason; *Leave out* takes a file out, and on a file to create, or one with an error, the type,
  the state — `blocked` with its kind, reason and origin — and the assignee are corrected in
  place.

*Import N tickets* asks first, then executes with the corrections. What the execution refuses shows
on the files it names, and the report stays as it was, so a file is left out and the execution
started again. Done, the page counts what it created and leads to the backlog and the board. A dry
run past its day says so and offers a new one.

**Export.** The download icon of a project's header, *Export the tickets*, saves the project's archive
under the name the server gives it, for anybody who reads the project; a tenant's settings have
*Export the tenant* for its administrators. Beside each the page says how many tickets the archive
holds and how many confidential tickets it leaves out because its reader cannot read them. The
archive is the one the API answers ([Exporting](#exporting)).

## The steps

The same through the API, with a token.

**1. Pack the files.** A `tar.gz` or a `zip`, or the Markdown files themselves as several parts of
one upload. Whatever it holds is listed; only ticket files are read — `NNN-<slug>.md`,
`local_NNN-<slug>.md` and an export's `<PROJECT>-<n>.md` —, and everything else is skipped with
its reason.

```bash
tar czf tickets.tar.gz docs/tickets                                    # example: open and archived tickets
```

**2. Make the dry run.** It reads every file, analyses it against the project as it stands, and
imports nothing:

```bash
curl -sS -X POST -H "Authorization: Bearer $COWORK_TOKEN" \
  -F file=@tickets.tar.gz \
  https://cowork.example.com/api/v1/tenants/acme/projects/VKO/imports   # example host, tenant and project
```

The answer is `201` with the job and its report; `Location` names the job, and
`GET …/imports/{import}` reads it again for twenty-four hours. Then it expires: a read and an
execution answer `404`, and the next dry run starts over.

**3. Read the report.** `summary` counts the files by outcome and says how many open and how many
confidential tickets the execution would create and the highest number it brings. Each entry of
`files` is one file of the upload, in its order:

| `outcome` | Means | What to do |
|---|---|---|
| `create` | the execution creates it as the entry shows | read it: `type` with `type_reason`, `state`, `columns`, `assignee`, `parent`, `note`, `questions`, `links`, `confidential` with `confidential_reason`, `warnings` |
| `conflict` | the project holds its number already, as `conflict` names, or a purged ticket held it | exclude it; the project's ticket stays as it is |
| `error` | it cannot be imported as it stands; `errors` name the field and the line — a body longer than 200,000 characters, or a question's options or answer longer than 100,000, as the import would write them, is one | exclude it, or fix the file and make a new dry run |
| `skip` | it is no ticket file; `reason` says why | nothing: a skipped file never imports |

The report says what it decided, never silently: a type detected from the title and the rule
that matched; a record without frontmatter imported as one done task, severity `low`, security
`none`, effort `S`; a done ticket without a `shipped:` line closed with the note `imported from
archive; the source carried no verification note`, a dropped one without a reason with `imported
from archive; the source carried no reason`; a `blocked-by:` that names a kind or an ADR beside a
state that is not `blocked`, offered as `block_candidate` and kept in the body under
`## Related`; a reference to a ticket outside the upload and the project, kept under `## Related`
with its source path; an assignee that names no member who sees the project, left unassigned; the
rule that set or left the confidential flag.

**4. Correct and execute.** The execution takes the job's id and a list of corrections, each
naming a file by its `path`: `exclude`, or a `type`, a `state` — `blocked` with its `block`,
`{"kind", "reason", "from"}`, whose kind is not `ticket` —, or an `assignee`, a member's id or
`null`:

```bash
cat > corrections.json <<'EOF'                                         # example: three files of a report
{"corrections": [
  {"path": "docs/tickets/archive/101-old-notes.md", "exclude": true},
  {"path": "docs/tickets/117-the-export-is-slow.md", "type": "bug"},
  {"path": "docs/tickets/125-waits-on-the-provider.md", "state": "blocked",
   "block": {"kind": "external", "reason": "the provider opens the firewall"}}
]}
EOF
curl -sS -X POST -H "Authorization: Bearer $COWORK_TOKEN" -H "Content-Type: application/json" \
  --data @corrections.json \
  https://cowork.example.com/api/v1/tenants/acme/projects/VKO/imports/0199a3c2-…/execution   # example
```

The execution analyses the files again with the corrections and writes everything in one
transaction, or nothing: `409 import_conflict` names, as `file:<path>`, every file it would
import that still has an error or a conflict — one a ticket filed since the dry run took
included —; exclude it and execute again. A correction that breaks a rule is `400` at its
`/corrections/<i>/…`. The answer is the executed job, every `create` now `created`; a second
execution is `409 import_executed`. The streams hear of it once, and the project's lists load
again in every browser that shows them.

**5. Check the count.** `summary.created` is what was created and `summary.open` how many of those
are open; the open ones should be as many as the source's tickets that are neither done nor
dropped:

```bash
grep -rlE --include='[0-9]*.md' --include='local_[0-9]*.md' \
  '^state:[[:space:]]*(filed|analysed|decided|in-progress|review|blocked)([[:space:]]|$)' docs/tickets | wc -l   # example
```

**6. Decide in the repository** what becomes of the files: they are history now. This repository's
own `docs/tickets/README.md` carries that note from the day its tickets are imported.

**What an import does not bring:** the bytes of attachments — an export lists them, and the import
reports a document's `attachments:` as not brought —, comments, time, the activity, the order of
the rank — the open tickets join the bottom of the project's rank, a parent before its children
and otherwise by number —, and who reported a ticket: the person who executes the import is the
reporter of every ticket it creates, and the acts name the job.

## Exporting

Whoever reads a project exports it — a viewer, a `read` token, an agent; the archive holds what
that reader may read:

```bash
curl -sS -f -H "Authorization: Bearer $COWORK_TOKEN" -o vko.tar.gz \
  https://cowork.example.com/api/v1/tenants/acme/projects/VKO/export     # example
curl -sS -f -H "Authorization: Bearer $COWORK_TOKEN" -OJ \
  https://cowork.example.com/api/v1/tenants/acme/export                  # example: the whole tenant
```

Or, on a person's machine with [`cowork-mcp`](claude-code.md) configured, unpacked into a new or
empty directory, never over a file:

```bash
cowork-mcp export acme/VKO ./vko-export                                  # example
```

On Linux and macOS its directories are `0700` and its files `0600`. Windows applies no such mode:
the files take the access list of the directory they are written to, so export under your profile
there, not into a directory other accounts may read.

The archive holds `manifest.json` — the projects, the counts, who exported it and when, and how
many confidential tickets it leaves out because its reader cannot read them —, `links.json` with
every link whose two ends the reader sees, `attachments.json` with the attachments' names, types,
sizes and paths, and each ticket as its Markdown document at `<tenant>/<PROJECT>-<n>.md`, done and
dropped ones included. A tenant export holds every project its reader sees, archived ones too.
Each export is recorded as `exported` on the project or the tenant, in the tenant's audit view. The
archive is streamed as it is read, a page of tickets at a time, one export at a time per replica —
another waits for it —, and must be written within the request timeout; a tenant too large for that
is exported project by project. A transfer cut off in the middle is an export that failed: fetch it
again.

## The export as a backup

cowork makes no backups: the database's and the bucket's are their operators'
([ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)).
The export is a second line beside them, readable without cowork, which the installation fetches on
its own schedule. Give it a token of its own: `read` scope, restricted to the tenant, named for the
job — in the browser's token page, or `POST /api/v1/me/tokens` from a session with
`{"name": "nightly export", "scope": "read", "tenant": "acme"}` (`# example`). A token expires
after its lifetime (`COWORK_TOKEN_MAX_LIFETIME` at the most): renew it before then. A read token of a
tenant administrator exports every confidential ticket of the tenant; one of a member exports what
that member reads, and counts the rest.

A CronJob that fetches the tenant export every night — an illustration, not a maintained
manifest:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: cowork-export                                   # example
spec:
  schedule: "17 3 * * *"                                # example: every night at 03:17
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      backoffLimit: 2
      template:
        spec:
          restartPolicy: Never
          securityContext:
            runAsNonRoot: true
            runAsUser: 100                              # example: a uid that is not root
            seccompProfile: {type: RuntimeDefault}
          containers:
            - name: export
              image: curlimages/curl:8.16.0             # example: pin the release you run
              securityContext:
                allowPrivilegeEscalation: false
                readOnlyRootFilesystem: true
                capabilities: {drop: [ALL]}
              env:
                - name: COWORK_TOKEN
                  valueFrom:
                    secretKeyRef: {name: cowork-export-token, key: token}   # example
              command: ["/bin/sh", "-ec"]
              args:
                - >-
                  curl --fail-with-body -sS -H "Authorization: Bearer $COWORK_TOKEN"
                  -o "/backup/acme-$(date -u +%Y%m%d).tar.gz"
                  https://cowork.example.com/api/v1/tenants/acme/export
              volumeMounts:
                - {name: backup, mountPath: /backup}
          volumes:
            - name: backup
              persistentVolumeClaim: {claimName: cowork-export}            # example
```

It calls the installation's own address, so the token travels over TLS; the backend's Service
inside the cluster answers plain HTTP. Keep the archives as the backups they are: they hold the
tickets' text, the persons' identities and, for an administrator's token, the confidential tickets
([docs/security/import-and-export.md, H-75](../security/import-and-export.md#h-75)). Rotation and
retention are the volume's, or whatever stores the files further.

**Restoring tickets from an export.** The import reads an export's documents and its links
manifest, so a project comes back through it, with its numbers, states, questions and the links
among its tickets — into a project that holds none of its numbers, a new installation's or a new
project. An import is a job on one project: from a tenant export, pack each project's documents
with the two manifests and import them one project at a time.

```bash
tar xzf acme-20261006.tar.gz                                            # example
tar czf vko.tar.gz manifest.json links.json acme/VKO-*.md                # example: one project
```

A link to another project is reported and left out, and the person makes it again after both
projects are back; what the archive does not carry — the comments, the time, the activity, the
attachments' bytes, the rank — does not come back (ADR 0051 Residual risks). The round trip is the
integration tier's proof of the grammar: a project exported, imported into an empty project and
exported again is the same archive up to the keys and the times.

## When something is refused

The import page shows each of these where it belongs: an upload's refusal under the files, an
execution's on the files it names, a `404` as a dry run that is gone.

| Answer | Cause | What to do |
|---|---|---|
| `413 payload_too_large` | the body, or the files unpacked, are above `COWORK_MAX_IMPORT_BYTES`, or the upload holds more than 10,000 files | split the upload by directory, or raise the variable — and the Ingress's body limit and the backend's memory with it |
| the controller's own `413` page | the Ingress's body limit is below the upload | raise it ([installation.md](installation.md#expose-it)) |
| `504 timeout` | the dry run, the execution or the export took longer than `COWORK_REQUEST_TIMEOUT`, or waited that long for another import or export on the replica; nothing was written | split the import, or raise the timeout and the controller's read timeout with it |
| `400 validation_failed` at `/file` | the upload is no readable archive, names a path twice or a path above 1,024 bytes, or has a part not named `file` | repack it |
| `409 project_archived` | the project is archived | import into another project |
| `409 import_conflict` | a file the execution would import has an error or a conflict | exclude it, or fix the source and make a new dry run |
| `409 import_executed` | the dry run was executed already | read the job: its report is what was created |
| `404` for a job | it is another project's, or a dry run past its twenty-four hours | make a new dry run |
| `403 agent_forbidden` | an agent's token, or the `X-Cowork-Agent` header | import with a person's own token, without the header |
