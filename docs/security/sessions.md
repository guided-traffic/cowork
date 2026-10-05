# Browser sessions

What a browser session is, how a request is resolved to one, how a session of the identity
provider keeps up with the person's groups, what a session may do, how it ends and what is
recorded, as built on 2026-10-04. How a password becomes a session — the login, the lockout, the
accounts — is [local-accounts.md](local-accounts.md); how a login through the identity provider
does, and what its groups decide, is [identity-provider.md](identity-provider.md); what keeps
another site from writing with a session is [csrf.md](csrf.md); what a personal access token may do
is [tokens.md](tokens.md).

## A session is a cookie and a row

A login ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D1, D2) makes a
random value of 256 bits, stores its SHA-256 and sends the value once, as the cookie
`__Host-cowork-session` with `HttpOnly; Secure; SameSite=Lax; Path=/`, no `Domain` and a
`Max-Age` of the session's absolute lifetime
([`api/session.go`](../../backend/internal/api/session.go) `sessionCookie`;
[`auth/session.go`](../../backend/internal/auth/session.go) `GenerateSession`;
`TestLocalLoginStartsASession`, `TestSessionCookieAttributes`).

- **Only the hash is stored.** The `sessions` row holds the person, `token_hash` (unique, 32
  bytes), `user_agent_hash`, and three times the backend wrote from its own clock: `created_at`,
  `last_seen_at` and `expires_at`, the absolute limit
  ([migration 16](../../backend/internal/store/migrations/000016_sessions.up.sql)). A copy of
  the table gives nobody a cookie; the value is in no column, which the integration test
  asserts by searching the row's text for it.
- **The prefix does what the record asks of the cookie.** `__Host-` makes a browser refuse the
  cookie unless it is `Secure`, has `Path=/` and no `Domain` — and refuse it from a sibling
  subdomain. `Secure` is set in every environment; browsers treat `localhost` as a secure
  context, so development needs no exception. A browser does not store the cookie from a plain
  `http://` page that is not `localhost`: an installation without TLS in front cannot log in.
- **A session is made at login and never before, and its value is never reused.** A login
  that presents a session cookie ends that session in the transaction that makes the new one,
  whoever's it was (D5) — the local login and the identity provider's alike.
- **A session of the identity provider holds more** (`method` `oidc`, where the local login's is
  `local`; [migration 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)):
  the groups of its login or last refresh and when they were read (`groups`,
  `groups_refreshed_at`), the issuer's refresh token sealed for this session alone
  (`refresh_token_sealed`, [identity-provider.md](identity-provider.md#what-cowork-keeps-of-the-issuers-tokens)),
  and the earliest next refresh after the issuer could not be reached (`refresh_retry_at`). No
  token of the issuer reaches the browser.

## One resolver for a cookie and a token

`authenticate` in [`api/authn.go`](../../backend/internal/api/authn.go) resolves a request to
its person ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D6). The
operation's own security requirement in the API document says which credentials it takes
(`credentialsOf`); everything after the resolver — the tenant boundary, the role, the
predicates — is the code a token's request runs.

- **A request with an `Authorization` header is a token's, whatever else it carries.** Its
  cookie is not looked at. A token and a cookie in one request therefore never combine, a
  token's request is never held to the CSRF check that belongs to cookies, and a cookie never
  rides on a token's authority (`TestSessionWritesAreCSRFChecked`: a viewer's cookie beside a
  member's token writes as the member).
- **Otherwise the cookie decides**, on an operation that takes one. Malformed, unknown, ended,
  past a limit, its person deactivated: the same `401 unauthenticated`, with
  `WWW-Authenticate: Bearer realm="cowork"`, and a `Set-Cookie` that tells the browser to drop
  the cookie. One indexed lookup finds the row; its transaction names the cookie's hash in
  `app.session_hash`, which the sessions policy admits exactly that row for, and reads the
  person once the row names it ([`store/sessions.go`](../../backend/internal/store/sessions.go)
  `LookupSession`; `TestSessionLookupFindsThePresentedRowOnly`).
- **Two limits** ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D3):
  the absolute one, `COWORK_SESSION_LIFETIME` (12 hours) from the login, and the idle one,
  `COWORK_SESSION_IDLE` (2 hours) from the last use. A request moves the idle clock — at most
  once a minute, `store.SessionTouchInterval`, so the idle limit is exact to the minute and a
  busy page costs one write a minute — and never past the absolute limit. The backend's clock
  decides both (`sessionLive`; `TestSessionLifetimes` moves a fake clock past each).
