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

## Phase 1 — Decide

**Goal:** every question that phase 2 needs is an ADR.

**Needs:** A1–A12, B1–B8, C6–C7, D4–D5, E1–E6, G1, K2–K5.

**Delivers:** ADRs 0004 onward (the domain model, the tenancy model, the API contract shape,
the token design, the audit model, the data access layer); the first tickets of phase 2; the
OpenAPI document's skeleton if Q-E1 lands on spec-first.

**Verified when:** the catalog holds no question tagged phase 2, and each ADR's Status says
"Not built" with the ticket that will build it named nowhere but in `docs/tickets/`.

**Effort:** several sessions of decisions, little code.

## Phase 2 — Core domain and API

**Goal:** Claude can create, read, update, link and move tickets in a tenant's project with a
personal access token, through the documented API.

**Delivers:**

- Migrations: tenants (exists), users, memberships, projects, tickets, links, open questions,
  comments, activity/audit, tokens; RLS policies if Q-B1 says so.
- The data access layer (Q-B7), repositories per aggregate, integration tests per repository
  against PostgreSQL.
- Token authentication middleware; the token issue/revoke endpoints; the audit log with agent
  attribution (Q-D4).
- The API under `/api/v1/tenants/{tenant}/…` for projects, tickets, links, questions,
  comments, transitions; `GET …/tickets/{key}/markdown`; the error shape (Q-E2); ETags (Q-E5);
  idempotency keys (Q-D7).
- API tests with `httptest` against a real PostgreSQL (the integration tier grows an API
  suite).
- `docs/developer/` pages for the domain and the API; `docs/security/` pages for tokens and
  tenancy, each with its "What this does not cover".

**Verified when:** a scripted `curl` session with a token creates a project, files a ticket,
opens a question, links two tickets, moves a state, and the audit log shows every step with
the agent header; a token of tenant A cannot see tenant B (a test proves the refusal, not the
absence of a bug).

**Effort:** L.

## Phase 3 — UI v1

**Goal:** the owner runs the daily work in the browser.

**Needs:** A15, F1–F7; the API of phase 2.

**Delivers:** the generated API client; a temporary login with a token (until phase 4); tenant
and project navigation; ranked backlog with drag order; kanban with drag between states;
ticket detail with Markdown body, fields, links, timeline, open questions with an answer form;
"next for me" and "open decisions" across tenants; search; light and dark theme; Playwright
smoke tests in CI (the first end-to-end tier).

**Verified when:** the owner files, prioritises and closes a real ticket of this repository
through the UI without touching the API, and the Playwright suite covers that path.

**Effort:** L.

## Phase 4 — OIDC and authorization

**Goal:** people log in through the identity provider; only allowed groups get in; roles hold.

**Needs:** C1–C5, C8–C10.

**Delivers:** the OIDC code flow with PKCE; server-side sessions; the group gate and the
group → tenant/role mapping; the admin group; CSRF protection; the role checks on every
endpoint; a `docker compose` for local Dex; security pages for identity, sessions and
authorization.

**Verified when:** a user outside the allowed groups is refused with a test that proves it; a
viewer cannot write; a member of one tenant cannot list another; the session survives a pod
restart and dies on logout.

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

**Delivers:** metrics; webhooks; SSE for the board; per-token rate limits if the audit log
asks for them; the tenant Markdown export; the published chart and image through the release
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
