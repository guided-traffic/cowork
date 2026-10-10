---
id: T89
title: a tenant is a team now, and cowork still calls it a tenant everywhere
state: decided
severity: low         # the word misleads every person and every client; nothing breaks
security: none
threat:
urgency: later        # rule 4: the rename and its depth are decided
effort: L
filed-from: the owners walk through the UI under make dev, 2026-10-10
opened: 2026-10-10
decided: 2026-10-10
done:
---

## Current state

The owner decided that an installation is an organisation's and a tenant is a team inside it (T88
Q1); "tenant" is the wrong word for that, and the owner wants "team" everywhere, the API included
(Q1 below). ADR 0005 D1 and ADR 0023 D1 state it as amended, not built. Today "tenant" is the word of
every surface:

- **The API:** 75 of the 97 paths of
  [openapi.gen.json](../../backend/api/openapi.gen.json) are under `/api/v1/tenants/{tenant}/…`; the
  path and query parameter `tenant`, the properties `tenant`, `tenants` and `restricted_tenant`, and
  seven schemas named for it; the generated clients follow — the Go client of `cowork-mcp` and the
  Angular client in `frontend/src/app/api/`.
- **`cowork-mcp`'s tools:** `internal/tools` names `Tenant` about a hundred times and takes a
  `"tenant"` argument in twenty places — the names an agent reads.
