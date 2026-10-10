---
id: T87
title: the UI does not yet fit the owners daily work — what the owner found walking through it before use
state: decided
severity: medium      # the owner works around it every day; nothing breaks
security: none
threat:
urgency: next         # rule 3: severity medium, and the trigger is live — the owner starts using cowork
effort: M
filed-from: the owners walk through the UI under make dev, 2026-10-10
opened: 2026-10-10
decided: 2026-10-10
done:
---

## Current state

The owner walked through the UI under `make dev` and named what gets in the way of daily use. This
ticket holds the requirements, discussed in detail, for another session to build; nothing here is
built yet.

### 1. The sidebar shows one tenant at a time, and none on the person-level pages

- The sidebar of [shell.html](../../frontend/src/app/layout/shell.html) has three parts: "For you"
  (always), the tenant's pages (Overview, Board, Tickets, Members, Accounts, Group mappings, Audit
  record, Tokens, Time, Deleted tickets, Settings — each by role), and a section headed "Projects"
  with the tenant's projects and a `+` for a new one.
- The tenant's pages and its projects render only under `@if (session.tenant(); as tenant)`
  ([shell.html:146](../../frontend/src/app/layout/shell.html#L146)). The person-level pages
  (`/me/next`, `/me/inbox`, `/me/assigned`, `/me/decisions`, `/me/search`) are outside `/t/:tenant`;
  leaving a tenant route sets the session's tenant to `null`
  ([tenant-scope.ts:45-48](../../frontend/src/app/layout/tenant-scope.ts#L45-L48)), so a click on an
  item under "For you" hides every project.
- Inside a tenant the sidebar lists that tenant's projects only:
  [projects.service.ts:32](../../frontend/src/app/core/projects.service.ts#L32) follows
  `session.workTenant()`. Another tenant's projects are reached through the tenant switcher in the
  top bar.
- The API has no project list across tenants: `GET /api/v1/tenants/{tenant}/projects` only (paged,
  `include_archived`); the `/api/v1/me/…` routes are `next`, `assigned`, `decisions`, `inbox`,
  `search` and the person's own settings ([ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D2).
  The event stream is per tenant as well (`/api/v1/tenants/{tenant}/events`), and the UI connects
  the current tenant's only ([tenant-scope.ts:43](../../frontend/src/app/layout/tenant-scope.ts#L43)).
- Who may create a project is per tenant: an administrator always, a member while the tenant's
  setting `members_create_projects` allows it
  ([tenant.service.ts:33-38](../../frontend/src/app/core/tenant.service.ts#L33-L38)) — today read for
  the current tenant only.
- The top bar carries a tenant switcher while the person may open more than one tenant, otherwise
  the one tenant's name ([shell.html:8-28](../../frontend/src/app/layout/shell.html#L8-L28)). For a
  global administrator its list is their memberships and every other tenant of the installation
  ([session.service.ts:128-138](../../frontend/src/app/core/session.service.ts#L128-L138)); it is
  their only way into a tenant they oversee without a role. The tiles of every tenant the person may
  open exist on the start page for a person without a membership
  ([home.ts:40-55](../../frontend/src/app/features/home/home.ts#L40-L55)).
- [ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D2 and D4 and
  [ADR 0018](../adr/0018-the-views-of-the-first-release.md) D6 state the answers below as amended on
  2026-10-10, marked not built; the code follows the rules before.

Impact: the owner works across many tenants and projects; from any "For you" page every project is
gone, and from inside one tenant every other tenant's projects are a switch away.

### 2. The tenant's pages take most of the sidebar, and are rarely needed

- The tenant's block in [shell.html:146-241](../../frontend/src/app/layout/shell.html#L146-L241)
  holds up to eleven links, each its own route under `/t/{slug}`
  ([app.routes.ts](../../frontend/src/app/app.routes.ts)): Overview (the dashboard), Board (the
  tenant's board across its projects), Tickets (the tenant's list), Time, and Members, Accounts,
  Group mappings, Audit record, Tokens, Deleted tickets, Settings. Which ones show depends on the
  role: Accounts, Audit record, Tokens and Deleted tickets for an administrator, Group mappings for an
  administrator or a global administrator's oversight, Board, Tickets and Time for anybody who works
  in the tenant, Overview, Members and Settings for everybody.
- Settings ([tenant-settings.ts](../../frontend/src/app/features/tenant/tenant-settings.ts)) holds the
  tenant's own settings, its attachment usage and its export.
- No tenant page is a dialog today; the UI's only dialogs are small forms (a new project, a new
  account, a new mapping, a new token, a password reset).

Impact: an administrator's sidebar is mostly configuration they open a few times a month, and it
pushes the daily links — the person's lists and the projects — out of view.

### 3. A second tenant cannot be made in the UI

- The UI offers the form for a new tenant
  ([first-tenant.ts](../../frontend/src/app/features/home/first-tenant.ts): the slug checked as it
  is typed, a retry sent with the same `Idempotency-Key`) only while the installation has no tenant
  at all: `session.tenants().length === 0` in
  [home.ts:105-110](../../frontend/src/app/features/home/home.ts#L105-L110).
- `POST /api/v1/tenants` takes a global administrator's session only
  ([tenants.go:461-465](../../backend/internal/api/tenants.go#L461-L465)); a token gets `403`, so
  `cowork-mcp` cannot do it either. The creator becomes the tenant's first administrator
  ([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md) D7).

Impact: the owner cannot open a tenant for a new client without a hand-made request carrying his
session cookie.

### 4. The sidebar's footer shows the commit beside the version

- The foot of the sidebar shows `{{ v.version }} ({{ v.commit }})` from `GET /api/v1/version`
  ([shell.html:284-289](../../frontend/src/app/layout/shell.html#L284-L289)): a release reads
  `0.14.0 (<short hash>)`, and `make dev`, whose backend is built without the linker's values, reads
  `dev (unknown)`. The version is the release tag (`VERSION`, the commit `git rev-parse --short HEAD`,
  [Makefile:12-15](../../Makefile#L12-L15)).
- The owner needs the tag only; the hash is noise.

## Required changes

### 1. The sidebar lists every tenant of the person with its projects, on every page

- The sidebar shows **every tenant the person is a member of**, one below the other, on every page —
  the person-level pages included (Q1).
- Each tenant is a group **headed by the tenant's name** (the heading "Projects" goes away), with
  **that tenant's projects below it** (Q1); from the sidebar the person can see and manage every
  tenant and its projects at any time.
- **A click on the tenant's name opens its Overview**, the page that sums up the whole tenant (Q3,
  Q5).
- A tenant the person only oversees as a global administrator, without a membership
  ([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
  D2), gets no group.
- **The tenant switcher and the tenant's name leave the top bar** (Q5): the sidebar is the way
  between the person's tenants. A global administrator gets **"All tenants" in the person menu**,
  a page of its own with every tenant of the installation and the person's role in it or "no role"
  — the start page's tiles under a route of their own — and from there the oversight of a tenant
  without a role.
- **The project lists come from a new `GET /api/v1/me/projects`** (Q7), built like the other
  person-level lists: one iteration per tenant of the person
  ([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D5), every
  project naming its tenant, `?tenant=<slug>` narrowing to one. The current tenant's projects stay
  live through its event stream as today; the other tenants' are loaded again when the tab regains
  focus, when the person enters a tenant and when their memberships change. `cowork-mcp` may list
  them as well.

Not asked, built on the recommendation for the owner to critique the result:

- The tenants in the order the switcher has today, by slug
  ([session.service.ts:137](../../frontend/src/app/core/session.service.ts#L137)); each tenant's
  projects as today — by key, archived ones left out, each leading to its board.
- Each tenant's group can be collapsed; the browser remembers per tenant whether it is (local storage,
  a convenience only); the current tenant's group is always open, its name and the current project
  marked active.

### 2. The sidebar holds the daily links only; a tenant's configuration is a dialog

- The sidebar is reserved for the links that matter in daily work (Q2).
- **A tenant's sidebar group is its name, a small gear next to the name, and its projects**, nothing
  else (Q2, Q3).
- **The Overview carries the tenant-wide Board, Tickets and Time as tabs** beside the dashboard (Q3).
- **The gear opens one modal dialog for the tenant's configuration**, with everything else as its
  tabs, each by the role that shows the page today (Q2, Q3): Members, Accounts, Group mappings,
  Tokens, Settings, Audit record, Deleted tickets. Audit record and Deleted tickets keep their
  filters, paging, restore and purge, so the dialog is large, close to the full window.
- **The dialog keeps today's addresses** (Q4): `/t/{slug}/members`, `/t/{slug}/accounts`,
  `/t/{slug}/group-mappings`, `/t/{slug}/tokens`, `/t/{slug}/settings`, `/t/{slug}/audit` and
  `/t/{slug}/deleted-tickets` each show the dialog on its tab over that tenant's Overview. The gear
  navigates there; closing goes back to the page before, or to the Overview when the address was
  opened directly. Reload, the back button and a bookmark keep working, and the dialog's services
  keep taking their tenant from the route, as [accounts.service.ts:33](../../frontend/src/app/core/accounts.service.ts#L33)
  and the others do today. The gear of another tenant therefore makes that tenant the current one,
  its event stream included.

### 3. A global administrator makes a tenant on the "All tenants" page

The "All tenants" page of Q5 carries a **"New tenant"** button that opens today's form of
[first-tenant.ts](../../frontend/src/app/features/home/first-tenant.ts) as a dialog (Q6). Once the
tenant exists the person, its administrator, finds it in the sidebar, and the UI opens its Overview.
The start page keeps the form for an installation without any tenant.

### 4. The footer shows the version only

- [shell.html:286](../../frontend/src/app/layout/shell.html#L286) renders `{{ v.version }}` alone;
  "backend unreachable" stays as it is.
- The test "shows the version and the commit of the backend"
  ([shell.spec.ts:891-896](../../frontend/src/app/layout/shell.spec.ts#L891-L896)) expects `1.0.0`
  and is renamed for the version alone.
- `GET /api/v1/version` keeps `commit` and `build_time`: an operator reads the running build there
  ([build-test-lint.md](../developer/build-test-lint.md)); only the UI stops showing the hash.

### The words

A tenant is a team now, and "team" replaces "tenant" in the UI and the API (T89): the pages of this
ticket read "All teams", "New team" and the team's name, whichever release ships them first.

### The tests that prove it

- The frontend unit tier: the shell lists every team of the person with its projects on a
  person-level page and inside a team, none for a team a global administrator only oversees; a
  team's group collapses and remembers it; the gear opens the dialog on the tab of its address, each
  tab by the role that shows it today, and closing returns to the page before or to the dashboard;
  the top bar has no switcher and no team name; "All teams" is in the person menu of a global
  administrator only; the footer shows the version alone.
- The backend integration tier: `GET /api/v1/me/projects` answers every project the person sees in
  each of their teams and nothing of a team they left, a restricted project only for those on its
  list (ADR 0021 D5's unions, ADR 0034 D4).
- The end-to-end tier: a person with two teams reaches a project of the other team from a "For you"
  page in one click, and opens and closes the gear's dialog by its address.

## Open questions
## Open questions

### Q1: What does the sidebar show of the projects?

(a) every tenant of the person with its projects, on every page; (b) the last tenant opened stays
in the sidebar on the person-level pages; (c) as today, with a list of the tenants under "For you".
Recommended: (a) — cowork is one person across many projects, and "For you" spans tenants already.

**Answer:** (a), 2026-10-10 — every tenant the person is a member of, one below the other, each
headed by its name with its projects below, so that every tenant and its projects can be seen and
managed at any time.

### Q2: What does a tenant's group hold besides its projects?

(a) the name leading to the Overview, a menu beside it with the tenant's pages, the projects below;
(b) every group expanded with all its pages and its projects; (c) the pages for the current tenant
only, as a block above the groups. Recommended: (a) — every page of every tenant two clicks away, one
row per tenant plus its projects.

**Answer:** neither, 2026-10-10 — the tenant's pages take far too much room for how rarely they are
needed; the sidebar is reserved for the links of daily work, and every setting of a tenant goes into
a modal dialog for the tenant's configuration, opened by a small gear next to the tenant's name.

### Q3: Which tenant pages are daily work, and which go into the dialog?

(a) the group is name, gear and projects; the name leads to the Overview with Board, Tickets and Time
as tabs; the dialog holds the other seven pages; (b) Board, Tickets and Time stay links in every
group, the dialog holds the other seven; (c) as (a), with Audit record and Deleted tickets as pages
of their own, linked from the dialog. Recommended: (a) — one rule (the sidebar is the work, the gear
is the rest), and the sidebar grows with projects, not with tenants times pages.

**Answer:** (a), 2026-10-10.

### Q4: Does the gear's dialog have an address of its own?

(a) yes, today's routes, the dialog over the tenant's Overview; (b) a query parameter on any page,
the dialog over the page the person is on, its seven services freed from the page's tenant; (c) no
address, the old routes redirect to the Overview. Recommended: (a) — reload, back and bookmarks keep
working, ADR 0023 D4 holds, and the services keep taking their tenant from the route.

**Answer:** (a), 2026-10-10.

### Q5: What becomes of the tenant switcher in the top bar?

(a) it goes, with the tenant's name; a global administrator reaches every tenant of the installation
through "All tenants" in the person menu; (b) it stays for a global administrator only; (c) it stays
as it is. Recommended: (a) — the daily way is the sidebar, the rare administration sits behind a
menu, and nothing is listed twice.

**Answer:** (a), 2026-10-10 — and the Overview, which sums up the whole tenant, is what a click on the
tenant in the sidebar opens.

### Q6: Where does a global administrator make another tenant in the UI?

(a) a "New tenant" button on the "All tenants" page, today's form as a dialog; (b) "+ New tenant"
at the foot of the sidebar for a global administrator; (c) as today, the API only. Recommended: (a) —
a rare act of the installation's administration, next to the list of every tenant, with the form
reused.

**Answer:** (a), 2026-10-10.

### Q7: Where do the sidebar's project lists come from, and how do they stay current?

(a) a new `GET /api/v1/me/projects`, one iteration per tenant, the current tenant live through its
stream, the others loaded again on focus, on entering a tenant and on a membership change; (b) one
`…/projects` per membership from the frontend, kept current the same way; (c) a person-level event
stream across tenants. Recommended: (a) — one request instead of N, the known pattern, and usable by
`cowork-mcp`.

**Answer:** (a), 2026-10-10.
