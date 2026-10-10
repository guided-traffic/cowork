---
id: T89
title: a tenant is a team now, and cowork still calls it a tenant everywhere
state: in-progress
severity: low         # the word misleads every person and every client; nothing breaks
security: none
threat:
urgency: release      # rule 2: gated on the expand release and on its clients moving
effort: L
blocked-by: release   # the contract waits until no supported client reads the names before
filed-from: the owners walk through the UI under make dev, 2026-10-10
opened: 2026-10-10
decided: 2026-10-10
done:
---

## Current state

The expand release shipped in 0.15.0 (ADR 0005 D1, ADR 0023 D1, ADR 0046 D7 as made concrete 2026-10-10):
every surface says team, and the names before stay served beside the new ones for one release. What
remains is the contract release, which removes them once no supported client reads them. What the
expand left, and where:

- **The deprecated path family:** `tools/specbundle` writes a twin under `/api/v1/tenants/{tenant}/…`
  (and `/api/v1/tenants`) for every team path, tagged `tenants`, a renamed operation keeping the
  operationId of the release before ([main.go](../../backend/tools/specbundle/main.go)
  `renamedOperations`); `asTeamPath` in [api.go](../../backend/internal/api/api.go) answers a twin as its
  team path; both generators leave the tag out
  ([oapi-codegen.yaml](../../backend/api/oapi-codegen.yaml), [ng-openapi-gen.json](../../frontend/ng-openapi-gen.json));
  the document test holds every team path to its twin
  ([document_test.go](../../backend/api/document_test.go)).
- **Deprecated names in the document and the handlers:** the query parameter `tenant` beside `team` on
  six person-level operations (`MeTenant`); the properties `tenant`, `tenants`, `restricted_tenant` on
  Membership, Token, TokenCreate, MemberToken, RepositoryBindingRef, RepositoryProposal, InboxEntry,
  MyTicket, Decision, SearchHit and ExportManifest; `group_by=tenant`; the membership event's `tenant`.
- **Configuration and chart:** `COWORK_BOOTSTRAP_TENANT_SLUG`, `COWORK_BOOTSTRAP_TENANT_NAME`,
  `COWORK_ATTACHMENT_TENANT_QUOTA` read with a warning; the chart reads `bootstrap.tenant.*` and
  `backend.config.attachmentTenantQuota` as fallbacks and renders both variable names for an image
  rollback; the three consistency gauges carry `tenant` beside `team`, and the two alerts aggregate by both.
- **cowork-mcp:** `create_project`'s `tenant` argument, `search`'s scope `tenant`, the old keys of
  `token check --json` (`tenant`) and `lookup --json` (`Tenant`), and `.cowork.yaml`'s key `tenant`, which
  every repository keeps alone until every machine runs a cowork-mcp of the expand release or later
  (cowork-mcp 0.14 ignores a file that names `team`).
- **Bindings that keep the old operation names:** the opaque cursors and the idempotency bindings of
  `searchTenant`, `searchMyTenants`, `listTenantTickets`, `listTenantTime`, `listTenantTokens`,
  `listTenants`, `createTenant`, so that a cursor or a retry survives a rollout across the expand release
  and the one before.
- **Stored text:** a body or comment written before the expand names its images under
  `/api/v1/tenants/…/content`; the renderer takes both families
  ([richtext.go](../../backend/internal/richtext/richtext.go)).
- **Path quotes in other ADRs** still name `/api/v1/tenants/…`, true while the twin family is served.
- **The image workflow's label** says "multi-tenant" (.github/workflows/build.yml:62).
- **Older deprecations of the same kind:** a token's `restricted_project_id` beside `restricted_project`
  (ADR 0035 D2); and, since the relations across teams (0.17.0), the reads `GET …/tickets/{number}/links`
  and `GET …/tickets/{number}/prerequisites`, kept with their old meaning beside `GET …/relations` and
  `GET …/prerequisite-tree` (ADR 0012 D6, ADR 0046 D7).

## Required changes

### The contract release (a release after the expand, once no supported client reads the old names)

- specbundle writes no twins; `asTeamPath`, the `tenants` tag and `TenantSlug` go; the document test
  holds that no path names `{tenant}` and no operation is deprecated for the rename.
- The deprecated parameters, properties, `group_by=tenant` and the event's `tenant` go from the document,
  the handlers and the clients; the export stops writing `tenant` and the importer keeps reading it for
  good (an archive outlives a release).
- The old variables are no longer read; the chart stops reading the old values and rendering the old
  variable names; the gauges drop `tenant`; the alerts aggregate by `team` alone.
- cowork-mcp drops the old argument, scope and JSON keys, and stops reading `.cowork.yaml`'s `tenant`
  (the release notes tell repositories to move first).
- The cursors and the idempotency bindings move to the new operation names, accepting the old ones
  during that release (a cursor or a retry made by the release before).
- The stored image URLs: a migration rewrites `/api/v1/tenants/` to `/api/v1/teams/` in the stored bodies
  and comments (ADR 0028: a rewrite of rows in a release of its own), or the renderer keeps mapping the old
  family for good; the old content route goes only after one of them.
- The ADRs that quote `/api/v1/tenants/…` are amended in place; ADR 0005, 0023, 0046 record the contract
  as built.
- The image workflow's label says team; a token's `restricted_project_id` goes, and so do the deprecated
  reads of a ticket's links and prerequisites.
- The problem code `tenant_slug_taken` becomes `team_slug_taken` and the repository proposal's reason
  `only-tenant` becomes `only-team`; that release's own clients — the UI and cowork-mcp — take both values
  for it. The stored values keep the old word, as the database's names do, and the UI keeps labelling them
  team: the audit record's `entity_type` `tenant` and payload key `tenant`, a local account's origin
  `tenant` (Q3). ADR 0005 D1, ADR 0047 and ADR 0066 say so.
- Until 1.0 no commit carries a breaking mark (ADR 0003 D9); the change says in its body what it removes.

### The tests that prove it

- The document test: no twin, no deprecated name of the rename, no `{tenant}`.
- The integration tier: an old path, parameter and property answer as the API answers a path, a
  parameter or a field it does not have (404, 400).
- The configuration's unit tests: an old variable is not read.
- The import of an archive written by 0.14 and by the expand release.

## Open questions

### Q3: Do the values that still name a tenant change in the contract release?

The expand renamed every name a client sends or reads beside the old one, but a value cannot carry two
words in one field, so these kept the old word: the problem code `tenant_slug_taken` (its title says team),
the repository proposal's reason `only-tenant`, and the stored values the UI labels as team — the audit
record's `entity_type` `tenant` and payload key `tenant`, a local account's origin `tenant`.

- **(a) Recommended:** the contract release changes the two wire values a client reads —
  `tenant_slug_taken` to `team_slug_taken`, `only-tenant` to `only-team` — its own clients knowing both for
  that release; the stored values stay, as the database's names do, and the UI keeps labelling them: an
  audit row is append-only (ADR 0026 D3) and is read as written.
- **(b)** every value keeps the old word for good, like the database's names: no client breaks, and an
  agent keeps reading `tenant_slug_taken`.
- **(c)** the wire values change and the stored values are rewritten by a migration as well: an
  append-only record rewritten for a word, which ADR 0026 forbids for the audit rows.

**Answer:** (a), 2026-10-10.
