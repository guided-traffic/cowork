# Team isolation and visibility inside a team

How one team's data stays out of another team's reach, who belongs to a team and in which
role — group mappings, grants, the last administrator — and who inside a team sees which project,
ticket, act, event, notification and time entry, and what the person-level lists and stream gather
across a person's teams, and what a search finds, as built on 2026-10-07. What a token or an agent may do with
what it can see is [tokens.md](tokens.md); how a request reaches the backend at all, and where the
database credentials live, is [trust-boundaries.md](trust-boundaries.md); where a person's groups
come from, and when a mapped membership follows them, is
[identity-provider.md](identity-provider.md); what becomes of an upload's bytes is
[attachments.md](attachments.md).

## Two lines

The team is the isolation unit
([ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)). Two
independent mechanisms keep it
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md)):

1. **The request layer.** The team is named in the path and admitted before any handler
   runs; every query runs in a transaction bound to that team and names `tenant_id` itself
   (ADR 0021 D4); inside the team, the visibility predicates decide which projects and
   tickets the caller sees.
2. **The database.** Row-level security, forced on every table, admits only the
   transaction's team's rows to a runtime role that cannot bypass it. A query that forgets
   its team filter returns no rows of another team, not foreign rows.

The database keeps the name a team had before: a team is a row of `tenants`, a team-bound row
carries its `tenant_id`, and a transaction names its team in `app.tenant_id`, read through
`app_tenant_id()` (ADR 0005 D1) — which is why the policies and queries below say tenant.

