# ADR 0031: Server-Side Sessions in an HttpOnly Cookie — an Opaque Id, a Row in PostgreSQL, Revocation Is a Delete, and the Identity Provider's Tokens Never Reach the Browser

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"browser session mechanism?": server-side sessions, over the identity provider's JWT in the
browser and over a stateless signed cookie. The rules of D5–D7 were put to the owner with the
question and explicitly confirmed.

**Not built.** No session table, no cookie, no login route.

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
the old for decryption until every session that used it is gone).

**D2 — Cookie attributes:** `HttpOnly; Secure; SameSite=Lax; Path=/`, no `Domain`. The
cookie's name is fixed. `Secure` is set in every environment; browsers treat `localhost` as
secure, so development needs no exception.

**D3 — Lifetimes:** an absolute lifetime (`COWORK_SESSION_LIFETIME`, default twelve hours)
and an idle timeout (`COWORK_SESSION_IDLE`, default two hours); a request within the idle
window extends the session up to the absolute limit. Expired rows are removed by a job
([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D5).

**D4 — Revocation is a delete, and it is immediate.** Logout deletes the row (and calls the
issuer's `end_session_endpoint` when discovery names one). Leaving the allow-list,
deactivation of the person ([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D5), and an administrator's "end all sessions of this person" delete rows; the next request
with a deleted session's cookie is unauthenticated.

**D5 — Session fixation and binding.** The session id is created at login, never before, and
never re-used; a login while a session exists replaces it. The cookie is bound to no path
below `/` and to no subdomain.

**D6 — The same authorization layer serves cookies and tokens.** A request is resolved to
(person, agent flag, token id or session id) by one resolver: a session cookie yields the
person with no agent flag; a personal access token (its own record) yields the person with
its flags. Everything after the resolver — tenant membership, role, policies — is the same
code.

**D7 — Sessions are recorded, never by id.** Login, logout, revocation and refresh outcomes
are audit rows ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md))
with the person and the cause; the session id appears in no log and no audit row. The
session table is not tenant-bound ([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D6) and is readable by the person for their own sessions and by a global administrator.

## Consequences

- Nothing a script in the page can read carries authority; an XSS can act within the page
  (which the sanitiser of [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
  D6 and the CSRF record limit) but cannot carry the session away.
- One indexed lookup per request; a cleanup job; one Secret more in the chart.
- `SameSite=Lax` alone is not CSRF protection for the API; the CSRF record adds the origin
  check and the custom header on unsafe methods.
- The groups refresh of ADR 0030 D5 is a column update on the row and a UserInfo call every
  fifteen minutes per active session.

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
- D3's defaults (twelve hours, two hours) are the owner's working day; a tenant with a
  stricter policy configures shorter and cannot configure per tenant in the first release.

## References

- [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) — the login the session follows
- [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md) D5 — the refresh and the revocation this record makes possible
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3, D4 — one origin
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md), [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D5 — audit and the cleanup job
