# Tenant isolation and visibility inside a tenant

How one tenant's data stays out of another tenant's reach, who belongs to a tenant and in which
role — group mappings, grants, the last administrator — and who inside a tenant sees which project,
ticket, act, event and time entry, as built on 2026-10-04. What a token or an agent may do with
what it can see is [tokens.md](tokens.md); how a request reaches the backend at all, and where the
database credentials live, is [trust-boundaries.md](trust-boundaries.md); where a person's groups
come from, and when a mapped membership follows them, is
[identity-provider.md](identity-provider.md); what becomes of an upload's bytes is
[attachments.md](attachments.md).

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
by column on the tenants, projects (their restriction among them), tokens, tickets, questions,
comments, stakes, time entries, persons (the identity provider's columns among them), local
accounts, sessions (the groups refresh's among them), memberships and group mappings (a role and a
version each) and a project's access list (its role), table-wide on `ticket_counters`, `idempotency_keys` and
`login_locks` — `DELETE` only on `ticket_links`, `ticket_interest`, `idempotency_keys`, `sessions`,
`login_attempts`, `login_locks`, `memberships`, `group_mappings` and `project_access`, and only
`INSERT` and `SELECT` on `audit_events`, which makes the audit record append-only by grant
([ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D3).
`TestTheAuditRecordIsAppendOnly` shows that `UPDATE`, `DELETE`, `TRUNCATE` and switching
row-level security off are refused, and that a grant to itself grants nothing. The role
inserts a tenant, a person, a membership, a token, a session, a local account, a group mapping or
an entry of an access list only where a policy of migrations
[15](../../backend/internal/store/migrations/000015_local_accounts.up.sql),
[16](../../backend/internal/store/migrations/000016_sessions.up.sql) and
[20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)–[22](../../backend/internal/store/migrations/000022_membership_administration.up.sql)
admits it — an administrator of the current tenant, a global administrator creating a tenant, the
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

except the tables ADR 0021 D6 names one by one, whose rows a person reads across tenants or
which have no tenant at all:

| Table | Its policy admits |
|---|---|
| `tenants` | the row inside its own tenant's transaction, and to its members; updates only inside its own transaction; read by the login, the start-up synchronisation and the identity provider named in `app.job`, so a login can ask whether any tenant exists; inserted by a global administrator or the synchronisation |
| `users` | the person, everyone who shares the current tenant with them, the login and the synchronisation, and the identity provider every person; a tenant's administrator also the persons a lookup by address or username names (`app.person_lookup`, below); inserted by an administrator of the current tenant (never a global administrator, never a person of the identity provider), by the synchronisation, or by the identity provider (only a person of the provider, without a username); updated by the administrators of the accounts their tenant manages, by the synchronisation, and by the identity provider (only its own persons) |
| `memberships` | the tenant's rows inside the tenant, and the person's own rows everywhere; a grant inserted by an administrator into their own tenant, by a global administrator for themselves as `admin`, or by the synchronisation, and changed and removed by an administrator of the tenant; a mapped membership inserted, changed and removed by the identity provider alone |
| `tokens` | the person's own rows, and during the lookup the one row whose hash the transaction names in `app.token_hash`; the administrators of a managed account and the synchronisation read and revoke its tokens; inserted for the person's own account only |
| `idempotency_keys` | the person's own rows, and every row to the expiry job named in `app.job` |
| `audit_events` | a tenant's rows inside that tenant, an installation-level row to the person it names; a row is inserted only into the context it belongs to |
| `local_accounts` | the person's own row, the managing tenant's administrators, the login and the synchronisation; inserted for `tenant` by an administrator of that tenant and for `config` by the synchronisation, updated by the person only while a `tenant` account |
| `sessions` | the person's own rows, the one row whose hash the transaction names in `app.session_hash`, the administrators of a managed account, a global administrator for reading, and the two jobs that end sessions; inserted for the person's own only |
| `login_attempts`, `login_locks` | the login, its expiry job and the synchronisation; the administrators of a managed account read and clear the rows of its username |

Two tenant-bound tables carry policies beside `tenant_isolation`. `group_mappings` is read across
tenants by the identity provider, which derives a person's memberships in every tenant at once, and
the bootstrap tenant's mapping is inserted by the synchronisation
([migration 21](../../backend/internal/store/migrations/000021_group_mappings.up.sql)). On it and
on `project_access` a write needs an administrator of the current tenant in the data layer as well
as in the handler: `AS RESTRICTIVE` policies, which a write must pass in addition to whatever a
permissive policy admits — `group_mappings_admin_insert`, `…_update`, `…_delete` (the bootstrap's
insert excepted) and `project_access_admin_insert`, `…_update`, `…_delete`
([migration 22](../../backend/internal/store/migrations/000022_membership_administration.up.sql)).
A project's restriction is held the same way, by a trigger, because a policy sees the row and not
the column and a member may rename a project: `projects_restriction_guard`, `BEFORE UPDATE OF
restricted`, refuses a change of `restricted` unless the caller is an administrator of the tenant
(SQLSTATE `42501`) — a superuser, whom row-level security does not bind either, excepted
(`TestPoliciesOfThePersonsAndTheirAccounts`).

At the start of every transaction the store sets `app.tenant_id`, `app.user_id`,
`app.restricted_project_id`, `app.job` and `app.session_hash` — the hash of the session cookie
a request presented, which is how a request finds its own session row — with
`set_config(…, true)`, which dies with the
transaction ([`store/tx.go`](../../backend/internal/store/tx.go) `setContext`). One transaction
sets one more: the lookup of a person a tenant's administrator grants a role to names the address
or username in `app.person_lookup`, read through `app_person_lookup()`
([`store/members.go`](../../backend/internal/store/members.go) `FindPerson`). The person
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
reads, the transaction that counts and decides a login attempt, the person lookup, the claim of a
groups refresh, and the identity provider's transactions — a login, the application of a refresh's
answer, a token's gate check — which set their own context. The connection pool is
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
hidden is absent from the list, and the `blocked` filter, the prerequisites of the done act and
a ticket's `open_prerequisites` count only the blockers the caller sees.

## Members, grants and group mappings

Who belongs to a tenant is `memberships`, and a person has up to two rows in a tenant, one per
source ([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D2–D4; [`api/members.go`](../../backend/internal/api/members.go)):

- **A mapped membership** is derived by the identity provider from the person's groups and the
  tenant's group mappings — the highest role the mappings give — at a login, a groups refresh, a
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
  role (`origins`) and whether the person has a local account, to every member of the tenant
  ([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
  D7; `TestLoginThroughDex`); the person's e-mail address, which tells two persons of one name apart,
  to the tenant's administrators only — `null` to everyone else, and for a person without one
  (`addressFor`; `TestAdministratorsSeeTheAddress`).

**A group mapping** gives the members of a group of the issuer a role in one tenant: the name
matched exactly, case and all, against the groups claim; one mapping per group and tenant
(`409 mapping_exists`); several matched groups give the highest role. A change of its role takes
`If-Match`. `includes_caller` tells the editor whether their own groups, as of their last login or
refresh, hold it (`TestTheMappingEditorsOwnRole`). A tenant's administrator may map any group name
the issuer could send, `admin` included: everyone behind the gate whose stored groups hold it holds
the role at once, and whoever joins the group at the issuer from their next login — the mapping is
the tenant's word, the group the issuer's ([H-31](#h-31)).

**The person lookup.** `POST …/members` names the person by an e-mail address — a value with `@`,
compared without regard to case with the address the issuer asserted at the person's last login,
never one the issuer marked unverified, and only among the persons of the configured issuer, so
that without a provider an address finds nobody — or by a local account's username, with or without
`local:`, normalised as the login normalises it. The person must exist and be active: one who never
logged in through the issuer and has no local account is `404 person_not_found`; an address two
active persons share is `409 person_ambiguous`, and nobody is granted; a grant that exists is
`409 grant_exists`. The lookup runs in the administrator's transaction in their tenant and names the
key in `app.person_lookup`: the `users` policy admits the persons that match it and no other person
of the installation ([`store/members.go`](../../backend/internal/store/members.go) `FindPerson`;
`TestAddMemberByAddressOrUsername`). How far the issuer's address can be trusted is
[identity-provider.md](identity-provider.md#h-26) H-26; what the lookup and a mapping tell an
administrator about persons beyond their tenant is [H-31](#h-31).

**Who may.** The tenant's administrators, never an agent — the hard-off rule "administration"
([tokens.md](tokens.md)). Every act that can give access takes a browser session: adding a member,
setting a grant, making or changing a mapping, restricting or opening a project, putting a person on
its access list. Removing a grant, a mapping or an access entry only takes access away, and an
administrator's `admin`-scope token may do it too ([tokens.md](tokens.md#what-only-a-session-does)).
The mappings and a project's access list are read by the tenant's administrators — with a token's
`read` scope — and the member list by every member.

**`409 last_admin`.** A change of a grant or of a mapping that would leave the tenant without an
administrator who can log in — mapped or granted, active, and a local account or a person of the
configured issuer whom the gate admitted at their last login, refresh or check — is refused and
changes nothing, the administrator's own grant and their own mapping included (`lastAdmin`;
`TestGrantsAndTheLastAdministrator`, `TestTheMappingEditorsOwnRole`,
`TestTheLastAdministratorMustBeAbleToAct`). Each of these changes takes the tenant's lock first
(`LockTenant`), so two administrators who take each other's role away at the same moment are
decided one after the other, and the second meets `last_admin`
(`TestTwoAdministratorsCannotRemoveEachOther`). Adding a member and the access list take no lock and
meet no check: they take no administrator away. A derivation at a login, a refresh or a token's gate
check is never refused ([identity-provider.md](identity-provider.md#h-29) H-29), and a deactivation
is not held to the rule ([local-accounts.md](local-accounts.md#h-32) H-32).

**Every change is recorded** — a grant, a mapping, a restriction and an access entry by the
administrator; the memberships a mapping's change derives by `system:identity-provider` with the
cause `mapping`, in the same transaction — and announced as `membership.changed`
([below](#the-event-stream-carries-what-its-subscriber-could-read)).

## The project restriction

A restricted project is visible to the tenant's administrators and to the persons on its access
list (`project_access`), each with the lower of their tenant role and their entry, `member` or
`viewer` ([`projects.go`](../../backend/internal/api/projects.go) `projectRole`; ADR 0034 D3). An
administrator restricts or opens a project with `PUT …/projects/{project}/restriction` — `If-Match`
on the project's version, a browser session only, recorded as the project's `updated` — and keeps its
list with `GET`, `PUT` and `DELETE …/projects/{project}/access/{person_id}`: the person must be a
member of the tenant (`404 person_not_found`), putting a person on the list or changing their entry
takes a session, taking one off a token as well (`TestProjectRestrictionAndAccessList`,
`TestRolesFromTheIdentityProviderHold`). The list may be written before the project is restricted,
so nobody on it loses the project in between; its entries count while the project is restricted.
The runtime role may change `projects.restricted` and nothing else of a project's restriction, the
trigger lets only a tenant administrator change it, and writing the list needs a tenant
administrator in the data layer as well (above). An entry shows the person's e-mail address: the
list is the administrators' to read.

**The entries are on the project's list, not in the member list.** Every member, a viewer included,
reads the member list, and an entry there would name a restricted project to members who must not
learn it exists; the access list is read by the tenant's administrators alone (ADR 0034 D7).

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

**`membership.changed`.** An act on who belongs to the tenant or who sees a project — a grant, a
derived membership, a mapping, a project's restriction, an entry of an access list — is published
in its transaction as well, with the keys of what changed and an audience, and sent as
`{person_id, project_id, mapping_id}`, each where it applies, never content
([`store/notify.go`](../../backend/internal/store/notify.go) `MembershipChange`). A membership and a
restriction reach every member of the tenant: the member list is theirs to read, and a project
restricted or opened was visible to them at one of the two moments. A mapping reaches the tenant's
administrators, who alone read the mappings. An access entry reaches the administrators and the
person it names, never the members who do not see the project (`Filter.Admits`;
`TestMembershipAudiences`, `TestMembershipEventsReachTheirAudience`). The stream of a token
restricted to a project hears, of these, only the events that name its project, or name its own
person and no project: the token knows nothing of the tenant beyond its project
(`TestMembershipEventsOfAProjectRestrictedStream`, `TestARestrictedStreamHearsOnlyItsProject`).

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
- The done act — by hand, or the `PATCH` that fills the last progress stage — is refused only
  by the open prerequisites the closer can see (`ListOpenPrerequisites`): a ticket can be
  closed over an open prerequisite its closer cannot see, without an override and without a
  mention in the act, and its `open_prerequisites` reads 0 to that closer.
- Rule `v1:icebox-decision` counts an open decision that blocks the ticket whether or not the
  reader can see it (`GetUrgencyInputs`), and every reader sees the derived urgency and the
  rule's name. When such a decision opens or settles, the tickets it blocks are derived again;
  a standing override stays, and no act is recorded on their timelines — the change shows in
  `urgency_derived` and `urgency_rule` alone.
- Each derived progress stage is the effort-weighted mean of the same stage of every child not
  dropped, confidential ones included (`ticket_derived_stage`), and a parent whose children are
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
administrator lifts the flag, or a restricted project, which a tenant's administrator makes in a
browser session. A tenant with neither has nothing to reveal.

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

<a id="h-31"></a>
### H-31 — A tenant's administrator can bring any group's people into their tenant, and learn who exists

Live in an installation with several tenants whose people log in through one identity provider, or
hold local accounts. Every tenant shares the issuer's one namespace of groups, and a person belongs
to the installation, not to a tenant. A tenant's administrator may map any group
([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D7 as decided), and the **mapping** derives at once a membership for every person of the configured
issuer behind the gate whose stored groups hold it, whichever tenants they belong to: mapping a group
every person holds, or a guessed department's, brings them all into the administrator's tenant —
their names and e-mail addresses in its member list, the tenant in their own list of tenants — with
the mapped role until the mapping goes; removing it takes the memberships away again, and the record
keeps both. The **person lookup** answers whether an address or a username names an active person of
the installation: a `201` that grants them, `404 person_not_found`, or `409 person_ambiguous`, which
tells without granting anybody that two persons share an address. Both acts are an administrator's,
take a browser session, and are recorded with the administrator as actor — the derived memberships
as `system:identity-provider` in the same request — but neither asks the persons, or the tenants
they belong to. That crosses the line between clients
[ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) draws between
tenants. Mitigation: tenants whose administrators must not learn about each other's people belong in
installations of their own; within one installation nothing keeps an administrator to the groups of
their own people.

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
that role reads and writes every tenant's rows within the role's grants. The policies that admit
a system actor — the login, the start-up synchronisation, the identity provider — admit whoever
names it in `app.job`, and a person's own rows whoever names the person: such a process also
creates tenants, persons, global administrators, memberships, group mappings, sessions and tokens
for any person, and changes any person's groups and administrator flag. What it still cannot do
is what the grants withhold: change a policy or switch `FORCE` off, rewrite or delete the audit
record, change a token's scope, restriction, agent flag, capabilities or expiry, bind a person to
another username or another identity of the issuer, or clear a token's revocation — the owner's
trigger `tokens_revocation_is_final` refuses that, and the runtime role can neither drop nor
disable it. It can revoke any token.