The second line makes a forgotten team filter harmless. Inside a team the first line is the
one that holds the visibility predicate: a query that forgot it would show a restricted project or a
confidential ticket to everyone in the team, which is why a unit test holds every query to the
predicate ("Visibility inside a team"). Several tables carry a second line inside the team as
well, restrictive policies that hold them to whom they belong: a notification and a saved filter to
their person, an import job and a consistency check's result to the team's administrators and
the jobs that need them ([below](#row-level-security-forced-on-every-table)). Neither line stops a process that runs SQL of an
attacker's choosing — see "A compromised serving process" at the end.

## The team is in the path, and a refusal looks like absence

Every team-bound route lives under `/api/v1/teams/{team}/…`, plus the key resolver
`/api/v1/tickets/{team}/{key}` ([ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D1,
D3). After authentication and before validation and the handler, `boundary`
([`backend/internal/api/tenant.go`](../../backend/internal/api/tenant.go)) reads the team
by its slug together with the caller's memberships, in a transaction whose policies admit
only the caller's own memberships and teams. It answers the same `404 not_found` — "no such
team" — when the slug is unknown, when the person is not a member, when the token is
restricted to another team, and when a project-restricted token calls a team route
outside its project (ADR 0023 D5,
[ADR 0047](../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D5).
`TestATokenOfOneTenantCannotSeeAnother` compares the refusal's type, title, status, detail
and code with the answer for an unknown slug. The one person the boundary admits without a
membership is a global administrator, to the team's administration and nothing else
([below](#a-global-administrator-without-a-role)).

**The names before are the same routes.** For this release every team path, and `/api/v1/teams`
itself, is also served under `/api/v1/tenants` — `/api/v1/tenants/{tenant}/…`, the name a team had
before, as a deprecated twin in the API document — so that a client of 0.14 keeps working
([ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1,
[ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md) D7). A twin is no route
of its own: the API's handler reads a twin's path as its team path before it finds the route
(`asTeamPath` in [`api/api.go`](../../backend/internal/api/api.go)), so a twin meets the same
authentication, the same security requirement, the same boundary and the same handler as its team
path and answers as it does; only the request log keeps the path as it was sent.
`TestNoTwinIsEverRouted` routes every twin of the document and finds its team path's operation
every time, `TestATwinIsAnsweredAsItsTeamPath` holds the rewrite to the family alone, and
`TestATwinAnswersAsItsTeamPath` compares the answers of both families byte for byte. The walks of
the boundary — `TestEveryTenantRouteRefusesAnotherTenantLikeNoTenant`, its session variant and
`TestAGlobalAdministratorWithoutARoleSeesTheAdministrationOnly` — take every operation of both
families from the document, so a route added under the team family is held to the boundary under
both names the day it exists. The resolver renamed only its parameter and has no twin.

Inside the team the same rule holds one level down: a project or a ticket the caller cannot
see answers the `404` of one that does not exist (`visibleProject`, `visibleTicket` in
[`projects.go`](../../backend/internal/api/projects.go) and
[`tickets.go`](../../backend/internal/api/tickets.go)), and so does every comment, question,
attachment, link, stake and time entry of such a ticket. A role that forbids an act on
something the caller can see answers `403`. A request without a valid token is answered
`401` before any team is looked up.

The person's own routes under `/api/v1/me` are not team routes; they show what is the
person's across teams — their memberships and their tokens. A restricted token sees only its
team's membership and itself there ([tokens.md](tokens.md) "Restrictions"). The ones that read
teams' data — the inbox, "next for me", "assigned to me", the open decisions and the search
([below](#the-person-level-lists-are-unions-one-team-at-a-time)), and the repository lookup
(`GET /api/v1/me/repositories/lookup`,
[ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D2) — read each of the person's teams in a transaction bound to that team, one after the
other ([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D5).
The lookup finds only the bindings of projects the caller sees, and names each binding's team; a token
restricted to a team reads that team only, and one restricted to a project reads that
project's bindings only ([`api/repositories.go`](../../backend/internal/api/repositories.go)
`LookupRepository`, `TestLookingUpARepository`).

**Every route under a team meets the boundary.** The one route that did not, GitHub's webhook,
which read its team by the slug itself, is removed with the webhook ([ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
Status), and with it the one answer — its `401` — that told a team's existence to a caller
without a credential.

**A turn of the chat stays in its team** on top of the boundary. Its tool calls are the person's
requests and could reach every team the person belongs to; the loopback that sends them refuses
every path outside the turn's team — the person's other teams and the `/api/v1/me` routes
included — and a search of every team looks through the turn's alone
([`chat.Loopback`](../../backend/internal/chat/loopback.go); `TestTheChatStaysInItsTeam`). A turn
therefore sends its provider one team's text, and nothing of another team reaches the provider
through it ([chat.md](chat.md#a-turn-works-in-its-team)).

## A global administrator without a role

A global administrator has no role in a team they were not given
([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2), but they see its administration, so that a team that lost its last administrator who can log
in can be given one again ([identity-provider.md](identity-provider.md#h-29) H-29,
[local-accounts.md](local-accounts.md#h-32) H-32):

- **They find every team.** `GET /api/v1/teams` lists the installation's teams by slug, each
  with the role the caller holds in it or `null`; anybody who is not a global administrator is
  `403 forbidden` ([`api/tenants.go`](../../backend/internal/api/tenants.go) `ListTeams`;
  `TestOnlyAGlobalAdministratorListsEveryTenant`).
- **They see the administration of a team without a role.** Where the boundary finds no
  membership, it admits a global administrator to four operations — `getTeam` (the team and its
  settings), `listMembers` (without the addresses, which are the team's administrators'),
  `listGroupMappings`, and `setMemberGrant` on their own person — and answers every other route of
  the team the `404` of an unknown team: no project, ticket, comment, question, attachment, time
  entry, event stream, audit row, account, chat or setting's change
  ([`api/tenant.go`](../../backend/internal/api/tenant.go) `oversight`, `oversees`, `overseen`).
  `TestAGlobalAdministratorWithoutARoleSeesTheAdministrationOnly` walks every route of the API
  document under a team as such a global administrator and compares each refusal with the unknown
  team's answer, so a route added later is held to it the day it exists. The handlers of the four
  read the boundary's mark (`tenantScope.Oversight`, `administrationRead`); every other handler
  asks for a role the scope does not carry and would answer `403` besides.
- **They grant themselves a role** with `PUT …/members/{their id}/grant`, in any role: a marked
  grant like any other, made under the team's lock, recorded in the team with them as its actor
  and announced to its members as `membership.changed` (`grantSelf`;
  `TestAGlobalAdministratorGrantsThemselvesARole`). It takes no administrator away and is never
  `409 last_admin`, which is what lets it recover a team without one
  (`TestAStrandedTenantIsRecoveredByTheSelfGrant`). A grant to anybody else is `403 forbidden`, and
  adding a member by address or username is not among the four. Afterwards the team answers them
  as any member of that role.
- **They raise a lower role.** A global administrator who holds a role below `admin` in the team —
  mapped or granted — sets their own grant through the same route the same way: made, or its role
  changed, recorded as the grant's `created` or `updated` with them as actor, announced, never
  `last_admin` (`ownGrant`, `setOwnGrant`;
  `TestAGlobalAdministratorWithALowerRoleRaisesTheirOwnGrant`). A viewer who is no global
  administrator is `403 forbidden`, as before. Once they hold `admin`, their own grant is an
  administrator's like any other: lowering it meets `last_admin` — also when another administrator
  gave them `admin` after the boundary read their role, which the change checks under the lock.
- **It is a browser session's.** The list takes a session in the document
  ([tokens.md](tokens.md#what-only-a-session-does)); the reach into a team without a role is held
  by the boundary to a session that no agent header marks. A token of a global administrator keeps
  the reach of the person's memberships — a leaked one lists no team and reads none the person
  is not in — and an agent, the chat in the UI among them, reaches none of it; the grant
  is session-only for everyone.

**What row-level security holds here.** `tenants` admits every row to a global administrator, so the
list and the boundary read the team in a transaction that names no team, and `memberships`
admits a global administrator's own grant in any role
([migration 26](../../backend/internal/store/migrations/000026_global_admin_self_grant.up.sql)), and
the change of their own grant's role inside the team's transaction. With no team set, their transaction reads no team's members, mappings, projects, tickets, time,
attachments, comments or audit rows (`TestPoliciesOfTheGlobalAdministratorsReach`). Inside the
team's transaction the team-bound policies admit whomever the request layer admitted, as they
do for a member (ADR 0021 D3): the four operations are the request layer's list, and the data
layer does not repeat it.

## Two database roles

| Role | Owns | Holds | Used by |
|---|---|---|---|
| owner | every object of the schema | DDL | `cowork migrate`, the chart's migration run — the init container, or the migration Job in job mode —, and `cowork serve` while `COWORK_MIGRATE_ON_START` is true |
| runtime | nothing | only what the migrations grant it; the migration run names it in the session setting `cowork.runtime_role` | `cowork serve`, `cowork check-consistency`, and the bootstrap `cowork migrate` runs after the schema with `COWORK_MIGRATE_BOOTSTRAP=true` — the migration Job's |

The runtime role's grants are narrow: `UPDATE` only where the API changes something — column
by column on the teams, projects (their restriction among them), tokens, tickets, questions,
comments, stakes, time entries, persons (the identity provider's columns among them), local
accounts, sessions (the groups refresh's among them), repository bindings, memberships and group
mappings (a role and a version each), a project's access list (its role), a person's chat
capabilities, a notification's read mark, saved filters, a consistency check's result and an import
job — and, until a contract migration drops them, the tables of GitHub's webhook, which no code writes
since its removal ([ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md) Status) —, table-wide on
`ticket_counters`, `idempotency_keys` and `login_locks` — `DELETE` only on `ticket_links`,
`ticket_interest`, `project_repositories`, `idempotency_keys`, `sessions`, `login_attempts`,
`login_locks`, `memberships`, `group_mappings`, `project_access`, `saved_filters`,
`notifications`, `github_webhook_secrets`, `github_deliveries`, `consistency_acceptances` and
`import_jobs`, and on a ticket and what belongs only to it — its questions, comments and their
revisions, attachments, time entries and their revisions, pull-request links — where restrictive
policies admit the purge of a deleted ticket alone ([below](#a-deleted-ticket-answers-like-a-missing-one)), and only
`INSERT` and `SELECT` on `audit_events`, which makes the audit record append-only by grant
([ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D3); the one
update of an audit row is the purge's, through the owner's function `purge_ticket_audit`, which
empties the content of a deleted ticket's rows and nothing else.
`TestTheAuditRecordIsAppendOnly` shows that `UPDATE`, `DELETE`, `TRUNCATE` and switching
row-level security off are refused, and that a grant to itself grants nothing. The role
inserts a team, a person, a membership, a token, a session, a local account, a group mapping or
an entry of an access list only where a policy of migrations
[15](../../backend/internal/store/migrations/000015_local_accounts.up.sql),
[16](../../backend/internal/store/migrations/000016_sessions.up.sql),
[20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)–[22](../../backend/internal/store/migrations/000022_membership_administration.up.sql),
[25](../../backend/internal/store/migrations/000025_group_mappings_global_admin.up.sql) and
[26](../../backend/internal/store/migrations/000026_global_admin_self_grant.up.sql)
admits it — an administrator of the current team, a global administrator creating a team, the
person for their own token and session, or a named system actor: the login, the start-up
synchronisation, the identity provider — and updates only the columns those grants list
(`TestPoliciesOfThePersonsAndTheirAccounts`, `TestPoliciesOfTheSessions`). A policy reads settings
the process itself writes, so these policies hold a defect, not a compromised process (below).

`cowork migrate` refuses a runtime role that is the owner role by name, and checks the
runtime role from the owner's connection before and after the run
([`store/migrate.go`](../../backend/internal/store/migrate.go) `Migrate`); `cowork serve`
checks its own connection's role before it listens
([`store/roles.go`](../../backend/internal/store/roles.go) `CheckRuntimeRole`). Both refuse a
role that is a superuser, has `BYPASSRLS`, owns a relation of the `public` schema, or is a
member of the role that owns `tenants` — each of which could read past row-level security or
switch it off (ADR 0021 D2). The integration tier asserts the refusal of the owner role, a
superuser and a member of the owner role
([`test/integration/store_test.go`](../../backend/test/integration/store_test.go)); the
`BYPASSRLS` refusal is read from the code and exercised by no test. `cowork serve` also
refuses a dirty schema and pending migrations.

## Row-level security, forced, on every table

Every table the migrations create has row-level security enabled and forced — forced, so the
policy binds the table's owner as well — with at least one policy and a grant to the runtime
role; a unit test holds the migration files to that without a database
([`store/policy_test.go`](../../backend/internal/store/policy_test.go)
`TestEveryTableHasItsPolicyAndGrant`). Every table that carries a `tenant_id` has the
canonical policy

```sql
CREATE POLICY tenant_isolation ON <table>
    USING (tenant_id = app_tenant_id())
    WITH CHECK (tenant_id = app_tenant_id());
```

except the tables ADR 0021 D6 names one by one, whose rows a person reads across teams or
which have no team at all:

| Table | Its policy admits |
|---|---|
| `tenants` | the row inside its own team's transaction, and to its members; every row to a global administrator (`tenants_read`, migration 26); updates only inside its own transaction; read by the login, the start-up synchronisation, the identity provider, GitHub's webhook and the consistency check named in `app.job` — so a login can ask whether any team exists and the check every team (migrations 41, 42); the webhook's job is named by no code since its removal; inserted by a global administrator or the synchronisation |
| `users` | the person, everyone who shares the current team with them, the login and the synchronisation, and the identity provider every person; a team's administrator also the persons a lookup by address or username names (`app.person_lookup`, below); inserted by an administrator of the current team (never a global administrator, never a person of the identity provider), by the synchronisation, or by the identity provider (only a person of the provider, without a username); updated by the administrators of the accounts their team manages, by the synchronisation, and by the identity provider (only its own persons) |
| `memberships` | the team's rows inside the team, and the person's own rows everywhere; a grant inserted by an administrator into their own team, by a global administrator for themselves in any role (migration 26), or by the synchronisation, and changed and removed by an administrator of the team — a global administrator's own also changed by them (migration 26); a mapped membership inserted, changed and removed by the identity provider alone |
| `tokens` | the person's own rows, and during the lookup the one row whose hash the transaction names in `app.token_hash`; the administrators of a managed account and the synchronisation read and revoke its tokens; an administrator of the current team reads and revokes every token of a member of it that is unrestricted or restricted to it, and no token restricted to another team (`app_tenant_reaches_token`, migration 35; [tokens.md](tokens.md#h-57) H-57); inserted for the person's own account only |
| `idempotency_keys` | the person's own rows, and every row to the expiry job named in `app.job` |
| `audit_events` | a team's rows inside that team, an installation-level row to the person it names; a row is inserted only into the context it belongs to |
| `local_accounts` | the person's own row, the managing team's administrators, the login and the synchronisation; inserted for `tenant` by an administrator of that team and for `config` by the synchronisation, updated by the person only while a `tenant` account |
| `sessions` | the person's own rows, the one row whose hash the transaction names in `app.session_hash`, the administrators of a managed account, a global administrator for reading, and the two jobs that end sessions; inserted for the person's own only |
| `login_attempts`, `login_locks` | the login, its expiry job and the synchronisation; the administrators of a managed account read and clear the rows of its username |
| `chat_capabilities` | the person's own row alone, read, inserted and updated by the person, with no delete grant ([migration 24](../../backend/internal/store/migrations/000024_chat_capabilities.up.sql); [chat.md](chat.md#the-chats-mark-its-capabilities-and-what-only-a-session-does)) |

Seventeen of the twenty-two team-bound tables carry policies beside `tenant_isolation`: the
ticket and what belongs only to it — its questions, comments and their revisions, attachments, time
entries and their revisions, pull-request links —, whose deletes the purge alone may make
([below](#a-deleted-ticket-answers-like-a-missing-one)); `notifications` and `saved_filters`, held to
their person ([below](#the-person-level-lists-are-unions-one-team-at-a-time),
[below](#saved-filters-are-their-owners-and-a-shared-one-an-administrators-to-withdraw));
`github_webhook_secrets` to the team's administrators and the webhook's job, `github_deliveries`'s
writes to that job and its reads and deletes past the team to its expiry job, and
`ticket_pull_requests`' inserts to the webhook's job and its deletes to the purge
([migration 41](../../backend/internal/store/migrations/000041_github_webhook.up.sql)) — tables
no code reads or writes since the webhook's removal but the purge, which deletes a deleted ticket's
links, and which a contract migration of a later release drops;
`consistency_checks` and `consistency_acceptances` to the team's administrators and the check's job
([migration 42](../../backend/internal/store/migrations/000042_attachment_consistency.up.sql));
`import_jobs` to a job's maker, the team's administrators, its expiry job and the purge
([migrations 43](../../backend/internal/store/migrations/000043_import_jobs.up.sql) and
[45](../../backend/internal/store/migrations/000045_import_jobs_of_their_writer.up.sql)); and
`group_mappings` and `project_access`. `group_mappings` is read across
teams by the identity provider, which derives a person's memberships in every team at once, and
the bootstrap team's mapping is inserted by the synchronisation
([migration 21](../../backend/internal/store/migrations/000021_group_mappings.up.sql)). On it and
on `project_access` a write needs an administrator of the current team in the data layer as well
as in the handler: `AS RESTRICTIVE` policies, which a write must pass in addition to whatever a
permissive policy admits — `group_mappings_admin_insert`, `…_update`, `…_delete` (the bootstrap's
insert excepted) and `project_access_admin_insert`, `…_update`, `…_delete`
([migration 22](../../backend/internal/store/migrations/000022_membership_administration.up.sql)).
A mapping's insert and update need a global administrator besides — `app_is_tenant_admin() AND
app_is_global_admin()` ([migration 25](../../backend/internal/store/migrations/000025_group_mappings_global_admin.up.sql);
[below](#members-grants-and-group-mappings)) — and its delete any administrator of the team.
A project's restriction is held the same way, by a trigger, because a policy sees the row and not
the column and a member may rename a project: `projects_restriction_guard`, `BEFORE UPDATE OF
restricted`, refuses a change of `restricted` unless the caller is an administrator of the team
(SQLSTATE `42501`) — a superuser, whom row-level security does not bind either, excepted
(`TestPoliciesOfThePersonsAndTheirAccounts`). An administrator's unshare of another person's saved
filter is held by a trigger for the same reason ([below](#saved-filters-are-their-owners-and-a-shared-one-an-administrators-to-withdraw)).

At the start of every transaction the store sets `app.tenant_id`, `app.user_id`,
`app.restricted_project_id`, `app.job` and `app.session_hash` — the hash of the session cookie
a request presented, which is how a request finds its own session row — with
`set_config(…, true)`, which dies with the
transaction ([`store/tx.go`](../../backend/internal/store/tx.go) `setContext`). Three transactions
set one more each: a token's lookup names the presented hash in `app.token_hash`
([`store/tokens.go`](../../backend/internal/store/tokens.go) `LookupToken`); the lookup of a person a
team's administrator grants a role to names the address or username in `app.person_lookup`, read through `app_person_lookup()`
([`store/members.go`](../../backend/internal/store/members.go) `FindPerson`), and a team
administrator's unshare of another person's saved filter names that filter in
`app.saved_filter_id`, read through `app_saved_filter_id()`, for its single statement
([`store/filters.go`](../../backend/internal/store/filters.go) `Writer.UnshareAnothersFilter`,
[below](#saved-filters-are-their-owners-and-a-shared-one-an-administrators-to-withdraw)). The person
is the authenticated caller, carried in the context and never a call site's argument
([`store/caller.go`](../../backend/internal/store/caller.go)); the team is the one the
boundary admitted, which every handler passes on. The policies read the settings through
`app_tenant_id()`, `app_user_id()` and
`app_restricted_project_id()`, which turn an unset or empty setting into `NULL`: a pooled
connection keeps an empty string after its transaction, a bare cast of it would raise, and
`NULL` matches no row. A unit test holds every policy to the guarded form
(`TestPoliciesReadSettingsGuarded`), and `TestTheContextDiesWithItsTransaction` shows that a
connection that just served a team sees nothing once its transaction ended.

Every query on cowork's data runs inside one of the store's wrappers — `InTenant` and
`Installation`, which are read-only transactions; `Mutate`, which commits a write only
together with an audit row per act; `RunJob`, a background job under a system actor — or in
the token and session lookups, the last-used write and the session's idle clock, the login's
reads, the transaction that counts and decides a login attempt, the person lookup, the claim of a
groups refresh, the identity provider's transactions — a login, the application of a refresh's
answer, a token's gate check —, the read of a person's chat capabilities (`ChatCapabilities`) and the
consistency check's reads past the teams (`jobRead`), which set their
own context. The connection pool is unexported; outside the wrappers the store reads only the schema
version and the role catalog for its start-up checks, answers the readiness check's `Ping`, and holds
the listener connection of the event stream
([`store/store.go`](../../backend/internal/store/store.go)).

`TestUnfilteredQueryUnderTenantSeesNothingOfAnother` is the proof across teams: it walks the
catalog for every table with a `tenant_id` column, seeds each with a row of a second team,
and asserts that an unfiltered query as the runtime role under the first team sees none of
them — a new table is covered the day it is created.

A migration whose own statements rewrite rows lifts the force for itself. The rank's backfill
([migration 17](../../backend/internal/store/migrations/000017_ticket_rank.up.sql)) rewrites
`tickets` as the owner with no team set, which the forced policy would hide every row from,
so it runs `NO FORCE` before the backfill and `FORCE` after it
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D1); later
backfills do the same on `tickets` and `ticket_interest`, and the rewrites of a capability's old
name ([migration 38](../../backend/internal/store/migrations/000038_horizon_names_only.up.sql),
[migration 40](../../backend/internal/store/migrations/000040_capability_checks_set_horizon_only.up.sql))
on `tokens`, `chat_capabilities` and `saved_filters`. Each file runs as one transaction, which
holds a table exclusively from its first `ALTER` on: no other transaction sees the table
unforced, and a failed run rolls the lift back with the rest.
The runtime role is held by the policy either way — the force concerns the owner alone.
`TestLiftedForceIsRestoredInTheSameMigration` holds every lift to a restore in the same file,
which `TestEveryTableHasItsPolicyAndGrant` alone would not notice, and
`TestRankMigrationKeepsNumberOrder` reads the force back after the run.

## A row never points into another team

A plain foreign key ignores row-level security, so a row of one team could name a parent of
another. Every reference from one team-bound row to another is therefore a composite key
that includes `tenant_id`: a project's access list and ticket counter to the project; a
ticket to its project, to its parent — of the same project as well
([ADR 0008](../adr/0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D2)
— and to the ticket a block waits on; both ends of a link; questions, comments, stakes, time
entries and attachments to their ticket; a comment's revisions to the comment, a time
entry's revisions to the entry, and a comment's attachment to a comment of the same ticket; a
token's project restriction to a project of its team restriction; a repository binding to its
project; a notification to its ticket; a pull-request link to its ticket; an acceptance of a lost
file to its attachment; an import job to its project, and an imported ticket to its job (migrations
3, 4, 8–14, 23, 30 and 41–43). The one plain key from a team-bound row is a notification's
`audit_event_id`, to the audit record, whose rows carry their team.
A link therefore never crosses a team, whatever the API does
([ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D2).

References to persons are plain foreign keys, because a person is not team-bound. The API
admits as assignee only a person who can see the ticket's project, and asks a question only
of a person who can see the ticket. The audit record's `ticket_id` has no foreign key on
purpose: an act outlives its ticket (ADR 0026).

## Visibility inside a team

Three SQL functions decide what the caller sees within the team (migrations
[7](../../backend/internal/store/migrations/000007_visibility.up.sql) and
[8](../../backend/internal/store/migrations/000008_tickets.up.sql)):

- `app_is_tenant_admin()` — the caller holds the `admin` role in the current team.
- `app_project_visible(project)` — the project is the token's own if the token is restricted
  to one; it belongs to the team; and it is unrestricted, or the caller is an
  administrator, or the caller is on its list.
- `app_ticket_visible(project, confidential, assignee, reporter)` — the project is visible,
  and the ticket is not confidential or the caller is an administrator, its assignee or its
  reporter.

They are not policies: row-level security holds the team, the predicates hold the project
restriction and the confidential flag
([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D4, [ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D4). A unit test reads every query file and fails on a query that reads `tickets` with fewer
calls of `app_ticket_visible` than tickets it reads, or that reads `projects` and no ticket
without `app_project_visible`, unless the query states why it is exempt; a predicate on a
joined ticket does not stand in for the ticket the query reads
([`store/queries_test.go`](../../backend/internal/store/queries_test.go)). Two things are
outside that test's reach. A query on what belongs to a ticket that does not join `tickets` —
the comment a write changes, the per-ticket attachment count, the activity rows — is held by
the handlers, which run it only after reading the ticket through the predicate. The one query
built at run time, the ticket list
([`store/tickets.go`](../../backend/internal/store/tickets.go) `ListTickets`), adds the
predicate in code; `TestConfidentialTickets` and `TestRestrictedProjectTickets` compare the
list with the single read for each kind of caller.

Among the exemptions, each with its reason written in its query file:

| Query | Why it reads past the predicate |
|---|---|
| `GetWrittenTicket` | a write's answer rereads the row it wrote; a reassignment can take a confidential ticket out of its writer's sight in the same transaction |
| `TicketFacts` | the publication of a committed act; each event stream filters (below) |
| `ParentChainContains`, `BlocksPathExists` | integrity walks that answer yes or no (H-3) |
| `CanSeeProject` | whether another person — an assignee — sees what the caller reads |
| `ListWatchers` | whom an act tells: the watchers of a ticket, each then held to their own sight of it by `person_sees_ticket` ([the person-level lists](#the-person-level-lists-are-unions-one-team-at-a-time)) |
| `ProjectKeyTaken` | whether a project key is taken (H-3) |
| `GetRepositoryBinding` | whether the team binds a repository at all: the identity and path are unique in the team, and the `409 repository_bound` names the project only when the caller sees it |
| `LastRank`, `ListUnrankedTickets`, `GetTicketRank`, `NextRankedTicket`, `PreviousRankedTicket`, `ListRankKeys` | the rank keys of the project a write hands a key out in: a new key lies between keys that exist, a hidden ticket's included, so none is handed out twice, and a rebalancing spreads every key, so a hidden ticket keeps its place (H-3) |
| `GetScoreInputs` | the inputs of the score of a ticket the writer read through the predicate in the same transaction, read again after its write |
| `TenantAttachmentUsage`, `ListCheckedAttachments` | the team's stored bytes, and the files of the team whose bytes are missing, for its administrators, who see every ticket ([attachments.md](attachments.md#the-consistency-check)) |
| `ImportNumbersTaken` | whether a number exists in the project, as its unique key holds it ([import-and-export.md](import-and-export.md#what-an-import-creates)) |
| `ExportHiddenConfidential` | the count of the confidential tickets an export leaves out ([import-and-export.md](import-and-export.md#h-74) H-74) |
| `GetPurgedTicket`, `DeletePurgedTicket` and the acts of the purge | the purge of a deleted ticket, which an administrator or the job named |

`person_sees_ticket` (migration 30) answers whether another person — not the caller — sees a ticket,
past the caller's predicate: `CanSeeTicket` (the person a question is asked of) and the recipients of a
notification read through it.

Where the predicate hides a related ticket, the visible one shows less rather than more: a
parent or a ticket a block waits on that the caller cannot see is left out of the ticket's
`parent` and `block.ticket` (the block's kind and reason remain), a link whose other end is
hidden is absent from the list, and the `blocked` filter, the prerequisites of the done act and
a ticket's `open_prerequisites` count only the blockers the caller sees. The team's dashboard
counts, names and measures only what the caller sees, and no deleted ticket, every one of its
queries under the predicate and the deletion filter
— a median of lead time or the oldest blocked ticket moves for nobody who cannot see the ticket
that moves it —, and a `project` filter that names a project the caller cannot see answers exactly
as one that names no project, its weak `ETag` included
([api_dashboard_test.go](../../backend/test/integration/api_dashboard_test.go)).

## A deleted ticket answers like a missing one

A team administrator deletes a ticket into the team's bin, restores it from there, and purges
it — or the job does, thirty days after the deletion
([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D1–D3, D7). Deleting, restoring and purging take the team role `admin` and `admin` scope; an agent
— a token's or the chat's — meets the hard-off rule `deleting, restoring or purging`
([tokens.md](tokens.md#capabilities-the-baseline-and-the-hard-off-list)). The purge, which nothing
undoes, takes a browser session besides: a token — an administrator's `admin` token included — is
`403 session_required` before anything is looked up (D7 as amended 2026-10-05,
[tokens.md](tokens.md#what-only-a-session-does); `TestPurgingTakesABrowserSession`); what a leaked
token can still do here is H-54. The bin is read with `read` scope; a token restricted to a project
reaches neither the bin nor any of its routes.

**The deletion is an application filter, not a policy.** Row-level security stays the team
alone (D3). Every query that reads a ticket carries `deleted_at IS NULL` beside the visibility
predicate — for the team's administrators too —, and a second unit test holds the query files to
it as the first holds them to the predicate (`TestEveryReadOfTicketsCarriesTheDeletionFilter`); the
list builder adds it in code (`TestTicketListLeavesTheDeletedOut`). Its exemptions name their
reasons in the query files: the bin and the purge, which read deleted tickets only; a writer's
reread; the publication of an act; a ticket read through the filter in the same transaction; the
rank keys and the numbers an import finds taken, because a deleted ticket keeps its key and its
number; the team's attachment usage and the consistency check's list of missing files, because a
deleted ticket's files have their rows and objects until the purge. The integrity walks still step over deleted
tickets, so that a restoration cannot close a cycle. A deleted ticket therefore answers exactly as a
ticket that does not exist: `404` on its routes — its rendered body included — and on the key
resolver, absent from every list, the full text and the search, the trees, the person-level lists
and the inbox and its count, a link to it absent, an act that names it redacted, a block on it
naming no ticket, a parent shown as hidden; `person_sees_ticket` answers no, so nobody is told of
it. Its deletion reaches, as `ticket.changed` with the kind `deleted`, the streams that could see
it — a person-level stream opened in another of the person's teams included —, with its key and
version, which they knew already. The integration tier walks all of it for an administrator, a
member and a viewer (`TestADeletedTicketAnswersLikeAMissingOne`), the search and the person-level
lists across two teams (`TestADeletedTicketLeavesSearchAndThePersonLevelLists`), and the bins of
two teams apart.

**The purge is the one path that deletes a ticket.** The runtime role may delete a ticket and what
belongs only to it — its comments and their revisions, questions, attachments, time entries and
their revisions, pull-request links — only through restrictive policies that name the job `ticket-purge` in `app.job`
and a deleted ticket ([migration 32](../../backend/internal/store/migrations/000032_ticket_deletion.up.sql)):
a delete outside the purge, a forgotten `WHERE` included, removes nothing, and the purge removes
nothing of a live ticket (`TestThePurgePoliciesHoldEveryDeleteToTheBin`). The notifications'
restrictive policies admit the purge for a deleted ticket's notifications only, and the purge
takes the ticket's file out of the report of the import that created it
([import-and-export.md](import-and-export.md#what-a-dry-run-keeps)). The audit rows of a
purged ticket keep its key, the actor and the act, their `before`, `after`, `reason` and `note`
emptied by `purge_ticket_audit`, a `SECURITY DEFINER` function of the owner role — the one place the
runtime role reaches an update of the audit record ([ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md)
D3): it empties nothing but those four fields, of the rows of one deleted ticket of the current
team, in a transaction that names the purge, and its `search_path` is fixed with `pg_temp` last.
The job reads the deleted tickets due of every team — their ids and teams only — through
`tickets_purge_due`, a policy that admits them in a transaction named `ticket-purge` **with no
team set**, and then binds itself to each team in turn; a request's purge always has a team,
so it never reads past it.

## Saved filters are their owner's, and a shared one an administrator's to withdraw

A saved filter carries its team and the canonical policy, and restrictive policies hold reading to
its owner or a shared filter and every write to its owner
([migration 33](../../backend/internal/store/migrations/000033_saved_filters.up.sql);
`TestTheSavedFilterPoliciesHoldAPersonToTheirOwn`) — and, since
[migration 39](../../backend/internal/store/migrations/000039_saved_filters_moderated_by_administrators.up.sql),
an administrator of the current team (`app_is_tenant_admin()`) to two acts on another person's
shared filter: a change into one that is not shared, and a delete
([ADR 0018](../adr/0018-the-views-of-the-first-release.md) D5 as amended 2026-10-06 — a filter
whose owner left the team stays shared until somebody withdraws it). A filter that is not shared
stays its owner's alone to read and to write. The unshared row is one only its owner reads, and
PostgreSQL holds an update's new row to the read policy, so the read policy admits an administrator
to the one filter the transaction names in `app.saved_filter_id`, which `Writer.UnshareAnothersFilter`
sets for its single statement and clears after it
(`TestTheSavedFilterPoliciesAdmitAnAdministratorToASharedFilter`). The policies see the row, not
the columns a statement sets, so that an administrator's unshare changes nothing but `shared` — not
the name, not the conditions — is held three times: by the handler (`mayChangeFilter`), by the query
(`UnshareSavedFilter`, which sets nothing else), and in the data layer by a trigger, as a project's
restriction is: `saved_filters_moderation_guard`, `BEFORE UPDATE`, refuses with SQLSTATE `42501`
any change of a filter that is not the caller's own to its name or its parameters, and any that
leaves it shared — a transaction with no person set, which the policies admit to no row, excepted
(the same test). The route takes the administrator's `admin`
scope and refuses every agent; each act is recorded under the administrator's name
(`TestAnAdministratorUnsharesOrDeletesAnotherPersonsSharedFilter`). Its parameters name projects,
tickets and persons as the lists take them. A member's shared filter that names a project or a ticket another
reader cannot see — or one that is gone — is answered to that reader `redacted`, its parameters and
warnings withheld, as an act that names a hidden ticket is; its name and its owner stay, because the
owner shared them. The name is free text, as a comment's is: what an owner writes into it, every
member of the team reads.

## Members, grants and group mappings

Who belongs to a team is `memberships`, and a person has up to two rows in a team, one per
source ([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D2–D4; [`api/members.go`](../../backend/internal/api/members.go)):

- **A mapped membership** is derived by the identity provider from the person's groups and the
  team's group mappings — the highest role the mappings give — at a login, a groups refresh, a
  token's gate check, and at once when an administrator makes, changes or removes a mapping, for
  every person of the configured issuer whose stored groups hold the group, who is active and whom
  the gate admitted at their last login, refresh or check
  ([identity-provider.md](identity-provider.md#memberships-follow-the-groups);
  `TestGroupMappingsDeriveAtOnce`, `TestARederivationLeavesWhoCannotAct`). Nothing is derived for
  a person outside the gate, who keeps the memberships they had. Nothing else writes one: the policies
  admit its insert, change and removal to the identity provider's transactions alone. An
  administrator takes a person's mapped membership away only through the mapping, which applies to
  everyone in the group, or at the issuer.
- **A grant** is an administrator's, marked as such: added for a person who exists
  (`POST …/members`), its role set (`PUT …/members/{person_id}/grant`) or removed
  (`DELETE …/grant`). No derivation touches it, and it touches no mapped membership.
- **The effective role** is the higher of the two. The member list shows every source with its own
  role (`origins`) and whether the person has a local account, to every member of the team
  ([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
  D7; `TestLoginThroughDex`); the person's e-mail address, which tells two persons of one name apart,
  to the team's administrators only — `null` to everyone else, and for a person without one
  (`addressFor`; `TestAdministratorsSeeTheAddress`).

**A group mapping** gives the members of a group of the issuer a role in one team: the name
matched exactly, case and all, against the groups claim; one mapping per group and team
(`409 mapping_exists`); several matched groups give the highest role. A change of its role takes
`If-Match`. `includes_caller` tells the editor whether their own groups, as of their last login or
refresh, hold it (`TestTheMappingEditorsOwnRole`). **Only a global administrator who administers
the team makes a mapping or changes its role**
([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D7; `mapsGroups`; `TestOnlyAGlobalAdministratorMapsAGroup`): every team shares the issuer's one
namespace of groups, and a mapping brings everyone in its group into the team at once — a group
every person holds, or a guessed department's, mapped by the administrator of one team would bring
the people of the others in with it. Any other administrator of the team is `403 forbidden` before
an idempotency key is kept or a row is written, and still reads the mappings, removes them and
grants roles by hand. The global administrator may map any group name the issuer could send, `admin`
included: everyone behind the gate whose stored groups hold it holds the role at once, and whoever
joins the group at the issuer from their next login — the mapping is the team's word, the group
the issuer's. The data layer holds the rule too: the restrictive policies on `group_mappings` admit
an insert or an update only to an administrator of the team who is a global administrator
(`app_is_global_admin()`, [migration 25](../../backend/internal/store/migrations/000025_group_mappings_global_admin.up.sql)),
so behind a handler that forgot `mapsGroups` the insert would meet SQLSTATE `42501` and the update
would find no row; the start-up's seeded mapping of
the bootstrap team is written as the job `bootstrap`
(`TestPoliciesOfThePersonsAndTheirAccounts`, `TestBootstrapSeedsTheAdministratorGroupsMapping`).

**The person lookup.** `POST …/members` names the person by an e-mail address — a value with `@`,
compared without regard to case with the address the issuer asserted at the person's last login,
only one the issuer marked verified (`email_verified: true`) — or, while
`COWORK_OIDC_EMAIL_TRUSTED` is `true`, one about which it said nothing; one it marked unverified
never ([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D3; `TestAnAddressTheIssuerSaidNothingAboutIsTrustedOnlyWhenConfigured`) — and only among the
persons of the configured issuer, so that without a provider an address finds nobody — or by a
local account's username, with or without
`local:`, normalised as the login normalises it. The person must exist and be active: one who never
logged in through the issuer and has no local account is `404 person_not_found`; an address two
active persons share is `409 person_ambiguous`, and nobody is granted; a grant that exists is
`409 grant_exists`. The lookup runs in the administrator's transaction in their team and names the
key in `app.person_lookup`: the `users` policy admits the persons that match it and no other person
of the installation ([`store/members.go`](../../backend/internal/store/members.go) `FindPerson`;
`TestAddMemberByAddressOrUsername`). How far the issuer's address can be trusted is
[identity-provider.md](identity-provider.md#h-26) H-26; what the lookup tells an administrator about
persons beyond their team is [H-31](#h-31).

**Who may.** The team's administrators, never an agent — the hard-off rule "administration"
([tokens.md](tokens.md)) — and a global administrator who does not hold `admin` in the team, for
their own grant only ([above](#a-global-administrator-without-a-role)). Every act that can give access takes a browser session: adding a member,
setting a grant, making or changing a mapping, restricting or opening a project, putting a person on
its access list — and making or changing a mapping takes a global administrator besides (above).
Removing a grant, a mapping or an access entry only takes access away, and an administrator's
`admin`-scope token may do it too ([tokens.md](tokens.md#what-only-a-session-does)).
The mappings, a project's access list and the tokens that can act in the team are read by the
team's administrators — with a token's `read` scope — and the member list by every member. The
token list holds the members' unrestricted tokens and those restricted to this team, and never a
token restricted to another team, not even by its name; revoking an unrestricted one ends it in
the person's other teams too — an act of a team's administrator that reaches past the team,
as the deactivation of an account the team manages does ([tokens.md](tokens.md#h-57) H-57). The mappings and the member list are also read by a
global administrator who holds no role in the team, in a browser session.

**`409 last_admin`.** A change of a grant or of a mapping, or the deactivation of a local account
the team manages, that would leave the team without an administrator who can log in — mapped or
granted, active, and a local account or a person of the configured issuer whom the gate admitted at
their last login, refresh or check — is refused and changes nothing, the administrator's own grant
and their own mapping included (`lastAdmin`; `TestGrantsAndTheLastAdministrator`,
`TestTheMappingEditorsOwnRole`, `TestTheLastAdministratorMustBeAbleToAct`,
`TestADeactivationLeavesTheTenantAnAdministrator`). Each of these changes takes the team's lock
first (`LockTenant`), so two administrators who take each other's role away — or deactivate each
other's account — at the same moment are decided one after the other, and the second meets
`last_admin` (`TestTwoAdministratorsCannotRemoveEachOther`,
`TestTwoAdministratorsCannotDeactivateEachOther`). Adding a member and the access list take no lock
and meet no check: they take no administrator away. A derivation at a login, a refresh or a token's
gate check is never refused ([identity-provider.md](identity-provider.md#h-29) H-29), and a
deactivation is held to the rule in the team that manages the account and in no other
([local-accounts.md](local-accounts.md#h-32) H-32). A team either leaves without an administrator
is given one again by a global administrator's grant to themselves
([above](#a-global-administrator-without-a-role)), which adds a role and meets no check.

**Every change is recorded** — a grant, a mapping, a restriction and an access entry by the
administrator; the memberships a mapping's change derives by `system:identity-provider` with the
cause `mapping`, in the same transaction — and announced as `membership.changed`
([below](#the-event-stream-carries-what-its-subscriber-could-read)).

## The project restriction

A restricted project is visible to the team's administrators and to the persons on its access
list (`project_access`), each with the lower of their team role and their entry, `member` or
`viewer` ([`projects.go`](../../backend/internal/api/projects.go) `projectRole`; ADR 0034 D3). An
administrator restricts or opens a project with `PUT …/projects/{project}/restriction` — `If-Match`
on the project's version, a browser session only, recorded as the project's `updated` — and keeps its
list with `GET …/projects/{project}/access`, and `PUT` and `DELETE …/access/{person_id}`: the person must be a
member of the team (`404 person_not_found`), putting a person on the list or changing their entry
takes a session, taking one off a token as well (`TestProjectRestrictionAndAccessList`,
`TestRolesFromTheIdentityProviderHold`). The list may be written before the project is restricted,
so nobody on it loses the project in between; its entries count while the project is restricted.
The runtime role may change `projects.restricted` and nothing else of a project's restriction, the
trigger lets only a team administrator change it, and writing the list needs a team
administrator in the data layer as well (above). An entry shows the person's e-mail address: the
list is the administrators' to read.

**The entries are on the project's list, not in the member list.** Every member, a viewer included,
reads the member list, and an entry there would name a restricted project to members who must not
learn it exists; the access list is read by the team's administrators alone (ADR 0034 D7).

A token restricted to a project carries the project into `app.restricted_project_id`, which
narrows `app_project_visible` to it. At the boundary it reaches only the routes with
`{project}` in their path, the project list, the team-wide ticket list, the team's search, the key
resolver and the event stream, each narrowed to its project by the predicate (`tenantWideForProjectTokens`
in [`tenant.go`](../../backend/internal/api/tenant.go)); every other team route answers it
`404`.

## The confidential flag

While a ticket is confidential it is visible to the team's administrators, its assignee and
its reporter, and to nobody else; its comments, questions, attachments, links, stakes, time
entries, activity, export and events follow it, because each is read through the ticket's
predicate (ADR 0065 D1).

- **Set automatically** when a ticket is filed with `security` `live` or `boundary`, and when
  a change makes the class `live` or `boundary` — from one of the two to the other as well —
  with a `confidential_set` act naming the class. A change that leaves the class as it is does
  not set again what an administrator lifted
  ([`tickets.go`](../../backend/internal/api/tickets.go) `applyTicketPatch`; ADR 0065 D2). An import
  sets it by the rule of the source ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
  D7, [import-and-export.md](import-and-export.md#what-an-import-creates)).
- **Never lifted automatically.** Changing the class back, `done` and `dropped` leave it set;
  no code path clears it except the administrator's act (ADR 0065 D3).
- **Set or lifted by hand** only through `PUT …/confidential`: the `admin` role and `admin`
  scope, never an agent — the hard-off rule "setting or lifting the confidential flag".
  Lifting needs a reason and a browser session — a token is `403 session_required`, because a
  lift shows the ticket to every member long after a leaked token's revocation; setting it stays
  open to an administrator's token. Both are recorded as `confidential_set` or
  `confidential_lifted` (`SetConfidential`; ADR 0065 D2, D3, D6;
  [tokens.md](tokens.md#acts-that-take-a-session-in-their-giving-direction)).
- **An agent sets it indirectly** by filing or classifying a ticket `live` or `boundary`,
  which is intended (ADR 0065 D6).
- **Assignment admits** (ADR 0065 D9): a person who can see the ticket's project sees a
  confidential ticket from the moment it is assigned to them. An agent admits nobody but its own
  person: it assigns a confidential ticket only to its person or to nobody, at a filing and on a
  change, and is otherwise refused by the hard-off rule "assigning a confidential ticket to anyone
  but the agent's person" ([`tickets.go`](../../backend/internal/api/tickets.go) `mayAssign`;
  [tokens.md](tokens.md#capabilities-the-baseline-and-the-hard-off-list), ADR 0043 D3). A person's
  token is held the same way and is otherwise `403 session_required`: admitting another person is a
  person's act in a browser session (ADR 0065 D9 as amended 2026-10-07).
- **The chat reads it for a person who sees it** and sends what it read to the provider the person
  picked, which for a hosted provider is a copy outside the installation — a risk the owner accepted
  ([chat.md H-37](chat.md#h-37)). The model is told never to copy a confidential ticket's text into
  another ticket, a comment or a question, and nothing enforces it: a steered model can carry the
  text where people who may not read it would ([chat.md H-38](chat.md#h-38)).

## The activity withholds what its reader cannot see

A ticket's activity (`…/activity`) is its audit rows, read after the ticket passed the
predicate; it leaves out time entries, the reads of ADR 0026 D5, the `booked`, `voided` and
`locked` acts, and a pull request's title or page changed at GitHub, an act GitHub's webhook of a
release up to 0.12.0 recorded. An act names the other tickets its payload mentions in `refs` — a link's other
end, the ticket a block waits on, the prerequisites a close overrode, the old and the new
parent. When the reader cannot see one of them, the act is shown with `redacted: true` and
without its `before`, `after`, reason and note
([`comments.go`](../../backend/internal/api/comments.go) `activityView`); who acted, when,
and which action remain visible.

A comment's text never enters the audit record, so a withdrawal hides it from the thread, the
comment's history and the activity alike
([ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D3). The
team's audit view (`…/audit`) is for administrators, who see every project and every
confidential ticket anyway; it is not filtered.

## The event stream carries what its subscriber could read

Every act of a ticket — its comment, question, link, interest and attachment acts included;
the reads of ADR 0026 D5 and the time entries not — is published with `pg_notify` on the
channel `cowork_events` inside the act's transaction, so it is delivered at commit and never
on a rollback ([`store/notify.go`](../../backend/internal/store/notify.go)). The notification
carries the audit row's id, the team, the project, the entity, the action, the ticket's key
and version, and the inputs of the confidential rule: the flag, the assignee and the reporter.
One connection per replica listens and hands each notification to the streams that follow its
team, each judging it by its filter of that team. An executed import's acts are not published
one by one: its one act on the project is, as `project.changed` with the kind `imported`, which
names no ticket.

A stream (`GET …/events`) passes authentication and the boundary like any route and computes
its filter when it connects ([`api/events.go`](../../backend/internal/api/events.go)
`streamFilter`): the projects the caller sees at that moment — a project-restricted token's
own only — and whether the caller is a team administrator. An event passes when its project
is in the set and its ticket is not confidential or the caller is an administrator, its
reporter or its assignee ([`events/hub.go`](../../backend/internal/events/hub.go)
`Filter.Admits`) — the inputs `app_ticket_visible` reads. The stream sends the event's name
and `{key, version, kind}`, never content; the client refetches through the API, which applies
the predicates again
([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D2, D3). A replay after a reconnect is filtered the same way. `TestEventStream` checks the
team, the project restriction and the confidential rule.

**`membership.changed`.** An act on who belongs to the team or who sees a project — a grant, a
derived membership, a mapping, a project's restriction, an entry of an access list — is published
in its transaction as well, with the keys of what changed and an audience, and sent as
`{team, tenant, person_id, project_id, mapping_id}` — the team's slug under `team`, and for this
release under `tenant` beside it, and the rest each where it applies —, never content
([`store/notify.go`](../../backend/internal/store/notify.go) `MembershipChange`). A membership and a
restriction reach every member of the team: the member list is theirs to read, and a project
restricted or opened was visible to them at one of the two moments. A mapping reaches the team's
administrators, who alone read the mappings. An access entry reaches the administrators and the
person it names, never the members who do not see the project (`Filter.Admits`;
`TestMembershipAudiences`, `TestMembershipEventsReachTheirAudience`). The stream of a token
restricted to a project hears, of these, only the events that name its project, or name its own
person and no project: the token knows nothing of the team beyond its project
(`TestMembershipEventsOfAProjectRestrictedStream`, `TestARestrictedStreamHearsOnlyItsProject`).

The filter follows the person. An act that can change what a stream admits — a project
created, a grant, a derived membership, a mapping, a project's restriction, an entry of an access
list — makes every stream that follows the team compute its filter of the team again, with the
person's role as the boundary reads it then and the projects they see, before it lets the team's
next event through: until it has, the hub hands the stream every event of the team unjudged and
the stream judges them itself, so neither a project the person gains nor one they lose waits for a
heartbeat (`Hub.Changes`, `Hub.Refilter`, `refilter` in
[`api/events.go`](../../backend/internal/api/events.go);
`TestAnAdmissionChangeHoldsTheFilterUntilTheStreamRefilters`,
`TestTheStreamAdmitsWhatAnActOpensAtOnce`). A project's creation is published for that and sent to
no client. A person the boundary no longer admits at that moment loses the stream opened on that
team; a person-level stream stops following another team they left
([below](#the-person-level-stream)). Every twenty
seconds the heartbeat checks the token and the membership again ([tokens.md](tokens.md) H-7) and
recomputes the filter too, which catches a change made in the database past the API within one
heartbeat (`TestStreamFollowsAccess`).

## The person-level lists are unions, one team at a time

The inbox, "next for me", "assigned to me" and "open decisions" (`GET /api/v1/me/inbox`, `…/next`,
`…/assigned`, `…/decisions`) are the one kind of answer that spans teams
([ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3). They are
built as ADR 0021 D5 has it: the person's memberships are read first, and each team is then read in
a transaction of its own, bound to that team and the caller, under the same predicates as the
team's own lists; the parts are merged in the application, and no query names two teams
([`api/inbox.go`](../../backend/internal/api/inbox.go) `personTenants`,
[`api/mylists.go`](../../backend/internal/api/mylists.go)). A team the person left is not read at
all; a global administrator without a role in a team has no membership there and reads nothing of
it. A token restricted to a team reads that team alone, and one restricted to a project its project
alone — `app.restricted_project_id` hides every project of another team. A `team` — or `tenant`,
its deprecated name — that names none of the person's teams is the boundary's `404`, whether or not
it exists, and "next for me"'s
`project` names a project within that team, one hidden from the person listing nothing. A cursor is
bound to its person and its narrowing, and carries the score's key and the ticket's id — the score is
shown on the ticket, computed from its own facts and its stakes, which whoever sees the ticket reads
([ADR 0013](../adr/0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D2) — so it is
not sealed. The place in the backlog beside each ticket counts only the open tickets of its horizon
the reader sees (`ListRankPlaces`, the predicate on every ticket it compares), so it tells nothing of
a hidden one. "Next for me" holds the person's own and the unassigned open tickets, never a
colleague's. `TestTheInboxIsThePersonsAcrossTheirTenants`, `TestNextForMeAcrossTenants`,
`TestAssignedToMeAcrossTenants` and `TestOpenDecisionsAcrossTenants` cover the teams, the
restricted project, the confidential ticket, the narrowing and the restricted tokens.

**A notification is its person's.** The act's own transaction writes it for each person the act
tells ([ADR 0020](../adr/0020-notifications-are-an-in-app-inbox-per-person.md) D2, D3), and only for an
active member of the team who sees, by `person_sees_ticket`, both the ticket it is about and the
ticket the act is on — never the actor
([`store/inbox.go`](../../backend/internal/store/inbox.go) `deliver`). A comment's mention is held to
the same sight before it is written: each person the comment's `mentions` names must be a member who
sees the ticket, or the comment is refused at `/mentions/<i>` and tells nobody
([ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D5;
`TestAMentionOfAPersonWhoCannotSeeTheTicketIsRefused`). That refusal tells the writer — who sees the
ticket — whether a person sees it, as a question's `asked_of` does: of a restricted project, whether a
member is on its access list, which only the team's administrators read otherwise ([H-58](#h-58)).
Reading it holds again: a
notification is listed and counted only while its person sees both tickets, so one whose ticket turned
confidential, whose project was restricted away, or whose team the person left is absent and counts
nowhere (`TestTheInboxIsThePersonsAcrossTheirTenants`); its act is shown as the ticket's activity shows
it, without the payload where it names a ticket the person cannot see. Inside the team, the
writer of an act inserts notifications for others, so the canonical policy alone would show any
person of the team another's inbox to a query that forgot its `user_id`; restrictive policies hold
reading and marking to `user_id = app_user_id()`, and deleting to the retention job
([migration 30](../../backend/internal/store/migrations/000030_notifications.up.sql);
`TestTheInboxPolicyHoldsAPersonToTheirOwn`). Marking read is the person's recorded act `read` in that
team, so its administrators read in the audit view when a person marked their notifications read —
the cost of ADR 0026 D1's rule that every write is an act.

## Search finds only what its reader sees

The search — `GET …/search` in a team, `GET /api/v1/me/search` across the person's teams
([ADR 0025](../adr/0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md)) —
runs in PostgreSQL in the reader's transaction, bound to one team, like every other read: no
external index holds any team's text, and isolation is the engine's
([`queries/read/search.sql`](../../backend/internal/store/queries/read/search.sql) `SearchTickets`).
Every text it reads — the ticket's title and body, its comments, its questions, its attachments'
file names, the key built of the project's key and the number, the title by trigram — is read with
`app_ticket_visible` on the ticket it belongs to, and the hit and its snippet are read through it
once more, so a restricted project the reader is not on, a confidential ticket of which they are
neither a team administrator, the assignee nor the reporter, and a project-restricted token's other
projects find nothing, by any word of any of their texts; the lint of the query files holds the
query to a predicate per read of `tickets`, and to `deleted_at IS NULL` beside each, so a deleted
ticket finds nothing either ([above](#a-deleted-ticket-answers-like-a-missing-one)). A withdrawn comment is not searched, as its text is
hidden from every route. The snippet is the matched text of the hit itself — the body, the comment,
the question, the file name — never another ticket's, and it is answered as text in parts, never as
markup. The person-level search is a union like the lists above: the person's memberships, a
restricted token's own team, each team read in a transaction of its own, the parts merged by rank
in the application, a narrowing `team` or `tenant` that names none of theirs answered like an
unknown one; a
global administrator without a role in a team searches nothing of it. A cursor is bound to its
reader, its narrowing and a hash of its query; it carries the rank of the last hit, which the reader's
own visible text gave it. `TestSearchNeverShowsWhatTheCallerCannotSee` holds hits and snippets to the
team, the restriction, the confidential rule — in the title, the body, a comment, a question, the
options, a file name, the key and by trigram — and the restricted tokens to their team and project;
`TestSearchFindsAndRanksWithSnippets` holds a withdrawn comment out. What the query words leave in
a log is [trust-boundaries.md](trust-boundaries.md#h-14) H-14; what its timing may say is H-53 below.

## The person-level stream

`GET …/events?me=true` is the one stream that spans teams: besides the person's unread count, it
carries every event of every team the person belongs to that the filter of that team admits
([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D1, D3 as amended on 2026-10-05, [events.md](../developer/events.md#the-person-level-stream)). It is
built the way the person-level lists are: the person's memberships are read first (`personTenants`),
and the filter of each team is computed in a transaction of that team's own, bound to it and the
caller — the person's role there, the projects they see there (`ListVisibleProjectIDs`), a
project-restricted token's project — so an event of a team passes on the person-level stream exactly
what it would pass on that team's own stream: its project among those the person sees there, the
confidential rule with the person's role there, the audience of a membership event
([`api/events.go`](../../backend/internal/api/events.go) `streamFilters`, `tenantFilter`). The hub
judges an event by the filter of the event's team and no other ([`events/hub.go`](../../backend/internal/events/hub.go)
`deliver`); the stream still sends keys and versions, never content.

- **A token restricted to a team does not span.** Its person-level stream follows its team alone —
  a token restricted to a project included, which is restricted to its project's team
  ([ADR 0035](../adr/0035-personal-access-tokens.md) D3, `streamReq.span`).
- **A role, not the global administrator's flag.** A team the person holds no role in is not
  followed: a global administrator without a role there hears nothing of it, as the person-level
  lists read nothing of it.
- **Leaving.** An act that takes a team from the person — a grant removed, a membership derived
  away — makes the stream compute that team's filter before the team's next event, find no
  membership, and stop following it. The act itself names the person and reaches them
  (`{"team": "<slug>", "tenant": "<slug>", "person_id": …}`); nothing of the team after it does, a
  later act that names them there — the removal of an access entry they left behind — included.
- **Joining.** A membership act that names the person in a team the stream does not follow makes
  the hub follow that team with an empty filter: every event of the team reaches the stream
  unjudged until it has read the person's membership there and computed the real filter, and is
  judged by that filter then, so nothing passes on the empty filter.
- **The heartbeat** reads every membership again, which catches a membership removed or added in the
  database past the API within one heartbeat ([tokens.md](tokens.md#h-7) H-7).
- **The replay** after a reconnect covers the teams the stream follows at the reconnect, each
  through its filter as computed then: a team left is not replayed, and one joined meanwhile is
  replayed from the reconnect's id on, under the person's sight there now.
- **The count** is counted per team, as the inbox counts, so a notification the person no longer
  sees does not count.

`TestThePersonLevelStream` asserts what never arrives: a question on a project restricted away from the
person, one on a confidential ticket they are neither assignee nor reporter of, and anything of a team
after the act that took it from them; `TestThePersonLevelStreamSpansThePersonsTenants` that nothing of a
project of another team hidden from them or of a confidential ticket there arrives, while a ticket
assigned to them there does within a second; `TestTheHeartbeatChecksEveryMembershipOfThePersonLevelStream`
the heartbeat's check; and `TestARestrictedTokensPersonLevelStreamStaysInItsTenant` that a
team-restricted token hears nothing of another team and counts its own team's notifications only.
What a person-level stream costs grows with the person's teams — one transaction per team when it
opens, at every heartbeat and on every act that changes what it admits of one.

## Time follows its own rule

Whose time entries a caller sees is `app_time_visible(person)`
([migration 13](../../backend/internal/store/migrations/000013_time_entries.up.sql)): their
own; every entry as a team administrator; everyone's as a `member` — not a `viewer` — while
the team's `time_visible_to_members` is on (ADR 0034 D5,
[ADR 0017](../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D9). Every time query — the lists, the sums, the team-wide list, the report — calls it next
to the ticket predicate, so an entry on a ticket the caller cannot see is invisible whoever
booked it. The unit test on the query files checks the ticket predicate only; the time
predicate is held by review and by `TestTimeVisibility`.

## What this does not cover

<a id="h-2"></a>
### H-2 — A confidential ticket's text is readable to whoever reads the database or a backup

Live whenever a ticket is confidential. The flag is a predicate in queries; nothing encrypts
the text at rest (ADR 0065 D8). The text sits in plain columns: the ticket's title, body,
threat and block reason; its questions; its comments and their revisions — a withdrawn
comment keeps its text in its row; the reasons of its stakes and the notes of its time entries; its
attachments' names, and a consistency check's list of the files whose bytes are missing; the audit record, which holds every replaced version of
the body, the titles, the questions and answers, and the reasons and notes of the acts, append-only and
without an end of retention (ADR 0026 D7); for a day, until the hourly expiry job removes
it, the stored response of a keyed creation — a ticket, a question, a comment — in
`idempotency_keys`; for a day as well, every file an import's dry run read, and until the
ticket's purge the report of the import that created it ([import-and-export.md, H-72 and
H-73](import-and-export.md#h-72)). Its attachments' bytes lie in the bucket
([attachments.md](attachments.md)). Whoever reads the database past row-level security — a
superuser, a role with `BYPASSRLS`, the owner role, which can switch `FORCE` off — or holds a
dump, a backup or the volume, reads all of it, and so does a compromised serving process
(below). Encryption at rest of confidential text is a different threat model and is not
built; guarding the database credentials, the volumes and the backups is the operator's
([ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)).

<a id="h-3"></a>
### H-3 — Integrity checks and derived values read tickets the caller cannot see

A rule that must hold for the whole team cannot ask only what its caller sees, and a rule
that asks only what its caller sees lets a hidden ticket slip by. Both kinds exist:

- The parent cycle refusal walks the ancestors past the predicate (`ticket_ancestor_or_self`):
  whether a re-parenting is refused with `409 parent_cycle` can depend on a confidential
  ancestor in the same project.
- The `blocks` cycle refusal walks the team's whole `blocks` graph past the predicate
  (`blocks_path_exists`): `409 link_cycle` can depend on confidential tickets and on tickets
  of restricted projects.
- The done act — by hand, or the `PATCH` that fills the last progress stage — is refused only
  by the open prerequisites the closer can see (`ListOpenPrerequisites`): a ticket can be
  closed over an open prerequisite its closer cannot see, without an override and without a
  mention in the act, and its `open_prerequisites` reads 0 to that closer.
- Each derived progress stage is the effort-weighted mean of the same stage of every child not
  dropped and not deleted, confidential ones included (`ticket_derived_stage`), and a parent whose children are
  all done shows 100 in each.
- A new project's key is refused as taken whether or not the caller can see the project that
  holds it (`ProjectKeyTaken`).
- A filing, a reopen and a move in the rank compute their key over every ticket of the project
  ([ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D2), so a key would
  tell where hidden tickets sit, how many were open when migration 17 spaced the keys over
  them, and — done and dropped take the key away — whether one is still open. No answer shows a
  key: not a ticket (`ticketView`), not the act of a move, which names only the neighbour, not a
  cursor, which carries its position sealed (`sealPosition`,
  [ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D1).
  Whether a move writes is decided over the tickets the mover can see (`NextSeenRankedTicket`,
  `PreviousSeenRankedTicket`): a ticket that sits next to its neighbour for the mover answers
  unchanged, whatever sits between unseen, and a move that writes puts the ticket where the
  mover sees it go whether or not a hidden ticket sits there. Two signals remain. A gap that
  moves of hidden tickets wore down no longer fails a move: before a key passes 32 characters the
  project's keys are spread again, every ticket's — a hidden one's included — keeping its place,
  with no act and no version (`rebalanceRank`); what is left of the signal is the time the move that
  spreads them takes, and a cursor of the project's list handed out before resumes at its old key's
  place among the new ones. And the one write that ranks the open tickets an
  earlier release left without a key — a hidden ticket's filing, reopen or move included —
  changes how the list shows them, with no act the caller sees: with `include_terminal` they
  move from among the done and dropped tickets, by number, to before them, and a cursor
  positioned on one changes. `TestRankAroundAHiddenTicket`, `TestRankKeyIsNeverShown` and
  `TestEightHundredMovesIntoOneGap` hold the rest.
- The sort of a project's rank by the score
  ([ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D3) reorders only the open
  tickets the sorter sees, in the keys they hold among themselves: a hidden ticket keeps its key, its
  place and its version, the act names only the tickets it moved, and the activity of a hidden ticket
  holds no sort (`TestSortByScore`); a reader who cannot see one of the tickets the act names reads it
  without its payload, as any act whose refs name a hidden ticket.

Each reveals at most that such a ticket or project exists — for the rank, at most that hidden
tickets were moved or filed — and who acted on it when — never its content. Live as soon as a
team has a confidential ticket, which a ticket classified `live` or `boundary` is until an
administrator lifts the flag, or a restricted project, which a team's administrator makes in a
browser session. A team with neither has nothing to reveal.

<a id="h-4"></a>
### H-4 — The event channel is readable by any role that can connect to the database

Live wherever a database role other than the two cowork roles can connect to cowork's
database. `LISTEN` needs no privilege beyond the connection, and a notification is not a row,
so row-level security does not apply to it. Such a role that runs `LISTEN cowork_events`
receives the notifications of every team: team and project ids, ticket keys (with the
team's slug and the project's key) and a project's key, versions, the acts' names, the
confidential flags, the ids of assignees and reporters, of a membership act's person and mapping,
and of the person an inbox change is for — no titles, no bodies, no comments. PostgreSQL grants
`CONNECT` on a new database to `PUBLIC`. The mitigation is the installation's:
`REVOKE CONNECT ON DATABASE <cowork's database> FROM PUBLIC`, with `CONNECT` granted to the
owner and the runtime role only; a superuser keeps its access regardless, and `pg_hba.conf`
decides who reaches the server at all. cowork does not check this at start.

<a id="h-5"></a>
### H-5 — Events are kept only for the replay window

Live today, by design (ADR 0054 D5). Each replica keeps the events of the last
`COWORK_SSE_REPLAY_WINDOW` (five minutes by default) per team in memory. A client that
reconnects with a `Last-Event-ID` its replica no longer holds — away for longer, or on a
replica that started later or lost its database listener meanwhile, which empties its buffers
when it hears the database again — gets `event: resync` and must refetch every list it shows;
while a replica's listener is down, new streams are refused with `503` and the open ones are
told to resync when it is back. Nothing is lost from the
record: the API and the audit record have every act, and a refetch goes through the
predicates. What is lost is the stream's continuity: a client that ignores `resync` shows a
stale view.

<a id="h-31"></a>
### H-31 — A team's administrator can learn whether an address names a person of the installation

Live in an installation with several teams whose people log in through one identity provider, or
hold local accounts. A person belongs to the installation, not to a team, and the **person lookup**
of `POST …/members` answers whether an address or a username names an active person of the
installation: a `201` that grants them a role, `404 person_not_found`, or `409 person_ambiguous`,
which tells without granting anybody that two persons share an address. An administrator who tries
addresses learns who exists beyond their team, and a `201` brings that person into the team —
their name and address in its member list, the team in their own list of teams — with the
granted role until the grant is removed. The lookup takes a browser session; a grant is recorded
with the administrator as actor, a lookup refused with `404` or `409` writes no audit row
(`findPerson` answers before the transaction). Neither asks the person, or the teams they belong
to. That crosses the line
[ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) draws between
teams. A group mapping, which brings everyone in its group into the team at once, is not this
gap's: only a global administrator who administers the team makes one or changes its role
([above](#members-grants-and-group-mappings)). Mitigation: teams whose administrators must not
learn about each other's people belong in installations of their own.

<a id="h-53"></a>
### H-53 — A search's duration depends on matches the reader cannot see

Dormant as far as measured — it was not measured. The search's indexes find every ticket, comment,
question and file name of the team that holds the words, hidden ones included, and the visibility
predicate drops the hidden ones afterwards, in the same query; the work, and with it the time to the
answer, grows with the hidden matches as well. A member who times many searches for a word could in
principle tell whether hidden texts of the team hold it — never which ticket, nor anything of its
text, and only inside a team they belong to. The answer itself is the same with and without the
hidden matches. Mitigation: none in cowork; a team whose members must not learn even that much
keeps such work in an installation of its own.

<a id="h-54"></a>
### H-54 — An administrator's `admin` token deletes, and the job purges what nobody restores

Live today. The purge takes a browser session
([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D7 as amended 2026-10-05, [tokens.md](tokens.md#what-only-a-session-does)): a token — a team
administrator's `admin` token included — answers `403 session_required` and purges nothing.
Deleting does not take one: the bin undoes a deletion, so it stays open to an `admin`-scope token,
and a leaked token of a team administrator can delete every ticket it sees, one request each.
Each stays in the bin as it was, restorable by an administrator, for thirty days; what nobody
restores in that time the purge job removes for good, and the audit record keeps who deleted it
through which token. Nothing tells the administrators that their bin filled — a deletion creates no
notification; the bin and the audit record show it to whoever looks. Mitigation: give scripts no
`admin` token; look at the bin; after the purge a backup is the only way back
([ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)).

<a id="h-55"></a>
### H-55 — A release before the deletion shows deleted tickets again in a rollback

Dormant until an image is rolled back over migration 32
([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D4) —
a rollback across more than one release, which no supported path takes
([upgrade.md](../operations/upgrade.md#rolling-back)). The
release before it knows no `deleted_at`: run over this schema, its queries list, show and export a
deleted ticket to whoever its visibility predicate admits, until the newer release runs again. A
purge it does not do; the purge job of the newer release takes the ticket in its time. Mitigation:
roll back across the deletion only with an empty bin, or purge first.

<a id="h-56"></a>
### H-56 — Deleted is not gone until the purge, and the purge leaves traces outside the tables

Live for every deleted ticket. Until the purge — thirty days, or an administrator's act in a browser
session — the deleted ticket, everything that hangs off it and its files stay in the database, the
bucket and the backups as they were; only the queries hide them, for everybody, administrators
included. After the purge, its key and the ids of its rows stay in the audit record by design
(ADR 0024 D2), its key in the log lines that name it (`ticket purged`, the request log's paths) and
in the event ring for the replay window (H-5); the stored answer of a keyed creation of the ticket, a
question or a comment keeps their text in `idempotency_keys` for up to a day after it was written
(H-2); a consistency check's result keeps the name of a file it listed as missing, and its ticket's
key, until the next check; and an object whose removal failed after the commit stays in the bucket
with no row naming it until a team administrator removes it from the next consistency check's list
([attachments.md](attachments.md#h-13)). Backups taken before the purge keep everything. A legal
retention shorter or longer than thirty days is not configurable.

<a id="h-58"></a>
### H-58 — A question's person asked and a comment's mention tell the writer who sees a ticket

Live today, in every team with a confidential ticket or a restricted project. Asking a question of
a person (`asked_of`) and mentioning a person in a comment (`mentions`) are refused at the field when
the person does not see the ticket ([ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md)
D5, `checkAskedOf`, `checkMentions`). The writer sees the ticket, and the refusal tells them one fact
more than the ticket shows: whether that member sees it — for a restricted project, whether the
member is on its access list, which only the team's administrators read otherwise; for a
confidential ticket, nothing the ticket does not show already, since it names its assignee and
reporter and the member list names the administrators. A writer can ask it member by member, and
nothing records a refused attempt. It tells nothing of the ticket's content or of another ticket.
Mitigation: none in cowork; the access list of a restricted project is a matter of the team's own
members, and an administrator who must keep it from them keeps the project's work in a team of
its own.

<a id="h-97"></a>
### H-97 — A job's read across the teams admits whole rows

Hardening. Four policies admit a job to rows of every team with no team set: `tickets_purge_due`
the deleted tickets due, `consistency_checks_counts` every team's check result,
`import_jobs_expiry_read` every dry run, and `audit_exports_read` every act of a project's or a
team's export, for the age of the last export a scrape reads
([migrations 32](../../backend/internal/store/migrations/000032_ticket_deletion.up.sql),
[42](../../backend/internal/store/migrations/000042_attachment_consistency.up.sql),
[43](../../backend/internal/store/migrations/000043_import_jobs.up.sql),
[46](../../backend/internal/store/migrations/000046_last_export_read_at_a_scrape.up.sql)). Each job
needs a few columns — the ids and teams, the counts, the expiry, the time of an act — and each
policy admits the whole row: a ticket's text, a check's lists of missing files and orphans, a dry
run's report and the files it keeps, an export act's exporter, token, agent and counts. A query of
those jobs that read more than it needs would read it across every team. The jobs' queries read
the columns they need; nothing in the data layer holds them to that.

<a id="h-98"></a>
### H-98 — A rollback by one release leaves the purge behind the schema

Dormant until an image is rolled back by one release — the rollback the upgrade page supports
([upgrade.md](../operations/upgrade.md#rolling-back)) — over migration 41 or 43. Rolled back from
0.9.0 to 0.8.0, the purge knows no pull-request link, whose key to its ticket has no cascade: the
first due ticket with a link fails the job's one transaction, and with it the purge of every team,
at every run until the newer release runs again — read in 0.8.0's `PurgeDeletedTickets`, not run.
Rolled back from 0.11.0 to 0.10.0, the purge knows no import job, and a purged ticket's text stays in
the report of the import that created it ([import-and-export.md](import-and-export.md#h-73) H-73).
Mitigation: run the newer release again before a purge is due, or purge nothing while rolled back.

### The owner credential in the serving process

The split of the two roles protects against a compromised serving process only while that
process does not hold the owner credential (ADR 0021, residual risks). The chart keeps it in
the migration run — the init container, or in job mode the migration Job, neither of which
serves a request. `cowork serve` with `COWORK_MIGRATE_ON_START=true` — the
binary's default, and `make run`'s — requires `COWORK_DATABASE_OWNER_URL`, or its components, and holds it for its
whole lifetime ([`cmd/cowork/main.go`](../../backend/cmd/cowork/main.go) `requireForServe`);
so does a serving container given the owner URL through `backend.extraEnv`. Such an
installation keeps the split against defects, not against a compromise.

### A compromised serving process

Row-level security constrains the forgotten filter, never the deliberate one (ADR 0021 D7).
The runtime role sets its own context, so a process that runs SQL of an attacker's choosing as
that role reads and writes every team's rows within the role's grants. The policies that admit
a system actor — the login, the start-up synchronisation, the identity provider — admit whoever
names it in `app.job`, and a person's own rows whoever names the person: such a process also
creates teams, persons, global administrators, memberships, group mappings, sessions and tokens
for any person, and changes any person's groups and administrator flag. What it still cannot do
is what the grants withhold: change a policy or switch `FORCE` off, rewrite or delete the audit
record — beyond what the purge's owner function does, which such a process reaches as well: it can
delete any ticket, name the purge and empty the `before`, `after`, `reason` and `note` of that
ticket's audit rows, never their key, actor, act or time —, change a token's scope, restriction, agent flag, capabilities or expiry, bind a person to
another username or another identity of the issuer, or clear a token's revocation — the owner's
trigger `tokens_revocation_is_final` refuses that, and the runtime role can neither drop nor
disable it. It can revoke any token.
