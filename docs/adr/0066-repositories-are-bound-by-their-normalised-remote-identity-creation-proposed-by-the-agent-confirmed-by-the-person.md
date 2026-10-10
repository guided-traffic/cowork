# ADR 0066: Repositories Are Bound by Their Normalised Remote Identity; the Lookup Spans the Person's Tenants; a Missing Project Is Proposed by the Agent and Created on the Person's Word; `.cowork.yaml` Is the Optional Override

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"repository binding?", after the owner reframed it: a file in the repository is acceptable
but not the point — entering a repository must find its project half-automatically by the
git remote, whatever mix of SSH and HTTPS URLs is in use, and when none exists the agent
proposes a project and a tenant and creates them once the owner says so. The owner also
granted the `create-project` capability this needs and, in the same breath, decided that
answers to open questions given in chat may be recorded and updated by the agent; both
change rules of earlier records, which state them in place (D7, D8). Amended 2026-10-02 (D5: the
person must be allowed to create projects by the tenant setting of
[ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D9, not necessarily be its administrator), the owner's answer to the question whether an
agent's `write` token may create a project. Amended 2026-10-06 (D7, D8: the rules they stated as
amendments of other records stand in those records —
[ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D3 and D4, [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D2,
[ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) D2 — and D7 and D8 point there;
the Consequences' line on ADR 0006 D3 and the References say what those records hold; by the
owner's rule of 2026-10-02 that every amendment is made in place in the record it changes
([docs/adr/README.md](README.md#keeping-them-current)); no rule changes). Amended 2026-10-10 by the
owner's rename of a tenant to a team
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1; D3: the argument
of `create_project` is `team`, as [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) D2
holds; D4: `.cowork.yaml` names the team as `team`, the key `tenant` still read for one release —
both present must name the same slug, else the file is refused naming both keys —, as the served
schema says; the proposal's `team` and `teams` beside the deprecated `tenant` and `tenants`), built
the same day ([`tools/workspace.go`](../../backend/internal/tools/workspace.go) `BindingFile`,
[`cowork-yaml.schema.json`](../../backend/api/cowork-yaml.schema.json)); the proposal's reason
`only-tenant` keeps its word. Made concrete by the implementer, open to the owner's objection: the
file `create_project` offers names `tenant:` in this release, since a `cowork-mcp` of the release
before drops a file that names `team`, and `team:` from the contract release on
([`tools/tool_project.go`](../../backend/internal/tools/tool_project.go)).

**Built** (phase 5, 2026-10-04; D7 and D8 since phase 2): D1 —
[`domain.NormaliseRemote`](../../backend/internal/domain/repository.go) with its table test,
the identity and the remote as last given, without credentials, in `project_repositories`
(migration 23), beside an optional sub-directory. D2 — `GET /api/v1/me/repositories/lookup`
with `remote` repeated in order of preference and `path`; the first remote with a binding that
covers the path decides, the most specific sub-directory first; the proposal's tenants are
those where the caller may create a project (D5), none for a project-restricted token, and the
reason says which rule chose (`only-tenant`, `remote-owner`, `choose`); the key splits the
name also at `_` and `.`, takes the first three letters when there is one part, and appends 2,
3, … while the key is taken. D3 — `session_start` and the `SessionStart` hook of
[ADR 0067](0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md);
the remotes of the working directory leave the machine without credentials. D4 — `.cowork.yaml`
read up to the repository root, its `path` the file's own directory when it names none; a
file whose `url` names another installation than `COWORK_URL` is ignored, with a note; the schema at `/api/v1/schemas/cowork-yaml.json`, without
authentication; `create_project` offers to write the file and writes nothing itself. D5 —
`POST …/projects` with `repository`, the project, its counter and the binding in one act;
`200` with the project that binds the repository already, `409 repository_bound` when the
caller cannot see it. D6 — `ambiguous` with the projects named; resolved through the API's
unbinding, as the UI has no page for it yet. Beside D1–D6: binding and unbinding a repository
of an existing project are routes of their own, `…/projects/{project}/repositories`, with the
need of creating a project; a repository another project of the tenant binds is
`409 repository_bound`, naming that project only to a caller who sees it.

## Context

[ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md) D3
made a repository an attribute of exactly one project with a reverse pointer in the
repository; [ADR 0041](0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md)
D4 and [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) made `session_start`
the moment the binding is read. The owner's projects are git repositories almost without
exception, cloned sometimes over SSH and sometimes over HTTPS, and a session should not
begin with a person writing a file: the remote is the identity, the tool should find the
project from it and, failing that, offer to create one. Creating a project was on the
agent's hard-off list ([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D3); the owner moved it to the selectable capabilities because creation is reversible
(archive) and attributable. Recording an answer was reserved to a person
([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D2); the
owner answers in chat and wants the agent to be the scribe — the person still decides, and
the record says who wrote it down.

## Decision

**D1 — A repository's identity is its normalised remote.** A remote URL is reduced to
`host/path`: scheme, user (`git@`, `https://user@`), a default port, a trailing `.git` and a
trailing slash removed; the host lower-cased; the path kept with its case and all its
segments (GitLab sub-groups). `git@github.com:guided-traffic/cowork.git`,
`https://github.com/guided-traffic/cowork` and `ssh://git@github.com:22/guided-traffic/cowork/`
are one identity, `github.com/guided-traffic/cowork`. A project stores the identity and the
last original form seen; the normalisation is a pure function with a table test of SSH,
HTTPS, scp-style, port, case and sub-group forms.

**D2 — The lookup spans the person's tenants.** `GET /api/v1/me/repositories/lookup?remote=`
normalises and searches every tenant of the caller ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
D3's union) and answers with exactly one binding, with several (a data error, reported), or
with none plus a **proposal**: the tenant — the person's only one; otherwise the tenant whose
projects already bind repositories under the same remote owner (`github.com/guided-traffic/*`
→ the tenant that holds the others); otherwise the list to choose from — a project key
derived from the repository name (the initials of hyphenated parts, `valkey-operator` → `VO`;
else the first letters; upper case, two to ten characters, [ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md)
D1; a number appended on collision), and the repository name as the project name.

**D3 — `session_start` binds or proposes.** It reads `git remote -v` in the working
directory, normalises every remote, calls the lookup. One binding: the session is bound. None:
the tool returns the proposal and the agent asks the person in chat — "create project
`guided-traffic/VO` for `github.com/guided-traffic/valkey-operator`?" — and on yes calls
~~`create_project(tenant, key, name, remote)`~~ `create_project(team, key, name, remote)` *(2026-10-10)*, which creates the project and binds the
repository in one recorded act. No: the session runs unbound and says so. No remote and no
file: unbound, no proposal.

**D4 — `.cowork.yaml` is optional and, when present, wins.** It is needed for a repository
without a remote, or to bind a fork to the original's project. Its fields are ~~`tenant`~~ `team`
*(2026-10-10: `tenant` still read for one release, both naming the same slug)*,
`project`, optional `path` (a sub-directory of a monorepo; the nearest file above the working
directory applies) and optional `url` (the installation, for people with several). When the
file and the server disagree, `session_start` reports the drift ([ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md)
D3) and the file's binding is used for the session. After creating a project the agent
offers to write the file; the default is no. The API serves a JSON schema for the file at
`/api/v1/schemas/cowork-yaml.json`.

**D5 — `create_project` is idempotent over the remote:** a second call with the same
identity returns the existing project and binds nothing twice. It requires the person to ~~be
`admin` of the tenant ([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D1)~~ *(amended 2026-10-02)* be allowed to create projects in the tenant (ADR 0034 D9) — the
agent inherits that — and the token's `create-project` capability (D7). It never
archives, restricts or deletes.

**D6 — A repository is still in at most one project** (ADR 0006 D3). A lookup with several
bindings is reported as a data error with the projects named; the person resolves it in the
UI.

**D7 — `create-project` is a selectable capability; [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
states the rule.** ~~Amendment to ADR 0043: `create-project` is a selectable capability, on by
default in the "full" profile and off in "assisted". It covers creating a project and binding a
repository; archiving, restricting and deleting projects, and everything about tenants, stay
on the hard-off list.~~ *(Moved 2026-10-06 into the record it changes, where it stands in place
since 2026-10-01: ADR 0043 D4 grants the capability — creating a project and binding a repository
— and names it among the switches "assisted" turns off; ADR 0043 D3 takes those two acts off the
hard-off list and keeps archiving, restricting and deleting projects and everything about tenants
on it.)*

**D8 — An agent may record and update an answer that a person gave; [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md),
ADR 0043 and ADR 0042 state the rule.**
~~Amendment to ADR 0011 D2 and ADR 0043 D3: an agent may record and update an
answer that a person gave. The owner answers questions in chat and the agent writes the
answer into the question entity. The answer's actor is the person; the record carries the
agent mark and `recorded_by_agent: true` ([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)),
and the activity list reads "answered by Hans, recorded via Claude Code". An answer recorded
by an agent may be updated by an agent of the same person; a person may always edit their
own answer. The capability is `record-answer`, on by default in "full", off in "assisted".
The MCP server gains `record_answer(question, answer)` ([ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md)
D2 is amended: the tool exists, the decision stays the person's).~~ *(Moved 2026-10-06 into the
records it changes, which state it in place since 2026-10-01: ADR 0011 D2 holds who may record
and change an answer and what the record and the ticket show, the last of it moved there
2026-10-06; ADR 0043 D4 the capability `record-answer` and its profiles, and D3 that recording a
person's answer left the hard-off list; ADR 0042 D2 the tool `record_answer`.)*

## Consequences

- Entering a repository and starting a session finds the project or offers to create it;
  the owner never writes a binding file unless a fork or a remote-less repository needs one.
- The normalisation is the one function that must be right; its table test is the guard, and
  a remote it cannot normalise is reported as the original string.
- D7 and D8 widen what a "full" agent token may do; both acts are attributable to the person
  and visible as the agent's in the record; "assisted" tokens keep the earlier limits.
- ADR 0006 D3's "two sides" are, in place since 2026-10-01: the server's binding the primary
  side, the file the optional override; the drift report stays.

## Alternatives Considered

- **A mandatory `.cowork.yaml` with tenant and project** — the first recommendation. Simple
  and explicit; a file to write in every repository before the first session, which the
  owner does not want. Lost as the primary mechanism, kept as the override.
- **A file with session defaults** (type, effort, assignee). Agent behaviour in a file every
  contributor can edit. Lost.
- **Server-side only, no file at all.** Loses the remote-less repository and the fork case,
  and the second side of the drift check. Lost; D4 keeps the file where it is needed.

## Residual risks

- D2's tenant heuristic by remote owner is a guess presented as a proposal; the person
  confirms or picks.
- D8 removes a structural guarantee — "only a person's hand writes an answer" — in favour of
  attribution: the record always says an agent wrote it down. The owner accepts that for his
  own working mode; a tenant that wants the guarantee issues "assisted" tokens.

## References

- [ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md) D3 — the binding model this record implements
- [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md), [ADR 0041](0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md) D4 — `session_start` and the working directory
- [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md) D3, D4, [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D2, [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) D2 — where the rules of D7 and D8 stand
- [ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D1, [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D1 — the key grammar and who may create projects
