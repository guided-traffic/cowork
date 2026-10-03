# Data access

How the backend reaches PostgreSQL: two roles, the transaction wrappers, the settings the
policies read, the visibility predicates and the lint that holds every query to them, the one
place SQL is built at run time, the advisory locks, the background jobs and the publication of
acts. The package is [`backend/internal/store/`](../../backend/internal/store/); the decisions
are [ADR 0027] (the wrappers), [ADR 0021] (row-level security, the roles), [ADR 0026] (the
audit record), [ADR 0034] D4 with [ADR 0065] D4 (the visibility predicate) and [ADR 0031] (the
sessions). Read against the tree on 2026-10-03.

## Two database roles

| Role | Connects through | Owns | Does |
|---|---|---|---|
| Owner | `COWORK_DATABASE_OWNER_URL` | every object of the schema | runs the migrations: `cowork migrate`, and `cowork serve` while `COWORK_MIGRATE_ON_START=true`; in the chart only the `migrate` init container holds it |
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
as one transaction. A migration that rewrites rows runs as the owner with no tenant set, and
the forced policy hides every row from it: `000017_ticket_rank` and `000019_progress_stages`
lift the force on `tickets` for their backfills and restore it later in the file
([ADR 0021] D1). A new enum value cannot be used in the transaction that adds it, so
`000018_ticket_state_review` adds `review` alone and `000019` uses it.
`TestLiftedForceIsRestoredInTheSameMigration` holds every lifted force to a restore in the same
file; the integration tier reads the force back after the run.

