---
id: T2
title: phase 2 (core domain and API) — Claude cannot yet create, read, update, link and move tickets with a token through a documented API
state: done
severity: high
security: none
threat:
urgency: now          # rule 1: measured-false statements in tracked files (the planning pointers below)
effort: L
blocked-by: T1
filed-from: docs/planning/project-plan.md phase 2, converted by ADR 0074 D2
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: phase 2 — the core domain and the API behind personal access tokens, verified by make verify-phase-2 and the isolation tests
---

## Current state

The family ticket of phase 2 ([ADR 0074](../adr/0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D2). **Goal:** Claude can create, read, update, link and move tickets in a tenant's project
with a personal access token, through the documented API.

- On `main` the backend has one table, [`tenants`](../../backend/internal/store/migrations/000001_tenants.up.sql),
  without a policy; three routes (health, readiness, version) in
  [`server.go`](../../backend/internal/httpserver/server.go); no authentication, no data-access
  layer, no OpenAPI document.
- The plan's phase-2 section is this ticket. The change that writes these tickets removes it
  from [project-plan.md](../planning/project-plan.md), removes the ended phases 0 and 1, takes
  out of phases 3–6 what moved into phase 2 (below), and makes phase 3 depend on phase 4's
  login: the UI cannot log in with a token, because the browser never holds one
  ([ADR 0035](../adr/0035-personal-access-tokens.md) D7,
  [ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)'s
  Consequences).
- **Decisions the phase builds on**, beyond the records it implements — the owner's answers of
  the conversion, amended into the records they change:
  - no login in phase 2: persons, tenants, memberships and tokens come from a test-only
    fixture over the administrative connection, `make dev-seed` in development (ADR 0038 D2,
    D6, D7; ADR 0035's Status);
  - two database roles from phase 2 on — an owner that migrates in an init container, a runtime
    role that owns nothing and is refused at start if it could bypass the policies
    ([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2,
    [ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)
    D1, D2, [ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
    D3–D5, [ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
    D5, D7, D8);
  - the source-address hash waits for the trust rule of forwarded addresses (ADR 0035 D2);
    refused uses of a dead token are recorded at most once per token, reason and hour (D9);
  - who creates projects is the tenant setting `members_create_projects`, a `write`-scope act
    either way ([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
    D1, D9; ADR 0035 D3; [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
    D4; [ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
    D5);
  - urgency rule set v1 with the default `later` ([ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md)
    D3);
  - and the owner's guidance, which amends no record: an agent-permission detail no record
    decides is built open and named in its ticket, three such gates are reviewed after
    experience (T22); the `/markdown` grammar ships as a v1 (T21, T23).
- **Moved into phase 2 from later phases:** the confidential flag except the import mapping
  (from phase 6; [ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
  D7 stays there); role checks on every route and the project-restriction predicate (from
  phase 4: ADR 0035 D3 makes a token's permission scope ∩ role, and ADR 0034 D4 with its
  Context rules out adding the predicate to queries later — the restriction's administration
  routes stay in phase 4); the agent baseline, capabilities and hard-off list (from phase 5:
  ADR 0043 D1, D5 and [ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
  D7 attach them to every marked request). **Added**, in no phase of the plan before: the
  owner/runtime role split with its start-up refusal (ADR 0021 D2) and the stale-schema refusal
  with the image-rollback defect it sits beside (ADR 0057 D3, T3).
- **Not in phase 2:** a login, sessions, CSRF, tenant creation and token creation (ADR 0038
  D6); a global administrator; the source-address hash; the person-level lists, search routes,
  rank moves and the score, notifications (phase 3); the MCP server and `/context` (phase 5);
  the importer (phase 6).
- **Planning pointers that ADR 0074 made false** — open decisions live in tickets now and the
  catalog is a tombstone: the README's status block ("decided in docs/planning/ before they are
  built") and its feature bullet "Planned, not guessed" ("a question catalog … worked one
  decision at a time"); [CLAUDE.md](../../CLAUDE.md)'s status line ("the product decisions are
  open"), its table row for open decisions and its section "The question catalog is worked one
  question at a time"; [architecture.md:112-118](../developer/architecture.md#L112-L118) ("Each
  is a decision in docs/planning/questions.md before it is code").

## Required changes

1. **The children, in this order.** Each lands with its tests, the pages that describe what it
   built, and the Status and index row of every ADR it builds:
   - T3 — two database roles, the start-up refusals, the schema-ahead start
   - T4 — the data-access layer: sqlc, the tenant and mutation wrappers, jobs
   - T5 — the OpenAPI contract toolchain, problem details with a code catalogue, request ids
   - T6 — request limits, signed cursors and the list builder, nginx sizing
   - T7 — persons, tenant settings, memberships, projects and their policies
   - T8 — tokens, the audit record, idempotency storage
   - T9 — authentication, agent marking, authorization, the tenant boundary, `make dev-seed`
   - T10 — the person and tenant routes, the tenant audit view
   - T11 — projects
   - T12 — tickets, the key resolver, filters, the confidential flag
   - T13 — links
   - T14 — transitions
   - T15 — open questions
   - T16 — comments and the activity list
   - T17 — interest
   - T18 — progress and time entries
   - T19 — attachments and object storage
   - T20 — the event stream
   - T21 — the `/markdown` export v1
2. The planning pointers corrected: the README's status block and feature bullet name the ADRs
   and the tickets; CLAUDE.md's status line, its home for open decisions (a ticket's
   `## Open questions`, ADR 0074 D1) and its catalog section; architecture.md's "What is
   planned and not built".
3. **The phase verification**, each run recorded here with what was run, against what, with
   what result:
   1. `make verify-phase-2`, a Make target around a script under `hack/`, run against both built
      images, PostgreSQL 18 and the S3 test server, starting from the token `make dev-seed`
      prints. With `Authorization: Bearer` and `X-Cowork-Agent: <name>/<model>/<session>` it
      creates a project, files two tickets, opens a question on one, links the two, and moves a
      state with `from`; then it reads the tenant audit view (T10) and asserts, for every step,
      the actor (the seeded person), `token_id`, `agent` (the header) and `agent_capabilities`.
      It stops at the first assertion that fails. It is run by hand, not as a required job —
      the end-to-end tier ([ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md))
      is phase 3's.
   2. A test that a token of tenant A gets `404` on every route family of tenant B — the
      cross-tenant harness of T9, extended by every child — asserting the refusal (a body equal
      to the unknown-slug body except `instance` and `request_id`), not the absence of data.
   3. A deliberately unfiltered query under tenant A returns nothing of tenant B, as the
      runtime role: T3's catalog-driven test over every tenant-bound table, run as a person who
      is a member of A only; the person-scoped tables (tenants, memberships, users, tokens,
      idempotency keys, installation-level audit rows) return that person's own rows and
      nothing else.
   4. The runtime role cannot bypass row-level security: neither superuser nor `BYPASSRLS`, it
      owns nothing and is no member of the owner role (asserted by the integration tier), and
      `serve` and `migrate` refuse a role that is any of these (T3).
4. **Phase close:** every child extracted and archived; `docs/security/` has its tokens,
   tenancy and attachments pages, each ending with `## What this does not cover`; the README
   reference covers every variable, value, route and problem code the phase added; the
   Status and index row of every ADR the phase built say what is built.

## Verified

Run on 2026-10-02 against the working tree:

1. `make verify-phase-2` ([`hack/verify-phase-2.sh`](../../hack/verify-phase-2.sh)) against both
   images built from this tree, PostgreSQL 18 from `make postgres-up` and the Chainguard MinIO
   from `make minio-up`, from the token `make dev-seed` printed, through the frontend's proxy
   with `X-Cowork-Agent`: a project, two tickets, a question, a `blocks` link and a transition
   with `from`; the audit view then showed every one of the seven acts with the seeded person,
   the seeded token, the agent header and the token's capabilities — "phase 2 verified".
2. `TestEveryTenantRouteRefusesAnotherTenantLikeNoTenant` walks all 53 operations under
   `{tenant}` in the document with a token of tenant A against tenant B and against an unknown
   slug: every answer is `404` with the same body except `instance` and `request_id` — passed.
3. `TestUnfilteredQueryUnderTenantSeesNothingOfAnother`, as the runtime role under tenant A with
   every tenant-bound table seeded in both tenants: nothing of B in any of them, no other tenant,
   no stranger; tokens, idempotency keys, memberships and installation-level audit rows answer
   the person's own rows only — passed.
4. The runtime role's attributes and the refusal of a role that could bypass the policies
   (`TestRuntimeRoleCheck`, `TestRuntimeRoleThatIsMemberOfTheOwnerIsRefused`) — passed.

Beside them: `make lint cyclo gosec`, the unit tests and the integration tests (PostgreSQL 18.6,
MinIO), both with `-race`, `make helm-lint helm-template`, `make generate` run twice without a
change, and both images run read-only together (ADR 0001's Status lists what was checked).

## Related

- T1 — every merge of the phase needs a green, protected `main`.
- T22 — the three agent gates the phase builds open.
- T23 — the `/markdown` grammar the phase ships as v1.
