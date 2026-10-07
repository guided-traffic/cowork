# ADR 0031: Server-Side Sessions in an HttpOnly Cookie — an Opaque Id, a Row in PostgreSQL, Revocation Is a Delete, and the Identity Provider's Tokens Never Reach the Browser

## Status

Accepted, amended 2026-10-03 (D1–D4, D6, D7 made concrete by the first implementation, which
has no identity provider yet; D2's claim that development needs no exception measured false for
Safari, so development serves HTTPS) and 2026-10-04 (D1, D3: the groups snapshot, its refresh and
the sealed refresh token built; D4: the issuer's logout is handed to the browser, not called; D6:
twelve routes take a session only; D7: the identity provider's ends), and again on 2026-10-04 after
the security review (D3: the refresh claims a lease and holds nothing while it asks the issuer; D4:
the sessions of a person of another issuer end at once), and for the chat in the UI (D6: thirteen
routes and the consent field take a session only; the agent header marks a session's request), and
on 2026-10-04 by the owner's answer to "does a change of the server key end the sessions of the
identity provider?" (D1: there is one server key and no rotation that keeps the old one; a change
fails closed, and its consequences are named), and for the global administrator's view of the
installation's tenants (D6: fourteen routes, [ADR 0035](0035-personal-access-tokens.md) D5), and
on 2026-10-04 by the owner's answers on the chat
([ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md); D6:
sixteen routes, no consent field; a session's request the agent header marks holds the person's
chat capabilities; built the same day), and on 2026-10-04 by the owner's decision on the routing
(Context: the one origin is the Ingress's,
[ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3; nothing of the decision changes), and on 2026-10-05 by the decision on the purge, built on the
recommendation
([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D7; D6: seventeen routes), and on 2026-10-06 on the owner's request that cowork keep his session
while he works and not ask for a click where the identity provider's session runs anyway, and by the
coordinator's decision of the same night that the idle limit hold for a tab nobody uses (D3: only
the person's activity — a write, or a read the browser's keep-alive marks after the person's input
— moves the idle clock, replacing "a request within the idle window extends the session"; the
sign-in that follows an ended provider session is
[ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D6). Date: 2026-10-01. Decided by the owner as the answer to the
catalog question "browser session mechanism?": server-side sessions, over the identity
provider's JWT in the browser and over a stateless signed cookie. The rules of D5–D7 were put
to the owner with the question and explicitly confirmed.

**Partly built** (phase 3, 2026-10-03): D1–D7 for the local login of
[ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) — the
`sessions` table (migration 16), the cookie, the resolver, both lifetimes, the `session-expiry`
job, revocation by delete and the audit rows
([`api/session.go`](../../backend/internal/api/session.go),
[`store/sessions.go`](../../backend/internal/store/sessions.go),
[docs/security/sessions.md](../security/sessions.md)). ~~Not built, because they belong to the
identity provider that does not exist yet: D1's groups snapshot, its refresh time and the
encrypted refresh token, D3's refresh, and D4's ends by leaving the allow-list and through the
issuer's `end_session_endpoint`.~~ D7's list of one's own sessions has no route: a person ends
their other sessions by changing their password.

**Built** (phase 4, 2026-10-04): D1's groups snapshot, its time and the sealed refresh token, D3's
refresh, and D4's ends by leaving the allow-list, by the issuer's refusal and through the issuer's
`end_session_endpoint` — the columns of
[migration 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql), the
refresh in [`api/identity.go`](../../backend/internal/api/identity.go) and
[`store/identity.go`](../../backend/internal/store/identity.go), the sealing in
[`auth/seal.go`](../../backend/internal/auth/seal.go)
([docs/security/identity-provider.md](../security/identity-provider.md)). A person of the identity
provider has no password, so no way to end their own other sessions. ~~Not built: D1's rotation that
keeps the old key for decryption — a changed `COWORK_SESSION_KEY` ends every provider session that
holds a refresh token at its next refresh.~~ *(Amended 2026-10-04 by the owner: D1 no longer decides a
rotation that keeps the old key; what a change of the key does is D1's rule, and built.)*

## Context

The UI and the API share one origin ~~behind nginx~~ *(since 2026-10-04 behind the Ingress, which
routes `/api/` and `/auth/` to the backend)* ([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3, D4); people log in through a standard OIDC code flow ([ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md));
their groups are re-evaluated during a session and a person who leaves the allow-list is
logged out at the next request ([ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D5). That last promise decides the mechanism: a token the browser holds cannot be taken back
before it expires, and a stateless cookie cannot either. A product whose access control is
"you are in while your group says so" needs a session the server can end.

## Decision

**D1 — A login creates a session row and a cookie carrying an opaque id.** The row holds the
person, the groups snapshot and the time of the last refresh (ADR 0030 D5), creation and
last-seen times, and a hash of the user agent. The cookie value is 256 random bits; only its
hash is stored. The identity provider's ID and access tokens are verified and discarded; a
refresh token is stored only if the groups refresh needs it, encrypted at rest with a server
key (`COWORK_SESSION_KEY`, a Secret in the chart, ~~rotated by issuing a new key and keeping
the old for decryption until every session that used it is gone~~ *(not built, 2026-10-04: below)*
*(amended 2026-10-04 by the owner, below: one key, no old one kept)*).
*(Amended 2026-10-03: the row
holds the person, `token_hash`, `user_agent_hash` — which nothing compares yet —, and
`created_at`, `last_seen_at` and `expires_at`, three times the backend writes from its own
clock; the groups snapshot arrives with the identity provider. ~~No refresh token exists to
encrypt, so `COWORK_SESSION_KEY` is not used for sessions:~~ it signs the list cursors and,
derived by HKDF with a label of its own, keys the hash of a login's source address
([ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D6).)*
*(Amended 2026-10-04: a session of the identity provider's login has the `method` `oidc` — a local
login's is `local` — the groups of its login or last refresh with their time (`groups`,
`groups_refreshed_at`), and the earliest next refresh after the issuer could not be reached
(`refresh_retry_at`). The refresh token is kept whenever the issuer gives one — the default scopes
ask for `offline_access` ([ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D4) — in `refresh_token_sealed`: AES-256-GCM under a key derived from `COWORK_SESSION_KEY` by
HKDF-SHA256 with the label `cowork oidc refresh token v1`, a random nonce, and the session's cookie
hash as additional data, so it opens for its own session only. **Not built: the rotation that keeps
the old key for decryption.** There is one key and no old one is kept, so a new server key leaves
the stored tokens unopenable, and each such session ends at its next refresh, its person logging in
again — the change fails closed. The ID and access tokens are verified, used and discarded.)*
*(Amended 2026-10-04, the owner's answer to "does a change of the server key end the sessions of the
identity provider?", over building a `COWORK_SESSION_KEY_PREVIOUS` that opens what the old key
sealed until the sessions holding it have ended: **there is one server key and no previous one is
kept.** A session lives twelve hours at most, a change of the key is rare and deliberate, and a
second key would be one more Secret to handle. Every key cowork derives from `COWORK_SESSION_KEY` by
HKDF-SHA256 under a label of its own changes with it
([`api/api.go`](../../backend/internal/api/api.go) `New`), and a change of the key does this, each
verified in the code:*
- *every sealed refresh token becomes unreadable (`cowork oidc refresh token v1`,
  [`auth/seal.go`](../../backend/internal/auth/seal.go)), so every session of the identity provider
  that holds one ends at its next groups refresh — `revoked`, cause `identity-provider` — and its
  person signs in again: the change fails closed. A provider session without a refresh token is not
  affected;*
- *a login through the provider under way at the moment fails with `oidc_failed`: its state cookie
  was sealed under the old key (`cowork oidc login v1`);*
- *the list cursors clients hold stop working, `400 invalid_cursor` (the cursor codec's keys);*
- *the login throttle's count of an address starts over, because the address is hashed under a key
  derived from it (`cowork login address v1`); the lockout of a username, which is kept by the
  username, stays;*
- *the audit rows' source hashes before and after the change cannot be compared: one address gets
  another hash (`cowork audit address v1`);*
- *a retry of an idempotent request whose key was stored before the change, within the key's
  twenty-four hours, no longer matches its stored fingerprint — an HMAC under a key derived from it
  (`cowork idempotency fingerprint v1`) — and is refused as `422 idempotency_mismatch`: neither
  replayed nor run a second time;*
- *the sessions of the local login survive: a session row is found by the SHA-256 of its cookie,
  which no key enters, and so is a personal access token.*
*The operations page names the same list for the operator
([installation.md](../operations/installation.md#the-secrets)).)*

**D2 — Cookie attributes:** `HttpOnly; Secure; SameSite=Lax; Path=/`, no `Domain`. The
cookie's name is fixed. `Secure` is set in every environment; ~~browsers treat `localhost` as
secure, so development needs no exception~~ *(measured false 2026-10-03, below)*. *(Amended 2026-10-03: the name is
`__Host-cowork-session`; the prefix makes a browser refuse the cookie unless it is `Secure`, has
`Path=/` and no `Domain` — D2's attributes — and refuse it from a sibling subdomain, and
`Max-Age` is the absolute lifetime. A browser stores no `Secure` cookie from a plain-HTTP origin
that is not `localhost`, so an installation without TLS in front cannot log in. Measured on
2026-10-03 with Playwright: WebKit, Safari's engine, does not store it from `http://localhost`
either — the login answers `200` with the cookie and the browser drops it — while Chromium does.
Development therefore serves HTTPS: `make dev` runs the Angular dev server with its self-signed
certificate on `https://localhost:4200`, and the end-to-end tier's WebKit run needs TLS in front
of the images. `Secure` stays unconditional; there is no development exception.)*

**D3 — Lifetimes:** an absolute lifetime (`COWORK_SESSION_LIFETIME`, default twelve hours)
and an idle timeout (`COWORK_SESSION_IDLE`, default two hours); ~~a request within the idle
window extends the session up to the absolute limit~~ *(amended 2026-10-06, below: the person's
activity within the idle window — a write, or a read the keep-alive marks — extends the session up
to the absolute limit)*. Expired rows are removed by a job
([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D5). *(Amended 2026-10-03: ~~a request~~ *(2026-10-06: a request of the person's activity, below)*
moves the idle clock at most once a minute, never past the
absolute limit, and the backend's clock decides both limits. The job is `session-expiry`, at
start and hourly; it keeps the table small and enforces nothing — a session past a limit is
refused at its next request whether or not the job has run. An open event stream checks its
session at every heartbeat and ends with it, and does not extend the idle limit.)* *(Amended
2026-10-04: a session of the identity provider refreshes its groups on its first request after
`COWORK_OIDC_GROUPS_REFRESH`, inside the resolver and before the request is served, ~~under a lock of
its row so that concurrent requests and replicas refresh once~~, and at an open stream's heartbeat
without moving the idle clock; what the refresh decides is
[ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D5.)* *(Amended after the security review, 2026-10-04: the refresh happens once because one request
claims it by a thirty-second lease on the row, in a short transaction; the issuer is asked with no
connection and no lock held, and a second short transaction applies the answer under the row's lock
while the lease is the claimant's. Concurrent requests and replicas find the lease taken and are
served on the session's groups without waiting.)* *(Amended 2026-10-06, on the owner's request of
that day and the coordinator's decision of the same night: **only the person's activity moves the
idle clock.** A session's request moves it when it is a write that passes the CSRF check of
[ADR 0037](0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md) D1, or a
read that carries the header `X-Cowork-Activity` with the value `input` — any other value, or none,
moves nothing ([`api/session.go`](../../backend/internal/api/session.go) `movesIdleClock`,
`ActivityHeader`, `ActivityInput`). The browser's keep-alive sends that read: while a page of the
shell is open, it notes the time of the person's last pointer press, key, wheel or touch, and every
five minutes, while the document is visible and there was such input since it last asked, it asks
`GET /api/v1/me` with the header
([`keep-alive.service.ts`](../../frontend/src/app/core/keep-alive.service.ts), the interceptor
`personActivity` in [`http.ts`](../../frontend/src/app/core/http.ts); no other request of the UI
sets it). No other read moves the clock — not the event stream's connections and reconnects, not its
heartbeats, not the polling fallback's reloads of
[ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md) D7,
not the reloads an event triggers —, so a tab that is only open reaches the idle limit whatever its
stream does, and the guarantee "a tab that is only open logs out" holds. Why: reading a ticket,
scrolling a board or writing a long comment sends no write, so the idle limit ended sessions under
the person's hands; and while every request moved the clock, the stream's reconnects and its
fallback's reloads — requests every fifteen seconds to every minute — kept a session that nobody
used up to the absolute limit (`TestOnlyThePersonsActivityMovesTheIdleClock` fails against the old
rule on a plain read, the stream's connection and a refused write; no browser ran it). No other site
can keep a session alive with the header: a page of another origin cannot send it, because a custom header
needs a CORS preflight and the API answers none (ADR 0037 D3); and a write that a page of the same
site sends, where `SameSite=Lax` lets the cookie ride along, fails the CSRF check and moves nothing
either — before this rule, a refused write moved the clock too. Where a limit ends a session of the
identity provider, the login page signs the person in again at their first input while the
provider's own session lives
([ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D6); a local session gets only the keep-alive, which needs the person's input.)*

**D4 — Revocation is a delete, and it is immediate.** Logout deletes the row ~~(and calls the
issuer's `end_session_endpoint` when discovery names one)~~ *(amended 2026-10-04: and, for a
session of the identity provider whose issuer's discovery names an `end_session_endpoint`, answers
`200` with `end_session_url` — that endpoint with `client_id` and `post_logout_redirect_uri` =
`COWORK_BASE_URL` + `/login`, without `id_token_hint`, since no ID token is kept — for the browser to
go to: an RP-initiated logout the browser carries out; cowork does not call the issuer. Every other
logout answers `204`)*. Leaving the allow-list,
deactivation of the person ([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D5), and an administrator's "end all sessions of this person" delete rows; the next request
with a deleted session's cookie is unauthenticated. *(Amended 2026-10-03: the rows are deleted
by a logout; by a login that presents a cookie, which ends that one in the transaction that
makes the new session; by a person's change of password, every other session of theirs; by a
tenant administrator's reset of a managed account's password, their "end all sessions" and their
deactivation of it, every session of the account; and by the start-up synchronisation when the
local administrator's configured password changed or the account is deactivated. "An
administrator" is the administrator of the tenant that manages the account
([ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D1).)*
*(Amended 2026-10-04: and by the identity provider — every session of a person whom a groups
refresh, or a login refused at the gate, finds outside the allow-list; the one session whose refresh
token the issuer refuses, or which no longer opens after the server key changed; ~~each session of a
provider that is no longer configured, at its next refresh~~ *(amended after the security review:
every session of a person who is not the configured issuer's — of another issuer, or of a provider
no longer configured — at the first request of any of them)*.)*

**D5 — Session fixation and binding.** The session id is created at login, never before, and
never re-used; a login while a session exists replaces it. The cookie is bound to no path
below `/` and to no subdomain.

**D6 — The same authorization layer serves cookies and tokens.** A request is resolved to
(person, agent flag, token id or session id) by one resolver: a session cookie yields the
person with no agent flag; a personal access token (its own record) yields the person with
its flags. Everything after the resolver — tenant membership, role, policies — is the same
code. *(Amended 2026-10-03: the resolver reads the `Authorization` header first. A request that
carries one is a token's, whatever else it carries, and its cookie is not looked at — so the two
credentials never combine, a token's request is never held to the CSRF check of
[ADR 0037](0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md), and
a cookie never rides on a token's authority; otherwise the cookie decides, on an operation whose
security requirement in the API document names it. A session has no scope of its own — the
pipeline writes `admin`, which leaves every decision to the person's role — and the principal
carries the cookie's hash, to find the row again, never the row's id. ~~Six~~ routes take a session
only and answer a token `403 session_required`: creating a token, a tenant or a local account,
resetting a password, changing one's own password and logging out
([ADR 0035](0035-personal-access-tokens.md) D5,
[ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D1, D5).)*
*(Amended 2026-10-04: ~~twelve~~ routes — the six above and the administration acts that can give
access: adding a member, setting a grant, making or changing a group mapping, restricting or opening
a project, putting a person on its access list (ADR 0035 D5).)* *(Amended 2026-10-04 for the chat in
the UI, [ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md):
thirteen routes, a turn of the chat the thirteenth ~~and one field — switching the tenant's
`chat_external_allowed` on — that a token is refused~~ (ADR 0035 D5). A session cookie still yields
the person with no agent flag, but an `X-Cowork-Agent` header on the session's request marks that
request as an agent's, ~~every capability~~ the capabilities the person chose for the chat
([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D5, amended again 2026-10-04) and the hard-off list
([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3): the chat's tool calls are such requests, and such a request is refused the ~~thirteen~~ routes.)*
*(Amended 2026-10-04: ~~thirteen~~ fourteen routes, the list of every tenant of the installation
the fourteenth ([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2, ADR 0035 D5); an agent-marked request is refused all ~~fourteen~~.)* *(Amended 2026-10-04 by the
owner's answers on the chat, ADR 0076: ~~fourteen~~ sixteen routes — stopping the person's running
turns of the chat and choosing the chat's capabilities the fifteenth and sixteenth (ADR 0035 D5);
the consent field is gone with the consent; an agent-marked request is refused all ~~sixteen~~.)*
*(Amended 2026-10-05 by the decision recorded in
[ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D7: ~~sixteen~~ seventeen routes, purging a deleted ticket the seventeenth (ADR 0035 D5); an
agent-marked request is refused all seventeen.)*

**D7 — Sessions are recorded, never by id.** Login, logout, revocation and refresh outcomes
are audit rows ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md))
with the person and the cause; the session id appears in no log and no audit row. The
session table is not tenant-bound ([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D6) and is readable by the person for their own sessions and by a global administrator.
*(Amended 2026-10-03: the actions are `logged_in`, `logged_out`, `password_changed`,
`password_reset`, `revoked` with the count of sessions ended, `deactivated`, and the job's
`expired`; the rows are installation-level and carry no `token_id`, which is how a browser
session is marked ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)
D1), except an administrator's act on a managed account, which is a row of the tenant. The
policies admit a person's own rows, the one row of the cookie presented, the administrators of a
managed account, a global administrator for reading, and the two jobs that end sessions by
name.)* *(Amended 2026-10-04: the identity provider's ends are `revoked` rows of
`system:identity-provider` with the cause `gate` or `identity-provider` and the count; a login
through it is the person's `logged_in` with the note `oidc`; every row written for a request
carries the keyed hash of the client's address ([ADR 0035](0035-personal-access-tokens.md) D2). A
refresh's outcome that ends nothing is recorded only where it changed the person's groups,
administrator flag or memberships.)*

## Consequences

- Nothing a script in the page can read carries authority; an XSS can act within the page
  (which the sanitiser of [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
  D6 and the CSRF record limit) but cannot carry the session away.
- One indexed lookup per request; a cleanup job; one Secret more in the chart.
- `SameSite=Lax` alone is not CSRF protection for the API; the CSRF record adds the origin
  check and the custom header on unsafe methods.
- *(Added 2026-10-06, D3.)* A person who only reads keeps the session through the keep-alive, whose
  reads come five minutes apart at most: an idle limit of about six minutes or less cannot be held
  that way, and such a person is signed out between two of them. A tab nobody uses ends at the idle
  limit whatever its stream does. Whoever holds a stolen cookie keeps it alive as the browser would,
  with the header or a write ([docs/security/sessions.md](../security/sessions.md) H-15).
- The groups refresh of ADR 0030 D5 is a column update on the row and a UserInfo call every
  fifteen minutes per active session. *(Amended 2026-10-04: a refresh grant at the issuer's token
  endpoint — and UserInfo where the refreshed ID token lacks the groups — inside the session's
  request, ~~under a lock of its row~~ *(amended after the security review: holding no connection and
  no lock while the issuer is asked)*.)*

## Alternatives Considered

- **The identity provider's JWT in the browser as a bearer token.** No session table; the
  token sits in script reach, cannot be revoked before expiry, and ADR 0030 D5 would need a
  blocklist — a session table under another name. Lost.
- **A stateless signed, encrypted cookie holding the state.** No lookup; no revocation
  before expiry, a cookie that grows with the groups snapshot, and a key rotation that ends
  every session at once. Lost.

## Residual risks

- D1's refresh-token storage exists only for issuers whose UserInfo needs it; the encryption
  key is a Secret the operator must rotate deliberately, and the operations page says how.
  *(Amended 2026-10-04: a refresh token is stored whenever the issuer gives one; whoever holds the
  database and the server key opens them, and cowork never revokes one at the issuer
  ([docs/security/identity-provider.md](../security/identity-provider.md) H-27). Rotating the key
  ends those sessions at their next refresh, which the operations page says.)*
- D3's defaults (twelve hours, two hours) are the owner's working day; a tenant with a
  stricter policy configures shorter and cannot configure per tenant in the first release.
- *(Added 2026-10-03.)* The cookie is the whole credential: nothing binds a session to the
  browser that logged in, so whoever obtains the value acts as the person until the idle limit,
  the absolute limit or an end. `HttpOnly` keeps a script from reading it and `Secure` with the
  `__Host-` prefix keeps it off plain HTTP; shorter lifetimes shrink the window. The security
  page names it as an open gap ([docs/security/sessions.md](../security/sessions.md), H-15).

## References

- [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) — the login the session follows
- [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md) D5 — the refresh and the revocation this record makes possible
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3, D4 — one origin
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md), [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D5 — audit and the cleanup job
- [ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) — the login that creates a session
- [ADR 0037](0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md) — what protects the cookie's writes
- [`backend/internal/api/session.go`](../../backend/internal/api/session.go), [`backend/internal/store/sessions.go`](../../backend/internal/store/sessions.go), [migration 16](../../backend/internal/store/migrations/000016_sessions.up.sql) — the implementation