The grants are per table and per column: `SELECT`, `INSERT` where rows are created, `UPDATE`
on the columns a route may change — table-wide only on `ticket_counters`, `idempotency_keys`
and `login_locks` — and `DELETE` only on `ticket_links`, `ticket_interest`,
`idempotency_keys`, `sessions`, `login_attempts` and `login_locks`. `audit_events` gets
`SELECT, INSERT` and nothing else — append-only is a grant ([ADR 0026] D3). `users`,
`tenants`, `memberships` and `tokens` are inserted by routes now — a person by an account's
creation or the bootstrap, a tenant by its creation, a grant by both, a token by its person —
and each insert has a policy that names who may (migration 15), with the columns a grant lists
(`global_admin` is the bootstrap's alone: the policy refuses it to a request); the application
makes the ids of the persons, tenants and grants it inserts (`uuid.NewV7`), because an
`INSERT … RETURNING` would have to pass the read policy of a row its writer has no membership of
yet. `project_access` has no write grant: no route writes it yet; the tests and `make dev-seed`
write persons, tenants, grants and tokens over the administrative connection too
([testing.md](testing.md#fixtures-of-the-integration-tier)).

## The wrappers

The pool is unexported ([`store.go`](../../backend/internal/store/store.go)); a query runs
inside one of these, and nothing else hands out a connection.

| Wrapper | Transaction | Bound to | Hands `fn` |
|---|---|---|---|
| `DB.InTenant(ctx, tenantID, fn)` | read-only | the tenant and the caller's person | `*Reader` |
| `DB.Installation(ctx, fn)` | read-only | no tenant: only the person-scoped policies admit rows | `*Reader` |
| `DB.Mutate(ctx, tenantID, fn)` | read-write; `uuid.Nil` for an installation-level act | the tenant and the caller | `*Writer` |
| `DB.RunJob(ctx, name, lockKey, fn)` | read-write, under the job's lock | no tenant, a system actor | `*Writer` |

A `Reader` ([`tx.go`](../../backend/internal/store/tx.go)) embeds the generated read queries
(`readq`), carries `TenantID` and `UserID`, and adds `ListTickets`. A `Writer` embeds a `Reader`
and the generated write queries (`writeq`), and adds `Record`, `Respond` and the lock methods.
`sqlc` generates the two packages from `queries/read/` and `queries/write/` with the migrations
as the schema ([`sqlc.yaml`](../../backend/sqlc.yaml)); a handler that only reads never holds a
write query.

Who a transaction acts for is a `store.Caller` in the context
([`caller.go`](../../backend/internal/store/caller.go)), put there by the API pipeline after
authentication: the person or a `system:<name>` actor, the token, the token's project
restriction, the agent mark, the agent's capabilities and the request id. The person is never a
call-site argument. `Mutate` refuses a context with neither or both of person and system actor.

Outside the wrappers, deliberately: `LookupToken` and `LookupSession` (read one token or session
by the hash the request presents, see below), `TouchTokenLastUsed` and `TouchSession` (the
last-used date and the idle clock, bookkeeping and not acts, [ADR 0035] D2,
[ADR 0031] D3), the login's own transactions ([below](#the-login-and-the-sessions)),
`CheckRuntimeRole`, `SchemaState`, `Ping`, and `Listen`.

`Open` registers `timestamptz` to scan in UTC and a tracer that logs a query slower than
`DefaultSlowQuery` (500 ms) by its sqlc name, never its arguments. A missing or invisible row
is sqlc's `pgx.ErrNoRows`, passed through the wrappers: the handler maps it to its own `404`
(or, for a conditional write, to the answer of the later request — see
[conventions.md](conventions.md)), and one it does not map is a `500`. `store.ErrNotFound`
comes only from `LookupToken`.

## The settings the policies read

`setContext` writes transaction-local settings (`set_config(…, true)`) at the start of every
wrapper's transaction; an empty value leaves a setting unset.

| Setting | Written from | Read by |
|---|---|---|
| `app.tenant_id` | the wrapper's tenant | `app_tenant_id()`: every `tenant_isolation` policy, the policies of `tenants`, `memberships`, `users`, `audit_events`, the visibility functions |
| `app.user_id` | `Caller.UserID` | `app_user_id()`: the person's own user row, memberships, tenants, tokens, idempotency keys and installation-level audit rows; the visibility functions |
| `app.restricted_project_id` | `Caller.RestrictedProjectID` | `app_restricted_project_id()` in `app_project_visible` |
| `app.job` | `RunJob`'s name; `login` for the login's own transactions | the `idempotency_keys` policy admits every row in a transaction named `idempotency-expiry`; the policies of migration 15 and 16 name `login`, `bootstrap`, `session-expiry` and `login-expiry` for the rows those system actors keep (`app_job()`) |
| `app.token_hash` | `LookupToken`, the hex SHA-256 of the presented token | the `tokens` policy admits exactly that row |
| `app.session_hash` | `LookupSession`, and `Caller.SessionHash` in every transaction of a session's request: the hex SHA-256 of the presented cookie | `app_session_hash()`: the `sessions` policies admit exactly that row — to read it, to end it |

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
those rows are read across tenants by their person or have no tenant ([ADR 0021] D6). The
policies of the last four, and the new write policies on the others, use five more functions
(migration 15): `app_job()` and `app_session_hash()`, which read the settings,
`app_is_global_admin()`, and `app_manages_account(user)` and `app_manages_username(name)` — the
current person is an administrator of the current tenant **and the account is one that tenant
manages** (`local_accounts.managing_tenant_id`). That last rule is where an account, which
belongs to the whole installation, meets a tenant: see
[docs/security/local-accounts.md](../security/local-accounts.md). `TestEveryTableHasItsPolicyAndGrant` checks all of it on the migration
files, without a database; application queries still filter by `tenant_id` as well
([ADR 0021] D4).

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
   an agent's, token, request id — and the idempotency key: the keyed request's own, or an
   `Event.IdempotencyKey` a client sent where no response is stored. The row's id is made in
   Go (`uuid.NewV7`): an `INSERT … RETURNING` would have to pass the read policy, which a system
   actor's installation-level row does not. Then it publishes the act (below).
5. A keyed mutation stores the `Result` the function set with `Respond`
   (`StoreIdempotencyKey`). That insert takes over an expired row and returns nothing when a
   concurrent request holds the same unexpired key: the attempt rolls back and the next round
   replays the winner's response; losing twice is an error.
6. It commits. The returned `*Result` is non-nil only for a replay.

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

## Visibility in SQL

Three functions carry the restriction and the confidential flag into every query:

| Function | Migration | True when |
|---|---|---|
| `app_project_visible(project)` | `000007_visibility` | the token is not restricted to another project; and the project is unrestricted, or the person is a tenant administrator or on its `project_access` list |
| `app_ticket_visible(project, confidential, assignee, reporter)` | `000008_tickets` | the project is visible; and the ticket is not confidential, or the person is a tenant administrator, its assignee or its reporter |
| `app_time_visible(person)` | `000013_time_entries` | the entry is the person's own, or the person is a tenant administrator, or a member while the tenant shows time to members |

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
| `ParentChainContains`, `BlocksPathExists` | integrity walks that answer yes or no |
| `GetUrgencyInputs`, `ListBlockedTickets` | the urgency derivation belongs to the ticket, not to the reader ([domain.md](domain.md#urgency)) |
| `CanSeeProject`, `CanSeeTicket` | whether *another* person sees a project or a ticket: the assignee, the person asked |
| `ProjectKeyTaken` | a key's existence, unique in the tenant whether or not the caller sees its project |
| `LastRank`, `ListUnrankedTickets`, `GetTicketRank`, `NextRankedTicket`, `PreviousRankedTicket` | the rank keys of the project a write hands a key out in: a new key lies between keys that exist, hidden tickets' included, so none is handed out twice ([domain.md](domain.md#rank)) |

The SQL functions `ticket_ancestor_or_self`, `blocks_path_exists`, `ticket_derived_progress`
(the implementation stage, kept for the release before the stages) and `ticket_derived_stage`
read the tenant's tickets past the predicate for the same reasons; row-level security still
holds them to the tenant. The ticket's columns count `open_prerequisites` in a subquery with
the predicate on every prerequisite, `GetWrittenTicket` included: a hidden one is never counted.

## The ticket list builder

`Reader.ListTickets(ctx, TicketFilter, TicketPage)` in
[`tickets.go`](../../backend/internal/store/tickets.go) is the one place SQL is built at run
time ([ADR 0027] D4). It starts from `t.tenant_id = $1 AND app_ticket_visible(…)`, adds one
condition per filter, and passes every filter value as a positional argument — no filter value
enters the SQL text; only the integer `LIMIT` and `OFFSET` are formatted in. The lint cannot see Go, so the builder adds the predicate itself, and
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
| `TicketOrder` | `ByRank` for a project's list — `ORDER BY rankedKey NULLS LAST, t.number`, `rankedKey` the key of an open ticket and none for a done or dropped one, whatever its column holds: the ranked by their key, then the unranked by number — and `NewestFirst` (id descending) for the tenant's; `Position` writes a row's cursor position, the id or `<key>.<number>` with an empty key for an unranked ticket, which the API seals ([api.md](api.md#paging)) |
| `TicketPage` | after a cursor position with `LIMIT` one above the page, or a numbered page with `LIMIT`/`OFFSET` and a `count(*)` total |

## Advisory locks

All locks are transaction-level: released by commit or rollback. A session-level lock would
outlive its work on an idle pooled connection ([ADR 0027] D5).

| First key | Name | Taken by | Orders |
|---|---|---|---|
| `0x636f776b` | `cowk` | `RunJob`, `pg_try_advisory_xact_lock(ns, lockKey)` | one replica per job |
| `0x636f7770` | `cowp` | `Writer.LockParents(projectID)` | re-parentings in a project, before the parent cycle walk |
| `0x636f7762` | `cowb` | `Writer.LockBlocks()` | new `blocks` links in the tenant, before the cycle walk |
| `0x636f7771` | `cowq` | `Writer.LockQuestions(ticketID)` | question numbers of a ticket |
| `0x636f7761` | `cowa` | `Writer.LockAttachments(ticketID)` | uploads to a ticket, before the per-ticket count |

The writer locks are `pg_advisory_xact_lock(ns, hashtext(id::text))`
([`jobs.go`](../../backend/internal/store/jobs.go)). The check that follows a lock is a new
statement and sees every write committed before the lock was granted, so two concurrent writes
cannot pass the check together. golang-migrate takes a single `bigint` key; the two-key space
never meets it. Two orderings are row locks, not advisory: the `ticket_counters` row and the
tenant row the time lock is read from `FOR SHARE` (`TimeLockedUntil`). The counter row is the
project's number lock and its rank lock in one: a filing updates it (`NextTicketNumber`), a move
and a return from done or dropped — a reopen, a withdrawal of a done by hand, a lower stage that
reopens — lock it `FOR UPDATE` (`LockProjectRank`) before they read a key, and each of them takes
it before it writes a ticket row, so they cannot deadlock over it
([domain.md](domain.md#rank)). It is a row of its own so that filing never waits for a change
of the project's settings.

## The login and the sessions

Three groups of store code run outside `Mutate`, by design, and each is small
([`login.go`](../../backend/internal/store/login.go), [`sessions.go`](../../backend/internal/store/sessions.go)):

- **Reads that name the login as their job** (`loginRead`: `LookupLogin`, `LocalLoginAvailable`,
  `AddressAttempts`). The login looks an account up by the username it was given, before it
  knows a person; the transaction sets `app.job = 'login'`, which the policies of `users`,
  `local_accounts`, `tenants` — the init state asks whether any exists — and the two login tables
  admit.
- **`RecordLoginAttempt`**, one write transaction under the system actor `system:login` and an
  advisory lock on the username (`cowl`, `hashtext(username)`): it reads the lock, counts the
  attempt in `login_attempts`, locks the username at the limit, and writes the audit rows of the
  failures and the lock through `Writer.writeEvents`. It commits **without** an act when it has
  none (a login that goes on, an attempt against a lock noted within the hour), which `Mutate`
  refuses: a counted attempt is bookkeeping, like the token's last-used day. The password has
  been verified before the call — Argon2id is never computed inside a transaction — and only its
  result goes in.
- **`LookupSession` and `TouchSession`**, as `LookupToken` and `TouchTokenLastUsed` are for
  tokens: the first finds the row of a cookie's hash through `app.session_hash` and reads its
  person; the second moves `last_seen_at` at most once per `SessionTouchInterval`.

Everything else of the login is `Mutate`: `CreateSession` ends the session the login presented,
inserts the new one and records `logged_in` as the person, whose `Caller` carries the replaced
cookie's hash; logout, the password change, the account routes and the creation of a token and a
tenant are handlers' `Mutate` calls like any other. The session's timestamps and the login's
windows come from the backend's clock (`Options.Now`) passed in as parameters, not from `now()`,
so a test moves one clock.

## Jobs

`DB.RunJob(ctx, name, lockKey, fn)` runs a job's work in one transaction under
`(cowk, lockKey)`. `ran` is false when another replica holds the lock. The job acts as
`system:<name>`, and the transaction sets `app.job = <name>`, which the policies of the job's
tables admit. A job that records no act commits nothing and is no error; so is `ErrNoChange`.

The jobs are the idempotency expiry, lock key `1` (`ExpireIdempotencyKeys` deletes the stored
responses past their twenty-four hours and records one `expired` act on `idempotency_keys` when
it removed any), the session expiry, key `2` (`ExpireSessions`: past the absolute or the idle
limit, an `expired` act on `sessions`) and the login expiry, key `3` (`ExpireLoginState`: the
attempts older than the lockout window and the locks of the `window` mode that ended, an
`expired` act on `login_attempts`). `runJobs` in [`main.go`](../../backend/cmd/cowork/main.go)
runs them at start and then every hour, on every replica; each lock lets one of them work. The
bootstrap of [`internal/bootstrap`](../../backend/internal/bootstrap/bootstrap.go) is a `RunJob`
too — key `4`, `system:bootstrap` — run once at start, and retried until the lock is free
(`bootstrap.Sync`).

## Publication

`Writer.publish` ([`notify.go`](../../backend/internal/store/notify.go)) runs for every act
`Mutate` writes that belongs to a tenant and a ticket, except the actions `downloaded` and
`exported` and the entity `time_entry`. It reads the ticket's project, version and confidential
facts (`TicketFacts`) and calls `pg_notify('cowork_events', <json>)` in the same transaction;
PostgreSQL delivers it at commit and never after a rollback ([ADR 0054] D4). `DB.Listen` holds
one connection outside the pool on the channel. The rest is [events.md](events.md).

[ADR 0015]: ../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md
[ADR 0031]: ../adr/0031-server-side-sessions-in-an-httponly-cookie.md
[ADR 0021]: ../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md
[ADR 0024]: ../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md
[ADR 0025]: ../adr/0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md
[ADR 0026]: ../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md
[ADR 0027]: ../adr/0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md
[ADR 0034]: ../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md
[ADR 0035]: ../adr/0035-personal-access-tokens.md
[ADR 0045]: ../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md
[ADR 0049]: ../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md
[ADR 0054]: ../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md
[ADR 0065]: ../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md