- **Configuration:** `COWORK_BOOTSTRAP_TENANT_SLUG`, `COWORK_BOOTSTRAP_TENANT_NAME`,
  `COWORK_ATTACHMENT_TENANT_QUOTA`; the chart's `bootstrap.tenant.*` and `storage`'s
  `attachmentTenantQuota` ([values.yaml:153](../../deploy/helm/cowork/values.yaml#L153),
  [values.yaml:496](../../deploy/helm/cowork/values.yaml#L496)).
- **Metrics:** the label `tenant` ([metrics.go:628](../../backend/internal/metrics/metrics.go#L628)),
  which the export-age metric of 0.14.0 carries.
- **The event stream:** a membership event names its tenant, `{"tenant": "<slug>", …}`
  ([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)).
- **The UI:** every label, the route prefix `/t/{slug}`, and the pages of T87 ("All tenants", "New
  tenant").
- **The database:** the table `tenants`, the column `tenant_id` on every tenant-bound table, the
  setting `app.tenant_id` and `app_tenant_id()` of row-level security — 39 of the 46 migrations.
  [ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D3: a
  migration never drops, renames or narrows what the previous release reads.
- **The documentation and the ADRs:** ADR 0005 is "a tenant is a client organisation and the
  isolation unit"; README.md's reference tables, docs/operations, docs/security/tenancy.md.
- **The export archive** names its tenant; an archive written by an earlier release must stay
  importable.

## Required changes

The target is [ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1
and [ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D1 as amended on 2026-10-10, marked not
built: **"team" on every surface a person, an agent or an operator reads, the API's paths included,
and not in the database** (Q1, Q2). The way is the one `urgency` took to `horizon`
([ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md)'s amendments of
2026-10-05 and 2026-10-06, under
[ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md) D7): the new names in
`/api/v1` beside the old ones for one release, the old ones deprecated and behaving as they did, then
removed in a release of their own once no supported client reads them.

### The expand release

- **The UI:** every label — the sidebar, the gear's dialog, "All teams", "New team", the person menu,
  every page and message. The route prefix `/t/{slug}` stays: its `t` reads as team, and no bookmark
  breaks.
- **The API:** `/api/v1/teams/{team}/…` beside `/api/v1/tenants/{tenant}/…`; the parameter `team`
  beside `tenant`, the properties `team`, `teams`, `restricted_team` beside the old ones, the schemas
  and the problem texts renamed, the old schemas kept as deprecated aliases where a client names them.
  The generated Go and Angular clients follow, and with them the code where the generated names reach
  it; the OpenAPI document marks every old name `deprecated: true`.
- **`cowork-mcp` and the plugin:** the tools' names, arguments and descriptions say team; a `tenant`
  argument is still taken for a release. `cowork-mcp` calls the new paths.
- **Configuration:** `COWORK_BOOTSTRAP_TEAM_SLUG`, `COWORK_BOOTSTRAP_TEAM_NAME`,
  `COWORK_ATTACHMENT_TEAM_QUOTA`, the chart's `bootstrap.team.*` and `attachmentTeamQuota`; the old
  names are still read, with a warning in the log that names the new one; both set and different is a
  configuration error that names both variables.
- **Metrics:** the label `team`, with `tenant` beside it for the release, so that a dashboard or an
  alert can move.
- **The event stream:** a membership event names `team` beside `tenant` for the release.
- **The export archive:** written with `team`; an archive with `tenant` is still read, for good, since
  an archive outlives a release.
- **The documentation:** README.md's reference tables, docs/operations, docs/security, docs/developer —
  with one line in docs/developer saying a team is stored as a tenant; every ADR that names an
  identifier that changes (the bootstrap variables of ADR 0032, the quota of ADR 0016 and ADR 0039,
  the label of ADR 0060, a membership event's `tenant` of ADR 0054, the archive's manifest of
  ADR 0051, the tools' `tenant` and `?tenant=` of ADR 0042) amended in place in the same change.
- **The database keeps its names** — `tenants`, `tenant_id`, `app.tenant_id`, `app_tenant_id()`.

### The contract release

The old paths, parameters, properties, arguments, variables, values and the metric label go, once no
supported client reads them; the ADRs record that it is built. Until 1.0 no commit carries a breaking
mark ([ADR 0003](../adr/0003-test-and-ci-policy.md) D9); the change says in its body what it removes.

### The tests that prove it

- The backend integration tier: every operation answers on its new path, and on its old one alike
  with the same body, for the release; a property and a parameter by either name.
- A unit test over the bundled document: every old name is marked deprecated, and every new path has
  its old twin until the contract release removes the pair.
- The configuration's unit tests: the new variable, the old one with its warning, both and different
  refused.
- The import of an archive written before the release.
- The frontend unit tier and the end-to-end tier: no "tenant" left in what the UI shows.

## Open questions

### Q1: What does the UI call a tenant now that it is a team?

"Tenant" is the word of the API (`/api/v1/tenants/…`), the code, the database, the chart, the
metrics, the ADRs and every page of the UI today.

- **(a) Recommended:** "Team" in the UI and in the documentation for the people who use it — the
  sidebar, the gear's dialog, "All teams", "New team", the person menu; the API, `cowork-mcp`, the
  code, the database, the chart and the ADRs keep "tenant", the exact technical term of the
  isolation unit, with one line in README.md and docs/developer saying a tenant is shown as a team
  (there is no glossary page). No client breaks.
- **(b)** "Team" everywhere, the API's paths included (`/api/v1/teams/…`): one word throughout, at
  the price of a breaking change for `cowork-mcp`, the plugin, every token's client and the generated
  clients, with both paths served for a release (ADR 0028's expand before contract).
- **(c)** "Tenant" stays: no change, and the word the owner finds wrong stays in front of every
  person.

**Answer:** (b), 2026-10-10 — "team" everywhere, the API's paths included.

### Q2: How deep does the rename go below the surface?

Everything a person, an agent or an operator reads can change without touching the stored data; the
database's names are read by nobody but the code, and ADR 0028 D3 forbids renaming what the previous
release reads, so a rename there takes two releases of its own.

- **(a) Recommended:** every surface — the UI and its routes, the API's paths, parameters, properties,
  schemas and problem texts, `cowork-mcp`'s tools and the plugin, the configuration's variables and
  the chart's values (the old names read for one release, with a warning), the metrics' label, the
  export archive written from then on (an older archive's `tenant` still read), the documentation
  and the ADRs (marked in place, never rewritten silently). The Go and TypeScript code follows the
  generated names where they reach it. The database keeps `tenants`, `tenant_id`, `app.tenant_id`
  and `app_tenant_id()`, with one line in docs/developer saying a team is stored as a tenant.
- **(b)** (a) and the database: one word down to the last column, at the price of an expand and
  contract over two releases — new names beside the old, every row-level security policy and
  function rewritten — for names no person sees.
- **(c)** the UI and the API's paths only, everything else keeps "tenant": the least work, and an API
  whose paths say team while its properties and parameters say tenant.

**Answer:** (a), 2026-10-10.
