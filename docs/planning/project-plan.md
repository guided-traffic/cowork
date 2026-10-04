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

## Done

Phases 0 (skeleton, 2026-09-29), 1 (the decisions, 2026-10-01) and 2 (the core domain and the
API, 2026-10-02, released as `0.1.0` on 2026-10-03 with a green pipeline, the images on Docker
Hub and the chart in its Helm repository) are done and deleted from this file
([ADR 0002](../adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
D10): what they built is in the ADRs' `Status` sections and their index, how it works in
[docs/developer/](../developer/README.md), and its gaps in [docs/security/](../security/README.md).

## Phase 3 — UI v1 (started 2026-10-03)

**Goal:** the owner and a second person run the daily work in the browser. The phase became a
family ticket and its children in [docs/tickets/](../tickets/README.md) when it started
([ADR 0074](../adr/0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D2), and this section was consumed by them. By the owner's answer of 2026-10-03 the local login
— sessions, the local administrator, local accounts, CSRF, the creation of tenants and tokens —
moved here from phase 4: the browser never holds a token
([ADR 0035](../adr/0035-personal-access-tokens.md) D7), so the UI needs the login first
([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)).

## Phase 4 — OIDC and authorization (built 2026-10-04)

**Goal:** people log in through the identity provider; only allowed groups get in; roles hold.
The phase became a family ticket in [docs/tickets/](../tickets/README.md) when it started
([ADR 0074](../adr/0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D2) and was built in the same change, on decisions the owner reviews before the release; what it
built is in the Status sections of ADR 0029–0035 and in
[identity-provider.md](../security/identity-provider.md).

## Phase 5 — The LLM interface, the VS Code workflow and the chat in the UI (built 2026-10-04)

**Goal:** a Claude Code session in any bound repository starts with its ticket context and
ends with the ticket updated by Claude; and in the UI, a chat panel at the right edge lets an
agent operate cowork for the person, through a model the installation names — a local LM
Studio first. The phase became tickets in [docs/tickets/](../tickets/README.md) when it started
([ADR 0074](../adr/0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D2) and was built on the branch on decisions the owner reviews before the release — the chat's
provisionally, as [ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)
says. What it built is in the Status sections of ADR 0040–0045, ADR 0066–0070 and ADR 0076, in
[claude-code.md](../operations/claude-code.md) and [chat.md](../operations/chat.md) for running it,
[mcp.md](../developer/mcp.md) and [chat.md](../developer/chat.md) for changing it, and
[agent-client.md](../security/agent-client.md) and [chat.md](../security/chat.md) for what it leaves
open; the workflow document it consumed is deleted (ADR 0074 D3). Verified on 2026-10-04: the MCP
server over stdio against a fresh backend, and the chat against LM Studio — a ticket filed, its
urgency set to `now`, moved to `analysed`, each act the chat's. Not verified: a live `claude`
session with the plugin, its hooks and its skills.

## Phase 6 — Import and cut-over

**Goal:** the sibling project's open tickets live in cowork; its `docs/tickets/` is retired.

**Needs:** H1–H5, E6.

**Delivers:** the importer for the Markdown ticket format; a dry-run report; the confidential
flag; the import of the sibling project's open tickets (that repository's own decision to
remove the directory is taken there, not here); the import of this repository's own tickets.
Then every other repository, one per session, as the workflow plan had it: its binding (its
remote; `.cowork.yaml` only for a fork or a repository without a remote,
[ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)),
the import, that repository's own decision on its `docs/tickets/`
([ADR 0064](../adr/0064-one-direction-import-and-export-no-synchronisation.md) D4), and the
three lines of [ADR 0069](../adr/0069-rules-stay-in-git-work-moves-to-cowork.md) D4 in its
`CLAUDE.md`; and the owner's global working rules about tickets rewritten once to point at
cowork.

**Verified when:** every imported ticket round-trips through `GET …/markdown` to a document
equal to the source up to the mapping documented in the ADR; the count of open tickets in the
UI equals the `grep -rH '^state:'` count in the source repository at import time.

**Effort:** M.

## Phase 7 — Hardening and 1.0

**Goal:** an installation the owner would leave running unattended.

**Needs:** G3–G8, A17, F4.

**Delivers:** metrics (ADR 0060); the inbound GitHub webhook on trial (ADR 0071); per-token rate limits if the audit log asks for them; the
tenant Markdown export; the published chart and image through the release
workflow; the chart's remaining references — the database component keys and the
`existingConfigMap` sources ([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
D3, D4), the example manifests (D1, D2) and the migration hook Job
([ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)
D2); request and response examples on every operation of the API document
([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md) D6); the operations
pages for upgrading and backups completed; the security pages reviewed against the code once
more.

**Verified when:** the release workflow has produced a tagged image and a chart index, an
upgrade from the previous release has run in a cluster, and every `H-<n>` in
`docs/security/` is either closed or explicitly accepted by the owner.

**Effort:** M.

## What is deliberately not planned

- Replacing git for ADRs and documentation; those stay in each repository (Q-I4).
- Running an LLM. cowork is what an LLM talks to, and its chat connects to a model the
  operator names; it never hosts one.
- Replacing GitHub pull requests or CI.
- Mobile clients, e-mail, calendars, time sheets.
