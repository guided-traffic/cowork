# ADR 0031: Server-Side Sessions in an HttpOnly Cookie — an Opaque Id, a Row in PostgreSQL, Revocation Is a Delete, and the Identity Provider's Tokens Never Reach the Browser

## Status

Accepted, amended 2026-10-03 (D1–D4, D6, D7 made concrete by the first implementation, which
has no identity provider yet; D2's claim that development needs no exception measured false for
Safari, so development serves HTTPS) and 2026-10-04 (D1, D3: the groups snapshot, its refresh and
the sealed refresh token built; D4: the issuer's logout is handed to the browser, not called; D6:
twelve routes take a session only; D7: the identity provider's ends), and again on 2026-10-04 after
the security review (D3: the refresh claims a lease and holds nothing while it asks the issuer; D4:
the sessions of a person of another issuer end at once). Date: 2026-10-01. Decided by the owner as the answer to the
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
provider has no password, so no way to end their own other sessions. Not built: D1's rotation that
keeps the old key for decryption — a changed `COWORK_SESSION_KEY` ends every provider session that
holds a refresh token at its next refresh.

## Context

The UI and the API share one origin behind nginx ([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
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
key (`COWORK_SESSION_KEY`, a Secret in the chart, rotated by issuing a new key and keeping
the old for decryption until every session that used it is gone *(not built, 2026-10-04: below)*).
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
and an idle timeout (`COWORK_SESSION_IDLE`, default two hours); a request within the idle
window extends the session up to the absolute limit. Expired rows are removed by a job
([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D5). *(Amended 2026-10-03: a request moves the idle clock at most once a minute, never past the
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
served on the session's groups without waiting.)*

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
*(Amended 2026-10-04: twelve routes — the six above and the administration acts that can give
access: adding a member, setting a grant, making or changing a group mapping, restricting or opening
a project, putting a person on its access list (ADR 0035 D5).)*

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
