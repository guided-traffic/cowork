# Data access

How the backend reaches PostgreSQL: two roles, the transaction wrappers, the settings the
policies read, the visibility predicates, the deletion filter and the lints that hold every query
to them, the crossings between teams, the one place SQL is built at run time, the dashboard's queries, the advisory locks, the
background jobs, the deletion and the purge of a ticket, the notifications an act writes, GitHub's
deliveries, the consistency check's tables and the publication of acts. The package is [`backend/internal/store/`](../../backend/internal/store/); the decisions
are [ADR 0027] (the wrappers), [ADR 0021] (row-level security, the roles), [ADR 0026] (the
audit record), [ADR 0034] D4 with [ADR 0065] D4 (the visibility predicate), [ADR 0024] (deletion),
[ADR 0031] (the sessions) and [ADR 0030] (the memberships the identity provider derives). Read
against the tree on 2026-10-06; the crossings between teams and the graph locks on 2026-10-10.

## Two database roles

| Role | Connects through | Owns | Does |
|---|---|---|---|
| Owner | `COWORK_DATABASE_OWNER_URL`, or its components `COWORK_DATABASE_OWNER_*` | every object of the schema | runs the migrations: `cowork migrate`, and `cowork serve` while `COWORK_MIGRATE_ON_START=true`; in the chart only the migration run holds it — the `migrate` init container, or the migration Job in job mode |
| Runtime | `COWORK_DATABASE_URL` | nothing | serves; held by row-level security on every table |

[`Migrate`](../../backend/internal/store/migrate.go) takes the owner URL and the runtime role's
name (`RoleFromURL` of the runtime URL). It refuses a runtime role equal to the owner, checks the
runtime role from the owner's connection before the run (superuser, `BYPASSRLS`, member of the
owner) and after it (the same, plus owning relations), and passes the name as the session
parameter `cowork.runtime_role` (`RuntimeRoleSetting`). A migration that creates a table
grants that role what it needs in a `DO` block over `current_setting('cowork.runtime_role')` —
`000002` grants `000001`'s `tenants`, and `000007` creates functions only; a run without the
setting fails, on purpose. `cowork serve` then calls `DB.CheckRuntimeRole` on its own pool
([`roles.go`](../../backend/internal/store/roles.go)) and refuses the same four facts, and
`checkDatabase` in [`main.go`](../../backend/cmd/cowork/main.go) reads `DB.SchemaState` and
refuses a dirty or a pending schema and warns about one that is ahead.

A migration file runs as one statement string over the simple protocol, so PostgreSQL runs it
as one transaction. A migration that rewrites rows runs as the owner with no `app.tenant_id` set, and
the forced policy hides every row from it: `000017_ticket_rank` and `000019_progress_stages`
lift the force on `tickets` for their backfills and restore it later in the file
([ADR 0021] D1). A new enum value cannot be used in the transaction that adds it, so
`000018_ticket_state_review` adds `review` alone and `000019` uses it.
`000031_attachment_names_searched_by_their_words` changes the expression of a stored generated
column (`ALTER COLUMN … SET EXPRESSION`), which rewrites every row of `attachments` with the forced
policy in place: row-level security governs queries, not the rewrite of a table, so no force is
lifted (verified on PostgreSQL 18.6 on 2026-10-05).
`TestLiftedForceIsRestoredInTheSameMigration` holds every lifted force to a restore in the same
file; the integration tier reads the force back after the run.

