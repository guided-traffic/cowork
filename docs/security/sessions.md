# Browser sessions

What a browser session is, how a request is resolved to one, how a session of the identity
provider keeps up with the person's groups, what a session may do, how it ends, what keeps it and
what brings a person back after it ended, and what is recorded, as built on 2026-10-09. How a
password becomes a session — the login, the lockout, the accounts — is
[local-accounts.md](local-accounts.md); how a login through the identity provider
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
  subdomain. `Secure` is set in every environment, with no exception for development: a browser
  does not store the cookie from a plain `http://` page that is not `localhost`, so an installation
  without TLS in front cannot log in, and WebKit, Safari's engine, does not store it from
  `http://localhost` either — measured with Playwright on 2026-10-03, while Chromium does —, so
  `make dev` serves HTTPS and the end-to-end tier puts TLS in front of the images
  ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D2).
- **A session is made at login and never before, and its value is never reused.** A login
  that presents a session cookie ends that session in the transaction that makes the new one,
  whoever's it was (D5) — the local login and the identity provider's alike.
- **A session of the identity provider holds more** (`method` `oidc`, where the local login's is
  `local`; [migration 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)):
  the groups of its login or last refresh and when they were read (`groups`,
  `groups_refreshed_at`), the issuer's refresh token sealed for this session alone
  (`refresh_token_sealed`, [identity-provider.md](identity-provider.md#what-cowork-keeps-of-the-issuers-tokens)),
  and the earliest next refresh — a minute after the issuer could not be reached, or thirty seconds
  ahead while a refresh is under way, its lease (`refresh_retry_at`). No
  token of the issuer reaches the browser.

## One resolver for a cookie and a token

`authenticate` in [`api/authn.go`](../../backend/internal/api/authn.go) resolves a request to
its person ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D6). The
operation's own security requirement in the API document says which credentials it takes
(`credentialsOf`); everything after the resolver — the team boundary, the role, the
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
  `COWORK_SESSION_IDLE` (2 hours) from the session's last request. **Every request of the session
  moves the idle clock but a write the CSRF check refuses**
  ([below](#what-keeps-a-session-and-what-brings-a-person-back);
  [`api/session.go`](../../backend/internal/api/session.go) `movesIdleClock`;
  `TestWhatMovesTheIdleClock`, `TestEveryRequestButARefusedWriteMovesTheIdleClock`). Such a request
  moves it at most once a minute, `store.SessionTouchInterval`, so the idle limit is exact to the minute
  and a busy page costs one write a minute, and never past the absolute limit. The backend's clock
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
request after the interval, inside the resolver, before the request is served — and the resolver
reads the session and its person again after that check, refresh or not, because a refresh may have
changed the person's administrator flag, so a request of a session of the identity provider costs two
lookups — and at an open event stream's heartbeat, which does not move the idle clock
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
decision to the person's role in the team ([ADR 0035](../adr/0035-personal-access-tokens.md)
D3, [tokens.md](tokens.md)). No agent rule applies to it — unless its request carries
`X-Cowork-Agent`: the header marks that request as an agent's, with every agent rule and the
capabilities the person chose for the chat, read anew for each such request — every capability is
what the header gives a plain token's request, never a session's —, and only narrows it; a malformed
one is `400`
([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3 as amended; `authenticateSession` in [`api/session.go`](../../backend/internal/api/session.go)).
The chat in the UI marks its tool calls so ([chat.md](chat.md)); a request without the header is the
person's.

- **Nineteen routes take a session only** and answer a token `403 session_required`
  ([ADR 0035](../adr/0035-personal-access-tokens.md) D5, [ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)
  D1, D5): creating a token (`POST /api/v1/me/tokens`), creating a team
  (`POST /api/v1/teams`), creating a local account (`POST …/accounts`), resetting its
  password (`PUT …/accounts/{username}/password`), unlocking it
  (`DELETE …/accounts/{username}/lockout`), changing one's own password
  (`PUT /api/v1/me/password`), logging out (`POST /auth/logout`), the six administration acts
  that can give access — adding a member, setting a grant, making or changing a group mapping,
  restricting or opening a project, putting a person on a project's access list —, a turn of the
  chat (`POST …/chat`) and stopping the person's turns (`DELETE …/chat/turns`), choosing the chat's
  capabilities (`PUT /api/v1/me/chat`), a global administrator's list of every team
  (`GET /api/v1/teams`), purging a deleted ticket (`DELETE …/deleted-tickets/{key}`,
  [ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
  D7), and removing the orphaned objects of a consistency check
  (`POST …/attachment-consistency/orphan-removal`,
  [ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
  D4). What creating a token, a team or an account, the two password acts, the six administration
  acts and choosing the chat's capabilities leave behind, what a purge
  or a removal destroys, and a lockout an unlock undoes, would outlive the revocation of a leaked
  token; a logout has no session of a
  token's to end, a turn acts with the person's session and its stop ends the session's person's
  turns, and the list shows a global administrator's view across the installation's
  clients, which a token of theirs does not get; the table and the rule are
  [tokens.md](tokens.md#what-only-a-session-does). The API document declares them with
  `sessionCookie` alone, and a unit test over the document holds the set to exactly these nineteen
  ([`backend/api/document_test.go`](../../backend/api/document_test.go)). A session's request the
  agent header marks is refused all nineteen with `403 agent_forbidden`. Every other operation
  that names a person takes either credential. Three acts of those
  operations take a session in their giving direction alone — widening the team's settings,
  lifting the confidential flag, assigning a confidential ticket to another person — and refuse a
  token `403 session_required` in the handler
  ([tokens.md](tokens.md#acts-that-take-a-session-in-their-giving-direction)).
- **A turn of the chat presents the session again with every tool call.** Each call is a request of
  its own through the whole pipeline with the person's cookie ([chat.md](chat.md)): it is
  authenticated anew — each call moves the idle clock like any request, as the turn's own `POST`
  did when the person sent it — and may run the groups
  refresh, and a session that ends while a turn runs refuses the turn's next call, which the model
  reads as a failed call.
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
  `TestEventStreamEndsWithItsSession`). Its heartbeats do not extend the idle limit; its
  connection does, a reconnect included, like the reloads its events or its polling fallback
  trigger — each a request of the session — so a tab that is only open can stay signed in up to the
  absolute limit ([H-109](#h-109); `TestEveryRequestButARefusedWriteMovesTheIdleClock`).

## How a session ends

Revocation is a delete and is immediate: the next request with the cookie is `401`
([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D4). In the browser, a
sign-in and a sign-out replace the document, so nothing the previous person loaded — their tokens,
a team's accounts, cached tickets — stays in memory for the next person in the same tab
([frontend.md](../developer/frontend.md#where-state-lives)). A session that a limit or another end
takes away is not one of them: the page goes to the login page by a route change ([H-92](#h-92)).

| What | Which sessions | Where |
|---|---|---|
| `POST /auth/logout` — for a session of the identity provider, with the issuer's logout handed to the browser where the issuer names one ([identity-provider.md](identity-provider.md#logout)) | the request's own | [`api/login.go`](../../backend/internal/api/login.go) `Logout` |
| a login that presents a cookie | that cookie's | `store.CreateSession`, `store.CompleteOIDCLogin` |
| `PUT /api/v1/me/password` | every other session of the person — and a login that verified the old password and has not made its session yet makes none, as for every change below ([local-accounts.md](local-accounts.md#what-the-login-answers)) | `ChangeMyPassword`; `store.CreateSession` |
| an administrator's reset of a managed account's password, `DELETE …/accounts/{username}/sessions`, `PUT …/deactivation` | every session of the account | [`api/accounts.go`](../../backend/internal/api/accounts.go) |
| the start-up synchronisation, when the configured password changed or the account is deactivated | every session of the local administrator | [`bootstrap/bootstrap.go`](../../backend/internal/bootstrap/bootstrap.go) |
| a groups refresh, or a login refused at the gate, that finds the person outside the identity provider's gate | every session of the person | [`store/identity.go`](../../backend/internal/store/identity.go) `ApplySessionRefresh`, `CompleteOIDCLogin` |
| a request of a session whose person is not the configured issuer's — another issuer's, or a provider no longer configured | every session of the person, at once | `EndProviderSessions` |
| the issuer refuses the session's refresh token, a refreshed ID token does not verify, or the token no longer opens because the server key changed | that session, at its refresh | `ApplySessionRefresh` |
| the idle or the absolute limit | the one past it, refused at its next request; the job removes the row | `sessionLive`, `ExpireSessions` |

## What keeps a session, and what brings a person back

**Every request of a session keeps it, but a write the CSRF check refuses**
([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D3 as amended 2026-10-07;
[`api/session.go`](../../backend/internal/api/session.go) `movesIdleClock`). Every read moves the
idle clock — a page that loads what it shows, the event stream's connections and reconnects, its
polling fallback's reloads, the reloads an event triggers —, and so does every write that passes
the CSRF check, at most once a minute. The UI sends nothing of its own to keep a session: a person
keeps it with the requests the pages make, and a page that asks nothing for the length of the idle
limit — one ticket read for two hours over a stream that stays connected in a team where nobody
acts — ends at it, as before 0.8.0. **A forged write extends nothing**: a write a page of the same site sends, where `SameSite=Lax`
lets the cookie ride along, fails the CSRF check and leaves the idle clock where it was
(`TestEveryRequestButARefusedWriteMovesTheIdleClock`). A read is a different matter: a link of
another site, or an image or a link on a sibling host, that names an address of the API is a read
of the session — the browser sends the `Lax` cookie with it — and moves the clock like any, a
recorded read that is then refused for the page it came from included
([csrf.md](csrf.md#the-reads-that-record-an-act)). What this leaves open — an open tab nobody uses keeps its session up
to the absolute limit — is [H-109](#h-109), the owner's accepted risk. The absolute limit is
untouched.

**Coming back after a limit ended the session**
([ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D6; [identity-provider.md](identity-provider.md#signing-in-again-without-a-click)). Per case:

| Case | What happens | What ends access |
|---|---|---|
| A session of the identity provider reaches the idle or the absolute limit | cowork's session ends as before — refused at its next request, the row gone with the job. The login page that follows, in a browser that remembers the provider, waits for a sign of a person and then signs them in again with `prompt=none`, without a click, while the provider's own session lives | **The provider's session policy.** An unattended, unlocked browser is one sign of a person away from cowork's content — an input, the window taking the focus, the tab coming back into view —, where it was one click away — the click passed without a password too ([H-62](identity-provider.md#h-62)). An open tab nobody touches does not sign itself in |
| A person signs out | the session ends, and the browser forgets that the person signs in through the provider — before the backend is asked — so the login page never signs them in again by itself | the sign-out, as before; the provider's own session ends only where its end-session endpoint is followed ([H-28](identity-provider.md#h-28)) |
| A local session reaches a limit | it ends; the login page waits for the form: a local sign-in forgets the provider, and the page signs in by itself only for the provider | the limits, as before |
| Another end of the table above — a revocation, a password change, the gate | the session ends; for a session of the provider, the login page's own sign-in meets the issuer and the gate again like any login | what ended it, as before, and the issuer and the gate at the next sign-in |

## What is recorded

Login, logout and the ends above are audit rows with the person and the cause —
`logged_in` (with the note `oidc` for the identity provider's), `logged_out`, `password_changed`,
`password_reset`, `revoked` with the count of sessions ended — by `system:identity-provider` with
the cause `gate` or `identity-provider` for the ends it decides —, and `deactivated`
([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D7). A session a login replaces
is recorded only by the new `logged_in`, and the sessions past a limit by the job's `expired`, which
counts the rows it removed and names no person. They are
installation-level rows with no `token_id`, the mark of a browser session
([ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D1); a
team administrator's act on a managed account is a row of the team. Each row written for a
request carries the keyed hash of the client's address ([tokens.md](tokens.md#what-is-recorded)).
**The cookie, its hash
and the row's id are in no audit row and no log line**: the request log carries method, path,
status, duration and the request id, and `TestNoPasswordCookieOrTokenIsLoggedOrRecorded`
records every log level through a login, a password change, an administrator's reset and a
logout and searches the log, the answers and every table of the login for the cookies. The
session table is not team-bound
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
(two hours without a request, which whoever holds the cookie sends with any read), the absolute
limit (twelve hours) or an end. The row holds the
SHA-256 of the `User-Agent` it was made with (`sessions.user_agent_hash`) and nothing compares
it. A person cannot list their sessions, and ends the others only by changing their password;
an administrator ends a managed account's through `DELETE …/accounts/{username}/sessions` — their
own as well, all of them, when their own account is one their team manages —, and no one but the
operator ends the local administrator's — by rotating its Secret and restarting. A person of the identity provider has no password to change (`403 forbidden`) and no
account an administrator manages: their other sessions end only at their limits, or at a refresh,
when the issuer refuses the refresh token or the gate no longer admits them — at the issuer,
disabling the person or taking them out of the allowed groups is the way; the operator ends each
session of the provider that holds a refresh token, at its next refresh, by changing
`COWORK_SESSION_KEY` ([identity-provider.md](identity-provider.md#h-27) H-27). Shorter limits (`COWORK_SESSION_LIFETIME`, `COWORK_SESSION_IDLE`) shrink the
window; a TLS-terminating proxy that does not log headers keeps the cookie off its disk.

The gaps of the identity provider's sessions — stale groups while the issuer cannot be reached, a
session that never learns the groups anew without a refresh token, the stored refresh tokens, the
issuer's own session after a logout, and the sign-in without a click while that session lives — are
[identity-provider.md](identity-provider.md) H-24, H-25, H-27, H-28 and H-62. Not a gap of its own:
the lifetimes are the installation's, not a team's. Which browser stores a `Secure` cookie from
`http://localhost` is measured, not assumed: Chromium does, WebKit does not
([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D2, with Playwright on
2026-10-03); Firefox was not measured. The integration tier tests the rule and sets the cookie by
hand; it runs no browser.

<a id="h-92"></a>
### H-92 — A session a limit ended leaves the tab's memory as it was

Live in every tab whose session ends while it is open. A `401` of the API sends the page to the login
page by a route change, not a new document
([`core/http.ts`](../../frontend/src/app/core/http.ts) `toSignIn`), so the services of the page keep
what they had loaded — tickets, a team's accounts, the person's tokens' metadata — until the next
sign-in or sign-out replaces the document. Whoever sits at the tab meanwhile reaches that state with
the browser's tools, and a page the router opens from it before a request of its own fails may show
it. Not verified in a browser: which pages show loaded state without asking the API first. A full
navigation on the first `401` would close it. Mitigation: a locked screen; close the tab.

<a id="h-93"></a>
### H-93 — The way back after a sign-in may be a read that records an act

Live today for the local login. The path a sign-in returns to is any path of the installation
([identity-provider.md](identity-provider.md#the-login) `safeReturnTo`, and the login page's
`safeReturn`), the API's included: a link of another site that leads to the login with a way back to an
attachment's bytes or a ticket's export or context makes the browser of the person who signs in fetch
that path with their new session — a read recorded in their name. After a local sign-in the login
page itself navigates there, a `same-origin` request that the recorded reads take
([csrf.md](csrf.md#the-reads-that-record-an-act)). After a sign-in through the identity provider,
a silent one included ([identity-provider.md](identity-provider.md#h-81) H-81), the way back is the
callback's redirect at the end of a chain through the issuer, which by the Fetch Metadata rules makes
the request `cross-site`, and the read is refused `403 csrf` — not verified in a browser. Nothing of
the answer reaches the other site. Refusing a way back under `/api/` and `/auth/` in both functions
would close it.

<a id="h-109"></a>
### H-109 — A tab open with nobody at it keeps its session up to the absolute limit

Live today, in every browser that leaves a page of cowork open; the owner's accepted risk of
2026-10-07 ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D3). Every request
of a session moves its idle clock but a write the CSRF check refuses, and an open tab makes requests
of its own with nobody at it: its event stream connects again after the connection was cut — by the
Ingress controller's timeouts, a restart of a backend pod, a network change of the machine —, its
polling fallback reloads while the stream is down, and its page reloads what an event of another
person's act changed. Whenever one of them falls inside each idle window, the session lives on until
`COWORK_SESSION_LIFETIME` (twelve hours) ends it, and the idle limit, `COWORK_SESSION_IDLE`, guards
only a tab that sends nothing — a stream that stays connected and a team where nobody acts. So an
unattended, unlocked browser with cowork open shows the person's teams to whoever sits at it for
up to the absolute limit, where the idle limit would have ended the session after two hours; for a
person of the identity provider, the login page then signs them in again at the first sign of a
person while the issuer's session lives ([identity-provider.md](identity-provider.md#h-62) H-62).
Not measured in a browser: how often an installation's stream reconnects, which depends on its
controller and its network. Mitigation: a shorter `COWORK_SESSION_LIFETIME`; a locked screen; close
the tab or sign out.
