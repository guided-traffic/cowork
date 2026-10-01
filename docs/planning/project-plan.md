# Project plan

How the skeleton becomes a tool the owner and Claude work in every day. Phases, not dates:
the owner works on many projects at once and the velocity is unknown; each phase names what
it delivers, how that is verified, and which questions of [questions.md](questions.md) it needs
answered first. A phase that starts becomes tickets under [docs/tickets/](../tickets/README.md)
(or, from phase 6 on, in cowork itself); a phase that ends is deleted from this file
([ADR 0002](../adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D10).

Written 2026-09-29.

## Working agreements

- **Decide before building.** Every phase starts by turning its questions into ADRs, one
  question per turn. Code written on an undecided question is speculation.
- **The API is a first-class product.** Claude is a user from phase 2 on; the UI comes after
  the API works through `curl`.
- **No phase closes without its tests** in the tiers of
  [ADR 0003](../adr/0003-test-and-ci-policy.md), and without the pages under `docs/` that
  describe what it built.
- **Verification is named.** "Done" in a ticket means: what was run, against what, with what
  result.

## Phase 0 — Skeleton (done 2026-09-29)

**Delivered:** the repository structure with `backend/` and `frontend/`; a Go 1.27 backend
with configuration, health endpoints, version endpoint, request log, JSON 404/405, embedded
migrations applied on start; an Angular 22 workspace with a shell, a version service, ESLint
and vitest; an nginx frontend container that serves the bundle and proxies `/api/`; a Helm
chart with two Deployments; two Containerfiles; the Makefile; the three GitHub workflows;
Renovate; the docs tree; the founding ADRs; this plan, the question catalog and the workflow
plan.

**Verified:** `make lint cyclo gosec vuln test-unit`, `make test-integration` against
`postgres:18`, `make frontend-lint frontend-test-coverage frontend-build`,
`make helm-lint helm-template`, `make test-release-tooling`, `make docker-build`; both images
run together, read-only, against the same database: the frontend starts before the backend
and follows it once it appears, and answers `/healthz`, `/api/v1/version` through the proxy,
the UI shell, a deep link and a hashed asset; `SIGTERM` stops the backend cleanly.

**Not verified:** the workflows on GitHub (runner pool, secrets — Q-G5 to Q-G7).

## Phase 1 — Decide (done 2026-10-01)

**Delivered:** the whole catalog, not only the phase-2 part — ADR 0004 to ADR 0073, one
question per turn over three days, with the amendments the later answers forced on the
earlier records; the catalog itself is a tombstone
([ADR 0074](../adr/0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)).

**Not done, by the owner's decision:** the conversion of phase 2 into tickets. It is the
first thing of the next session, after the pipeline ticket
([ADR 0061](../adr/0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md)
D5, [ADR 0073](../adr/0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md)).

## Phase 2 — Core domain and API

**Goal:** Claude can create, read, update, link and move tickets in a tenant's project with a
personal access token, through the documented API.

**Delivers:**

- Migrations: tenants (exists), users, memberships, projects, tickets, links, open questions,
  comments, activity/audit, tokens, interest, attachments (metadata), time entries; a forced
  row-level-security policy on every tenant-bound table and the per-transaction tenant
  context (ADR 0021).
- The data access layer (Q-B7), repositories per aggregate, integration tests per repository
  against PostgreSQL.
- Token authentication middleware; the token issue/revoke endpoints; the audit log with agent
  attribution (Q-D4).
- The API under `/api/v1/tenants/{tenant}/…` for projects, tickets, links, questions,
  comments, transitions, interest, attachments (upload with sniffing and allow-list, download
  streamed through the backend, ADR 0016), progress and time entries with sums and CSV
  (ADR 0017); the event stream with `NOTIFY` publication and the polling fallback
  (ADR 0054); the S3 client and `make minio-up` for the
  integration tier; `GET …/tickets/{key}/markdown`; the error shape (Q-E2); ETags (Q-E5);
  idempotency keys (Q-D7).
- API tests with `httptest` against a real PostgreSQL (the integration tier grows an API
  suite).
- `docs/developer/` pages for the domain and the API; `docs/security/` pages for tokens,
  tenancy and attachments, each with its "What this does not cover".

**Verified when:** a scripted `curl` session with a token creates a project, files a ticket,
opens a question, links two tickets, moves a state, and the audit log shows every step with
the agent header; a token of tenant A cannot see tenant B (a test proves the refusal, not the
absence of a bug); a deliberately unfiltered query under tenant A returns nothing of tenant B
(ADR 0021's second line, proven in the integration tier); the application role cannot bypass
row-level security.

**Effort:** L.

## Phase 3 — UI v1

**Goal:** the owner and a second person run the daily work in the browser.

**Needs:** the API of phase 2. Everything is decided (ADR 0004–0073); assignments,
per-person views, the inbox, the boards, the dashboard, PrimeNG with dark mode, signals and
services, the event stream's client side and the Playwright tier are in this phase.

**Delivers** ([ADR 0018](../adr/0018-the-views-of-the-first-release.md)): the generated API
client; a temporary login with a token (until phase 4); tenant and project navigation; the
member list of a tenant; ranked backlog with drag order and the score marker; the project
board with drag between states; the tenant board with swimlanes per project; saved filters;
the fixed dashboard; ticket detail with Markdown body, fields, assignee, the progress slider,
links, comment thread and activity list, attachments with upload and download, time entries,
open questions with an answer form; the time report; "next for me", "assigned to me" and
"open decisions" across the person's tenants; the in-app inbox; search; light and dark
theme; Playwright smoke tests in CI with two identities (the first end-to-end tier).

This is the largest phase of the plan by the owner's decision; it is cut into tickets per
view, and the two boards and the dashboard come last within it.

**Verified when:** the owner files a ticket, assigns it to a second identity, that identity
sees it in "assigned to me" and in its inbox, moves it and closes it — through the UI without
touching the API — and the Playwright suite covers that path with both identities.

**Effort:** XL.

## Phase 4 — OIDC and authorization

**Goal:** people log in through the identity provider; only allowed groups get in; roles hold.

**Needs:** C1–C5, C8–C10.

**Delivers:** the OIDC code flow with PKCE (ADR 0029); server-side sessions (ADR 0031); the
group gate, the group → tenant/role mapping and the manual grant (ADR 0030); the local
administrator synced from a Secret, the init state and the bootstrap tenant (ADR 0032); CSRF
protection; the role checks on every endpoint, project-level restrictions included if Q-C5
decides them; `make dev-up` with PostgreSQL and a minimal Dex; security pages for identity,
sessions, the local account and authorization.

**Verified when:** a user outside the allowed groups is refused with a test that proves it; a
viewer cannot write; a member of one tenant cannot list another; a member outside a restricted
project cannot read it; the session survives a pod restart and dies on logout.

**Effort:** M–L.

## Phase 5 — The LLM interface and the VS Code workflow

**Goal:** a Claude Code session in any bound repository starts with its ticket context and
ends with the ticket updated by Claude.

**Needs:** D1–D3, D6, I1–I5; [vscode-workflow.md](vscode-workflow.md).

**Delivers:** the MCP server (`cmd/cowork-mcp`, stdio) with the workflow tools; the
`.cowork.yaml` convention; the `SessionStart` hook and the skills; a `docs/operations/` page
for configuring Claude Code against an installation; the agent permission rules of Q-D5
enforced server-side.

**Verified when:** in this repository, `claude` starts, names the active ticket, Claude works,
records its state, opens a question, and finishes the ticket with a verification note, all
visible in the UI timeline with agent attribution.

**Effort:** M.

## Phase 6 — Import and cut-over

**Goal:** the sibling project's open tickets live in cowork; its `docs/tickets/` is retired.

**Needs:** H1–H5, E6.

**Delivers:** the importer for the Markdown ticket format; a dry-run report; the confidential
flag; the import of the sibling project's open tickets (that repository's own decision to
remove the directory is taken there, not here); the import of this repository's own tickets.

**Verified when:** every imported ticket round-trips through `GET …/markdown` to a document
equal to the source up to the mapping documented in the ADR; the count of open tickets in the
UI equals the `grep -rH '^state:'` count in the source repository at import time.

**Effort:** M.

## Phase 7 — Hardening and 1.0

**Goal:** an installation the owner would leave running unattended.

**Needs:** G3–G8, A17, F4.

**Delivers:** metrics (ADR 0060); the inbound GitHub webhook on trial (ADR 0071); per-token rate limits if the audit log asks for them; the
tenant Markdown export; the published chart and image through the release
workflow; the operations pages for upgrading and backups completed; the security pages
reviewed against the code once more.

**Verified when:** the release workflow has produced a tagged image and a chart index, an
upgrade from the previous release has run in a cluster, and every `H-<n>` in
`docs/security/` is either closed or explicitly accepted by the owner.

**Effort:** M.

## What is deliberately not planned

- Replacing git for ADRs and documentation; those stay in each repository (Q-I4).
- Running an LLM; cowork is what the LLM talks to.
- Replacing GitHub pull requests or CI.
- Mobile clients, e-mail, calendars, time sheets.
