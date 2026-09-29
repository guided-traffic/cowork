# Question catalog

The decisions that stand between the skeleton and a usable cowork, collected in one place so
they can be worked through **one at a time**. Every question names why it matters, the
sensible options with their cost, a recommendation with its reason, and an `**Answer:**` line.
An answered question becomes an ADR in the same session and is removed from this file
([ADR 0002](../adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D10).
The phase of [project-plan.md](project-plan.md) that needs each answer is given, so the order
of work is the order of questions.

Written 2026-09-29 from the founding brief and the skeleton. Recommendations are proposals,
not decisions.

Themes: [A Product and the ticket model](#a-product-and-the-ticket-model) ·
[B Tenancy and data](#b-tenancy-and-data) · [C Identity and access](#c-identity-and-access) ·
[D The LLM interface](#d-the-llm-interface) · [E API design](#e-api-design) ·
[F Frontend](#f-frontend) · [G Persistence and operations](#g-persistence-and-operations) ·
[H Import of the existing tickets](#h-import-of-the-existing-tickets) ·
[I The VS Code workflow](#i-the-vs-code-workflow) · [K Process and repository](#k-process-and-repository)

---

## A. Product and the ticket model

*Needed by phase 1 (decide) for phase 2 (core domain).*

### Q-A1: Who works in cowork?

**Why it matters.** Depth of roles, notifications, UI polish and the audit model all scale with
the number of humans.

- (a) The owner and Claude only; tenants keep the owner's clients apart, nobody else logs in.
- (b) The owner, Claude, and occasionally a client's people inside their own tenant (read,
  comment, prioritise).
- (c) A team product from the start.

**Recommendation:** (b). It is what the brief describes ("several users", "users link tickets
to take part in prioritisation") and it forces the tenancy and role model to be real without
building for a crowd.

**Answer:** _open_

### Q-A2: What is a tenant?

**Why it matters.** It fixes the isolation unit and whether a user can be in several.

- (a) A client or organisation; a user may belong to several tenants; tickets never cross.
- (b) A context of the owner (work, private, client X); single-user, switchable.

**Recommendation:** (a), with an n:m membership table. (b) is (a) with one member.

**Answer:** _open_

### Q-A3: What is a project?

- (a) One git repository.
- (b) A product or backlog unit that owns zero or more repositories (a repository is an
  attribute of a project, and a project can be e.g. "valkey-operator" or "homelab").

**Recommendation:** (b). The owner has projects that span repositories and repositories that
are not projects; the repository binding is a link, not the identity.

**Answer:** _open_

### Q-A4: How is a ticket addressed?

**Why it matters.** The key is what commits, branches, Claude and people say out loud.

- (a) `KEY-123`: a short project key and a per-project sequence, like Jira.
- (b) A tenant-wide sequence, `#123`.
- (c) UUID only.

**Recommendation:** (a). Greppable in commit messages, unambiguous across tenants when the
project key is unique per tenant, human-sized. UUIDv7 stays the primary key underneath.

**Answer:** _open_

### Q-A5: Ticket types and hierarchy

- (a) One type, labels for everything.
- (b) A small fixed set (`task`, `bug`, `feature`, `decision`, `question`) plus an optional
  parent for grouping (an epic is a ticket with children).
- (c) Configurable types per project.

**Recommendation:** (b). The existing tickets are already three kinds of thing; a `decision`
type maps onto the "open question that becomes an ADR" workflow.

**Answer:** _open_

### Q-A6: Workflow states

- (a) The states of the existing frontmatter: `filed → analysed → decided → in-progress → done | dropped`.
- (b) A generic board: `backlog, ready, in progress, review, done`.
- (c) Configurable per project.

**Recommendation:** (a) as the fixed set for v1, because it encodes the working method (an
analysis before a decision before work) and the importer maps 1:1; a board column is a state.
Configurability later if a second way of working appears.

**Answer:** _open_

### Q-A7: Which frontmatter fields become first-class?

The existing tickets carry `severity`, `security`, `threat`, `urgency`, `effort`, `blocked-by`,
`filed-from`, `opened/decided/done`, `shipped`, `dropped-reason`, `publication-accepted`.

- (a) All of them as columns with the same vocabularies.
- (b) A minimal core (state, priority, effort) and the rest as labels or body text.

**Recommendation:** (a), minus `filed-from` (becomes a typed link, Q-A9) and `publication-accepted`
(becomes part of the confidentiality flag, Q-H5). The vocabularies are the owner's method;
free text loses the ability to filter and to derive urgency.

**Answer:** _open_

### Q-A8: Enrichment: body text versus structured parts

- (a) One Markdown body with the conventional sections (current state, required changes, open
  questions, not verified).
- (b) Markdown body **plus** first-class open questions (question, options, recommendation,
  answer, answered-by, answered-at) as entities.
- (c) Fully structured: findings, changes and questions as entities.

**Recommendation:** (b). The body stays rewritable prose, and the one thing the workflow needs
to list across all projects — "which decisions wait for the owner" — becomes queryable and
answerable through the UI and the LLM interface with attribution.

**Answer:** _open_

### Q-A9: Links between tickets

- (a) Untyped "related".
- (b) A small typed, directed set: `blocks`, `relates-to`, `duplicates`, `parent-of`,
  `found-in` (replaces `filed-from`).

**Recommendation:** (b).

**Answer:** _open_

### Q-A10: "Users link tickets to take part in prioritisation" — what exactly is meant?

**Why it matters.** The brief's sentence admits two readings.

- (a) A user attaches themselves to a ticket (watch, +1, "I need this"); the count feeds the
  priority.
- (b) A user links **their** ticket to another to say "mine depends on / duplicates yours".

**Recommendation:** implement (a) as an `interest` relation (user, ticket, weight, note) and
let (b) be Q-A9's `blocks`/`duplicates`. Both are cheap once links exist.

**Answer:** _open_

### Q-A11: Priority model

- (a) Manual rank only (drag order in the backlog).
- (b) Computed score from severity, urgency, interest count and age.
- (c) Both: the score suggests, a manual rank within a project wins.

**Recommendation:** (c), rank stored as a sortable string (lexorank-style) so a drag is one
row update.

**Answer:** _open_

### Q-A12: Comments and activity

- (a) Comments only; the body is the state.
- (b) Comments plus an automatic activity log (state changes, field changes, links) shown in
  one timeline.

**Recommendation:** (b). The body stays "current state and nothing else" as the ticket rules
demand; discussion and the trail of who changed what live in the timeline. Claude's analysis
output goes into the body (rewritten), its reasoning into a comment.

**Answer:** _open_

### Q-A13: Attachments

- (a) None in v1; links only.
- (b) File upload to S3-compatible storage.

**Recommendation:** (a). Nothing in the current workflow needs a file that is not in a repository.

**Answer:** _open_

### Q-A14: Estimates and time tracking

**Recommendation:** effort as T-shirt size only (`XS S M L`), no time tracking. State if wanted.

**Answer:** _open_

### Q-A15: Which views are v1?

Candidates: per-project backlog (ranked list), per-project kanban, cross-project board per
tenant with swimlanes, cross-tenant "next for me", saved filters, search, a dashboard of open
decisions.

**Recommendation:** v1 = ranked backlog, kanban per project, "next for me" across everything,
"open decisions" list, full-text search. Swimlanes and dashboards in v2.

**Answer:** _open_

### Q-A16: Iterations or sprints?

**Recommendation:** no; continuous flow with an optional WIP limit per state. Say if a cadence
is wanted.

**Answer:** _open_

### Q-A17: Notifications

- (a) None in v1.
- (b) Outgoing webhooks (a Claude session or a chat bot can subscribe).
- (c) E-mail.

**Recommendation:** (a) for v1, (b) in the phase that builds the MCP interface.

**Answer:** _open_

---

## B. Tenancy and data

*Needed by phase 2.*

### Q-B1: How is tenant isolation enforced?

- (a) A `tenant_id` column on every row and application-level filtering.
- (b) (a) plus PostgreSQL row-level security: every request runs `SET LOCAL app.tenant_id`,
  policies refuse rows of other tenants even when a query forgets the filter.
- (c) One schema per tenant.

**Recommendation:** (b). Defence in depth for the class of bug that is most damaging in a
multi-tenant tool, one schema to migrate, and the RLS policy is itself testable in the
integration tier. (c) multiplies migrations and connection handling.

**Answer:** _open_

### Q-B2: Identifiers

**Recommendation:** `uuidv7()` primary keys everywhere (already in migration 000001) plus the
human key of Q-A4 for tickets. Confirm or change.

**Answer:** _open_

### Q-B3: How does a request name its tenant?

- (a) In the path: `/api/v1/tenants/{slug}/projects/{key}/tickets/{key}`.
- (b) In a header, with the token's default tenant as fallback.

**Recommendation:** (a). Visible in logs, in Claude's calls and in bookmarks; no ambiguity for
a token that is valid in several tenants.

**Answer:** _open_

### Q-B4: Deleting

**Recommendation:** soft delete (`deleted_at`) for tickets and projects, hard delete of a tenant
only through an explicit admin action; a purge job later.

**Answer:** _open_

### Q-B5: Search

**Recommendation:** PostgreSQL only — `tsvector` for bodies and comments, `pg_trgm` for keys and
titles. No external search engine.

**Answer:** _open_

### Q-B6: Audit log

- (a) The activity timeline of Q-A12 is enough.
- (b) An append-only `audit_events` table for every mutation with actor, token, agent header,
  before/after JSON, from v1.

**Recommendation:** (b). An LLM writes here; every write must be attributable and reviewable.

**Answer:** _open_

### Q-B7: Data access layer

- (a) `sqlc`: SQL files, generated typed Go, `pgx` underneath.
- (b) Hand-written `pgx` queries.
- (c) An ORM (bun, ent, gorm).

**Recommendation:** (a). The SQL stays visible (RLS policies and `SET LOCAL` need that), the
types are checked at generation time, and there is no query builder to learn.

**Answer:** _open_

### Q-B8: Down migrations

**Recommendation:** every migration ships its `down` file (the unit test enforces the pairing)
and nothing runs them in production; they exist for local iteration. Confirm.

**Answer:** _open_

---

## C. Identity and access

*Needed by phase 2 (tokens) and phase 4 (OIDC).*

### Q-C1: Which OIDC provider?

- (a) Dex — the owner maintains a Dex operator; Dex federates Google, GitHub or LDAP and has
  static users for local development.
- (b) Keycloak, Authentik or another full IdP.
- (c) A cloud IdP directly (Entra ID, Google).

**Recommendation:** (a). It is in-house, it makes the local development login a Dex static
password rather than a backdoor in cowork, and it gives the group claim a known shape.

**Answer:** _open_

### Q-C2: How do OIDC groups gate access?

**Why it matters.** The brief: "through OIDC groups I control whether a user can log in".

- (a) One deployment-wide allow-list of groups (`COWORK_OIDC_ALLOWED_GROUPS`); anyone in one
  of them may log in; tenant membership is managed inside cowork.
- (b) A mapping table group → (tenant, role) inside cowork; login is allowed when at least
  one mapping matches; membership follows the groups on every login.
- (c) Both: (a) is the gate, (b) seeds and refreshes membership, and a manual membership can
  be added on top.

**Recommendation:** (c). The gate stays a one-line operator setting; the mapping keeps the IdP
the source of truth for who belongs where; the manual override covers the client user who is
not in the owner's IdP groups.

**Answer:** _open_

### Q-C3: Browser session mechanism

- (a) Server-side session: opaque id in an `HttpOnly; Secure; SameSite=Lax` cookie, session
  row in PostgreSQL, the OIDC tokens never reach the browser.
- (b) The IdP's JWT in the browser, sent as a bearer token.

**Recommendation:** (a). No token in JavaScript reach, revocation is a row delete, and the same
authorization code path serves the API for both cookies and PATs.

**Answer:** _open_

### Q-C4: How does the first administrator come to exist?

- (a) A group name in the configuration (`COWORK_ADMIN_GROUP`); members are global admins.
- (b) The first user to log in becomes admin.

**Recommendation:** (a). (b) is a race on a fresh install.

**Answer:** _open_

### Q-C5: Roles

- (a) Global admin; per tenant `admin`, `member`, `viewer`.
- (b) (a) plus per-project overrides.

**Recommendation:** (a) for v1; project-level roles when a tenant actually needs to hide a
project from its own members.

**Answer:** _open_

### Q-C6: Personal access token design

Proposed: format `cwk_<32 random bytes, base62>` with a fixed prefix for secret scanners;
stored as SHA-256, shown once; scopes `read`, `write`, `admin`; optional restriction to one
tenant; mandatory expiry (default 90 days, maximum 1 year); `last_used_at`; revocable in the
UI; a name and an `agent` flag.

**Recommendation:** as proposed. Name what to change.

**Answer:** _open_

### Q-C7: Whom does a token act as?

- (a) The user who created it; the audit log shows "Hans via token *claude-laptop*".
- (b) Dedicated service accounts (`claude@tenant`).

**Recommendation:** (a). Every action stays attributable to a human who is accountable for
the agent; service accounts can be added when a non-human owner is needed.

**Answer:** _open_

### Q-C8: CSRF for the cookie session

**Recommendation:** `SameSite=Lax` plus an `Origin`/`Referer` check on unsafe methods plus a
required custom header (`X-Requested-With`) that a cross-site form cannot set. The UI and the
API share one origin through the nginx proxy (ADR 0001 D3, D4), which is what makes `Lax`
sufficient. Confirm.

**Answer:** _open_

### Q-C9: Local development login

- (a) A `COWORK_DEV_LOGIN` switch that creates a fake session.
- (b) Dex with static passwords in a `docker compose` file; cowork has no development-only
  authentication code.

**Recommendation:** (b). A switch that bypasses authentication is one misconfiguration away
from production.

**Answer:** _open_

### Q-C10: Rate limits and abuse

**Recommendation:** none in v1 beyond logging; per-token limits when the audit log shows a
reason. Confirm.

**Answer:** _open_

---

## D. The LLM interface

*Needed by phase 5; D4–D5 already shape phase 2.*

### Q-D1: REST only, MCP only, or both?

- (a) REST with an OpenAPI document; Claude calls it with `curl` or a generated client.
- (b) An MCP server on top of the REST API, with curated tools.
- (c) Both: REST is the contract, MCP is the ergonomic surface for Claude Code.

**Recommendation:** (c). The MCP server is a thin Go binary in this repository that speaks to
the same API with a PAT; nothing exists only in MCP.

**Answer:** _open_

### Q-D2: MCP transport

- (a) A local `stdio` binary configured in VS Code / Claude Code, PAT from the environment.
- (b) Remote streamable-HTTP MCP with OAuth.

**Recommendation:** (a) first: nothing to host, works offline against a port-forward, and the
PAT model already exists. (b) later for sessions that run in the cloud.

**Answer:** _open_

### Q-D3: Tool granularity

- (a) CRUD tools mirroring the REST resources.
- (b) Workflow tools: `next_ticket`, `get_ticket` (Markdown), `start_ticket`, `record_state`,
  `open_question`, `answer_question`, `add_comment`, `finish_ticket`, `search`, plus a generic
  `api_call` escape hatch.

**Recommendation:** (b). Fewer tokens per session, fewer ways to do the wrong thing, and the
tools encode the method (a `finish_ticket` that requires a verification note).

**Answer:** _open_

### Q-D4: Attribution of agent writes

**Recommendation:** an `X-Cowork-Agent: <name>/<model>/<session>` header recorded in the
audit log and shown in the timeline ("via Claude Code"). Confirm.

**Answer:** _open_

### Q-D5: What may an agent do without a human?

Proposed: create and update tickets, comments, links and state transitions up to `in-progress`
and to `review`-like states; **never** delete; **never** answer an open question (only a human
does, that is what the entity is for); `done` only with a verification note; no change of
manual rank.

**Recommendation:** as proposed; enforced server-side by the token's `agent` flag, not by
prompt discipline.

**Answer:** _open_

### Q-D6: Context delivery to the LLM

**Recommendation:** `GET …/tickets/{key}/markdown` returns the whole ticket (frontmatter,
body, open questions, links, last comments) as one Markdown document, the same format the
importer reads. One call, one context block.

**Answer:** _open_

### Q-D7: Idempotency for agent writes

**Recommendation:** an `Idempotency-Key` header on every unsafe request, honoured for 24 h.
Agents retry.

**Answer:** _open_

---

## E. API design

*Needed by phase 2.*

### Q-E1: Spec-first or code-first?

- (a) OpenAPI 3.1 document is the source; `oapi-codegen` generates the Go server interface and
  the request/response types; the Angular client is generated from the same document.
- (b) Code-first with annotations or a generator that reads Go.

**Recommendation:** (a). The document is the contract for the UI, for Claude and for the MCP
server; one place to review a change.

**Answer:** _open_

### Q-E2: Error shape

- (a) The skeleton's `{"error":{"code","message"}}`.
- (b) RFC 9457 `application/problem+json` (`type`, `title`, `status`, `detail`, `instance`, plus
  `code` and `errors[]` for validation).

**Recommendation:** (b). Standard, tooling knows it, nothing depends on (a) yet.

**Answer:** _open_

### Q-E3: Pagination

**Recommendation:** cursor-based on the UUIDv7 (time-ordered) with `limit`, never offsets.

**Answer:** _open_

### Q-E4: Filtering

**Recommendation:** explicit query parameters (`state=`, `type=`, `assignee=`, `label=`) in v1; a
`q=` mini-language when saved filters arrive.

**Answer:** _open_

### Q-E5: Concurrency control

**Recommendation:** a `version` column, returned as `ETag`, required as `If-Match` on `PATCH`;
`412` on a stale write. Two Claude sessions on one ticket are a real case.

**Answer:** _open_

### Q-E6: Bulk and import

**Recommendation:** one `POST …/import` that accepts the Markdown ticket format (Q-H2) for a
whole directory; no generic bulk endpoint.

**Answer:** _open_

---

## F. Frontend

*Needed by phase 3.*

### Q-F1: Component library

- (a) Angular Material (Material 3) with the CDK's drag-and-drop for the board.
- (b) PrimeNG.
- (c) Tailwind with headless components.

**Recommendation:** (a). First-party, accessible, the CDK covers the board, and the owner has
shipped with it before.

**Answer:** _open_

### Q-F2: State management

**Recommendation:** signals, `resource`/`httpResource` and injectable services; no NgRx unless
a feature proves the need.

**Answer:** _open_

### Q-F3: API client

**Recommendation:** generated from the OpenAPI document (Q-E1) into `frontend/src/app/api/`,
never hand-written.

**Answer:** _open_

### Q-F4: Live updates

**Recommendation:** polling with `If-None-Match` in v1; Server-Sent Events for the board in v2.

**Answer:** _open_

### Q-F5: Languages and theme

**Recommendation:** English only; light and dark from the system setting from the start.

**Answer:** _open_

### Q-F6: URL scheme

**Recommendation:** `/t/{tenant}/p/{project}/backlog`, `/t/{tenant}/p/{project}/board`,
`/t/{tenant}/tickets/{key}`, `/me/next`, `/me/decisions`. Bookmarkable, mirrors the API.

**Answer:** _open_

### Q-F7: End-to-end tests

**Recommendation:** Playwright against the built binary and a PostgreSQL service in CI, from
the first real workflow (phase 3). ADR 0003 D2 already reserves the tier.

**Answer:** _open_

---

## G. Persistence and operations

*Needed by phase 2 (G1) and phase 7.*

### Q-G1: Where do migrations run in production?

- (a) On pod start, as built (ADR 0001 D4).
- (b) A Helm pre-upgrade Job running `cowork migrate`; the pods start with
  `COWORK_MIGRATE_ON_START=false`.

**Recommendation:** (a) until a zero-downtime upgrade with an incompatible schema is actually
needed; the switch exists.

**Answer:** _open_

### Q-G2: How is PostgreSQL provided?

**Recommendation:** external, `database.existingSecret`; CloudNativePG documented as the
Kubernetes example; no subchart.

**Answer:** _open_

### Q-G3: Backups

**Recommendation:** the database's own (CloudNativePG/pgBackRest); cowork adds a Markdown
export of a tenant as a second, human-readable line. Nothing more.

**Answer:** _open_

### Q-G4: Metrics

**Recommendation:** Prometheus `/metrics` on a second backend listener (`:8081`, no auth, not
exposed by the Service by default), request and database pool metrics, in phase 7; nginx
metrics through its stub status only if something scrapes them.

**Answer:** _open_

### Q-G5: Container registry

**Recommendation:** Docker Hub `guidedtraffic/cowork-backend` and `guidedtraffic/cowork-frontend`,
as the workflows assume. **Verify** the `DOCKERHUB_PAT` secret is available to this repository.

**Answer:** _open_

### Q-G6: CI runners

**Why it matters.** Every job says `runs-on: self-hosted`, inherited from the sibling project.

**Recommendation:** keep it if the runner pool is organisation-wide; otherwise switch every
job to `ubuntu-latest` (the service container and Docker steps work there too). **Verify.**

**Answer:** _open_

### Q-G7: Release automation secrets

**Verify** that `APP_CLIENT_ID` and `APP_PRIVATE_KEY` (the GitHub App for semantic-release and
Renovate) are available to this repository, that the `gh-pages` branch exists for the chart,
and that GitHub Pages serves it.

**Answer:** _open_

### Q-G8: Toolchain version policy beyond what ADR 0001 D9 says

ADR 0001 D9 decides that Go and Angular track the newest stable release. Open is only the
cadence detail: does a new Go **minor** (1.28) automerge like a patch does, or wait for a
review?

**Recommendation:** automerge minors too, as the Renovate rule does today; the required gates
are the review. Confirm.

**Answer:** _open_

---

## H. Import of the existing tickets

*Needed by phase 6.*

### Q-H1: What is imported?

**Recommendation:** the open tickets of a repository's `docs/tickets/`; the archive stays in
git as history (an archived ticket is never a current rule, so it has no place in a live
backlog).

**Answer:** _open_

### Q-H2: Mapping

Proposed: frontmatter → fields (Q-A7); `id: T18` → an alias key kept searchable; the body →
Markdown body; `## Open questions` subsections with `**Answer:**` → question entities;
`filed-from` → a `found-in` link when it names a ticket; `blocked-by: T<n>` → a `blocks` link.

**Recommendation:** as proposed; the same format is what `GET …/markdown` produces, so import
and export are one grammar.

**Answer:** _open_

### Q-H3: Direction after the import

- (a) One-way: after the import the directory is removed from the repository and its
  `CLAUDE.md` points to cowork.
- (b) Two-way synchronisation between files and cowork.

**Recommendation:** (a). Two sources of truth is the problem being solved.

**Answer:** _open_

### Q-H4: Tenant and project for the sibling project

**Recommendation:** tenant `guided-traffic`, project key `VKO`. Name the others when they come.

**Answer:** _open_

### Q-H5: Embargoed tickets

**Recommendation:** a per-ticket `confidential` flag visible to tenant admins and the
assignee only, replacing the `local_` file prefix; the importer sets it from `security: live|boundary`
with an unfixed finding.

**Answer:** _open_

---

## I. The VS Code workflow

*Needed by phase 5. The plan itself is [vscode-workflow.md](vscode-workflow.md).*

### Q-I1: Repository binding

**Recommendation:** a `.cowork.yaml` at each repository root: `tenant`, `project`, optional
default labels; read by the MCP server and the hooks.

**Answer:** _open_

### Q-I2: Session start

**Recommendation:** a Claude Code `SessionStart` hook that asks cowork for the active ticket
of this repository and the next candidates and prints them as context.

**Answer:** _open_

### Q-I3: Commit and branch conventions

**Recommendation:** the ticket key as the conventional-commit scope or as a `Cowork-Ticket:`
trailer; branches `<type>/<KEY-123>-<slug>`. No apostrophes anywhere in a commit message.

**Answer:** _open_

### Q-I4: What stays in git

**Recommendation:** ADRs, developer, operations and security pages stay in the repository;
tickets, open questions and plans move to cowork.

**Answer:** _open_

### Q-I5: A CLI beside the MCP server?

**Recommendation:** not in v1; the MCP binary can grow a `cowork-cli` mode later.

**Answer:** _open_

### Q-I6: GitHub integration

**Recommendation:** v2: a webhook that links PRs to tickets and moves a ticket on merge.

**Answer:** _open_

---

## K. Process and repository

*Needed now.*

### Q-K1: Security documentation layout

The owner's general standard names a root `SECURITY_ARCHITECTURE.md`; the sibling project
re-decided on 2026-09-27 to one page per perspective under `docs/security/`, and this
repository follows that (ADR 0002 D9).

**Recommendation:** update the general standard to the per-perspective layout so the two
stop disagreeing.

**Answer:** _open_

### Q-K2: Repository visibility

**Why it matters.** Public means the embargo rules matter from the first security ticket, and
Pages, Docker Hub and Go Report Card work as written; private means the coverage badge URL and
the Go Report Card badge in the README do not resolve for anyone but members.

**Answer:** _open_

### Q-K3: Branch protection

**Recommendation:** `main` protected, every job of the "Test and Release" workflow required, no
direct pushes, squash merges.

**Answer:** _open_

### Q-K4: License

`LICENSE` is Apache-2.0 and the badge, the chart and the image labels say so. Confirm.

**Answer:** _open_

### Q-K5: How this catalog is worked

**Recommendation:** in sessions, one question per turn, in the order of the phases; each
answer becomes an ADR (or an amendment of one) in the same session and the question is deleted
here; a question that turns out to need code to answer becomes a ticket.

**Answer:** _open_
