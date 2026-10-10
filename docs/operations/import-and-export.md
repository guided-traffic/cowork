# Importing tickets and exporting them

How anyone who writes in a project — or their agent — brings a repository's Markdown tickets, or an
earlier export, into it, and how anyone who reads a project takes it out again as files: for a move, and as the
second line of a backup. The routes, their fields and the variable are in the
[README's reference](../../README.md#api-backend); what an import lets in and an export lets out is
[docs/security/import-and-export.md](../security/import-and-export.md); why it works this way is
[ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md),
[ADR 0063](../adr/0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md)
and [ADR 0064](../adr/0064-one-direction-import-and-export-no-synchronisation.md). In the browser, a
member who writes in the project imports on its import page and anyone who reads a project exports
it from the project's header ([below](#in-the-browser)); with Claude Code, `cowork-mcp import` does
it in one command ([below](#with-claude-code)); the steps after that are the same through the API,
with a token.

**After the import, cowork is the source.** The files in the repository are history or are
removed — the repository decides. cowork reads them once, never watches them, and never writes
to a repository; importing the same files again is a conflict, never an update — the execution
leaves such a file out (ADR 0064 D1, D3).

## Before you import

- **Who.** Whoever may create a ticket in the project: the role `member` in the team — on a
  restricted project, a place on its list as a member —, through a token the `write` scope; an
  agent's token too ([ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
  D6). A job — its dry run, its report, its execution — is the person's who made it, and the team's
  administrators'; another person gets `404` for it. An import through a token — an agent's or
  not — assigns a confidential ticket only to the token's own person, so it leaves such a ticket
  unassigned where its file names somebody else, and the report says so; the import page, a browser
  session, assigns as the file says.
- **Where.** The project exists, is not archived, and is the one the tickets belong to: an import
  goes into one project, and keeps the numbers of its files — `117-….md` becomes `<team>/<PROJECT>-117`.
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
`/t/<team>/p/<KEY>/imports` — a member or an administrator of the team sees it, for a project
that is not archived. Drop the files onto the page or choose them — a `tar.gz` or a `zip` of the directory, or
the Markdown files themselves —, and *Start the dry run*. Its report opens at an address of its own,
`/t/<team>/p/<KEY>/imports/<id>`, which a reload or a bookmark keeps for the dry run's twenty-four
hours:

- the summary counts the files by outcome, and says how many tickets would be open and confidential
  and the highest number;
- a panel names every file the execution will leave out — a conflict, or a file with an error — with
  why; the execution imports the rest;
- the table lists every file with its outcome, title, detected type and why, state, assignee,
  confidential flag, questions, *will be left out* where the execution leaves it out, and a line for
  each warning, error with its line, link, note or reason; *Leave out* takes a file out, and on a
  file to create, or one with an error, the type, the state — `blocked` with its kind, reason and
  origin — and the assignee are corrected in place.

*Import N tickets* asks first, then executes with the corrections. A correction the execution
refuses shows on the file it names, and the report stays as it was, so the file is left out and the
execution started again. Done, the page counts what it created and leads to the backlog and the board. A dry
run past its day says so and offers a new one.

**Export.** The download icon of a project's header, *Export the tickets*, saves the project's archive
under the name the server gives it, for anybody who reads the project; a team's settings have
*Export the team* for its administrators. Beside each the page says how many tickets the archive
holds and how many confidential tickets it leaves out because its reader cannot read them. The
archive is the one the API answers ([Exporting](#exporting)).

## With Claude Code

`cowork-mcp import` ([claude-code.md](claude-code.md#importing-a-repositorys-tickets)) runs the
steps below in one command, as the agent of the person whose token `COWORK_TOKEN` holds: it packs a
directory's Markdown files and an export's `manifest.json` and `links.json` — a link is not
followed, and nothing else of the directory is sent —, or sends an archive or one Markdown file as
it is, makes the dry run, prints its report and, without `--dry-run`, executes it without
corrections and prints the executed report:

```bash
cowork-mcp import acme/VKO docs/tickets --dry-run                       # example: read the report first
cowork-mcp import acme/VKO docs/tickets                                 # example: import what can be imported
```

Each file is a paragraph: its outcome and key, `why:` it was skipped, excluded or left out, its
links, and a line per error and warning with its field and line. A file left out is fixed and
imported in a new run; a family's children get their parent afterwards
(`PATCH …/tickets/<n>` with `{"parent": "<key>"}`). The full report stays readable as the job, for
its maker, at the address the dry run names.

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
  https://cowork.example.com/api/v1/teams/acme/projects/VKO/imports     # example host, team and project
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
| `conflict` | the project holds its number already, a deleted ticket's included, as `conflict` names; the execution leaves it out, `reason` says so | nothing, or exclude it; the project's ticket stays as it is |
| `error` | it cannot be imported as it stands; `errors` name the field and the line — a text longer than the API takes, as the import would write it, is one: a body of 200,000 characters, a question of 2,000, its options or answer of 100,000, a recommendation of 10,000, a threat, a block's reason or a dropped ticket's reason of 2,000, a done ticket's note of 10,000; the execution leaves it out | fix the file and import it in a new dry run, or let it be |
| `skip` | it is no ticket file, or a `/context` document; `reason` says why | nothing: a skipped file never imports |

A number a ticket held that was purged is no conflict: the file is imported under it, with a
warning that whatever named that key before — the purge's act, an old reference — names the new
ticket now. An assignee is the member the dry run named: one who became a member, or got onto a
restricted project's list, between the dry run and the execution is assigned nobody, with a warning
in the executed report.

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
  https://cowork.example.com/api/v1/teams/acme/projects/VKO/imports/0199a3c2-…/execution     # example
```

The execution analyses the files again with the corrections and writes, in one transaction, every
file it can import; it leaves out each file that still has an error or a conflict — one a ticket
filed since the dry run took included —, and the executed report keeps its outcome and its
`reason`. Nothing in the files refuses the execution. A correction that breaks a rule is `400` at
its `/corrections/<i>/…`. The answer is the executed job, every `create` now `created`; a second
execution is `409 import_executed`. What was left out is imported by a new dry run of the fixed
files. The streams hear of it once, and the project's lists load
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
  https://cowork.example.com/api/v1/teams/acme/projects/VKO/export       # example
curl -sS -f -H "Authorization: Bearer $COWORK_TOKEN" -OJ \
  https://cowork.example.com/api/v1/teams/acme/export                    # example: the whole team
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
sizes and paths, and each ticket as its Markdown document at `<team>/<PROJECT>-<n>.md`, done and
dropped ones included. A team export holds every project its reader sees, archived ones too.
Each export is recorded as `exported` on the project or the team, in the team's audit view. The
archive is streamed as it is read, a page of tickets at a time, one export at a time per replica —
another waits for it —, and must be written within the request timeout; a team too large for that
is exported project by project. A transfer cut off in the middle is an export that failed: fetch it
again.

## The export as a backup

cowork makes no backups: the database's and the bucket's are their operators'
([ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)).
The export is a second line beside them, readable without cowork, which the installation fetches on
its own schedule. Give it a token of its own: `read` scope, restricted to the team, named for the
job — in the browser's token page, or `POST /api/v1/me/tokens` from a session with
`{"name": "nightly export", "scope": "read", "team": "acme"}` (`# example`). A token expires
after its lifetime (`COWORK_TOKEN_MAX_LIFETIME` at the most): renew it before then. A read token of a
team administrator exports every confidential ticket of the team; one of a member exports what
that member reads, and counts the rest.

A CronJob that fetches the team export every night — an illustration, not a maintained
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
                  https://cowork.example.com/api/v1/teams/acme/export
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
retention are the volume's, or whatever stores the files further. The team's settings page shows
its administrators when it was last exported, and the chart's alert `CoworkExportOverdue` fires when
that lies too far back ([backups.md](backups.md#watching-the-schedule)).

**Restoring tickets from an export.** The import reads an export's documents and its links
manifest, so a project comes back through it, with its numbers, states, questions and the links
among its tickets — into a project that holds none of its numbers, a new installation's or a new
project. An import is a job on one project: from a team export, pack each project's documents
with the two manifests and import them one project at a time. The manifest names its team as
`team` and, for the importers of 0.14, as `tenant` beside it; an archive of a release before names
it as `tenant` alone, and the import reads it so for good.

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

The import page shows each of these where it belongs: an upload's refusal under the files, a
correction's on the file it names, a `404` as a dry run that is gone. A file with an error or a
conflict is no refusal: the execution leaves it out, and the report says why.

| Answer | Cause | What to do |
|---|---|---|
| `413 payload_too_large` | the body, or the files unpacked, are above `COWORK_MAX_IMPORT_BYTES`, or the upload holds more than 10,000 files — a `zip`'s directories count too | split the upload by directory, or raise the variable — and the Ingress's body limit and the backend's memory with it |
| the controller's own `413` page | the Ingress's body limit is below the upload | raise it ([installation.md](installation.md#expose-it)) |
| `504 timeout` | the dry run, the execution or the export took longer than `COWORK_REQUEST_TIMEOUT`, or waited that long for another import or export on the replica; nothing was written | split the import, or raise the timeout and the controller's read timeout with it |
| `400 validation_failed` at `/file` | the upload is no readable archive, names a path twice or a path above 1,024 bytes, or has a part not named `file` | repack it |
| `409 project_archived` | the project is archived | import into another project |
| `409 import_executed` | the dry run was executed already | read the job: its report is what was created |
| `404` for a job | it is another person's, another project's, or a dry run past its twenty-four hours | make a new dry run of your own |
| `403 forbidden` | the role is below `member` in the team, or the project's list makes it so | ask a team administrator for the role |
| `403 insufficient_scope` | the token's scope is `read` | import with a `write` token |
