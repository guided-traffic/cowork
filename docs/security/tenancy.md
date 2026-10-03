# Tenant isolation and visibility inside a tenant

How one tenant's data stays out of another tenant's reach, and who inside a tenant sees which
project, ticket, act, event and time entry, as built on 2026-10-03. What a token or an agent
may do with what it can see is [tokens.md](tokens.md); how a request reaches the backend at
all, and where the database credentials live, is [trust-boundaries.md](trust-boundaries.md);
what becomes of an upload's bytes is [attachments.md](attachments.md).

## Two lines

The tenant is the isolation unit
([ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)). Two
independent mechanisms keep it
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md)):

1. **The request layer.** The tenant is named in the path and admitted before any handler
   runs; every query runs in a transaction bound to that tenant and names `tenant_id` itself
   (ADR 0021 D4); inside the tenant, the visibility predicates decide which projects and
   tickets the caller sees.
2. **The database.** Row-level security, forced on every table, admits only the
   transaction's tenant's rows to a runtime role that cannot bypass it. A query that forgets
   its tenant filter returns no rows of another tenant, not foreign rows.

The second line makes a forgotten tenant filter harmless. Inside a tenant there is only the
first: a query that forgot the visibility predicate would show a restricted project or a
confidential ticket to everyone in the tenant, which is why a unit test holds every query to
the predicate ("Visibility inside a tenant"). Neither line stops a process that runs SQL of an
attacker's choosing — see "A compromised serving process" at the end.

## The tenant is in the path, and a refusal looks like absence

