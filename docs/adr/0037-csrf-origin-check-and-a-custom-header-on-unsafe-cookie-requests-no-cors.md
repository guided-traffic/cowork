# ADR 0037: CSRF — an Origin Check and a Custom Header on Every Unsafe Cookie-Authenticated Request, and No CORS, Ever

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "CSRF
for the cookie session?": origin check plus custom header, over a synchroniser token, over
`SameSite=Lax` alone, and over `SameSite=Strict`. The rules of D4–D6 were put to the owner
with the question and not objected to.

**Not built.** No session, no middleware.

## Context

The browser session is a cookie ([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md)
D2, `SameSite=Lax`), and the record says in the same breath that `Lax` is not CSRF protection
for an API over which tickets are deleted. The UI and the API are one origin behind nginx
([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3, D4) and the API sends no CORS headers; that single fact is what makes a stateless check
complete: a cross-origin page can submit a form but cannot set a custom header, and a
cross-origin `fetch` that sets one triggers a preflight the backend does not answer. Tokens
([ADR 0035](0035-personal-access-tokens.md) D7) carry no cookie and are outside this record.

## Decision

**D1 — On every `POST`, `PUT`, `PATCH` and `DELETE` of a cookie-authenticated request, the
backend requires two things:** the `Origin` header — or, when `Origin` is absent, the
`Referer` — equals `COWORK_BASE_URL` (scheme, host and port, exactly), and the header
`X-Requested-With: cowork` is present. Either missing or wrong answers `403` with the error
code `csrf`. A request with neither `Origin` nor `Referer` is refused, not waved through.

**D2 — `GET`, `HEAD` and `OPTIONS` never mutate**, so the check does not apply to them; an
endpoint that would mutate on `GET` is a defect, and the API record forbids it.

**D3 — The API sends no CORS headers, and no configuration enables them.** A second client
on another origin is a future record that would introduce a synchroniser token with it; it
is not a value in the chart.

**D4 — The Angular client sets `X-Requested-With: cowork` on every request** through one HTTP
interceptor; nothing else in the frontend needs to know the rule.

**D5 — Two routes are outside D1 by nature and protected otherwise.** The OIDC callback
(`/auth/callback`) is a `GET` from the identity provider, protected by the `state` and
`nonce` of [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D1. The local login form (`POST /auth/local`) carries no session yet; it is rate-limited by
[ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D6 and
still subject to the origin check of D1 (a cross-site login attempt is refused). Logout is a
`POST` and is subject to D1, so a page cannot log a person out.

**D6 — `COWORK_BASE_URL` is required whenever a cookie login exists** (an issuer or a local
account configured), and it must be the origin the browser sees — behind the Ingress, the
public URL. A mismatch is the first thing the operations page tells an operator to check
when every write answers `403 csrf`.

## Consequences

- Stateless: no token to issue, store, rotate or embed in forms; one middleware, one
  interceptor.
- `SameSite=Lax` stays, so a deep link from a chat opens the ticket with the session; D1
  carries the protection `Lax` does not.
- The one-origin decision of ADR 0001 becomes load-bearing: moving the UI to another origin
  later means revisiting this record, which D3 says aloud.
- `COWORK_BASE_URL` moves from "read by nothing" to required for any browser login; the
  README's configuration table and the chart's values say so in the change that builds the
  login.

## Alternatives Considered

- **A synchroniser token in the session, sent as a header.** The classic form; an issuing
  endpoint, storage per session and rotation, for a protection D1 already gives completely
  under one origin without CORS. Lost.
- **`SameSite=Lax` alone.** Blocks cross-site form `POST`s; browser differences, no defence
  against a sibling-subdomain attacker, and nothing for a client that forgets the attribute.
  Lost.
- **`SameSite=Strict`.** Stronger on paper; a deep link from outside arrives without the
  cookie and shows the login page — the workflow's daily case. Lost.

## Residual risks

- A browser extension or an XSS inside the origin can set the header; D1 defends against
  cross-site requests, not against code running inside the page — that is the sanitiser's
  job ([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6)
  and the attachment delivery's ([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
  D5).
- Privacy-hardened browsers that strip both `Origin` and `Referer` on same-origin requests
  are refused by D1; the UI tells the person why. Not verified against any particular
  browser; the integration tier tests the rule, not browsers.

## References

- [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D2 — the cookie this protects
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3, D4 — one origin
- [ADR 0035](0035-personal-access-tokens.md) D7 — tokens are outside this record
- [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) D1, [ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D6 — how the two login routes are protected instead
