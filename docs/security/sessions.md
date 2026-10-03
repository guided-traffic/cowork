# Browser sessions

What a browser session is, how a request is resolved to one, what it may do, how it ends and
what is recorded, as built on 2026-10-03. How a password becomes a session — the login, the
lockout, the accounts — is [local-accounts.md](local-accounts.md); what keeps another site from
writing with a session is [csrf.md](csrf.md); what a personal access token may do is
[tokens.md](tokens.md).

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
  whoever's it was (D5).

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

## What a session may do

A session acts as its person with no agent flag and with the person's whole role: it has no
scope of its own, which the pipeline writes as the scope `admin`, the one that leaves every
decision to the person's role in the tenant ([ADR 0035](../adr/0035-personal-access-tokens.md)
D3, [tokens.md](tokens.md)). No agent rule applies to it: the session path does not read
`X-Cowork-Agent`, so nothing in a session's request marks an agent
([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D6).

- **Six routes take a session only** and answer a token `403 session_required`
  ([ADR 0035](../adr/0035-personal-access-tokens.md) D5, [ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)
  D1, D5): creating a token (`POST /api/v1/me/tokens`), creating a tenant
  (`POST /api/v1/tenants`), creating a local account (`POST …/accounts`), resetting its
  password (`PUT …/accounts/{username}/password`), changing one's own password
  (`PUT /api/v1/me/password`) and logging out (`POST /auth/logout`). The first four make
  something that would outlive the revocation of a leaked token. The API document declares them
  with `sessionCookie` alone, and a unit test over the document holds the set to exactly these
  six ([`backend/api/document_test.go`](../../backend/api/document_test.go)). Every other
  operation that names a person takes either credential.
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
| `POST /auth/logout` | the request's own | [`api/login.go`](../../backend/internal/api/login.go) `Logout` |
| a login that presents a cookie | that cookie's | `store.CreateSession` |
| `PUT /api/v1/me/password` | every other session of the person | `ChangeMyPassword` |
| an administrator's reset of a managed account's password, `DELETE …/accounts/{username}/sessions`, `PUT …/deactivation` | every session of the account | [`api/accounts.go`](../../backend/internal/api/accounts.go) |
| the start-up synchronisation, when the configured password changed or the account is deactivated | every session of the local administrator | [`bootstrap/bootstrap.go`](../../backend/internal/bootstrap/bootstrap.go) |
| the idle or the absolute limit | the one past it, refused at its next request; the job removes the row | `sessionLive`, `ExpireSessions` |

## What is recorded

Login, logout and each of the ends above are audit rows with the person and the cause —
`logged_in`, `logged_out`, `password_changed`, `password_reset`, `revoked` with the count of
sessions ended, `deactivated`, and the job's `expired`
([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D7). They are
installation-level rows with no `token_id`, the mark of a browser session
([ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D1); a
tenant administrator's act on a managed account is a row of the tenant. **The cookie, its hash
and the row's id are in no audit row and no log line**: the request log carries method, path,
status, duration and the request id, and `TestNoPasswordCookieOrTokenIsLoggedOrRecorded`
records every log level through a login, a password change, an administrator's reset and a
logout and searches the log, the answers and every table of the login for the cookies. The
session table is not tenant-bound
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D6):
its policies admit a person's own rows, the one row of the cookie presented, the
administrators of a managed account, a global administrator for reading (no route reads them
for a global administrator yet), and the two jobs that end sessions by name
(`TestPoliciesOfTheSessions`).

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
restarting. Shorter limits (`COWORK_SESSION_LIFETIME`, `COWORK_SESSION_IDLE`) shrink the
window; a TLS-terminating proxy that does not log headers keeps the cookie off its disk.

Not built, and not a gap of its own: the groups snapshot, the groups refresh and the issuer's
`end_session_endpoint` of ADR 0031 D1, D4 belong to the identity provider; the lifetimes are
the installation's, not a tenant's. Not verified: that Safari stores a `Secure` cookie from
`http://localhost` — Chromium and Firefox do; the integration tier tests the rule and sets the
cookie by hand, it runs no browser.
