# What a token can do, and what an agent can do

What a personal access token is, how a request presenting one is checked, what it may then do
— through its scope, its person's role, its restriction and, for an agent, the capabilities
and the hard-off list — what only a browser session may do instead, how a dead token and a token
of a person outside the identity provider's gate are answered, and what is recorded, as built on
2026-10-05. Which tenants, projects and tickets a person can see at all is
[tenancy.md](tenancy.md); how a token comes to exist — its person, in a browser session — is
below and in [sessions.md](sessions.md); the identity provider whose groups a token's person is
held to is [identity-provider.md](identity-provider.md); the chat in the UI, an agent that holds no
token, is [chat.md](chat.md).

## A token is a bearer secret, stored as a hash

- **Form.** `cwk_` followed by 43 base62 characters that encode 32 bytes from `crypto/rand`,
  left-padded so every token has one length and matches `^cwk_[0-9A-Za-z]{43}$` — a pattern
  secret scanners and push protection can look for
  ([`backend/internal/auth/token.go`](../../backend/internal/auth/token.go);
  [ADR 0035](../adr/0035-personal-access-tokens.md) D1).
- **Storage.** The database keeps the SHA-256 of the whole token and nothing else of it
  (`tokens.token_hash`, unique, 32 bytes); the plaintext exists once, where the token is
  written. A plain hash is enough for a secret of 256 random bits: there is no dictionary of
  likely tokens to precompute. Nothing limits the rate of failed attempts
  ([ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
  D1); at 256 bits, guessing is not a practical attack.
- **In the browser.** Every API answer is `Cache-Control: no-store`, the creation's too
  ([`api/api.go`](../../backend/internal/api/api.go) `ServeHTTP`). The tokens page holds the
  plaintext in one signal and shows it in
  [`SecretDialog`](../../frontend/src/app/shared/secret-dialog.ts) in a read-only field; every
  way of closing the dialog sets the signal to `null`, and nothing writes the plaintext to
  storage, a URL, a log or a toast — `secret-dialog.spec.ts` and `tokens.spec.ts` hold it,
  down to no copy left in the page once it is closed. The copy button writes it to the
  clipboard; what the person does with it from there is outside cowork.
- **Transport.** Only `Authorization: Bearer cwk_…`; a value of another shape is refused
  before the database is asked. A token is never read from a query parameter or a cookie, and
  an unknown query parameter is refused anyway (ADR 0035 D7).
- **Lookup.** The resolver's transaction names the presented hash in `app.token_hash`, and
  the tokens policy admits that one row and no other; the person is read once the row names
  it ([`store/tokens.go`](../../backend/internal/store/tokens.go) `LookupToken`;
  `TestTokenLookupSeesThePresentedRowOnly`).
- **Origin.** Only the person creates their tokens, in a browser session
  (`POST /api/v1/me/tokens`, [ADR 0035](../adr/0035-personal-access-tokens.md) D5): a token
  calling it is `403 session_required`, no administrator creates a token for another person,
  and the password change that a temporary password demands comes first
  ([sessions.md](sessions.md)). The answer carries the plaintext once; the database keeps the
  SHA-256, and no audit row, stored answer or list holds the plaintext
  (`TestOnlyASessionCreatesATokenAndShowsItOnce`). A retried creation with an
  `Idempotency-Key` creates nothing twice and answers without the plaintext, which the stored
  answer never held. The lifetime is `COWORK_TOKEN_DEFAULT_LIFETIME` (90 days) unless the
  request asks for fewer days, and never more than `COWORK_TOKEN_MAX_LIFETIME` (one year): a
  longer request is shortened and the answer says what the token got
  (`TestATokensLifetimeIsClampedToTheMaximum`); the token form knows the bound before it asks, from
  `GET /auth/options`, which names it in whole days — public, like the password policy beside it:
  it tells an anonymous reader how long a token of the installation can live at most, and nothing
  of any token (`TestAuthOptions`). An agent token has at most `write` scope and
  every capability when the request leaves `capabilities` out; a list is the capabilities, and
  an empty one is none, the baseline only (ADR 0043 D4's nine switches all off); a restriction names a tenant the person belongs to
  and a project of it they see, and a tenant or project they cannot reach is "no such" in the
  same words as one that does not exist (`TestTokenCreationRules`). The test fixture and
  `make dev-seed` write tokens over an administrative connection, which is how `make dev` gets
  the token for its demo data (below). Whatever writes a token, the schema holds it to its rules:
  an expiry later than its creation, no `admin` scope on an agent token, capabilities only on an
  agent token and only the nine names, a project restriction only together with the tenant
  restriction and only for a project of that tenant
  ([migration 4](../../backend/internal/store/migrations/000004_tokens.up.sql)). The runtime
  role may insert a token for its own person alone (the policy of
  [migration 15](../../backend/internal/store/migrations/000015_local_accounts.up.sql)).
- **What a leaked token cannot leave behind.** Creating a token, a tenant or a local account,
  resetting a password and every administration act that can give access take a browser session
  ([below](#what-only-a-session-does)): each would hand whoever held a leaked token something
  that outlives the token's revocation. An `admin`-scope token of a tenant administrator still
  makes the acts that only remove or restrict access.
- **Development.** `make dev` mints a plain `admin`-scope token with the fixture and keeps it in
  the untracked `.dev/token` (mode 600 in a directory of mode 700) for the demo data, which it
  writes straight to the backend; nothing in the browser path holds a token — the browser logs in
  as the local administrator `dev` with a development-only password
  ([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
  D2, D4). Whoever can read `.dev/` on the developer's machine holds that token, against the
  development database only.
- **Listing.** `GET /api/v1/me/tokens` shows the person's tokens with their metadata — name,
  scope, agent flag, capabilities, restriction, dates, state — never the hash or the
  plaintext; revoked and expired tokens stay listed (ADR 0035 D6). A restriction names its
  project by key only while the person sees the project in a tenant they belong to — read in that
  tenant's transaction under the project predicate — and `null` otherwise, so the list names no
  project the person could not read (`projectKeys` in [`api/me.go`](../../backend/internal/api/me.go),
  `TestATokenNamesItsProjectByKey`); the project's id, deprecated beside it, is the token's own
  column.
- **A tenant's administrators see the tokens that can act in the tenant.**
  `GET /api/v1/tenants/{tenant}/tokens` lists every token of a member of the tenant that is
  unrestricted or restricted to this tenant, with its person and the same metadata, never the hash
  or the plaintext ([ADR 0035](../adr/0035-personal-access-tokens.md) D5 as amended 2026-10-05;
  [`api/tenanttokens.go`](../../backend/internal/api/tenanttokens.go)). A token restricted to
  another tenant is not in the list — not its name, not its id, not that it exists — and neither is
  a token of a person who is no member here. The query names exactly these rows, and so does the
  tokens policy of [migration 35](../../backend/internal/store/migrations/000035_tenant_tokens.up.sql)
  for an administrator of the current tenant (`app_tenant_reaches_token`), so a forgotten filter in a
  later query of a tenant transaction still shows no token restricted to another tenant
  (`TestTenantAdministratorsSeeAndRevokeTheTokensThatCanActInTheTenant`,
  `TestPoliciesOfThePersonsAndTheirAccounts`). The administrators of a tenant that manages a local
  account read every token of that account in the data layer, as they did before, because its
  deactivation revokes them all ([local-accounts.md](local-accounts.md)); the route shows them only
  the tokens that can act in their tenant. Listing takes the administrator role and `read` scope;
  anybody else is `403 forbidden`. In the browser it is the tenant's page *Tokens*, after the audit
  record ([`features/tenant/tenant-tokens.ts`](../../frontend/src/app/features/tenant/tenant-tokens.ts)).

## What a request with a token may do

An act is allowed when the person's role, the token's scope and, for an agent's request, the
agent rules all allow it. `auth.Authorize` checks them in that order — role, scope, hard-off
list, capability — and answers `403` with `forbidden`, `insufficient_scope` or
`agent_forbidden`, the last with a detail naming the rule (`hard-off: …`) or the capability
(`missing capability: …`) ([`auth/authorize.go`](../../backend/internal/auth/authorize.go);
ADR 0035 D3,
[ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D5). The role is the person's membership role in the tenant — in a restricted project the
lower of it and their entry on the project's list — read on every request, so a token never
reaches further than its person does at that moment.

| Scope | Reaches |
|---|---|
| `read` | every read of what the person may see, the tenant's member list included; for a tenant administrator also the tenant's audit view, its group mappings, a project's access list ([`api/members.go`](../../backend/internal/api/members.go) `adminRead`) and the tokens that can act in the tenant |
| `write` | additionally what a member does: filing and editing tickets, transitions, links, comments, questions and answers, stakes, progress, uploads, booking time; creating a project where the person may ([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D9); revoking another of the person's tokens |
| `admin` | additionally the administration acts a token may make: the tenant's settings including the time lock, archiving a project, setting or lifting the confidential flag, deleting a ticket and restoring it, which the bin undoes for thirty days ([tenancy.md](tenancy.md#a-deleted-ticket-answers-like-a-missing-one), [H-54](tenancy.md#h-54)) — never purging it, which takes a session —, withdrawing another person's comment ([`api/comments.go`](../../backend/internal/api/comments.go) `mayChangeComment`); for the local accounts the tenant manages, listing them, unlocking, deactivating and ending their sessions ([local-accounts.md](local-accounts.md)); removing a member's grant, a group mapping, or a person from a project's access list ([tenancy.md](tenancy.md#members-grants-and-group-mappings)); revoking a member's token that can act in the tenant — never the acts that only a session makes ([below](#what-only-a-session-does)) |

## What only a session does

Seventeen operations take a browser session only, and answer a token — whatever its scope, an
administrator's `admin` token included — `403 session_required` before anything is written. The API
document declares them with the session cookie alone, and a unit test over the document holds the
set to exactly these seventeen ([`backend/api/document_test.go`](../../backend/api/document_test.go)
`sessionOnly`; [ADR 0035](../adr/0035-personal-access-tokens.md) D5):

| Operation | Route | What a leaked token would leave behind |
|---|---|---|
| `createMyToken` | `POST /api/v1/me/tokens` | a token nobody thought to revoke |
| `createTenant` | `POST /api/v1/tenants` | a tenant, administered by the token's person |
| `createAccount` | `POST …/accounts` | an account, with a password its maker knows |
| `resetAccountPassword` | `PUT …/accounts/{username}/password` | a password only its setter knows |
| `addMember` | `POST …/members` | a person's grant into the tenant |
| `setMemberGrant` | `PUT …/members/{person_id}/grant` | a role |
| `createGroupMapping` | `POST …/group-mappings` | a role for everyone in a group of the issuer |
| `updateGroupMapping` | `PATCH …/group-mappings/{mapping_id}` | the same, raised |
| `setProjectRestriction` | `PUT …/projects/{project}/restriction` | a restricted project opened to every member |
| `setProjectAccess` | `PUT …/projects/{project}/access/{person_id}` | a person's way into a restricted project |
| `changeMyPassword` | `PUT /api/v1/me/password` | a password the person no longer knows |
| `logout` | `POST /auth/logout` | — a token has no session to end |
| `runChatTurn` | `POST …/chat` | — a turn's tool calls act with the person's session, and an agent that holds a token has the MCP server ([chat.md](chat.md)) |
| `stopChatTurns` | `DELETE …/chat/turns` | — it stops the session's person's turns, which a token never starts ([chat.md](chat.md#stop)) |
| `setMyChat` | `PUT /api/v1/me/chat` | what the person's agent in the browser may do, in every tenant of the person ([chat.md](chat.md#the-chats-mark-its-capabilities-and-what-only-a-session-does)) |
| `listTenants` | `GET /api/v1/tenants` | — it leaves nothing; it shows a global administrator every client of the installation, which a token of theirs does not reach ([tenancy.md](tenancy.md#a-global-administrator-without-a-role)) |
| `purgeTicket` | `DELETE …/deleted-tickets/{key}` | a ticket gone for good — its texts, its files and its time, its audit rows emptied; nothing undoes a purge ([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md) D7 as amended 2026-10-05, [tenancy.md](tenancy.md#a-deleted-ticket-answers-like-a-missing-one)) |

**The rule: an act that can give access, or make something that outlives the token's revocation,
takes a session; an act that only takes access away does not.** A route that does both — a grant
or a mapping raised or lowered, a project restricted or opened — takes a session for both. What
stays open to an administrator's `admin`-scope token removes or restricts access and leaves nothing
behind: listing, unlocking, deactivating a local account and ending its sessions
([local-accounts.md](local-accounts.md)); removing a member's grant, a group mapping, or a person
from a project's access list (`TestGrantsAndTheLastAdministrator`, `TestGroupMappingsDeriveAtOnce`,
`TestProjectRestrictionAndAccessList`). Deleting a ticket and restoring it stay open as well,
because the bin undoes either for thirty days; the purge, which nothing undoes, does not
(`TestPurgingTakesABrowserSession`; what a leaked token can still delete is
[tenancy.md H-54](tenancy.md#h-54)). No agent makes any administration act: an agent token's
scope is at most `write`, and a plain token marked by the header meets the hard-off rule
"administration".

A session's request that the agent header marks is refused all seventeen,
with `403 agent_forbidden`: what only a session does is a person's act, never an agent's
([`api/api.go`](../../backend/internal/api/api.go) `sessionRules`).

**A global administrator's token keeps the reach of the person's memberships.** The list of every
tenant above is a session's, and so is the reach into a tenant in which a global administrator holds
no role — its members, its group mappings, its settings and the grant to themselves: a token's
request there answers the `404` of an unknown tenant, held in the request layer rather than the
document ([tenancy.md](tenancy.md#a-global-administrator-without-a-role);
`TestAGlobalAdministratorWithoutARoleSeesTheAdministrationOnly`). A leaked token of a global
administrator reads no client of the installation the person is not a member of.

## Restrictions

- A token restricted to a tenant answers `404` on every other tenant, exactly as for an
  unknown slug ([`api/tenant.go`](../../backend/internal/api/tenant.go) `boundary`).
- A token restricted to a project — always inside its tenant restriction — carries the
  project into the visibility predicate. It reaches its project's routes, the project list,
  the tenant-wide ticket list, the key resolver and the event stream, each narrowed to the
  project — the stream's `membership.changed` to the events that name the project, or the token's
  own person and no project
  ([tenancy.md](tenancy.md#the-event-stream-carries-what-its-subscriber-could-read)) —
  and answers `404` on every other tenant route ([`api/tenant.go`](../../backend/internal/api/tenant.go)
  `tenantWideForProjectTokens`; [tenancy.md](tenancy.md) "The project restriction"). It is the
  token a repository binding wants (ADR 0035 D3).
- The person's own routes under `/api/v1/me` name no tenant, so the boundary does not run for
  them; the handlers hold a restricted token to its tenant instead. It sees the membership of
  its tenant only, lists only itself among the person's tokens, and revokes no token but itself
  ([`api/me.go`](../../backend/internal/api/me.go) `restricted`;
  `TestARestrictedTokenSeesItsTenantAndItselfOnly`).

## Expiry, revocation and refusals

- **Answers.** An unknown or malformed token: `401 unauthenticated`. A revoked token, or one
  whose person is deactivated: `401 token_revoked`. An expired token: `401 token_expired` with
  the date. A token of a person of the identity provider whom its gate no longer admits:
  `401 not_allowed` ([below](#the-identity-providers-gate)). Revocation is checked first, then
  expiry, then the gate. Every `401` carries
  `WWW-Authenticate: Bearer realm="cowork"` ([`api/authn.go`](../../backend/internal/api/authn.go)).
- **Refusals.** Every use of a revoked, expired or gated token is logged with the token's id and
  the reason, never the token. It is also recorded as an installation-level `refused` act of the
  token's person — reason `revoked`, `expired` or `not_allowed` — unless one with the same token and
  reason was recorded within the past hour
  (ADR 0035 D9), so a forgotten configuration that retries with a dead token cannot grow the
  append-only record without bound. The count and the insert are not serialised: two
  refusals in the same instant can both be recorded
  ([`store/tokens.go`](../../backend/internal/store/tokens.go) `RecordTokenRefusal`).
- **Revocation.** `DELETE /api/v1/me/tokens/{token_id}` is immediate, keeps the row and
  records the `revoked` act and who revoked. A token may always revoke itself, whatever its
  scope; revoking another of the person's tokens needs `write` scope and is hard-off for
  agents ([`api/me.go`](../../backend/internal/api/me.go) `RevokeMyToken`). Revoking twice,
  or twice at the same moment, answers `204` and records one act. A revocation is final: a
  trigger refuses any update that clears or changes `revoked_at` or `revoked_by`, whoever
  writes it ([migration 4](../../backend/internal/store/migrations/000004_tokens.up.sql)
  `tokens_revocation_is_final`; `TestRevocationIsFinal`). The `refused` act and the person's own
  `revoked` are installation-level rows, readable in the database only: no route shows them, and
  the per-token view of
  [ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D6 is not
  built. **A tenant's administrator revokes** a token of that tenant's list with
  `DELETE /api/v1/tenants/{tenant}/tokens/{token_id}`: immediate and final like the person's own,
  `revoked_by` the administrator, and recorded as the administrator's act `revoked` in the
  tenant's audit, naming the person, the token's name and whether it was unrestricted. A token the
  list does not show is `404`, as one that does not exist. It only takes access away, so an
  administrator's `admin`-scope token may; it is an administration act, so no agent may (the hard-off
  rule "administration"). **Revoking an unrestricted token ends it in every tenant of its person**
  — the page asks first and says so — and the record of it is in this tenant's audit alone
  ([H-57](#h-57)). A person's deactivation revokes every token they hold — an administrator's
  `PUT …/accounts/{username}/deactivation` on an account their tenant manages, and the
  start-up synchronisation for the local administrator — a `NULL` `revoked_by` meaning a
  system act ([local-accounts.md](local-accounts.md)); a password reset does not. No route
  deactivates a person of the identity provider.
- **Last use.** The last-used day is written at most once per token and UTC day — a note per
  replica and a conditional update — as bookkeeping, not as an act (ADR 0035 D2).
- **Open streams.** An event stream is not a next request: at every heartbeat it checks the
  token again — not revoked, not expired, its person not deactivated — or, for a stream opened
  with a session cookie, the session ([sessions.md](sessions.md)), and the tenant boundary,
  and ends when either fails ([`api/events.go`](../../backend/internal/api/events.go)
  `stillAdmitted`; [ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
  D5) — H-7.

## The identity provider's gate

A token of a person of the identity provider is held to the gate its person's login is held to
([ADR 0035](../adr/0035-personal-access-tokens.md) D8; `tokenGate` in
[`api/identity.go`](../../backend/internal/api/identity.go)). A person who is not the configured
issuer's — another issuer's, or anyone's of a provider no longer configured — is refused at every
request (`TestAPersonOfAnotherIssuerIsOutsideTheGate`), and so is one whose groups were last read —
at a sign-in, or by a session refresh that read them — longer ago than `COWORK_OIDC_GROUPS_MAX_AGE`,
a week by default, until they sign in to the browser once
(`TestGroupsOlderThanTheMaximumAgeRefuseTheTokens`). Otherwise, on the first request after the
person's last check plus `COWORK_OIDC_GROUPS_REFRESH` (fifteen minutes), the person's groups as of
their last login or the last session refresh that read them are judged against
`COWORK_OIDC_ALLOWED_GROUPS` and `COWORK_ADMIN_GROUP` as configured at that moment. Admitted, the
check is stamped and the request goes on; outside, it is `401 not_allowed` and the token is refused,
not revoked: it works again once the person is admitted. A refresh that reads groups outside the gate,
or a login refused at the gate, clears the stamp, so the person's tokens are refused at their very
next request (`TestLeavingTheGateStopsTheTokensAtOnce`, `TestLeavingTheAllowList`). A local account
meets no gate, and has no groups to age. The mechanism, and how old the groups a token is judged by
can be, is [identity-provider.md](identity-provider.md#the-token-gate) and its H-23.

## What is recorded

- Every act is an audit row written in the act's transaction; it names the person, the token —
  its id and its name, none for a browser session — the request id, the agent mark and, for an
  agent, the capability set that applied ([`store/tx.go`](../../backend/internal/store/tx.go)). Using a
  token is recorded through its acts, not per request (ADR 0035 D9); creating one is an
  installation-level `created` act of its person, naming its scope, agent flag, capabilities,
  restriction and expiry, never the token.
- **Every act made through a token shows the token**
  ([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
  D6). Beside the agent mark, the token's id and name are written with the act: on the audit row
  (`token_name`), on a comment and each edit of it, on a file, on a question as asked
  (`asked_by_token_*`) and its answer as recorded (`answered_by_token_*`, set or cleared by every
  answer), on a time entry and each correction of it
  ([migration 27](../../backend/internal/store/migrations/000027_acts_through_a_token.up.sql)), and
  on the ticket as it was filed (`reporter_agent`, `reporter_token_*`) and a stake as it was last
  set (`agent`, `token_*`, set or cleared by every write of it;
  [migration 28](../../backend/internal/store/migrations/000028_filing_and_stake_marks.up.sql);
  `actAgent`, `actToken` in [`api/tickets.go`](../../backend/internal/api/tickets.go)). The name is copied: the
  tokens policy shows a person their own tokens only, and the token's name never changes — the
  runtime role may update a token's revocation and last-used day and nothing else — so a revoked
  token's acts keep it. The API answers such an act with `token`, `{id, name}`, `null` for a browser
  session; no route answers a token's hash, its plaintext or a part of it, and the integration test
  holds the answers of a ticket's comments, files, questions and activity free of `cwk_`
  (`TestEveryActThroughATokenIsMarkedWithIt`). The UI shows a plain token's act as
  `token <name>` beside the person, an agent's as the agent with the token in its tooltip
  ([`shared/agent-mark.ts`](../../frontend/src/app/shared/agent-mark.ts)) — on an act of the
  activity, a comment, a question and its answer, a file, a time entry, the reporter and a stake's
  holder; only the person's own browser session, without the agent header, acts unmarked. What an
  agent reads names it as well: the context document says `through the token <name>` where it says
  `via <agent>` of an agent's act ([`markdown/context.go`](../../backend/internal/markdown/context.go)
  `via`), and so does the summary `session_start` writes
  ([`tools/start.go`](../../backend/internal/tools/start.go) `actLine`). The tenant's audit view
  names the token by `token_name` beside `token_id` in JSON, and in the CSV as its last column, so that
the columns released before keep their places. The mark tells, it does not bind: a
  plain token's request without the header is held to its person's role and its own scope and
  restriction, and to no agent rule ([below](#which-request-is-an-agents)).
- **The source of a request.** Every audit row written for a request carries `source_hash`, the
  HMAC-SHA-256 of the client's address under a key derived from `COWORK_SESSION_KEY` by HKDF-SHA256
  with the label `cowork audit address v1`
  ([`api/login.go`](../../backend/internal/api/login.go) `sourceHash`;
  [ADR 0035](../adr/0035-personal-access-tokens.md) D2;
  [migration 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)). The
  address is the client's under the rule of the trusted proxies
  ([local-accounts.md](local-accounts.md#the-client-address)), in its canonical form and whole —
  an IPv6 address too, where the login throttle counts its /64. The acts of a session and of a
  token carry it, and so do the rows of the login, of a token's refusal and of the identity
  provider's decisions in a request; the rows of the jobs and of the start-up, and every row
  written before this release, carry none. No route shows it — the tenant's audit view leaves it
  out — and the address itself is in no row (`TestAuditRowsCarryTheSourceHash`: two acts of one
  client, one hash; a job's rows, none). It tells one client's rows apart from another's under one
  server key; a new key gives the same address another hash, so rows from before a rotation do not
  compare with rows after it. With `COWORK_TRUSTED_PROXIES` empty, every browser's request through
  the Ingress has a controller pod's address, and the hash tells no browser behind it apart. Whoever holds the
  key reverses it ([H-30](#h-30)).
- Reads are not recorded, with two exceptions that mean data left the system (ADR 0026 D5):
  every download of an attachment's bytes — a `304` is not one — and every Markdown export of
  a ticket, which is never answered with `304`
  ([`api/export.go`](../../backend/internal/api/export.go)). The CSV forms of the time lists,
  the time report and the audit view are reads like any other.
- The answer to an abused token is the record and revocation (ADR 0039 D4): an administrator
  filters the tenant's audit view (`GET …/audit`) by token, person, action, entity type and
  period, as JSON or CSV — in the browser on the tenant's audit page, where an act's token is one
  click away from the acts it made ([`features/tenant/audit.ts`](../../frontend/src/app/features/tenant/audit.ts)). In CSV, a cell a spreadsheet would read as a formula — one starting
  with `=`, `+`, `-`, `@`, a tab or a carriage return — is prefixed with an apostrophe
  ([`api/tenants.go`](../../backend/internal/api/tenants.go) `neutralise`).

## Which request is an agent's

- The agent flag is written with the token and never changes; the runtime role cannot update
  it. A flagged token's request is an agent's whatever it sends; without an `X-Cowork-Agent`
  header it is recorded as `unknown-agent`
  ([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
  D2, D4).
- A plain token's request becomes an agent's when it sends the header, and then holds every
  capability: the header adds the agent rules and takes nothing away. No header value turns a
  flagged token's request into a person's
  ([`auth/principal.go`](../../backend/internal/auth/principal.go) `Mark`; ADR 0036 D3). Without the
  header a plain token's request is its person's — no agent rule, no capability set — and its acts
  still show the token ([above](#what-is-recorded)).
- A browser session's request becomes an agent's the same way: with the header it holds the
  capabilities its person chose for the chat — the default where the person chose none
  ([chat.md](chat.md#the-chats-mark-its-capabilities-and-what-only-a-session-does)) —, meets every
  agent rule and is refused what only a session does; its acts record the mark and no token ([`api/session.go`](../../backend/internal/api/session.go) `authenticateSession`;
  `TestTheAgentHeaderOnASession`). The chat in the UI marks every tool call so,
  `chat/<model>/<conversation>` ([chat.md](chat.md#the-chats-mark-its-capabilities-and-what-only-a-session-does)).
- The header is `name/model/session`, each part one to 64 printable ASCII characters that
  neither start nor end with a space; a malformed header is refused with `400`, never ignored.
- An agent token's scope is at most `write`. An administration act therefore refuses an agent
  token on its scope (`insufficient_scope`) before the hard-off rule is reached; the hard-off
  rule is what refuses it to a plain `admin` token marked by the header, and what refuses the
  hard-off acts that need only `write` to every agent.
- `GET /api/v1/me/token` answers the token a request presents — its metadata as the list shows
  it, the key of its project restriction — and what the request is: an agent's or not, the mark
  its acts record, the capabilities it holds ([`api/token.go`](../../backend/internal/api/token.go);
  [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
  D6). It shows a token nothing but itself, never another of its person's, and a browser session
  has no token to show (`404`). The MCP client reads it to tell the model the limits it will run
  into ([agent-client.md](agent-client.md)).

## Capabilities, the baseline and the hard-off list

| Capability | Guards |
|---|---|
| `decide` | `analysed → decided` |
| `close` | the done act, both ways to `done` ([ADR 0009](../adr/0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D5): done by hand, and the `PATCH` that brings the last of a ticket's three progress stages to 100 — only from `in-progress` or `review`, otherwise `agent_forbidden` (`mayClose` in [`api/transitions.go`](../../backend/internal/api/transitions.go)); the verification note stays required, and the open prerequisites the agent can see still refuse. Without it that `PATCH` is refused whole, and the stage keeps its value |
| `drop` | a move to `dropped` from any state but `done` and `dropped` |
| `override-urgency` | setting and withdrawing an urgency override |
| `interest` | a `need` or `urgent` stake |
| `upload` | uploading an attachment |
| `create-project` | creating a project |
| `record-answer` | answering a question, which for an agent writes its person's answer down: the answer is marked `recorded_by_agent`, and an agent changes only an answer an agent recorded |
| `rank` | a move in the rank (`moveTicketRank`); adopting the score is not built |

Without a capability, an agent with `write` scope whose person is a member has the baseline
(ADR 0043 D2, as the handlers build it): filing a ticket and editing its fields and its body,
comments, links, questions, the progress stages short of the done act, a `watch` stake, the
transitions `filed → analysed`, `decided → in-progress`, `in-progress → review`, into `blocked`
and back, and the acts of H-6. An agent's urgency override needs a reason as well as
`override-urgency` (`400` without one; a person may leave it out,
[ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3).

| Hard-off rule ([`auth/authorize.go`](../../backend/internal/auth/authorize.go)) | Refuses |
|---|---|
| administration | the tenant's settings, archiving a project, the tenant's local accounts, its members' grants, its group mappings, a project's restriction and access list |
| booking time | booking, editing and voiding time entries — refused before the `Idempotency-Key` is looked at |
| overriding the prerequisite refusal | `override_prerequisites` on the done act: a transition to `done`, or the `PATCH` that fills the last progress stage |
| setting or lifting the confidential flag | `PUT …/confidential` |
| token administration | revoking another token of the person |
| deleting, restoring or purging | `DELETE …/{number}`, `PUT …/deleted-tickets/{key}/restore`, `DELETE …/deleted-tickets/{key}` ([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md) D7) — the purge refuses an agent before this rule is reached: a token by `session_required`, a marked session as [above](#what-only-a-session-does) |

Four rules live in the handlers and answer `agent_forbidden` with their own detail: an agent
edits or withdraws only comments an agent of the same person wrote
([ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D4), it
withdraws only questions an agent asked, it changes only an answer an agent recorded, and with
`close` it closes a ticket from `in-progress` or `review` only, so that `close` never stands in
for `decide` (ADR 0043 D4).

## An agent's POST carries an Idempotency-Key

Every `POST` of an agent except a transition must carry an `Idempotency-Key`, or it is refused
with `400 idempotency_key_required` ([`api/server.go`](../../backend/internal/api/server.go)
`keyed`): filing a ticket, creating a project, a comment, a question, an upload — and booking
time, which is refused earlier. A transition is idempotent by the state it names as `from`; a
key sent with one is recorded on the act and not stored
([ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D2, D7). The key belongs to the token and is bound to a fingerprint — an HMAC under a key derived from
the server key — of the operation, its path and its body — for an upload the file's SHA-256, its name and its comment. The response is
stored with the act in the same transaction for twenty-four hours; a repetition replays it,
and the same key with a different request answers `422 idempotency_mismatch` (ADR 0045 D3,
D4). A person's `POST` may carry a key and need not. A key sent in a browser session has no
token to belong to and belongs to the person; a stored answer never holds a secret, so the
token-creating route stores its answer without the plaintext.

## What this does not cover

<a id="h-6"></a>
### H-6 — Some agent acts are open by default and await review

Live today. ADR 0043 gives an agent a baseline, nine capabilities and a hard-off list; the
acts below are on none of them and are built allowed, each recorded with the agent mark and
the capability set that applied:

- **Reassigning a confidential ticket.** Assignment admits the new assignee
  ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
  D9). An agent of a person who sees the ticket can assign it to anyone in the tenant who sees
  its project, and the disclosure cannot be taken back
  ([`api/tickets.go`](../../backend/internal/api/tickets.go) `applyRelations`).
- **Removing a `blocks` link.** An agent with `close` cannot override the prerequisite
  refusal, but it can remove the open `blocks` links into its ticket and then close it: two
  calls around a hard-off rule ([`api/links.go`](../../backend/internal/api/links.go)
  `UnlinkTickets`).
- **Backward moves and reopens.** `in-progress → decided` or `→ analysed`,
  `decided → analysed`, `review → in-progress`, `dropped → filed`, the withdrawal of a done by
  hand and the `PATCH` that lowers a stage of a ticket done by its stages need no capability,
  only a reason, so a token without `decide` and `close` — the "assisted" set — can undo what
  its person kept for themselves, a person's close included
  ([`api/transitions.go`](../../backend/internal/api/transitions.go) `checkTransition`,
  [`api/tickets.go`](../../backend/internal/api/tickets.go) `stageInputs`).
- **Removing its person's stake**, or lowering it to `watch`, whatever weight the person gave
  it; only setting `need` or `urgent` needs `interest`
  ([`api/interest.go`](../../backend/internal/api/interest.go)).
- **Editing an open question its person asked** — its text, options, recommendation and the
  person asked — also one the person asked without an agent, while withdrawing a question is
  limited to what an agent asked ([`api/questions.go`](../../backend/internal/api/questions.go)
  `mayEdit`).
- **Editing a project's** name, description and WIP limits, while creating one needs
  `create-project` ([`api/projects.go`](../../backend/internal/api/projects.go) `edit`).
- **Saving, changing, sharing and deleting its person's saved filter** ([`api/filters.go`](../../backend/internal/api/filters.go)):
  a shared filter shows every member of the tenant its name and conditions, with the person as its
  owner.

The integration tests assert the first six as allowed and an agent's saved filter as its marked act, so closing one is a deliberate change. Nobody can switch them off per installation; a person who wants none of them gives an
agent a `read` token, and an administrator finds them in the tenant's audit view by token.

<a id="h-7"></a>
### H-7 — An open event stream outlives a revocation by up to one heartbeat

Live today. A stream checks its token — or its session — and its person's memberships at every
heartbeat, every twenty seconds; the interval is not configurable. Between two heartbeats a
stream whose token was revoked or expired, whose session ended or whose person was deactivated
still receives the events its filters admitted at the last heartbeat — the keys, versions and
kinds of the acts, no content, of every tenant a person-level stream follows. A person who leaves a
tenant or loses a project by an act — a grant removed, a membership derived away, a restriction, an
access entry — loses it at the stream before that tenant's next event, because every such act makes
the stream compute its filter of the tenant again
([tenancy.md](tenancy.md#the-event-stream-carries-what-its-subscriber-could-read),
[the person-level stream](tenancy.md#the-person-level-stream)); only such a change made in the
database past the API waits for the heartbeat. Every request the
client makes with the dead token or session is refused at once; the window is the stream's alone.

<a id="h-30"></a>
### H-30 — With the server key, an audit row's address hash gives the address back

Live today, on every row that carries a source hash. The hash keeps the client's address out of the
record against whoever reads the database, a dump or a backup alone. Whoever also holds
`COWORK_SESSION_KEY` derives the key and computes the hash of any address they guess: an IPv4
address is one of 2^32, a search one machine works through, so every IPv4 hash in the record
reverses — and the record keeps its rows for good
([ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D7). An IPv6
address lies beyond trying them all, but a guessed one — an office's network, a known client — is
confirmed with one computation. The login throttle's hashes have the same property and live fifteen
minutes ([trust-boundaries.md](trust-boundaries.md#where-the-credentials-live)). The hash is a
pseudonym against a copy of the database, not against whoever runs the installation. Mitigation:
guard the server key as the credential it is, and keep it out of the backups of the database.

<a id="h-49"></a>
### H-49 — A token's name is readable by everyone who reads its acts

Live by design, the owner's choice
([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D6). Until it, a token's name was its person's alone: the tokens policy shows a person their own
tokens. Now every act made through a token carries its name, and whoever reads the act reads it —
every member who can see the ticket, a viewer included, the tenant's administrators, and the agent
of any of them that reads the ticket's parts through the API. A name that says more than what the
token is for — a client, a host, a project nobody else is meant to know of — says it to all of
them, and keeps saying it after the token is revoked, because the acts keep the name. The token's
form says so under the name, and the token page says that what a token does is marked with it.
Mitigation: name a token for its use.

<a id="h-50"></a>
### H-50 — Some views of an act through a token show the person alone

Live today, in two places. **Rows written before migrations 27 and 28**: an audit row of that time
names its token's id and not its name, so the activity shows the act as made through a token it
cannot name; a comment, a file, a question, a time entry, a filing or a stake of that time carries
no mark, and a plain token's act there reads as its person's — the activity still marks the act,
except a booking, which the activity leaves out, and the tenant's audit view filters the record by
token. Nothing is backfilled. **Two fields of the API**: a link's `created_by` and an urgency
override's `by` name the person whether the person, an agent or a plain token made it; no view of
the UI shows either, and the act behind each — `linked`, `overridden` — is marked in the activity.
An image rolled back to the release before migration 27 writes no mark on any of these rows.
Mitigation: the activity, and the tenant's audit view by token.

<a id="h-57"></a>
### H-57 — A tenant's administrators read an unrestricted token's metadata and end it everywhere

Live by design, the owner's choice of 2026-10-05
([ADR 0035](../adr/0035-personal-access-tokens.md) D5). An unrestricted token reaches every tenant
its person belongs to, so it is in the token list of each of them. **What its administrators read
of it is the person's across their tenants**: the token's name — free text, which may name a client,
a host or a project of another tenant ([H-49](#h-49)) — and its last-used day, which may be a day it
was used only in another tenant, so an administrator learns that the person worked with it
somewhere that day. **What its administrators do to it reaches the person's other tenants**: a
revocation ends it there too, and is recorded in the revoking tenant's audit alone; the other
tenants' administrators see their requests with it refused, and their own token list shows it
revoked, but not by whom or why. A token restricted to a tenant shows in that tenant's list only.
Mitigation: a token for one tenant's work is restricted to that tenant, and named for its use; a
person who works for several clients keeps an unrestricted token for nothing a client's
administrator should not see or end.