Every tenant-bound route lives under `/api/v1/tenants/{tenant}/…`, plus the key resolver
`/api/v1/tickets/{tenant}/{key}` ([ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D1,
D3). After authentication and before validation and the handler, `boundary`
([`backend/internal/api/tenant.go`](../../backend/internal/api/tenant.go)) reads the tenant
by its slug together with the caller's memberships, in a transaction whose policies admit
only the caller's own memberships and tenants. It answers the same `404 not_found` — "no such
tenant" — when the slug is unknown, when the person is not a member, when the token is
restricted to another tenant, and when a project-restricted token calls a tenant route
outside its project (ADR 0023 D5,
[ADR 0047](../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D5).
`TestATokenOfOneTenantCannotSeeAnother` compares the refusal's type, title, status, detail
and code with the answer for an unknown slug.

Inside the tenant the same rule holds one level down: a project or a ticket the caller cannot
see answers the `404` of one that does not exist (`visibleProject`, `visibleTicket` in
[`projects.go`](../../backend/internal/api/projects.go) and
[`tickets.go`](../../backend/internal/api/tickets.go)), and so does every comment, question,
attachment, link, stake and time entry of such a ticket. A role that forbids an act on
something the caller can see answers `403`. A request without a valid token is answered
`401` before any tenant is looked up.

The person's own routes under `/api/v1/me` are not tenant routes; they show what is the
person's across tenants — their memberships and their tokens — and no tenant's tickets. A
restricted token sees only its tenant's membership and itself there ([tokens.md](tokens.md)
"Restrictions").

## Two database roles

| Role | Owns | Holds | Used by |
|---|---|---|---|
| owner | every object of the schema | DDL | `cowork migrate`, the chart's migration init container, and `cowork serve` while `COWORK_MIGRATE_ON_START` is true |
| runtime | nothing | only what the migrations grant it; the migration run names it in the session setting `cowork.runtime_role` | `cowork serve` |

The runtime role's grants are narrow: `UPDATE` only where the API changes something — column
by column on the tenants, projects, tokens, tickets, questions, comments, stakes, time
entries, persons, local accounts and sessions, table-wide on `ticket_counters`, `idempotency_keys` and `login_locks` — `DELETE` only on `ticket_links`,
`ticket_interest`, `idempotency_keys`, `sessions`, `login_attempts` and `login_locks`, and only
`INSERT` and `SELECT` on `audit_events`, which makes the audit record append-only by grant
([ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D3).
`TestTheAuditRecordIsAppendOnly` shows that `UPDATE`, `DELETE`, `TRUNCATE` and switching
row-level security off are refused, and that a grant to itself grants nothing. The role
inserts a tenant, a person, a membership, a token, a session or a local account only where a
policy of migrations
[15](../../backend/internal/store/migrations/000015_local_accounts.up.sql) and
[16](../../backend/internal/store/migrations/000016_sessions.up.sql) admits it — an
administrator of the current tenant, a global administrator creating a tenant, the person for
their own token and session, or a named system actor — and updates only the columns those
grants list (`TestPoliciesOfThePersonsAndTheirAccounts`, `TestPoliciesOfTheSessions`).

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

except the tables ADR 0021 D6 names one by one, whose rows a person reads across tenants or
which have no tenant at all:

| Table | Its policy admits |
|---|---|
| `tenants` | the row inside its own tenant's transaction, and to its members; updates only inside its own transaction; read by the login and the start-up synchronisation named in `app.job`, so the login can ask whether any tenant exists; inserted by a global administrator or the synchronisation |
| `users` | the person, everyone who shares the current tenant with them, and the login and the synchronisation; inserted by an administrator of the current tenant (never a global administrator) or the synchronisation, updated by the administrators of the accounts their tenant manages and by the synchronisation |
| `memberships` | the tenant's rows inside the tenant, and the person's own rows everywhere; a marked grant inserted by an administrator into their own tenant, by a global administrator for themselves as `admin`, or by the synchronisation |
| `tokens` | the person's own rows, and during the lookup the one row whose hash the transaction names in `app.token_hash`; the administrators of a managed account and the synchronisation read and revoke its tokens; inserted for the person's own account only |
| `idempotency_keys` | the person's own rows, and every row to the expiry job named in `app.job` |
| `audit_events` | a tenant's rows inside that tenant, an installation-level row to the person it names; a row is inserted only into the context it belongs to |
| `local_accounts` | the person's own row, the managing tenant's administrators, the login and the synchronisation; inserted for `tenant` by an administrator of that tenant and for `config` by the synchronisation, updated by the person only while a `tenant` account |
| `sessions` | the person's own rows, the one row whose hash the transaction names in `app.session_hash`, the administrators of a managed account, a global administrator for reading, and the two jobs that end sessions; inserted for the person's own only |
| `login_attempts`, `login_locks` | the login, its expiry job and the synchronisation; the administrators of a managed account read and clear the rows of its username |

At the start of every transaction the store sets `app.tenant_id`, `app.user_id`,
`app.restricted_project_id`, `app.job` and `app.session_hash` — the hash of the session cookie
a request presented, which is how a request finds its own session row — with
`set_config(…, true)`, which dies with the
transaction ([`store/tx.go`](../../backend/internal/store/tx.go) `setContext`). The person
is the authenticated caller, carried in the context and never a call site's argument
([`store/caller.go`](../../backend/internal/store/caller.go)); the tenant is the one the
boundary admitted, which every handler passes on. The policies read the settings through
`app_tenant_id()`, `app_user_id()` and
`app_restricted_project_id()`, which turn an unset or empty setting into `NULL`: a pooled
connection keeps an empty string after its transaction, a bare cast of it would raise, and
`NULL` matches no row. A unit test holds every policy to the guarded form
(`TestPoliciesReadSettingsGuarded`), and `TestTheContextDiesWithItsTransaction` shows that a
connection that just served a tenant sees nothing once its transaction ended.

Every query on cowork's data runs inside one of the store's wrappers — `InTenant` and
`Installation`, which are read-only transactions; `Mutate`, which commits a write only
together with an audit row per act; `RunJob`, a background job under a system actor — or in
the token and session lookups, the last-used write and the session's idle clock, the login's
reads and the transaction that counts and decides a login attempt, which set their own
context. The connection pool is
unexported; outside the wrappers the store reads only the schema version and the role catalog
for its start-up checks, and holds the listener connection of the event stream
([`store/store.go`](../../backend/internal/store/store.go)).

`TestUnfilteredQueryUnderTenantSeesNothingOfAnother` is the proof across tenants: it walks the
catalog for every table with a `tenant_id` column, seeds each with a row of a second tenant,
and asserts that an unfiltered query as the runtime role under the first tenant sees none of
them — a new table is covered the day it is created.

One migration lifts the force for itself. The rank's backfill
([migration 17](../../backend/internal/store/migrations/000017_ticket_rank.up.sql)) rewrites
`tickets` as the owner with no tenant set, which the forced policy would hide every row from,
so it runs `NO FORCE` before the backfill and `FORCE` after it
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D1). The
file runs as one transaction, which holds `tickets` exclusively from its first `ALTER` on: no
other transaction sees the table unforced, and a failed run rolls the lift back with the rest.
The runtime role is held by the policy either way — the force concerns the owner alone.
`TestLiftedForceIsRestoredInTheSameMigration` holds every lift to a restore in the same file,
which `TestEveryTableHasItsPolicyAndGrant` alone would not notice, and
`TestRankMigrationKeepsNumberOrder` reads the force back after the run.

## A row never points into another tenant

A plain foreign key ignores row-level security, so a row of one tenant could name a parent of
another. Every reference from one tenant-bound row to another is therefore a composite key
that includes `tenant_id`: a project's access list and ticket counter to the project; a
ticket to its project, to its parent — of the same project as well
([ADR 0008](../adr/0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D2)
— and to the ticket a block waits on; both ends of a link; questions, comments, stakes, time
entries and attachments to their ticket; a comment's revisions to the comment, a time
entry's revisions to the entry, and a comment's attachment to a comment of the same ticket; a
token's project restriction to a project of its tenant restriction (migrations 3, 4, 8–14).
A link therefore never crosses a tenant, whatever the API does
([ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D2).

References to persons are plain foreign keys, because a person is not tenant-bound. The API
admits as assignee only a person who can see the ticket's project, and asks a question only
of a person who can see the ticket. The audit record's `ticket_id` has no foreign key on
purpose: an act outlives its ticket (ADR 0026).

## Visibility inside a tenant

Three SQL functions decide what the caller sees within the tenant (migrations
[7](../../backend/internal/store/migrations/000007_visibility.up.sql) and
[8](../../backend/internal/store/migrations/000008_tickets.up.sql)):

- `app_is_tenant_admin()` — the caller holds the `admin` role in the current tenant.
- `app_project_visible(project)` — the project is the token's own if the token is restricted
  to one; it belongs to the tenant; and it is unrestricted, or the caller is an
  administrator, or the caller is on its list.
- `app_ticket_visible(project, confidential, assignee, reporter)` — the project is visible,
  and the ticket is not confidential or the caller is an administrator, its assignee or its
  reporter.

They are not policies: row-level security holds the tenant, the predicates hold the project
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

The exemptions, each with its reason written in its query file:

| Query | Why it reads past the predicate |
|---|---|
| `GetWrittenTicket` | a write's answer rereads the row it wrote; a reassignment can take a confidential ticket out of its writer's sight in the same transaction |
| `TicketFacts` | the publication of a committed act; each event stream filters (below) |
| `ParentChainContains`, `BlocksPathExists` | integrity walks that answer yes or no (H-3) |
| `GetUrgencyInputs`, `ListBlockedTickets` | the urgency derivation's inputs and the tickets that depend on them (H-3) |
| `CanSeeProject`, `CanSeeTicket` | whether another person — an assignee, a person asked — sees what the caller reads |
| `ProjectKeyTaken` | whether a project key is taken (H-3) |
| `LastRank`, `ListUnrankedTickets`, `GetTicketRank`, `NextRankedTicket`, `PreviousRankedTicket` | the rank keys of the project a write hands a key out in: a new key lies between keys that exist, a hidden ticket's included, so none is handed out twice (H-3) |

Where the predicate hides a related ticket, the visible one shows less rather than more: a
parent or a ticket a block waits on that the caller cannot see is left out of the ticket's
`parent` and `block.ticket` (the block's kind and reason remain), a link whose other end is
hidden is absent from the list, and the `blocked` filter and the prerequisites of `done`
count only the blockers the caller sees.

## The project restriction

A restricted project is visible to the tenant's administrators and to the persons on its list
(`project_access`), each with the lower of their tenant role and their entry
([`projects.go`](../../backend/internal/api/projects.go) `projectRole`; ADR 0034 D3). No
route restricts a project or writes its list: the runtime role may not update
`projects.restricted` and may only read `project_access`. Today the restriction therefore
applies only to projects and lists written past the API, the way the test fixture writes them.

A token restricted to a project carries the project into `app.restricted_project_id`, which
narrows `app_project_visible` to it. At the boundary it reaches only the routes with
`{project}` in their path, the project list, the tenant-wide ticket list, the key resolver and
the event stream, each narrowed to its project by the predicate (`tenantWideForProjectTokens`
in [`tenant.go`](../../backend/internal/api/tenant.go)); every other tenant route answers it
`404`.

## The confidential flag

While a ticket is confidential it is visible to the tenant's administrators, its assignee and
its reporter, and to nobody else; its comments, questions, attachments, links, stakes, time
entries, activity, export and events follow it, because each is read through the ticket's
predicate (ADR 0065 D1).

- **Set automatically** when a ticket is filed with `security` `live` or `boundary`, and when
  a change makes the class `live` or `boundary` — from one of the two to the other as well —
  with a `confidential_set` act naming the class. A change that leaves the class as it is does
  not set again what an administrator lifted
  ([`tickets.go`](../../backend/internal/api/tickets.go) `applyTicketPatch`; ADR 0065 D2).
- **Never lifted automatically.** Changing the class back, `done` and `dropped` leave it set;
  no code path clears it except the administrator's act (ADR 0065 D3).
- **Set or lifted by hand** only through `PUT …/confidential`: the `admin` role and `admin`
  scope, never an agent — the hard-off rule "setting or lifting the confidential flag".
  Lifting needs a reason; both are recorded as `confidential_set` or `confidential_lifted`
  (`SetConfidential`; ADR 0065 D2, D3, D6).
- **An agent sets it indirectly** by filing or classifying a ticket `live` or `boundary`,
  which is intended (ADR 0065 D6).
- **Assignment admits** (ADR 0065 D9): a person who can see the ticket's project sees a
  confidential ticket from the moment it is assigned to them. An agent can do that too
  ([tokens.md](tokens.md) H-6).

## The activity withholds what its reader cannot see

A ticket's activity (`…/activity`) is its audit rows, read after the ticket passed the
predicate; it leaves out time entries, the reads of ADR 0026 D5, and the `booked`, `voided` and
`locked` acts. An act names the other tickets its payload mentions in `refs` — a link's other
end, the ticket a block waits on, the prerequisites a close overrode, the old and the new
parent. When the reader cannot see one of them, the act is shown with `redacted: true` and
without its `before`, `after`, reason and note
([`comments.go`](../../backend/internal/api/comments.go) `activityView`); who acted, when,
and which action remain visible.

A comment's text never enters the audit record, so a withdrawal hides it from the thread, the
comment's history and the activity alike
([ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D3). The
tenant's audit view (`…/audit`) is for administrators, who see every project and every
confidential ticket anyway; it is not filtered.

## The event stream carries what its subscriber could read

Every act of a ticket — its comment, question, link, interest and attachment acts included;
the reads of ADR 0026 D5 and the time entries not — is published with `pg_notify` on the
channel `cowork_events` inside the act's transaction, so it is delivered at commit and never
on a rollback ([`store/notify.go`](../../backend/internal/store/notify.go)). The notification
carries the audit row's id, the tenant, the project, the entity, the action, the ticket's key
and version, and the inputs of the confidential rule: the flag, the assignee and the reporter.
One connection per replica listens and hands each notification to the streams of its tenant.

A stream (`GET …/events`) passes authentication and the boundary like any route and computes
its filter when it connects ([`api/events.go`](../../backend/internal/api/events.go)
`streamFilter`): the projects the caller sees at that moment — a project-restricted token's
own only — and whether the caller is a tenant administrator. An event passes when its project
is in the set and its ticket is not confidential or the caller is an administrator, its
reporter or its assignee ([`events/hub.go`](../../backend/internal/events/hub.go)
`Filter.Admits`) — the inputs `app_ticket_visible` reads. The stream sends the event's name
and `{key, version, kind}`, never content; the client refetches through the API, which applies
the predicates again
([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D2, D3). A replay after a reconnect is filtered the same way. `TestEventStream` checks the
tenant, the project restriction and the confidential rule.

The filter follows the person: every twenty seconds the heartbeat checks the token and the
membership again ([tokens.md](tokens.md) H-7) and recomputes the visible projects with the
person's current role (`Hub.Refilter`). A project restricted away from the person, a lowered
role or a project created after the stream opened counts within one heartbeat — a change made
in the database as well as one through the API (`TestStreamFollowsAccess`).

## Time follows its own rule

Whose time entries a caller sees is `app_time_visible(person)`
([migration 13](../../backend/internal/store/migrations/000013_time_entries.up.sql)): their
own; every entry as a tenant administrator; everyone's as a `member` — not a `viewer` — while
the tenant's `time_visible_to_members` is on (ADR 0034 D5,
[ADR 0017](../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D9). Every time query — the lists, the sums, the tenant-wide list, the report — calls it next
to the ticket predicate, so an entry on a ticket the caller cannot see is invisible whoever
booked it. The unit test on the query files checks the ticket predicate only; the time
predicate is held by review and by `TestTimeVisibility`.

## What this does not cover

<a id="h-2"></a>
### H-2 — A confidential ticket's text is readable to whoever reads the database or a backup

Live whenever a ticket is confidential. The flag is a predicate in queries; nothing encrypts
the text at rest (ADR 0065 D8). The text sits in plain columns: the ticket's title, body,
threat and block reason; its questions; its comments and their revisions — a withdrawn
comment keeps its text in its row; the audit record, which holds every replaced version of
the body, the titles, the questions and answers, and the reasons and notes of the acts, append-only and
without an end of retention (ADR 0026 D7); and for a day, until the hourly expiry job removes
it, the stored response of a keyed creation — a ticket, a question, a comment — in
`idempotency_keys`. Its attachments' bytes lie in the bucket
([attachments.md](attachments.md)). Whoever reads the database past row-level security — a
superuser, a role with `BYPASSRLS`, the owner role, which can switch `FORCE` off — or holds a
dump, a backup or the volume, reads all of it, and so does a compromised serving process
(below). Encryption at rest of confidential text is a different threat model and is not
built; guarding the database credentials, the volumes and the backups is the operator's
([ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)).

<a id="h-3"></a>
### H-3 — Integrity checks and derived values read tickets the caller cannot see

A rule that must hold for the whole tenant cannot ask only what its caller sees, and a rule
that asks only what its caller sees lets a hidden ticket slip by. Both kinds exist:

- The parent cycle refusal walks the ancestors past the predicate (`ticket_ancestor_or_self`):
  whether a re-parenting is refused with `409 parent_cycle` can depend on a confidential
  ancestor in the same project.
- The `blocks` cycle refusal walks the tenant's whole `blocks` graph past the predicate
  (`blocks_path_exists`): `409 link_cycle` can depend on confidential tickets and on tickets
  of restricted projects.
- `done` is refused only by the open prerequisites the closer can see
  (`ListOpenPrerequisites`): a ticket can be closed over an open prerequisite its closer
  cannot see, without an override and without a mention in the act.
- Rule `v1:icebox-decision` counts an open decision that blocks the ticket whether or not the
  reader can see it (`GetUrgencyInputs`), and every reader sees the derived urgency and the
  rule's name. When such a decision opens or settles, the tickets it blocks are derived again,
  and a standing override ends with an `overridden` act on their timelines in the name of the
  person who changed the hidden decision.
- The derived progress is the effort-weighted mean of every child not dropped, confidential
  ones included (`ticket_derived_progress`).
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
  mover sees it go whether or not a hidden ticket sits there. Two signals remain. A move into a
  gap that moves of hidden tickets wore down — 635 to 762 moves into one gap — fails as an
  internal error, as any exhausted gap does. And the one write that ranks the open tickets an
  earlier release left without a key — a hidden ticket's filing, reopen or move included —
  changes how the list shows them, with no act the caller sees: with `include_terminal` they
  move from among the done and dropped tickets, by number, to before them, and a cursor
  positioned on one changes. `TestRankAroundAHiddenTicket` and `TestRankKeyIsNeverShown` hold
  the rest.

Each reveals at most that such a ticket or project exists — for the rank, at most that hidden
tickets were moved or filed — and who acted on it when — never its content. Live as soon as a
tenant has a confidential ticket, which a ticket classified `live` or `boundary` is until an
administrator lifts the flag; a restricted project only the test fixture can make today. A
tenant with neither has nothing to reveal.

<a id="h-4"></a>
### H-4 — The event channel is readable by any role that can connect to the database

Live wherever a database role other than the two cowork roles can connect to cowork's
database. `LISTEN` needs no privilege beyond the connection, and a notification is not a row,
so row-level security does not apply to it. Such a role that runs `LISTEN cowork_events`
receives the notifications of every tenant: tenant and project ids, ticket keys (with the
tenant's slug and the project's key), versions, the acts' names, the confidential flags and
the ids of assignees and reporters — no titles, no bodies, no comments. PostgreSQL grants
`CONNECT` on a new database to `PUBLIC`. The mitigation is the installation's:
`REVOKE CONNECT ON DATABASE <cowork's database> FROM PUBLIC`, with `CONNECT` granted to the
owner and the runtime role only; a superuser keeps its access regardless, and `pg_hba.conf`
decides who reaches the server at all. cowork does not check this at start.

<a id="h-5"></a>
### H-5 — Events are kept only for the replay window

Live today, by design (ADR 0054 D5). Each replica keeps the events of the last
`COWORK_SSE_REPLAY_WINDOW` (five minutes by default) per tenant in memory. A client that
reconnects with a `Last-Event-ID` its replica no longer holds — away for longer, or on a
replica that started later or lost its database listener meanwhile, which empties its buffers
when it hears the database again — gets `event: resync` and must refetch every list it shows;
while a replica's listener is down, new streams are refused with `503` and the open ones are
told to resync when it is back. Nothing is lost from the
record: the API and the audit record have every act, and a refetch goes through the
predicates. What is lost is the stream's continuity: a client that ignores `resync` shows a
stale view.

### The owner credential in the serving process

The split of the two roles protects against a compromised serving process only while that
process does not hold the owner credential (ADR 0021, residual risks). The chart keeps it in
the migration init container. `cowork serve` with `COWORK_MIGRATE_ON_START=true` — the
binary's default, and `make run`'s — requires `COWORK_DATABASE_OWNER_URL` and holds it for its
whole lifetime ([`cmd/cowork/main.go`](../../backend/cmd/cowork/main.go) `requireForServe`);
so does a serving container given the owner URL through `backend.extraEnv`. Such an
installation keeps the split against defects, not against a compromise.

### A compromised serving process

Row-level security constrains the forgotten filter, never the deliberate one (ADR 0021 D7).
The runtime role sets its own context, so a process that runs SQL of an attacker's choosing as
that role reads and writes every tenant's rows within the role's grants. What it still cannot
do is what the grants withhold: change a policy or switch `FORCE` off, rewrite or delete the
audit record, create a tenant, a person, a membership or a token, or change a token's scope,
restriction, agent flag, capabilities or expiry, or clear a token's revocation — the owner's
trigger `tokens_revocation_is_final` refuses that, and the runtime role can neither drop nor
disable it. It can revoke any token.
