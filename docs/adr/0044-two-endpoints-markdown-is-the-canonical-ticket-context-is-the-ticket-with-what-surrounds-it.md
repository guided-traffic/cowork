# ADR 0044: Two Endpoints — `/markdown` Is the Canonical Ticket for Import and Export, `/context` Is the Ticket With What Surrounds It, for Reading

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"context delivery to the LLM?": two endpoints, over one document with a read-only marker
(the recommendation), over JSON, and over a token-budgeted document. The rules of D4–D6 were
put to the owner with the question and not objected to.

Amended 2026-10-02 (D1: the key list of grammar v1; D6: the key that lists the attachments).
D1 named no key for the transition's note or reason, for the block, or for the attachments;
the first implementation spells them as the tickets page of this repository does, a v1 that
is reviewed after experience. Amended 2026-10-03 (D1: the keys of the three progress stages of
[ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D2, whose amendment says the export carries them; written when the stages were built). Amended
2026-10-04 (D2: a person's act through a token is named by the token, by the owner's rule that an
act an agent or a token makes is always marked, [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6; built the same day,
`TestRenderContextNamesTheTokenOfAPersonsAct`). Amended 2026-10-05 (D1: the key `urgency` is
`horizon`, the word [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D1
gives the API as amended that day; D2: an act on the horizon reads as what it did; D3: the importer
reads `urgency` as `horizon`), built the same day in grammar v1, amended in place — no reader
parses the export yet, the golden files changed with it — except D3's, which arrives with the
importer.

Amended 2026-10-06 by the owner, reviewing grammar v1 after experience (D1, the Context, the
Residual risks). D1 writes a person as `Name <identity>`, the way git writes an author, over the
display name alone (v1, which an import resolves by a name that is not unique) and over the
identity alone (resolvable, less readable); built the same day, the golden files changed with it,
since no reader parses the export yet. The Context says where the links go: into the project
export only, as a links manifest beside the tickets with each link once
([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4), over
`/markdown`'s frontmatter, which would write a link at both its ends, and over an export without
them; nothing is built for it before the export itself. D1's question form and state notes stay
as v1 writes them until the importer of phase 6 has read this repository's tickets with them,
over the recommendation inside the options text and over a key of cowork's own for the
verification note. The owner left the identity string to the implementer, to be chosen from what
the code holds and justified in D1; that choice, and the rest of D1's paragraph on persons — the
name without `<` and `>`, a person without an identity written by name alone, how the importer
resolves an identity —, are the implementer's, made concrete the same day and ~~open to the owner's
objection~~ *(the identity string accepted by the owner the same day; the rest open to the owner's
objection)*.


Amended 2026-10-06 for GitHub's webhook of
[ADR 0071](0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
(D2: `## Pull requests`, between `## Attachments` and `## Recent activity`, written only when the
ticket has one; made concrete by the implementer and built the same day, open to the owner's
objection — `writePullRequests` in [`internal/markdown/context.go`](../../backend/internal/markdown/context.go),
the golden file `context-pull-requests.md`). Amended 2026-10-09 with the webhook's removal, which the
owner dropped before its trial (D2: `## Pull requests` and the activity's names of a pull request
and a commit are gone, the function and the golden file with them; ADR 0071 Status).

~~**Partly built**~~ **Built** *(whole since 2026-10-06, with D3)* (phase 2, 2026-10-02; the stages and the state `review` since 2026-10-03; a person as `Name <identity>` since 2026-10-06): D1, D5 and D6 for `/markdown`
([`internal/markdown`](../../backend/internal/markdown/), golden files in its `testdata/`); every
call is recorded, and in phase 2 every caller is a token. D2, D4, and D5 and D6 for `/context`
since 2026-10-04 ([`RenderContext`](../../backend/internal/markdown/context.go)): the comments
quoted as block quotes, so a comment's text never reads as a section of the document; the
tree to a depth of eight, stopping at what the caller cannot see; the comments and the acts up
to 100 each; at most 200 links, nodes and attachments, without a note when it stops; the
activity is the ticket's activity list, which leaves out the exports. ~~D3 arrives with the
importer of [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md).~~
*(2026-10-06.)* D3 is built with the importer of
[ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
([`internal/importer`](../../backend/internal/importer/)), and the importer reads grammar v1 back:
every golden file of `/markdown` in `internal/markdown/testdata` parses without an error and renders
again to its own bytes — the one whose body has a heading of the questions' name with the warning
[ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)'s Residual risks
foresee —, and every golden file of `/context` is refused
(`TestParseReadsTheGoldenFilesOfGrammarV1`); the integration tier's round trip (ADR 0051 D5)
holds. Made concrete the same day by the implementer, open to the owner's objection:
D1's key `confidential` (below), built with its golden file `confidential.md`.

## Context

[ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D4 fixed the
Markdown form of a ticket — frontmatter from the columns, the body, the open questions in a
fixed order — as the one grammar the importer reads and the export writes. An LLM reading a
ticket needs more than the ticket: its links, the tree of what must be done before it can
close ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D6), the last comments,
the attachments, the recent activity. None of those are fields of the ticket; they are other
entities pointing at it, and an import that read them back would duplicate them. The owner
chose to keep the two documents apart by URL rather than by a marker inside one.

*(Amended 2026-10-06 by the owner:)* The links still leave with the project. The project export
writes them once each, in a links manifest beside the tickets
([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4), so
`/markdown` stays the single ticket and no link is written at both its ends. A backup restored
through the importer
([ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D2) keeps the links between the tickets of a project; a link to another project is reported and
omitted (ADR 0051 D2, D9, and its Residual risks).

## Decision

**D1 — `GET …/tickets/{number}/markdown` returns the canonical ticket and nothing else:**
the frontmatter rendered from the columns (`key`, `title`, `type`, `state`, `severity`,
`security`, `threat`, ~~`urgency`~~ `horizon` *(amended 2026-10-05)*, `effort`, `progress`, `assignee`, `parent`, `opened`,
`decided`, `done`, and the transition note or reason where the state has one), the body,
and `## Open questions` with their `**Answer:**` lines, in the fixed order of ADR 0011 D4.
It is what the importer reads and what a repository file looks like; a round trip through it
is lossless. *(Made concrete 2026-10-02, grammar v1:)* the keys in that order, an absent value
omitted — `threat` only when `security` is not `none`, ~~`urgency`~~ `horizon` *(amended
2026-10-05)* the effective value, the horizon set or else `later`,
~~`assignee` the display name~~ `assignee` the person as `Name <identity>` *(amended
2026-10-06, below)*, `parent` the full key, dates as UTC dates — *(made concrete
2026-10-03:)* `progress-refinement` before `progress`, which is the implementation stage, and
`progress-review` after it, each always written, as the ticket shows them — then the state's note:
`shipped` (the verification note of the `done` act), `dropped-reason`, and for `blocked` the
keys `blocked-by` (the kind), `blocked-reason` and `blocked-from`; then `attachments` (D6).
Strings are written plain when YAML reads them back unchanged, otherwise double-quoted. After
the body, `## Open questions` is always written and is the last heading of that name;
`### Q<n>: …` in number order, the options verbatim, `**Recommendation:** …` when there is
one, and `**Answer:**` with the answer, `_open_` or `_withdrawn_`. The response is
`text/markdown; charset=utf-8` with the ticket's `ETag` and is never answered `304`.
*(Made concrete 2026-10-06 by the implementer; confirmed by the owner 2026-10-09, over no key and over a manifest of the confidential keys:)* `confidential: true`
follows `threat` while the ticket's flag is set, and nothing is written while it is not. The flag
is a column ([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D1), so a round trip without it would not be lossless: a ticket an administrator flagged whose
class does not set the flag would come back readable by every member (ADR 0065 D7). Only a reader
of the ticket gets its document, so the key tells nobody more than the ticket does.

*(Amended 2026-10-06 by the owner: a person is written the way git writes an author.)* The name
is the display name, for a reader; the identity is the one the person already has, for the
importer. *(The identity string chosen 2026-10-06 by the implementer, as the owner asked, from
what the code holds, ~~and open to the owner's objection~~ and accepted by the owner the same day:)* A local account is `local:<username>`,
its identity
([ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D2: the
username is unique in the installation). A person of the identity provider is
`oidc:<issuer>#<subject>`, the pair that is the person's stable key
([ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D5). The issuer is written out although an installation has one: a `sub` is unique only for its
issuer, the export also moves work between installations (ADR 0059), and an assignee is admitted
to a confidential ticket ([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D9), so a subject resolved against another issuer could hand a ticket to the wrong person. `#`
ends the issuer because an issuer has no fragment (OpenID Connect Discovery; the configuration's
`checkIssuer` refuses one). *(Made concrete 2026-10-06 by the implementer, open to the owner's
objection:)* the name drops `<` and `>`, as git's does, so the first `<` starts the identity, and
the value is double-quoted, as every value with `<` or `:` is. A person with neither identity —
whom no route makes — is written by name alone. The importer resolves the identity and keeps the
name for its report; an identity it cannot resolve, a provider identity of another issuer among
them, is reported, never guessed by name.

*(Kept 2026-10-06 by the owner:)* the question form and the state notes — `### Q<n>:`, the
options verbatim, `**Recommendation:**`, `**Answer:**` with `_open_` or `_withdrawn_`, and
`shipped` carrying the verification note of the `done` act — stay as v1 writes them until the
importer of phase 6 has read this repository's tickets with them; what it cannot map is the
evidence for a change.

**D2 — `GET …/tickets/{number}/context` returns the ticket for reading:** the whole of D1,
followed by read-only sections in a fixed order — `## Links` (typed, with the reverse view
and each target's key, state and assignee), `## Prerequisites` (the tree, state, assignee and
progress per node, the count of open ones), `## Recent comments` (the last `comments`,
default 10, with actor, agent mark and time; a withdrawn comment as `[withdrawn]` without
text), `## Attachments` (name, type, size, URL; never content), `## Recent activity` (the
last `activity`, default 10). `comments=0` or `activity=0` omits a section. The document
starts with one line `<!-- cowork: context of <key>, exported <time> by <person> (via
<agent>) — not an import format -->`. *(Amended 2026-10-04, [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6: where no agent made an
act — a comment, an act of the activity, the request for the document itself — and a token did,
the place of `via <agent>` says `through the token <name>`, or `through a token` where the act did
not record the name; the first line `(through the token <name>)`. Everything else reads as
before.)* *(Amended 2026-10-05, ADR 0010 D1: the act on the horizon, which the audit record keeps
as `overridden`, reads `set the horizon to <value>`, or `returned the ticket to later` where the
horizon set was cleared.)* ~~*(Amended 2026-10-06, ADR 0071 D6:)* after `## Attachments`, `## Pull
requests` lists what GitHub's webhook linked — each pull request by its repository and number,
each default-branch commit by its repository and short id, its title quoted, as text from outside
the tenant, its state with the time of its merge, its author and where its key was read, and its
page — and is written only when the ticket has one, so a tenant without the webhook reads the
document as before; an act on a pull request or a commit names it in the activity, `merged pull
request github.com/acme/app#34`. `/markdown` names no pull request: D1's document is the canonical
ticket alone.~~ *(Removed 2026-10-09 with the webhook, ADR 0071 Status: the document has no
`## Pull requests`, and an act the webhook recorded in a release up to 0.12.0 reads by its name
alone, `system:github — merged`.)*

**D3 — The importer reads D1's form only.** A file that carries D2's sections is refused
with the line where the first read-only section starts, so a context export is never
imported by mistake. *(Amended 2026-10-05:)* It reads the key `urgency`, the name `horizon` had
before — in an export written before 2026-10-05 and in the ticket files of a repository, whose
frontmatter names it so — as `horizon`; a file that names both with different values is an
error of the report, not a guess. *(Built 2026-10-06:)* a context document is known by its first
line, the marker of D2 — after a byte order mark, if any —, and is an error of the report at the
line of its `## Links`, or of the marker where it has none; the execution waits until the person
excludes it ([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
D2).

**D4 — `get_ticket` of the MCP server calls `/context`;** `session_start` calls it for the
active ticket with `comments=5&activity=10` ([ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md)).
A session that wants the canonical file calls `/markdown` through `api`.

**D5 — Both exports through a token are recorded** ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)
D5): data left the system either way.

**D6 — Attachments appear as metadata and a URL in both documents' scope: never as
content** ([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D5); D1 lists them in the frontmatter as names only, D2 with type, size and URL. *(Made
concrete 2026-10-02: D1's key is `attachments`, a list of the names in upload order.)*

## Consequences

- Two formats, each with one job: `/markdown` is a file, `/context` is a reading. The
  importer's grammar stays exactly ADR 0011 D4 and gains a refusal, not a marker to skip.
- An LLM session reads one document (`/context`) per ticket; nothing is lost against the
  one-document variant except that a context export cannot be re-imported — which D3 makes
  an explicit refusal rather than a silent skip.
- Two routes to document and test; the context sections are rendered from the same queries
  the UI's detail page uses, so they cannot drift from it.
- A token-budgeted variant (`?budget=`) is an amendment to `/context` alone, if context
  windows ever press.

## Alternatives Considered

- **One document, two zones separated by a marker** — the recommendation: one call, one
  format, the importer skips below the marker. The owner preferred two URLs over a convention
  inside a document. Lost.
- **JSON for the LLM.** Precise; more tokens than Markdown for the same content, and the body
  is Markdown anyway. Lost.
- **A token budget that trims sections from the bottom.** Useful when windows press; a
  heuristic count today. Deferred as an amendment to D2.

## Residual risks

- D1's frontmatter now carries `assignee` and `parent`; an import maps them ~~by name and key~~
  *(amended 2026-10-06:)* by the assignee's identity and the parent's key and reports what it
  could not resolve, as the import record will say.
- *(2026-10-06.)* The identity leaves the system with the document. Every reader of the ticket —
  any role, a `read` token, an agent and a configured chat provider through `/context` — reads
  the assignee's username, which the API's person shows already, or the issuer and the subject of
  a person of the identity provider, which the API shows nowhere else together — the subject alone
  is already the display name of a person whose issuer sends no `name`, no `preferred_username`
  and no address. It is an identifier, not a credential: a login still goes through the issuer or
  the password.
- *(2026-10-06.)* An issuer that ends in a bare `#` is not conformant, but `checkIssuer` lets its empty fragment
  through; the first `#` of such a person's identity would end the issuer too early. Not handled;
  the importer meets it first. *(2026-10-06: the importer cuts such an identity at its first `#`,
  finds an issuer other than the installation's, and assigns nobody, with a warning — it fails
  closed. Read in the code, not covered by a test.)*
- *(2026-10-06.)* D3 knows a context document by its first line only. A context document whose
  marker line was removed reads as a ticket of grammar v1 whose body ends with the read-only
  sections, and its dry run reports no error: nothing in the report marks it, and only a person
  reading the imported body would notice.
- Two documents mean two caches and two ETags for one ticket; the API record treats them as
  two resources.

## References

- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D4 — the canonical grammar `/markdown` keeps
- [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D6, [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md), [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md) D5 — what `/context` adds and what it never includes
- [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) — the tools that call each
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D5 — exports are recorded
- [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) D5, [ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D2 — the identities D1 writes; [`person`](../../backend/internal/markdown/markdown.go) and the query `ExportPerson` ([`export.sql`](../../backend/internal/store/queries/read/export.sql)) as built
- [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4 — the project export and its links manifest