The grants are per table and per column: `SELECT`, `INSERT` where rows are created, `UPDATE`
on the columns a route may change — table-wide only on `ticket_counters`, `idempotency_keys`
and `login_locks` — and `DELETE` only on `ticket_links`, `ticket_interest`,
`project_repositories`, `idempotency_keys`, `sessions`, `login_attempts`, `login_locks`,
`memberships`, `group_mappings`, `project_access`, `saved_filters` — to its owner, and a shared one
to an administrator of the team, by a restrictive policy —, `notifications` — to its retention job and the purge alone — and `consistency_acceptances` — to the consistency check's job alone ([below](#the-consistency-checks-tables)) —, and, since migration 32,
on `tickets`, `questions`, `comments`, `comment_revisions`, `attachments`, `time_entries` and
`time_entry_revisions`, which restrictive policies hold to the purge of a deleted ticket
([below](#deletion-and-the-purge)) — and, since migration 41, on `github_webhook_secrets`,
`github_deliveries` and `ticket_pull_requests`, of which only the purge's delete of a deleted
ticket's `ticket_pull_requests` still runs ([below](#github-webhooks-tables)). `audit_events` gets `SELECT, INSERT` and
nothing else — append-only is a grant ([ADR 0026] D3). `users`, `tenants`, `memberships` and
`tokens` are inserted by routes — a person by an account's creation, the bootstrap or a first login
through the identity provider, a team by its creation, a grant by an administrator or the
bootstrap, a mapped membership by the identity provider, a token by its person — and each insert
has a policy that names who may (migrations 15, 20, 22), with the columns a grant lists
(`global_admin` is the bootstrap's and the identity provider's alone: the policy refuses it to a
request); the application makes the ids of the persons, teams, memberships and mappings it
inserts (`uuid.NewV7`), because an
`INSERT … RETURNING` would have to pass the read policy of a row its writer has no membership of
yet. Migrations 20 to 22 add the identity provider's columns of `users` and `sessions` with their
grants, `group_mappings`, and the writes of `memberships`, `project_access` and
`projects.restricted`; the tests and `make dev-seed` write persons, teams, grants and tokens over
the administrative connection too ([testing.md](testing.md#fixtures-of-the-integration-tier)).

## The wrappers

The pool is unexported ([`store.go`](../../backend/internal/store/store.go)); a query runs
inside one of these, and nothing else hands out a connection.

| Wrapper | Transaction | Bound to | Hands `fn` |
|---|---|---|---|
| `DB.InTenant(ctx, tenantID, fn)` | read-only | the team and the caller's person | `*Reader` |
| `DB.InTenantSnapshot(ctx, tenantID, fn)` | read-only, `REPEATABLE READ`: every read sees one snapshot — the export, which counts first and then reads a page at a time | the team and the caller's person | `*Reader` |
| `DB.Installation(ctx, fn)` | read-only | no team: only the person-scoped policies admit rows | `*Reader` |
| `DB.Mutate(ctx, tenantID, fn)` | read-write; `uuid.Nil` for an installation-level act | the team and the caller | `*Writer` |
| `DB.RunJob(ctx, name, lockKey, fn)` | read-write, under the job's lock | no team, a system actor | `*Writer` |

A `Reader` ([`tx.go`](../../backend/internal/store/tx.go)) embeds the generated read queries
(`readq`), carries `TenantID` and `UserID`, and adds `ListTickets` and `CountTickets`, its count under the same predicates. A `Writer` embeds a `Reader`
and the generated write queries (`writeq`), and adds `Record`, `Respond` and the lock methods.
`sqlc` generates the two packages from `queries/read/` and `queries/write/` with the migrations
as the schema ([`sqlc.yaml`](../../backend/sqlc.yaml)); a handler that only reads never holds a
write query.

Who a transaction acts for is a `store.Caller` in the context
([`caller.go`](../../backend/internal/store/caller.go)), put there by the API pipeline after
authentication: the person or a `system:<name>` actor, the token and its name, the token's team and
project restrictions, the agent mark, the agent's capabilities and the request id. The person is never a
call-site argument. `Mutate` refuses a context with neither or both of person and system actor.

Outside the wrappers, deliberately: `LookupToken` and `LookupSession` (read one token or session
by the hash the request presents, see below), `TouchTokenLastUsed` and `TouchSession` (the
last-used date and the idle clock, bookkeeping and not acts, [ADR 0035] D2,
[ADR 0031] D3), the login's own transactions ([below](#the-login-and-the-sessions)), the identity
provider's ([below](#the-identity-providers-transactions)), `FindPerson` (the person lookup of a
member's addition, which sets `app.person_lookup`), `CheckRuntimeRole`, `SchemaState`, `Ping`, and
`Listen`; and `jobRead` — a read-only transaction that names a job and no
team, through which `LastConsistencyCheck` and a scrape read the consistency check's results of
every team, and the scrape every team's last export ([below](#the-consistency-checks-tables)).

`Open` registers `timestamptz` to scan in UTC and a tracer that logs a query slower than
`DefaultSlowQuery` (500 ms) by its sqlc name, never its arguments, and counts a statement that
failed by its kind — a closed set of SQLSTATE meanings and client causes, `queryErrorKind` —; with
a registry in `Options.Metrics` it lets a scrape read the pool's statistics and the schema state
([metrics.md](metrics.md)). A missing or invisible row
is sqlc's `pgx.ErrNoRows`, passed through the wrappers: the handler maps it to its own `404`
(or, for a conditional write, to the answer of the later request — see
[conventions.md](conventions.md)), and one it does not map is a `500`. `store.ErrNotFound`
comes only from `LookupToken`.

## The settings the policies read

`setContext` writes transaction-local settings (`set_config(…, true)`) at the start of every
wrapper's transaction; an empty value leaves a setting unset. A team is stored as a tenant, and the
database keeps the word — the setting `app.tenant_id`, the column `tenant_id`, the table `tenants`,
the policies `tenant_isolation` and the functions `app_tenant_id()` and `app_is_tenant_admin()` —, as
does the Go that hands it on (`InTenant`, `tenantID`, `Writer.LockTenant`;
[ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1).

| Setting | Written from | Read by |
|---|---|---|
| `app.tenant_id` | the wrapper's team | `app_tenant_id()`: every `tenant_isolation` policy, the policies of `tenants`, `memberships`, `users`, `audit_events`, the visibility functions |
| `app.user_id` | `Caller.UserID` | `app_user_id()`: the person's own user row, memberships, teams, tokens, idempotency keys and installation-level audit rows; the visibility functions |
| `app.restricted_project_id` | `Caller.RestrictedProjectID` | `app_restricted_project_id()` in `app_project_visible` and `ticket_sight` |
| `app.restricted_tenant_id` | `Caller.RestrictedTenantID`, a token's team restriction (migration 47) | `app_restricted_tenant_id()` in `ticket_sight`: a token restricted to one team reads every other team's tickets by their heads ([below](#crossings-between-teams)) |
| `app.crossing` | no wrapper: each crossing function sets its kind as its first statement and restores what it found; every other `SECURITY DEFINER` function empties it first (migration 47) | `app_crossing()`: the eight crossing policies of the owner role, and the trigger `tickets_crossing_guard` ([below](#crossings-between-teams)) |
| `app.job` | `RunJob`'s name; `login` for the login's own transactions; `identity-provider` for the identity provider's, and for the derivation inside an administrator's change of a mapping; `ticket-purge` for the purge job and for the purge's part of an administrator's request (`Writer.PurgeTicket`); `team-deletion` for `DB.EndTeamRelations`, which `end_team_relations` demands; `consistency-check` for its job, and for the read-only transactions of `jobRead` that read its results across the teams — the schedule's and a scrape's | the `idempotency_keys` policy admits every row in a transaction named `idempotency-expiry`; the policies of migrations 15, 16, 20–22, 30, 32, 41, 42, 43 and 46 name `login`, `bootstrap`, `session-expiry`, `login-expiry`, `identity-provider`, `notification-expiry`, `ticket-purge`, `github-webhook`, `github-delivery-expiry`, `consistency-check` and `import-expiry` for the rows those system actors keep (`app_job()`) — the two of migration 41 named by no code since GitHub's webhook was removed ([below](#github-webhooks-tables)) |
| `app.token_hash` | `LookupToken`, the hex SHA-256 of the presented token | the `tokens` policy admits exactly that row |
| `app.session_hash` | `LookupSession`, and `Caller.SessionHash` in every transaction of a session's request: the hex SHA-256 of the presented cookie; in the identity provider's transactions the session a login replaces or a refresh holds | `app_session_hash()`: the `sessions` policies admit exactly that row — to read it, to end it |
| `app.person_lookup` | `FindPerson` only: the address or username an administrator adds a member by | `app_person_lookup()`: the `users` policy admits the persons it names to an administrator of the current team, and no other person of the installation (migration 20) |
| `app.saved_filter_id` | `Writer.UnshareAnothersFilter` only, for its one statement: the saved filter a team administrator unshares | `app_saved_filter_id()`: the read policy of `saved_filters` admits that filter, unshared, to an administrator of the current team (migration 39) — PostgreSQL holds an update's new row to the read policy |

A transaction-local setting reads `''`, not `NULL`, on a pooled connection after its
transaction ended, and a bare `''::uuid` raises. Every policy therefore reads a setting through
`NULLIF(current_setting('app.…', true), '')` — inside `app_tenant_id()`, `app_user_id()` and
`app_restricted_project_id()` — or compares `app.job` for equality, so an unset context matches
no row. `TestPoliciesReadSettingsGuarded` in
[`policy_test.go`](../../backend/internal/store/policy_test.go) holds every migration line to
that.

Every table the migrations create has row-level security enabled and forced, a policy and a
grant; every table outside the named list carries `tenant_id` and the canonical
`tenant_isolation` policy (`USING` and `WITH CHECK` on `tenant_id = app_tenant_id()`). The named
list — `tenants`, `users`, `memberships`, `tokens`, `idempotency_keys`, `audit_events`,
`local_accounts`, `sessions`, `login_attempts`, `login_locks` — has policies of its own, because
those rows are read across teams by their person or have no `tenant_id` ([ADR 0021] D6). The
policies of the last four, and the new write policies on the others, use five more functions
(migration 15) — and `app_person_lookup()` since migration 20: `app_job()` and `app_session_hash()`, which read the settings,
`app_is_global_admin()`, and `app_manages_account(user)` and `app_manages_username(name)` — the
current person is an administrator of the current team **and the account is one that team
manages** (`local_accounts.managing_tenant_id`). That last rule is where an account, which
belongs to the whole installation, meets a team: see
[docs/security/local-accounts.md](../security/local-accounts.md). `TestEveryTableHasItsPolicyAndGrant` checks all of it on the migration
files, without a database; application queries still filter by `tenant_id` as well
([ADR 0021] D4).

`group_mappings` carries `tenant_id` and the canonical policy, and three more: the identity
provider reads every team's mappings, the bootstrap inserts the administrator group's, and
**restrictive** policies (`AS RESTRICTIVE`) hold every write to an administrator of the current
team — an insert and an update to one who is a global administrator as well
(`app_is_tenant_admin() AND app_is_global_admin()`, the handler's `mapsGroups` in the data layer,
[ADR 0030] D7), a delete to any (the bootstrap's insert excepted, which names no person). `tenants`
admits every row to a global administrator and `memberships` their own marked grant in any role, and
the change of its role in the team's transaction,
([migration 26](../../backend/internal/store/migrations/000026_global_admin_self_grant.up.sql),
[ADR 0034] D2): the list of every team and the boundary's admission of a global administrator
without a role read the team in an `Installation` transaction; inside the team's transaction no
policy tells them from a member, and the operations they reach are the boundary's list
([api.md](api.md#the-team-boundary)). A
restrictive policy is ANDed with the permissive ones instead of ORed: it narrows what any other
policy admits, so a later permissive policy cannot widen who writes a mapping. `project_access` has
three restrictive policies on `app_is_tenant_admin()` alone (migrations
[21](../../backend/internal/store/migrations/000021_group_mappings.up.sql),
[22](../../backend/internal/store/migrations/000022_membership_administration.up.sql),
[25](../../backend/internal/store/migrations/000025_group_mappings_global_admin.up.sql);
`TestPoliciesOfThePersonsAndTheirAccounts`). `notifications` carries `tenant_id` and the canonical
policy, and restrictive ones that hold reading and marking to the notification's own person
(`user_id = app_user_id()`) — the writer of an act inserts notifications for others, and a forgotten
`user_id` filter must not show one person another's inbox (`TestTheInboxPolicyHoldsAPersonToTheirOwn`)
— and deleting to the job `notification-expiry`, which a permissive policy admits past the team
([migration 30](../../backend/internal/store/migrations/000030_notifications.up.sql)); migration
32 admits the purge's delete of the notifications of a deleted ticket beside it. `tokens` admits an
administrator of the current team, since
[migration 35](../../backend/internal/store/migrations/000035_tenant_tokens.up.sql), every token of
a member of the team that is unrestricted or restricted to it — to read and to revoke, the rows of
the team's token list (`app_tenant_reaches_token`, `ListTenantTokens`) — and no token restricted
to another team; the queries name the same rows. On
`memberships` the writes are split by source instead: a grant is inserted, changed and removed by
an administrator of its team, a mapped membership only in a transaction named
`identity-provider`. `saved_filters` carries `tenant_id` and the canonical policy, and restrictive
ones that hold reading to the filter's owner or a shared filter, and inserting, changing and
deleting to its owner (`owner_id = app_user_id()`,
[migration 33](../../backend/internal/store/migrations/000033_saved_filters.up.sql);
`TestTheSavedFilterPoliciesHoldAPersonToTheirOwn`) — and since
[migration 39](../../backend/internal/store/migrations/000039_saved_filters_moderated_by_administrators.up.sql)
to an administrator of the current team (`app_is_tenant_admin()`) changing another person's
shared filter into one that is not shared, deleting it, and reading it back unshared while
`app.saved_filter_id` names it — and the trigger `saved_filters_moderation_guard` refuses
(SQLSTATE `42501`) any other change of a filter that is not the caller's own, its name or its
parameters, which a policy cannot see; the queries `UnshareSavedFilter` and `DeleteSharedSavedFilter`
name `shared` as the policies do, and the unshare runs only through `Writer.UnshareAnothersFilter`
([`store/filters.go`](../../backend/internal/store/filters.go)), which names the filter
(`TestTheSavedFilterPoliciesAdmitAnAdministratorToASharedFilter`). `import_jobs` carries `tenant_id`
and the canonical policy, restrictive ones that admit reading to the job's maker
(`created_by = app_user_id()`), an administrator of the current team (`app_is_tenant_admin()`),
the job `import-expiry` and the purge (`ticket-purge`), inserting to the maker in their own name and
the administrator, and changing to the maker, the administrator and the purge — which takes a purged
ticket's file out of its job's report —, and a restrictive delete that admits only the expiry job and
only a dry run; the expiry job's own permissive read and delete reach the dry runs of every team
with no `app.tenant_id` set
([migration 43](../../backend/internal/store/migrations/000043_import_jobs.up.sql), the maker since
[migration 45](../../backend/internal/store/migrations/000045_import_jobs_of_their_writer.up.sql);
`TestTheImportJobPoliciesAdmitItsMakerAndTheAdministrators`). A dry run holds the content of the
files it read, an embargoed finding's among them, so a query that forgot its caller must show
another member nothing ([import-and-export.md](import-and-export.md)).

### GitHub webhook's tables

GitHub's webhook ([migration 41](../../backend/internal/store/migrations/000041_github_webhook.up.sql),
[ADR 0071]) added three tables, each with `tenant_id` and the canonical policy, and the releases from
0.9.0 to 0.12.0 wrote them. The webhook is removed (ADR 0071 Status): no code reads or writes
`github_webhook_secrets` or `github_deliveries` any more, and their rows, their restrictive policies
— the secret's reading held to an administrator or the job `github-webhook`, a delivery's writes to
that job, its expiry to `github-delivery-expiry` — and the `tenants` policy's admission of the job
`github-webhook` stay until a contract migration of a later release drops them
([ADR 0028] D3). `ticket_pull_requests` references its ticket, so the purge of a deleted ticket still
deletes the ticket's rows (`DeleteTicketPullRequests`, held to the purge by a restrictive policy);
nothing else touches them. The notifications of the reason `merged` such a release made stay in
`notifications`, and the inbox's reads leave them out ([below](#notifications)).

## Mutate: acts, idempotency, publication

`DB.Mutate(ctx, tenantID, fn)` is the only way a route commits a write ([ADR 0027] D3):

1. With an idempotency key in the context (`store.WithIdempotency`, set by the API's `keyed`),
   it first looks the key up in an `Installation` read — per token, unexpired. The same
   fingerprint returns the stored `Result` without running `fn`; another fingerprint is
   `ErrIdempotencyMismatch`.
2. It begins a transaction, writes the settings and runs `fn` with a `Writer`. An error rolls
   back and is returned. `ErrNoChange` is the function saying the request changes nothing: the
   caller answers with the current state and no act.
3. A function that recorded no act gets `ErrNoAct`, and nothing commits.
4. For every act (`Writer.Record(store.Event{…})`) it writes one `audit_events` row with the
   caller's facts — person or system actor, agent mark, the capability set when the request is
   an agent's, token and its name (`tokenName`: the name only with the token, [ADR 0036] D6),
   request id, the keyed hash of the client's address (`Caller.SourceHash`, none for a job) — and
   the idempotency key: the keyed request's own, or an
   `Event.IdempotencyKey` a client sent where no response is stored. An act with `Event.System`
   set is a system actor's recorded in the request: it carries the request id and the source hash,
   never the caller's person, token or agent mark — the identity provider's derivation in an
   administrator's change of a mapping. The row's id is made in
   Go (`uuid.NewV7`): an `INSERT … RETURNING` would have to pass the read policy, which a system
   actor's installation-level row does not. Then it writes the act's notifications
   ([below](#notifications)) and publishes the act ([below](#publication)).
5. A keyed mutation stores the `Result` the function set with `Respond`
   (`StoreIdempotencyKey`). That insert takes over an expired row and returns nothing when a
   concurrent request holds the same unexpired key: the attempt rolls back and the next round
   replays the winner's response; losing twice is an error.
6. It commits, and only then counts the acts in the metrics, by action and actor (`countActs`), as
   `RunJob`, `RecordLoginAttempt` and the identity provider's transactions do after theirs. The
   returned `*Result` is non-nil only for a replay.

| `Event` field | Holds |
|---|---|
| `EntityType`, `EntityID` | what the act is on |
| `TicketID`, `TicketKey` | the ticket it belongs to; the key survives a purge ([ADR 0024] D2) |
| `Action` | an `audit_action` enum value (migration `000005`) |
| `Before`, `After` | the changed fields only; `nil` writes `NULL` |
| `Reason`, `Note` | the act's reason and note |
| `ExplainedBy` | the comment written in the same request ([ADR 0015] D2) |
| `Refs` | the other tickets the payload names; a reader who cannot see one of them gets the act without its payload ([domain.md](domain.md#comments-and-the-activity-list)) |
| `IdempotencyKey` | a key recorded, not stored ([ADR 0045] D7) |
| `System` | a system actor, `system:<name>`, whose act this is though the request's transaction records it; empty for the caller's own act |
| `Membership` | a `MembershipChange` — the person, the project, the mapping, the audience — which publishes the act as `membership.changed` ([events.md](events.md)); nil for every other act |
| `NewProject` | the project the act created, published so that the streams admit its events at once and sent to no client ([events.md](events.md#publication)); `uuid.Nil` for every other act |
| `ProjectRank` | a `ProjectChange` — the project and its key — which publishes an act on a project's tickets as a whole, the sort by the score and an import's execution, as `project.changed`; nil for every other act |
| `Quiet` | the act is written and never published: another act of the transaction announces it — an import's acts on what it creates, which the job's act `imported` announces |
| `Notices` | whom the act tells in their inbox and why ([notifications](#notifications)); none for an act that tells nobody |
| `InboxOf` | the person whose inbox the act changed without a notice — their own notifications marked read — whose person-level streams hear `inbox.changed` |
| `Published` | what the act's publication tells of its ticket — the project, the version, the confidential rule's inputs — where the ticket is gone when the act is written: a purge's; nil reads them at publication (`TicketFacts`) |

## Visibility in SQL

Three functions carry the restriction and the confidential flag into every query:

| Function | Migration | True when |
|---|---|---|
| `app_project_visible(project)` | `000007_visibility` | the token is not restricted to another project; and the project is unrestricted, or the person is a team administrator or on its `project_access` list |
| `app_ticket_visible(project, confidential, assignee, reporter)` | `000008_tickets` | the project is visible; and the ticket is not confidential, or the person is a team administrator, its assignee or its reporter |
| `app_time_visible(person)` | `000013_time_entries` | the entry is the person's own, or the person is a team administrator, or a member while the team shows time to members |
| `person_sees_ticket(tenant, ticket, person)` | `000030_notifications` | whether *another* person — not the caller — sees a ticket: a member of the team; the project unrestricted, or the person an administrator or on its list; the ticket not confidential, or the person an administrator, its assignee or its reporter. `CanSeeTicket` (the person a question is asked of) and `NoticeRecipients` (whom an act tells) read through it; the caller's token restriction does not narrow it |

**The lint.** `TestEveryReadOfProjectsAndTicketsCarriesTheVisibilityPredicate` in
[`queries_test.go`](../../backend/internal/store/queries_test.go) splits every file under
`queries/*/` into its named queries. A query with `FROM tickets` or `JOIN tickets` must call
`app_ticket_visible(` at least as often as it reads `tickets`; one that reads `projects` and no
ticket must call `app_project_visible(`. A predicate on a joined ticket does not stand in for
the one on the ticket the query reads.

**Exemptions are named.** A query that must read past the predicate carries
`-- visibility: exempt (<why>)` in its block. Today:

| Query | Why |
|---|---|
| `GetWrittenTicket` | the writer's reread of the row it wrote — a reassignment can take a confidential ticket out of the writer's sight, and the answer shows what the write left |
| `TicketFacts` | the publication of a committed act; the streams filter |
| `CanSeeProject` | whether *another* person sees a project: the assignee |
| `ListWatchers` | whom an act tells: the watchers of a ticket, each then held to their own sight of it by `NoticeRecipients` ([notifications](#notifications)) |
| `ProjectKeyTaken` | a key's existence, unique in the team whether or not the caller sees its project |
| `GetRepositoryBinding` | a binding's existence: a repository and sub-directory are unique in the team whether or not the caller sees the project that holds them; the handler names the project only when the caller sees it. The other queries of `project_repositories` join `projects` and call `app_project_visible` |
| `TenantAttachmentUsage` | the bytes of every attachment of the team, for the quota and its administrators: a file counts whether or not the caller sees its ticket; it reads no ticket, and names it anyway |
| `ListCheckedAttachments` | the consistency check's list of the missing files, every one of the team whose bytes are missing, for the team's administrators, who see every ticket; read by the job, which has no person |
| `LastRank`, `ListUnrankedTickets`, `GetTicketRank`, `NextRankedTicket`, `PreviousRankedTicket`, `ListRankKeys` | the rank keys of the project a write hands a key out in: a new key lies between keys that exist, hidden tickets' included, so none is handed out twice, and a rebalancing spreads every key, so every ticket keeps its place ([domain.md](domain.md#rank)) |
| `GetScoreInputs` | the inputs of the score of a ticket the caller read through the predicate in this transaction, read again after the write that changed one ([domain.md](domain.md#the-score)) |
| `ImportNumbersTaken` | a number's existence in the project an import goes into, unique whether or not the caller sees the ticket that holds it — a deleted one's included, also exempt from the deletion filter ([import-and-export.md](import-and-export.md#the-dry-run)) |
| `LinkEndKey` | the key of the other end, in the team, of a link the caller removes, whatever they see of it: the removal is an act on both tickets; also exempt from the deletion filter, a link to a deleted ticket is removed with its act as well |
| `ExportHiddenConfidential` | the count of the confidential tickets of the projects the caller sees that an export leaves out, which the manifest says ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md) D5); it asks the predicate `IS NOT TRUE`, since it answers `NULL` for a ticket without an assignee |

The search (`SearchTickets`, [search.md](search.md#the-query)) reads tickets in seven places — the
ticket's text, a title by trigram, the key, a comment, a question, a file name, the page's hits — and
calls the predicate in each; `ListTicketImages`, the raster attachments a rendered text may show
([rendered-markdown.md](rendered-markdown.md)), joins its tickets with the predicate as well.

The SQL functions `ticket_ancestor_or_self`, `blocks_path_exists`, `ticket_derived_progress`
(the implementation stage, kept for the release before the stages), `ticket_derived_stage` and
`person_sees_ticket` read the team's tickets past the predicate for the same reasons; row-level security still
holds them to the team. Since migration 32, `ticket_derived_stage` leaves a deleted child out and
`person_sees_ticket` answers no for a deleted ticket; the two integrity walks still step over
deleted tickets, so that a restoration can never close a cycle. Since migration 47 this release
calls none of the first four: the walks and the derivation cross teams as crossings
([below](#crossings-between-teams)), and the four stay for the release before, which a rollback
runs ([ADR 0028] D3).

**The deletion filter.** A deleted ticket answers like a missing one ([ADR 0024] D1, D3): beside
every call of the visibility predicate on a ticket, the query says `<alias>.deleted_at IS NULL` — an
application filter, not a policy, so the bin and the purge can invert it under the same row-level
security. `TestEveryReadOfTicketsCarriesTheDeletionFilter` in
[`queries_test.go`](../../backend/internal/store/queries_test.go) holds every query that reads
`tickets` to the filter once per ticket it reads, unless its block names `-- deletion: exempt
(<why>)`: the bin's two queries and the purge's, which read deleted tickets only; `GetWrittenTicket`,
the writer's reread (its joined tickets keep the filter); `TicketFacts`, because a deletion, a
restoration and a purge are published too; `ListCheckedAttachments`, the consistency check's list, in
which a deleted ticket's file has its row and its object until the purge; `GetTicketRank` and `GetScoreInputs`, a ticket read
through the filter in the same transaction; and the rank keys of `LastRank`, `ListUnrankedTickets`,
`NextRankedTicket`, `PreviousRankedTicket` and `ListRankKeys`, because a deleted ticket keeps its
key, which its restoration brings back — a rebalancing spreads it with the others —, and no key may
be handed out twice; and `ImportNumbersTaken`, because a deleted ticket keeps its number. The
ticket's columns count `open_prerequisites` through the crossing `open_prerequisite_count`,
`GetWrittenTicket` included: an open direct prerequisite of any team whose state the caller reads in
a head, never a placeholder — one of the caller's own team in a project restricted from them counts
by its head since migration 47. The deprecated tree (`ListPrerequisites`, `ListDependents`) is the
one walk of the queries that returns tickets: it calls the predicate on every ticket it steps to, so
it never passes a hidden one, and it keeps each link once per depth, never each path
([domain.md](domain.md#the-prerequisite-tree)); the tree across teams is the crossing
`prerequisite_heads`.

## Crossings between teams

A ticket's parent, its children and its links may be tickets of another team
([ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3 as amended
2026-10-10). What a caller reads or changes of a ticket of another team passes through a
**crossing**, a `SECURITY DEFINER` function of the owner role, and through nothing else
([ADR 0021] D7; [migration 47](../../backend/internal/store/migrations/000047_relations_across_teams.up.sql),
[`store/crossing.go`](../../backend/internal/store/crossing.go)). No query of the runtime role is
widened: every forced policy holds it to the transaction's team as before, so a query a handler
writes still reads one team, and a forgotten filter still yields nothing of another.

**How a crossing is admitted.** Eight permissive policies `TO` the owner role —
`tickets_crossing_read`, `tickets_crossing_derive`, `tickets_crossing_detach`,
`projects_crossing_read`, `tenants_crossing_read`, `project_access_crossing_read`,
`ticket_links_crossing_read`, `ticket_links_crossing_delete` — admit a read, or one write, only while
`app_crossing()` names a kind they serve; the migration creates them in a `DO` block for
`current_user`, the owner. A crossing function is plpgsql: its `DECLARE` keeps the value it finds
(`prev text := NULLIF(current_setting('app.crossing', true), '')`), its first statement is
`PERFORM set_config('app.crossing', '<kind>', true)`, and it restores `prev` before every `RETURN`
and at its end. Every other `SECURITY DEFINER` function, `purge_ticket_audit`, empties the setting
first. A function's `SET` clause cannot carry it: PostgreSQL refuses a custom setting there to an
owner that is not a superuser (SQLSTATE `42501`) unless a superuser grants `SET ON PARAMETER`, which
no installation's owner role holds (`TestPostgreSQLRefusesTheCrossingInASetClause`, verified on
PostgreSQL 18.6). A missed restore leaks nothing: the policies bind the owner role alone, code runs
as the owner at run time only inside a `SECURITY DEFINER` function, and each sets its own value
first; the runtime role gains nothing by setting `app.crossing` itself
(`TestTheCrossingIsTheOwnersAlone`).

| Kind | Function | Called by | Reads or writes |
|---|---|---|---|
| `head` | `relation_heads(anchors, kinds)` | `Reader.RelationHeads`, `Reader.ParentHeads` | the parent, the children and the links of tickets of the caller's team they see, each other end with its sight; an assignee only for a ticket of the caller's own team they read |
| `head` | `readable_ticket(team, project, number)` | `Reader.ReadableTicket` | whether the caller reads the ticket a key names, of any team: a parent or a link's other end before it is set, in the write's own transaction |
| `head` | `prerequisite_heads(root, up, depth, after, limit)` | `Reader.PrerequisiteHeads` | the prerequisite tree, or its mirror, across teams, by heads; the walk goes on only from a ticket the caller reads |
| `head` | `open_prerequisite_count(ticket)` | the ticket select's `open_prerequisites` | the open direct prerequisites whose state the caller reads in a head |
| `head` | `open_prerequisite_targets()` | the list builder's `Blocked` filter (`openBlocker`) | the tickets of the team such a prerequisite blocks, read once for a list |
| `head` | `open_prerequisite_heads(ticket)` | `Reader.OpenPrerequisiteHeads` | those prerequisites by their heads: what refuses `done`, and what an override names |
| `walk` | `parent_chain_reaches(candidate, ticket)` | `Writer.ParentChainReaches` | yes or no: a new parent closes a cycle, across teams; under the parents' graph lock |
| `walk` | `blocks_reach(from, to)` | `Writer.BlocksReach` | yes or no: a new `blocks` link closes a cycle, across teams; under the blocks' graph lock |
| `derive` | `refresh_derived(parents)` | `Writer.RefreshDerived`, every refresh of a parent | the derived stages of parents and their ancestors from their children of every team, the deepest first, no version and no act; a parent of the caller's own team also has its own stages seeded when its last child leaves, is marked done by hand when it gains one while done, and has its `updated_at` moved — a parent of another team gets the three derived columns alone (migration 48) and is told on its team's streams as `ticket.changed` of the kind `derived` ([publication](#publication)) |
| `act` | `relations_elsewhere(ticket)` | `Reader.RelationsElsewhere` | every relation of a ticket of the team whose other end is a ticket of another team: that team, ticket and key, and its head as an outsider reads it — where an act is recorded, never shown to the caller |
| `purge` | `end_relations_elsewhere(ticket)` | `Writer.PurgeTicket` | a purged ticket's children elsewhere made roots and the links another team keeps to it deleted; every far end answered for its act; only inside the purge of a deleted ticket of the team |
| `purge` | `end_team_relations(team)` | `DB.EndTeamRelations` | every relation between a team and the others ended, both directions; only in a transaction named `team-deletion` with no team set; no route calls it |

The store calls the functions with pgx itself, not through sqlc: sqlc does not type the columns of a
function that returns a table as they are — a placeholder's key and title are `NULL` —, so
`crossing.go` scans them into its own types, and the query lints, which read the query files, do not
see them; the lints below read the functions instead.

**The sight.** `ticket_sight(team, project, confidential, assignee, reporter)`, a plain function the
crossings call and nobody else executes, answers what the caller sees of a ticket at the other end of
a relation: `placeholder` for a confidential ticket they are not admitted to — admitted is a member
of its team who administers it or is its assignee or its reporter — and for a confidential one
outside a token's restriction (`app.restricted_tenant_id`, `app.restricted_project_id`); `head` for
any other ticket outside a token's restriction, of a team they hold no role in, or of a project
restricted from them; `sees` otherwise. A deleted ticket answers like a missing one: every crossing
that returns tickets leaves it out. Every head function asks besides `app_is_member()` — the caller
holds a role in the transaction's team —, behind the boundary that admits nobody else.

**What a crossing writes.** The trigger `tickets_crossing_guard` refuses (SQLSTATE `42501`) an update
of a ticket inside a crossing that changes more than its kind may: `derive` the three derived
columns, and on a ticket of the transaction's own team the seeded stages, `done_by_hand` and
`updated_at` besides; `purge` the `parent_id`. An act on a ticket of
another team is no crossing's: `Writer.RecordElsewhere(ctx, far, events...)` binds the transaction to
the far team — `set_config('app.tenant_id', …)` —, writes the audit rows, their notifications and
their publication there as the caller's acts, under that team's own policies, and binds it back; a
`FarEnd` has unexported fields, so only a crossing of the store makes one, and a handler cannot
record into a team of its choosing.

**The lints and the start-up check.** `TestEverySecurityDefinerFunctionIsFencedIn`
([`policy_test.go`](../../backend/internal/store/policy_test.go)) reads every `SECURITY DEFINER`
function of the migrations — its fixed `search_path`, its first statement setting `app.crossing` to
its kind or `''`, a crossing's restore before every `RETURN` and at its end, no `SET` clause naming
the setting —, and `TestTheCrossingPoliciesNameTheOwnerAlone` holds the eight policies to the owner.
`TestOnlyTheCrossingFunctionsCross` ([`crossing_test.go`](../../backend/internal/store/crossing_test.go))
refuses `app.crossing` in any Go or query file of the backend, and a binding of `app.tenant_id`
outside `setContext`, `inTenant`, `flushIn` and `RecordElsewhere`; `TestEveryCrossingFunctionDecidesSight`
requires every head function to call `ticket_sight(`, to leave the deleted out and to ask
`app_is_member()`, every walk to answer yes or no, and every other crossing to be listed with what
it returns. `cowork serve` calls `DB.CheckCrossing` after `CheckRuntimeRole`
([`roles.go`](../../backend/internal/store/roles.go)): every crossing policy must name the owner of
`tickets` and nobody else, and every crossing function must be that owner's and run with its
rights — a change of ownership past the migrations would leave policies that no function meets, the
heads absent and the walks blind to other teams.

## The ticket list builder

`Reader.ListTickets(ctx, TicketFilter, TicketPage)` in
[`tickets.go`](../../backend/internal/store/tickets.go) is the one place SQL is built at run
time ([ADR 0027] D4). It starts from `t.tenant_id = $1 AND app_ticket_visible(…) AND
t.deleted_at IS NULL` (`live`), adds one
condition per filter, and passes every filter value as a positional argument — no filter value
enters the SQL text; only the integer `LIMIT` and `OFFSET` are formatted in. The lints cannot see Go, so the builder adds the predicate and the deletion filter itself
(`TestTicketListLeavesTheDeletedOut`), and
`TestTicketListSelectsWhatTheQueriesSelect` holds its column list and joins equal to
`GetTicketByNumber`: a list row and a single ticket are one type (`store.TicketRow`).

| Part | Does |
|---|---|
| `ValueSet` | a repeatable vocabulary filter: `In` combines with OR, `NotIn` is the `!` negation |
| `PersonSet` | ids, plus `None` / `NotNone` for an empty column (assignee, parent) |
| terminal states | `done` and `dropped` are hidden unless `IncludeTerminal` or `States.In` names them |
| `Blocked` | an open ticket the caller can see blocks the ticket |
| `HasOpenQuestions`, `Interest` | an open question exists; the caller's or anyone's stake exists |
| progress filters | on the implementation stage the ticket shows: derived while it has children, else its own |
| `DoneAfter` | `t.done_at > …`, the tickets done after a time; like the opened and updated bounds it excludes the bound ([ADR 0049] D1) |
| `Query` | `search @@ plainto_tsquery('cowork_simple', …)` ([ADR 0025]) |
| `TicketOrder` | `ByRank` for a project's list — `ORDER BY rankedKey NULLS LAST, t.number`, `rankedKey` the key of an open ticket and none for a done or dropped one, whatever its column holds: the ranked by their key, then the unranked by number —, `NewestFirst` (id descending) for the team's, and `ByScore` for a team's part of a person-level list — `ORDER BY t.score_key DESC, t.id` ([domain.md](domain.md#the-score)); `Position` writes a row's cursor position, the id, `<key>.<number>` (`RankPosition`) with an empty key for an unranked ticket, which the API seals, or `<score key>/<id>` (`ScorePosition`), which it does not ([api.md](api.md#paging)) |
| `TicketPage` | after a cursor position with `LIMIT` one above the page, or a numbered page with `LIMIT`/`OFFSET` and a `count(*)` total |

## The dashboard's queries

[`queries/read/dashboard.sql`](../../backend/internal/store/queries/read/dashboard.sql) holds the
ten reads of the team's dashboard ([api.md](api.md#the-dashboard)), which the handler runs in one
`InTenant` transaction. Each reads `tickets` once and calls `app_ticket_visible` on it with the
deletion filter `t.deleted_at IS NULL` beside it — the two lints above hold them to that like any
query, and none is exempt — and says so in a `-- visibility:` line; the time adds
`app_time_visible` on every entry, as the time report does. So a confidential ticket or a
restricted project counts, is named and moves a median only for whoever sees it, and a deleted
ticket — its questions and its time with it — for nobody until it is restored; the integration
tier proves it tile by tile with a ticket the member cannot see, and once for a ticket deleted and
restored ([`api_dashboard_test.go`](../../backend/test/integration/api_dashboard_test.go)).

- **The counted projects** are two `text[]` arguments, `projects` and `without_projects`: a
  project counts when its key is in the first, or the first is empty and the project is not
  archived, and never when its key is in the second. The handler passes empty arrays, never `NULL`,
  which would match nothing.
- **The blocked ticket's start** is read from the audit record: the latest `transitioned` act of
  the ticket whose `after` names the state `blocked` (`max(created_at)`, on `audit_by_ticket`), or
  the ticket's `updated_at` where no act records one — a ticket blocked past the API. The act is
  read only for a ticket the predicate has let through.
- **Times come from the handler's clock**, not `now()`: the age buckets' cuts, the weeks' first
  Monday and the windows' ends are arguments, so a test fixes the clock (`Options.Now`) and the
  database's own time does not move a bucket. The week is `date_trunc('week', done_at AT TIME ZONE
  'UTC')`, a Monday; the median is `percentile_cont(0.5)` over the seconds from `opened_at` to
  `done_at`, `0` with no ticket, which the handler answers as `null` by the count beside it.
- **No migration and no index** came with them: the indexes there are cover their filters —
  `tickets_by_state`, `time_entries_by_day`, `audit_by_ticket` — and each tile reads the team's
  visible tickets anew. Their plans and their time over a large team have not been measured.

## Advisory locks

All locks are transaction-level: released by commit or rollback. A session-level lock would
outlive its work on an idle pooled connection ([ADR 0027] D5).

| First key | Name | Taken by | Orders |
|---|---|---|---|
| `0x636f776b` | `cowk` | `RunJob`, `pg_try_advisory_xact_lock(ns, lockKey)` | one replica per job |
| `0x636f7767` | `cowg` | `Writer.LockGraph(GraphParents)`, key `1`, and `Writer.LockGraph(GraphBlocks)`, key `2` — the installation's, `pg_advisory_xact_lock(ns, key)` | a new parent, before the parent cycle walk; a new `blocks` link — a link, a block that names a ticket, an import that may make one —, before the `blocks` cycle walk; both across every team ([crossings](#crossings-between-teams)) |
| `0x636f7771` | `cowq` | `Writer.LockQuestions(ticketID)` | question numbers of a ticket |
| `0x636f7761` | `cowa` | `Writer.LockAttachments(ticketID)` | uploads to a ticket, before the per-ticket count |
| `0x636f7775` | `cowu` | `Writer.LockAttachmentQuota()`, where `COWORK_ATTACHMENT_TEAM_QUOTA` is set, before the ticket's attachment lock | the team's uploads, before the sum against its quota |
| `0x636f7773` | `cows` | `CompleteOIDCLogin` (`forSubject`), `pg_advisory_xact_lock(ns, hashtext(json_build_array(issuer, subject)::text))`, the login's first lock, before it reads the person | the logins of one identity of the issuer, its issuer and subject: the second of two first logins at once finds the person the first made and carries on as a returning person's login |
| `0x636f7769` | `cowi` | the identity provider's transactions, and `RederiveGroup` per person in an administrator's change of a mapping | what the identity provider decides about one person: a login, a refresh's answer, a token's gate check, a mapping's derivation |
| `0x636f7772` | `cowr` | `DB.ReserveLoginAttempt`, `pg_advisory_xact_lock(ns, hashtext(encode(address, 'hex')))` | the attempts to prove a password of one client address, before the throttle's count ([the login](#the-login-and-the-sessions)) |
| `0x636f7774` | `cowt` | `Writer.LockTenant()`, first in an administrator's change of a grant (`PUT`, `DELETE …/grant`) or of a mapping (create, change, remove) and in the deactivation of an account (`PUT …/accounts/{username}/deactivation`) | the changes of who administers the team, before the `last_admin` check: the second of two concurrent changes sees the first committed |

The writer locks are `pg_advisory_xact_lock(ns, hashtext(id::text))`
([`jobs.go`](../../backend/internal/store/jobs.go)), the graph locks `pg_advisory_xact_lock(ns, key)`
with one key per graph. The graph locks replaced the per-project parent lock and the per-team
`blocks` lock in migration 47's release, since both graphs cross projects and teams: a lock per team
taken in a fixed order cannot hold a cycle the walk finds only as it goes, and four writers in a ring
close one that none of them sees. They are the first locks a transaction takes — the parents' before
the blocks', which `LockGraph` refuses the other way round —, before the rank's row lock and any
ticket row: the transition to `blocked` takes the blocks' lock before it writes the ticket, and an
import's execution before it inserts. A transaction waiting for one holds no other but the parents'
lock, so two writers never wait on each other in a circle. The check that follows a lock is a new
statement and sees every write committed before the lock was granted, so two concurrent writes
cannot pass the check together. golang-migrate takes a single `bigint` key; the two-key space
never meets it. A transaction that takes a team's lock and persons' locks takes the team's first
and the persons' in the order of their ids; none takes a team's lock after a person's, so the two
cannot deadlock. A transaction takes the lock of its own team and of no other, so the teams'
locks need no order among themselves. The subject's lock (`cows`) stands before both: a login
through the identity provider takes it first of all, before it reads the person, and then the
person's lock and no team's; no transaction takes the subject's lock after a team's or a person's,
and a login takes the lock of its own subject and of no other, so it closes no cycle. The order is
the subject's lock, the team's, the persons' by ascending id. Two orderings are row locks, not advisory: the
`ticket_counters` row and the
team row the time lock is read from `FOR SHARE` (`TimeLockedUntil`). The counter row is the
project's number lock and its rank lock in one: a filing updates it (`NextTicketNumber`), a move
and a return from done or dropped — a reopen, a withdrawal of a done by hand, a lower stage that
reopens — lock it `FOR UPDATE` (`LockProjectRank`) before they read a key, and each of them takes
it before it writes a ticket row, so they cannot deadlock over it
([domain.md](domain.md#rank)). It is a row of its own so that filing never waits for a change
of the project's settings.

## The login and the sessions

Four groups of store code run outside `Mutate`, by design, and each is small
([`login.go`](../../backend/internal/store/login.go), [`sessions.go`](../../backend/internal/store/sessions.go)):

- **Reads that name the login as their job** (`loginRead`: `LookupLogin`, `LocalLoginAvailable`).
  The login looks an account up by the username it was given, before it
  knows a person; the transaction sets `app.job = 'login'`, which the policies of `users`,
  `local_accounts`, `tenants` — the init state asks whether any exists — and the two login tables
  admit.
- **`ReserveLoginAttempt`**, one write transaction under the system actor `system:login` and an
  advisory lock on the client address (`cowr`, `hashtext` of the address hash in hex), before the
  password is hashed: it counts the address's attempts within `AddressWindow` and, below
  `COWORK_LOGIN_ADDRESS_LIMIT`, writes this attempt into `login_attempts` (`failed` false) and
  answers its id; at the limit it writes nothing. The count and the row are one step, so parallel
  attempts of one address cannot all pass the count before any of them is written. The runtime
  role may not update `login_attempts`, so the outcome replaces the reservation: a delete and an
  insert in the next transaction.
- **`RecordLoginAttempt`**, one write transaction under the system actor `system:login` and an
  advisory lock on the username (`cowl`, `hashtext(username)`): it reads the lock, replaces the
  attempt's reservation by the row of its outcome in `login_attempts`, locks the username at the
  limit, and writes the audit rows of the
  failures and the lock through `Writer.writeEvents`. It commits **without** an act when it has
  none (a login that goes on, an attempt against a lock noted within the hour), which `Mutate`
  refuses: a counted attempt is bookkeeping, like the token's last-used day. The password has
  been verified before the call — Argon2id is never computed inside a transaction — and only its
  result goes in.
- **`LookupSession` and `TouchSession`**, as `LookupToken` and `TouchTokenLastUsed` are for
  tokens: the first finds the row of a cookie's hash through `app.session_hash` and reads its
  person; the second moves `last_seen_at` at most once per `SessionTouchInterval`, for every
  request of the session but a write the CSRF check refuses (`movesIdleClock` in
  [`api/session.go`](../../backend/internal/api/session.go)).

Everything else of the login is `Mutate`: `CreateSession` reads the account's password hash again
`FOR SHARE` (`LockLoginPassword`) and answers `ErrPasswordChanged` when it is not the hash the login
verified — a change of the password updates that row, so it waits for the share lock or is seen by
it —, ends the session the login presented,
inserts the new one and records `logged_in` as the person, whose `Caller` carries the replaced
cookie's hash; logout, the password change, the account routes and the creation of a token and a
team are handlers' `Mutate` calls like any other. The session's timestamps and the login's
windows come from the backend's clock (`Options.Now`) passed in as parameters, not from `now()`,
so a test moves one clock.

## The identity provider's transactions

What the identity provider decides runs in transactions of its own, outside `Mutate` and
`RunJob`, as the system actor `system:identity-provider`
([`identity.go`](../../backend/internal/store/identity.go); [ADR 0030] D6). `beginIdentity` opens a
read-write transaction whose settings name the job `identity-provider`, the request id, the source
hash and the session hash a login replaces or a refresh holds; `forPerson` names the person in
`app.user_id` — which admits the person's sessions and memberships — and takes the advisory lock
`cowi` of the person before anything is read that the decision depends on. A login takes the
subject's lock (`cows`, `forSubject`, on the issuer and the subject) before that, as its first
statement after the settings: the person of a first login has no id to lock until the login has
made them, so two first logins of one identity at once would both find no person and the second would fail on
`users_oidc_identity_key`; under the subject's lock the second waits, then finds the person the
first made and carries on as a returning person's login
(`TestTwoFirstLoginsOfOneIdentityMakeOnePerson`). The acts are written
through the same `Writer.flush` as `Mutate`'s, team by team: `flushIn` sets `app.tenant_id` for
the rows of one team and clears it again, because a decision about one person writes rows in
every team whose mappings it touches.

| Function | Decides | Writes |
|---|---|---|
| `CompleteOIDCLogin` | a verified login, under the subject's lock and then the person's: the gate, deactivated, the init state | a refusal (`login_refused`, and for a known, active person outside the gate their groups with the gate's stamp and the administrator flag cleared, and the end of their sessions — no memberships, which stay as they were); or the person kept or made, the memberships derived in every team (`deriveEverywhere`), the session — with its groups and sealed refresh token — and the person's own `logged_in`, as the person |
| `ClaimSessionRefresh` | whether this request refreshes the session: one short transaction **as the person**, no job, that moves `refresh_retry_at` thirty seconds ahead where the refresh is due and nobody holds it (`ClaimSessionRefresh` in `sessions.sql`), and returns the sealed refresh token | the lease only; no act. The API then asks the issuer with no transaction open |
| `ApplySessionRefresh` | the issuer's answer, under the person's lock and the session's row `FOR UPDATE`, only while `refresh_retry_at` is still the claimed lease | read: the session's groups, and the person's (`keepSnapshot`) unless their `oidc_groups_at` is newer than the read, the memberships while the gate admits them; judged — nothing was read: the session's row only (`SetSessionGroups`); outside the gate: the end of every session of the person; refused: the end of this session; unreachable: the retry time and a rotated refresh token (`DeferSessionRefresh`) |
| `EndProviderSessions` | nothing to decide: the person is not the configured issuer's | the end of every session of the person, `revoked` with the cause `gate` |
| `CheckTokenGate` | a token's person against the gate, on their stored issuer and groups | nothing when outside; when admitted, the check's stamp, the administrator flag and the memberships |

Two more pieces run inside an administrator's `Mutate`. `RederiveGroup(group, issuer)` — after a
mapping is made, changed or removed, under the team's lock the handler took first — names the job
`identity-provider` in the administrator's transaction, finds every person of the configured issuer
whose stored groups hold the group (`ListPersonsInGroup`, ordered by id), takes each one's lock in
that order, reads the person again under it and passes over one who is deactivated, of another
issuer, or has no gate stamp (`GetPersonForDerivation`), brings the others' mapped membership in the
transaction's team in line, records each change as an `Event` with
`System: system:identity-provider`, and names the job no more. `FindPerson` is a read-only
transaction of the administrator's, in their team, that names the address or username in
`app.person_lookup` and looks an address up among the configured issuer's persons only — one the
issuer marked verified, or, with `PersonLookup.EmailTrusted` (`COWORK_OIDC_EMAIL_TRUSTED`), one it
said nothing about.

The mapped memberships themselves are written by `applyMapped` only — insert, role change or
delete, by the row's id — and a grant is never among them: the policies of
[migration 22](../../backend/internal/store/migrations/000022_membership_administration.up.sql) admit
the writes of a `mapping` row to the job `identity-provider` alone, and those of a `grant` row to an
administrator of its team alone. What the identity provider may write of `users` is its own
persons — `oidc_issuer` set, no username — never a local account
([migration 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)).

## Jobs

`DB.RunJob(ctx, name, lockKey, fn)` runs a job's work in one transaction under
`(cowk, lockKey)`. `ran` is false when another replica holds the lock. The job acts as
`system:<name>`, and the transaction sets `app.job = <name>`, which the policies of the job's
tables admit. A job that records no act commits nothing and is no error; so is `ErrNoChange`.
Every run that took the lock, or failed before it could, is recorded in the metrics by the job's
name — its duration, whether it failed, and its failures in a row —; one another replica ran is
not, nor one the end of the context cut short ([metrics.md](metrics.md)).

The jobs are the idempotency expiry, lock key `1` (`ExpireIdempotencyKeys` deletes the stored
responses past their twenty-four hours and records one `expired` act on `idempotency_keys` when
it removed any), the session expiry, key `2` (`ExpireSessions`: past the absolute or the idle
limit, an `expired` act on `sessions`) and the login expiry, key `3` (`ExpireLoginState`: the
attempts older than the lockout window and the locks of the `window` mode that ended, an
`expired` act on `login_attempts`) and the notification expiry, key `5` (`ExpireNotifications`: the
notifications read more than `ReadRetention`, ninety days, ago, an `expired` act on `notifications`;
an unread one stays) and the ticket purge, key `6` (`PurgeDeletedTickets`,
[below](#deletion-and-the-purge)), which works in the teams: it writes each team's
acts there through `Writer.inTenant`, which binds the job's transaction to the team for the work
and its acts and unbinds it after; `RunJob` commits when acts were written that way, too. The
consistency check, key `8` (`CheckConsistency`, `system:consistency-check`,
[below](#the-consistency-checks-tables)), works in the teams the same way, and records one
installation-level act after them; it is not hourly: `runJobs` asks every hour whether it is due
(`LastConsistencyCheck`, `ConsistencyCheckDue` — no result yet, or the last run before the latest
03:00 UTC) and runs it then, and never without object storage. Each job holds a key of its own; a
unit test reads every `RunJob` call of the backend and refuses a key two jobs share. Key `7` was
the expiry of GitHub's deliveries, removed with the webhook; no job takes it. The import expiry, key `9` (`ExpireImportJobs`, [`store/imports.go`](../../backend/internal/store/imports.go)),
deletes the dry runs past their twenty-four hours in every team with the files they hold and
records one `expired` act on `import_jobs` when it removed any; an executed job stays. Key `10` is
`DB.EndTeamRelations` (`system:team-deletion`), the end of every relation between a team and the
others ([crossings](#crossings-between-teams)), which no schedule and no route runs while the
deletion of a team is not built.
`runJobs` in [`main.go`](../../backend/cmd/cowork/main.go)
runs them at start and then every hour, on every replica; each lock lets one of them work. Its log
lines name a job as the metrics do, by its system actor's name: `idempotency-expiry`,
`session-expiry`, `login-expiry`, `notification-expiry`, `import-expiry`, `ticket-purge`,
`consistency-check`. The
bootstrap of [`internal/bootstrap`](../../backend/internal/bootstrap/bootstrap.go) is a `RunJob`
too — key `4`, `system:bootstrap` — run once at start, and retried until the lock is free
(`bootstrap.Sync`).

## Deletion and the purge

A ticket is deleted into its team's bin and purged from it ([ADR 0024] D1–D3, D7;
[migration 32](../../backend/internal/store/migrations/000032_ticket_deletion.up.sql),
[`store/deletion.go`](../../backend/internal/store/deletion.go),
[`queries/*/deletion.sql`](../../backend/internal/store/queries/write/deletion.sql)):

- **Deleting** sets `deleted_at` and `deleted_by` and raises the version (`MarkTicketDeleted`, a
  compare-and-set on `deleted_at IS NULL`); **restoring** clears both and raises it again
  (`RestoreTicket`). Nothing else changes: the ticket keeps its links, its rank key and what hangs
  off it, which the deletion filter hides with it. Both refresh the parent's derived stages.
- **The bin** is the one view that inverts the filter: `ListDeletedTickets` and `GetDeletedTicket`
  read the deleted tickets under the visibility predicate.
- **Purging** is `Writer.PurgeTicket`, the same for an administrator's request — in a browser
  session, which the document requires of `purgeTicket` (ADR 0024 D7 as amended 2026-10-05) — and
  the job. It names
  `ticket-purge` in `app.job` for its part of the transaction — the job's transaction is named so
  already — and then, on a ticket it reads `FOR UPDATE` (`GetPurgedTicket`, `deleted_at IS NOT
  NULL`): empties its audit rows through `purge_ticket_audit`; deletes its notifications and those
  whose act is on it, its attachments' rows, its comments' revisions and comments, its questions,
  its time entries' revisions and entries, its stakes, its links and the pull requests GitHub's webhook of a release up to 0.12.0 linked to it; makes its children roots
  (`DetachChildren`, no version: their parent was hidden since the deletion); turns a block that
  waits on it into an external reference to its key (`ReleaseBlocksOn`, an `updated` act on each
  such ticket with its version raised); ends its relations into other teams through the crossing
  `end_relations_elsewhere` — a child there becomes a root, its version unchanged, with an `updated`
  act whose reason is "the parent was purged" and which names no ticket; a link to or from it goes,
  with an `unlinked` act on the other end, read from its side —, each act in the record of the team
  it changes (`Writer.RecordElsewhere`); and deletes the ticket. Its act `purged` counts what went —
  never what it said — and carries `Published`, since the row is gone when the act is written. It
  returns the attachment ids, whose objects the caller removes **after the commit**
  (`api.RemovePurgedObjects`): a rollback would otherwise leave rows that name missing bytes, and a
  failure afterwards leaves an object no row names, which is logged with its key.
- **The policies.** Every delete of a ticket or of what belongs only to it must pass a restrictive
  policy that names the job `ticket-purge` and a deleted ticket (`app_ticket_deleted`), so a delete
  outside the purge — a forgotten `WHERE` included — removes nothing, and the purge removes nothing
  of a live ticket; `ticket_links` and `ticket_interest` keep the deletes their own routes make. The
  notifications' restrictive read and delete policies admit the purge for a deleted ticket's
  notifications. The job finds the due tickets of every team through `tickets_purge_due`, a
  permissive read of the deleted tickets in a transaction named `ticket-purge` **with no
  `app.tenant_id` set** — a request always has one, so its purge never widens what it reads.
- **The audit rows** keep the key, the actor and the act; their `before`, `after`, `reason` and
  `note` are emptied ([ADR 0026] D3) by `purge_ticket_audit(ticket)`, a `SECURITY DEFINER` function
  owned by the owner role — the runtime role may not update an audit row — executable by the runtime
  role alone, with its `search_path` fixed to the schema and `pg_temp` last, emptying `app.crossing`
  first since migration 47 (`TestEverySecurityDefinerFunctionIsFencedIn`). It refuses outside the purge and for a ticket that
  is not deleted, and the policy `audit_purge` admits its update to the owner role in the purge of
  the current team only. The act of the purge is written by the wrapper in the same transaction,
  so both commit or neither does.
- **The job** (`PurgeDeletedTickets`, key `6`, `system:ticket-purge`) purges up to 200 tickets
  deleted longer than `PurgeAfter`, thirty days, ago, team by team; `runJobs` removes their
  objects and logs each key.

## The consistency check's tables

[Migration 42](../../backend/internal/store/migrations/000042_attachment_consistency.up.sql) keeps
the check of [ADR 0059] D4 ([storage.md](storage.md#the-consistency-check)) in two team-bound
tables under the canonical policy, with restrictive ones beside it:

| Table | Holds | Read by | Written by |
|---|---|---|---|
| `consistency_checks` | one row per team, its latest result: the id the confirmations name, the counts, the two lists as JSON, an administrator's confirmed removal | the team's administrators and the job; across the teams, in a transaction named `consistency-check` with no team set (`consistency_checks_counts`, the counts a scrape reads) | the job inserts and replaces the row (`SaveConsistencyCheck`, an upsert under a new id); an administrator updates the counts and the lists (`RecordOrphanRemoval`, `RecordDanglingAcceptance`) |
| `consistency_acceptances` | an administrator's acceptance that an attachment's bytes are lost; a foreign key to `attachments` with `ON DELETE CASCADE`, so the purge's delete of the row takes it along | the team's administrators and the job | an administrator inserts, as themselves (`accepted_by = app_user_id()`); the job deletes those of whole attachments |

The job reads every team with no `app.tenant_id` set: migration 42 adds `consistency-check` to the jobs
`tenants_read` admits. A member's transaction reads neither table, whatever its query says. The
grants: `SELECT, INSERT` and `UPDATE` of the result's columns on `consistency_checks`, no `DELETE`;
`SELECT, INSERT, DELETE` on `consistency_acceptances`.

Beside the results a scrape reads every team's last export from the audit record
(`ListLastExports`, [metrics.md](metrics.md#the-consistency-family)):
[migration 46](../../backend/internal/store/migrations/000046_last_export_read_at_a_scrape.up.sql)
adds the permissive policy `audit_exports_read`, which admits a transaction named
`consistency-check` with no `app.tenant_id` set to the rows of `audit_events` whose action is `exported` on
the entity `project` or `tenant`, and to no other row, and the partial index
`audit_exports_by_tenant` on `(tenant_id, created_at)` over the same rows, so the latest export of a
team is one step of an index the size of its exports. The team's administrators read their
team's latest one beside the check (`LastTenantExport`) in the team's own transaction, under
`audit_read`.

## Notifications

The inbox of [ADR 0020] is rows, written by the act that causes them in its transaction and
referencing its audit row (D3): `notifications` (migration 30) holds the team, the person, the
ticket the notification is about, the audit row and the reason, and `read_at`. The handler that
records an act names whom it tells in `Event.Notices`; `Writer.deliver`
([`store/inbox.go`](../../backend/internal/store/inbox.go)), called by `writeEvents` after the audit
row, writes them:

| Act | Notice | Recipients before the filter |
|---|---|---|
| a filing with an assignee; `assigned` | `assigned` | the assignee |
| `asked`; an edit that asks the question of another person | `asked` | the person asked |
| `answered` | `answered` | the asker |
| `transitioned` — a transition, the done act and the reopen of the stages | `state_changed` | the watchers of the ticket |
| `transitioned` to `done` or `dropped` | `blocker_closed`, about each ticket it blocks | the watchers of that ticket |
| `commented`, the explaining comment of a write included | `commented` | the watchers of the ticket |
| `commented` with `mentions`; `edited` that adds a person to them | `mentioned`, before `commented` | the persons it mentions, or the persons the edit adds |
| `interest` that makes a stake `urgent` | `urgent` | the assignee |

The watchers (`ListWatchers`, [ADR 0013] D6) are everyone with a stake, the assignee, the reporter,
whoever asked or was asked an open question on the ticket, and whoever a comment on it that is not
withdrawn mentions (`comments.mentions`, migration 36). One act tells a person once about a ticket:
`deliver` keeps whom it told per ticket and leaves them out of the act's later notices, so a watcher
a comment mentions is told `mentioned`, not also `commented`. `NoticeRecipients` keeps of the
persons named those who are not deactivated, are not the actor — whose own act, and whose agent's,
tells them nothing — and see, by `person_sees_ticket`, both the ticket the notification is about and
the ticket the act is on (for `blocker_closed` the blocker). `InsertNotifications` writes one row per
recipient, and `deliver` publishes an `inbox` notification per person whose inbox changed
([events.md](events.md#publication)). A withdrawal of a question tells nobody: only its asker may
withdraw it.

The inbox is read per team through `ListInbox`, `CountUnread` and `FindNotification`
([`queries/read/inbox.sql`](../../backend/internal/store/queries/read/inbox.sql)), each with the
predicate on both tickets, so a notification of a ticket the person no longer sees counts nowhere,
and each leaves out a notification of the reason `merged`, which GitHub's webhook of a release up to
0.12.0 made and no answer names any more ([ADR 0071] Status); it is marked read by `MarkNotificationRead` and `MarkInboxRead`, under the same predicate, as the
person's act `read`.

## Publication

`Writer.publish` ([`notify.go`](../../backend/internal/store/notify.go)) runs for every act
written — by `Mutate`, by a job in a team and by the identity provider's transactions alike — that belongs to a team
and carries an `Event.Membership`, carries an `Event.NewProject` or an `Event.ProjectRank`, or names
a ticket, except the actions `downloaded` and `exported`, the entity `time_entry`, and an act marked
`Event.Quiet`, which `writeEvents` in [`tx.go`](../../backend/internal/store/tx.go) skips. The sort of
a project's rank and an import's execution send the project and its key, as `project.changed`. A ticket's act reads the ticket's
project, version and confidential facts (`TicketFacts`, which reads a deleted ticket too), or takes
them from `Event.Published` for a purged one; a membership act sends the keys of its
`MembershipChange` and its audience. Either way it calls `pg_notify('cowork_events', <json>)` in the same transaction;
PostgreSQL delivers it at commit and never after a rollback ([ADR 0054] D4). `DB.Listen` holds
one connection outside the pool on the channel. An act `RecordElsewhere` writes in another team is
published like any act, on that team's streams. One notification is no act's: `refresh_derived`
calls `pg_notify` itself for a parent of another team whose derived stages it changed —
`ticket.changed` of the kind `derived`, with an id `uuidv7()` makes for it, and the parent's
project, version, confidential flag, assignee and reporter for the streams' filter — since the
change records no act ([crossings](#crossings-between-teams)). The rest is [events.md](events.md).

[ADR 0013]: ../adr/0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md
[ADR 0015]: ../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md
[ADR 0020]: ../adr/0020-notifications-are-an-in-app-inbox-per-person.md
[ADR 0031]: ../adr/0031-server-side-sessions-in-an-httponly-cookie.md
[ADR 0021]: ../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md
[ADR 0024]: ../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md
[ADR 0025]: ../adr/0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md
[ADR 0026]: ../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md
[ADR 0027]: ../adr/0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md
[ADR 0028]: ../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md
[ADR 0030]: ../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md
[ADR 0034]: ../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md
[ADR 0035]: ../adr/0035-personal-access-tokens.md
[ADR 0036]: ../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md
[ADR 0045]: ../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md
[ADR 0049]: ../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md
[ADR 0054]: ../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md
[ADR 0065]: ../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md
[ADR 0071]: ../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md