- **The job** `session-expiry` removes the rows past a limit, at start and hourly under its
  own advisory lock, and records one `expired` act by `system:session-expiry` when it removed
  any. It keeps the table small; it enforces nothing, because a session past a limit is refused
  at its next request whether or not the job has run.

## A session of the identity provider keeps up with the groups

A session the identity provider's login made reads the person's groups again every
`COWORK_OIDC_GROUPS_REFRESH` (fifteen minutes)
([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D5, [ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D1, D3): on its first
request after the interval, inside the resolver, before the request is served — the resolver then
reads the session and its person again, because the refresh may have changed the person's
administrator flag — and at an open event stream's heartbeat, which does not move the idle clock
([`api/session.go`](../../backend/internal/api/session.go) `authenticateSession`,
[`api/identity.go`](../../backend/internal/api/identity.go) `checkProviderSession`,
`refreshSession`, `streamStillAdmitted`). One request claims the refresh with a thirty-second lease
on the session's row and asks the issuer, with the session's refresh token, holding no database
connection and no lock; the session's other requests meanwhile are served at once on the groups it
holds. What the issuer answers decides:

- the groups, judged by the gate: inside, the session goes on with them; outside, **every** session
  of the person ends at once;
- a refusal of the refresh token — an OAuth error answer about the person — a refreshed ID token that
  does not verify, or a token that no longer opens because the server key changed — no previous key
  is kept to open it ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D1; what
  else a change of the key does: [installation.md](../operations/installation.md#the-secrets)):
  **this** session ends, and the request is `401` like any ended session's;
- no answer, a timeout, a `5xx`, a `429`, a temporary OAuth error, or the issuer refusing cowork's
  own client: the session is served on the groups it holds, and asks again a minute later.

A session without a refresh token, or whose issuer sends no groups at a refresh, is judged on the
person's groups as they stand, and nothing of the person changes. A person who is not the configured
issuer's — another issuer's, or anyone's of a provider no longer configured — loses every session at
the first request of any. What the refresh writes, records and leaves open is
[identity-provider.md](identity-provider.md#the-groups-refresh) and its H-24, H-25 and H-27. A
session of the local login has no groups and no refresh.

## What a session may do

A session acts as its person with no agent flag and with the person's whole role: it has no
scope of its own, which the pipeline writes as the scope `admin`, the one that leaves every
decision to the person's role in the tenant ([ADR 0035](../adr/0035-personal-access-tokens.md)
D3, [tokens.md](tokens.md)). No agent rule applies to it — unless its request carries
`X-Cowork-Agent`: the header marks that request as an agent's, with every capability and every
agent rule, and only narrows it; a malformed one is `400`
([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3 as amended; `authenticateSession` in [`api/session.go`](../../backend/internal/api/session.go)).
The chat in the UI marks its tool calls so ([chat.md](chat.md)); a request without the header is the
person's.

- **Seventeen routes take a session only** and answer a token `403 session_required`
  ([ADR 0035](../adr/0035-personal-access-tokens.md) D5, [ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)
  D1, D5): creating a token (`POST /api/v1/me/tokens`), creating a tenant
  (`POST /api/v1/tenants`), creating a local account (`POST …/accounts`), resetting its
  password (`PUT …/accounts/{username}/password`), changing one's own password
  (`PUT /api/v1/me/password`), logging out (`POST /auth/logout`), the six administration acts
  that can give access — adding a member, setting a grant, making or changing a group mapping,
  restricting or opening a project, putting a person on a project's access list —, a turn of the
  chat (`POST …/chat`) and stopping the person's turns (`DELETE …/chat/turns`), choosing the chat's
  capabilities (`PUT /api/v1/me/chat`), a global administrator's list of every tenant
  (`GET /api/v1/tenants`), and purging a deleted ticket (`DELETE …/deleted-tickets/{key}`,
  [ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
  D7). What the first twelve and the chat's capabilities make, and what a purge destroys, would
  outlive the revocation of a leaked token, a turn acts with the person's session and its stop ends the session's
  person's turns, and the list shows a global administrator's view across the installation's
  clients, which a token of theirs does not get; the table and the rule are
  [tokens.md](tokens.md#what-only-a-session-does). The API document declares them with
  `sessionCookie` alone, and a unit test over the document holds the set to exactly these seventeen
  ([`backend/api/document_test.go`](../../backend/api/document_test.go)). A session's request the
  agent header marks is refused all seventeen with `403 agent_forbidden`. Every other operation
  that names a person takes either credential.
- **A turn of the chat presents the session again with every tool call.** Each call is a request of
  its own through the whole pipeline with the person's cookie ([chat.md](chat.md)): it is
  authenticated anew, moves the idle clock like any request and may run the groups refresh, and a
  session that ends while a turn runs refuses the turn's next call, which the model reads as a
  failed call.
- **A temporary password gates the session.** While the account's password is one an
  administrator set, the session may read `GET /api/v1/me`, change the password and log out;
  every other route is `403 password_change_required`
  ([ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)
  D4; `sessionRules` in [`api/api.go`](../../backend/internal/api/api.go)). A token the person
  already holds is not gated: it never had the password.
- **Its writes are CSRF-checked** ([csrf.md](csrf.md)).
- **An open event stream checks its session at every heartbeat** — the session row exists,
  neither limit has passed, the person is not deactivated — and ends when one fails
  ([`api/events.go`](../../backend/internal/api/events.go) `stillAdmitted`;
  `TestEventStreamEndsWithItsSession`). An open stream does not extend the idle limit: a tab
  that is only open logs out.

## How a session ends

Revocation is a delete and is immediate: the next request with the cookie is `401`
([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D4). In the browser, a
sign-in and a sign-out replace the document, so nothing the previous person loaded — their tokens,
a tenant's accounts, cached tickets — stays in memory for the next person in the same tab
([frontend.md](../developer/frontend.md#where-state-lives)).

| What | Which sessions | Where |
|---|---|---|
| `POST /auth/logout` — for a session of the identity provider, with the issuer's logout handed to the browser where the issuer names one ([identity-provider.md](identity-provider.md#logout)) | the request's own | [`api/login.go`](../../backend/internal/api/login.go) `Logout` |
| a login that presents a cookie | that cookie's | `store.CreateSession`, `store.CompleteOIDCLogin` |
| `PUT /api/v1/me/password` | every other session of the person | `ChangeMyPassword` |
| an administrator's reset of a managed account's password, `DELETE …/accounts/{username}/sessions`, `PUT …/deactivation` | every session of the account | [`api/accounts.go`](../../backend/internal/api/accounts.go) |
| the start-up synchronisation, when the configured password changed or the account is deactivated | every session of the local administrator | [`bootstrap/bootstrap.go`](../../backend/internal/bootstrap/bootstrap.go) |
| a groups refresh, or a login refused at the gate, that finds the person outside the identity provider's gate | every session of the person | [`store/identity.go`](../../backend/internal/store/identity.go) `ApplySessionRefresh`, `CompleteOIDCLogin` |
| a request of a session whose person is not the configured issuer's — another issuer's, or a provider no longer configured | every session of the person, at once | `EndProviderSessions` |
| the issuer refuses the session's refresh token, a refreshed ID token does not verify, or the token no longer opens because the server key changed | that session, at its refresh | `ApplySessionRefresh` |
| the idle or the absolute limit | the one past it, refused at its next request; the job removes the row | `sessionLive`, `ExpireSessions` |

## What is recorded

Login, logout and each of the ends above are audit rows with the person and the cause —
`logged_in` (with the note `oidc` for the identity provider's), `logged_out`, `password_changed`,
`password_reset`, `revoked` with the count of sessions ended — by `system:identity-provider` with
the cause `gate` or `identity-provider` for the ends it decides —, `deactivated`, and the job's
`expired` ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D7). They are
installation-level rows with no `token_id`, the mark of a browser session
([ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D1); a
tenant administrator's act on a managed account is a row of the tenant. Each row written for a
request carries the keyed hash of the client's address ([tokens.md](tokens.md#what-is-recorded)).
**The cookie, its hash
and the row's id are in no audit row and no log line**: the request log carries method, path,
status, duration and the request id, and `TestNoPasswordCookieOrTokenIsLoggedOrRecorded`
records every log level through a login, a password change, an administrator's reset and a
logout and searches the log, the answers and every table of the login for the cookies. The
session table is not tenant-bound
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D6):
its policies admit a person's own rows, the one row of the cookie presented, the
administrators of a managed account, a global administrator for reading (no route reads them
for a global administrator yet), and the two jobs that end sessions by name
(`TestPoliciesOfTheSessions`). The identity provider's transactions, and the refresh's claim, which
runs as the person, reach a session through the first two: they name the person and the session's
hash. The runtime role may update the idle clock
and, for the groups refresh, the groups, their time, the sealed refresh token and the retry time —
nothing else of a row (migrations [16](../../backend/internal/store/migrations/000016_sessions.up.sql)
and [20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)).

## What this does not cover

<a id="h-15"></a>
### H-15 — A stolen session cookie is a session until it ends

Live today. The cookie is the whole credential: nothing binds a session to the browser that
logged in. `HttpOnly` keeps a script in the page from reading it, and `Secure` and the
`__Host-` prefix keep it off plain HTTP and away from sibling subdomains; but whoever obtains
the value — from a compromised machine, from a proxy in front that logs request headers, from
a browser extension — presents it from anywhere and acts as the person until the idle limit
(two hours without a request), the absolute limit (twelve hours) or an end. The row holds the
SHA-256 of the `User-Agent` it was made with (`sessions.user_agent_hash`) and nothing compares
it. A person cannot list their sessions, and ends the others only by changing their password;
an administrator ends a managed account's through `DELETE …/accounts/{username}/sessions`, and
no one but the operator ends the local administrator's — by rotating its Secret and
restarting. A person of the identity provider has no password to change (`403 forbidden`) and no
account an administrator manages: their other sessions end only at their limits, or at a refresh,
when the issuer refuses the refresh token or the gate no longer admits them — at the issuer,
disabling the person or taking them out of the allowed groups is the way. Shorter limits (`COWORK_SESSION_LIFETIME`, `COWORK_SESSION_IDLE`) shrink the
window; a TLS-terminating proxy that does not log headers keeps the cookie off its disk.

The gaps of the identity provider's sessions — stale groups while the issuer cannot be reached, a
session that never learns the groups anew without a refresh token, the stored refresh tokens and the
issuer's own session after a logout — are [identity-provider.md](identity-provider.md) H-24, H-25,
H-27 and H-28. Not a gap of its own: the lifetimes are the installation's, not a tenant's. Not
verified: that Safari stores a `Secure` cookie from `http://localhost` — Chromium and Firefox do;
the integration tier tests the rule and sets the cookie by hand, it runs no browser.
