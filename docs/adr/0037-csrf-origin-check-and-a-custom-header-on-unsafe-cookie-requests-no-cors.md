# ADR 0037: CSRF — an Origin Check and a Custom Header on Every Unsafe Cookie-Authenticated Request, and No CORS, Ever

## Status

Accepted, amended 2026-10-03 (D1, D5, D6 made concrete by the first implementation) and
2026-10-04 (D5, D6: the identity provider's callback and its base URL built), and on 2026-10-04
by the owner's decision on the routing (Context: the one origin is the Ingress's, which routes
`/api/` and `/auth/` to the backend,
[ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3; nothing of the decision changes), and on 2026-10-06 for GitHub's webhook of
[ADR 0071](0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
(D5: a third route outside D1, a public write whose credential is a signature over its body, which
carries no origin check; made concrete by the implementer and built the same day, open to the
owner's objection), and on 2026-10-07 by the owner's answer recorded in
[ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D5 (Residual risks:
the reads that record an act take a session's request only from the installation's own pages;
built 2026-10-09). Date:
2026-10-01. Decided by the owner as the answer to the catalog question "CSRF
for the cookie session?": origin check plus custom header, over a synchroniser token, over
`SameSite=Lax` alone, and over `SameSite=Strict`. The rules of D4–D6 were put to the owner
with the question and not objected to.

**Partly built** (phase 3, 2026-10-03): D4 — the frontend's interceptor
([`http.ts`](../../frontend/src/app/core/http.ts)) sends `X-Requested-With: cowork` on every
request — and the backend's check is built with the sessions: D1–D3, D5 for the local login and
the logout, and D6
([`api/session.go`](../../backend/internal/api/session.go) `csrf`,
[docs/security/csrf.md](../security/csrf.md)). ~~Not built: the OIDC callback of D5, which waits
for the identity provider.~~ **Built** (phase 4, 2026-10-04): the OIDC callback of D5, protected
by the `state` and the `nonce` of
[ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) D1
in a sealed cookie ([docs/security/identity-provider.md](../security/identity-provider.md)).

## Context

The browser session is a cookie ([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md)
D2, `SameSite=Lax`), and the record says in the same breath that `Lax` is not CSRF protection
for an API over which tickets are deleted. The UI and the API are one origin ~~behind nginx~~
*(since 2026-10-04 behind the Ingress, which routes `/api/` and `/auth/` to the backend and the
rest to the frontend)*
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
*(Amended 2026-10-03: the comparison is with the origin `COWORK_BASE_URL` names — lower-case
scheme and host, the scheme's default port dropped, as a browser writes `Origin`. An `Origin` of
`null`, a second `Origin` or `Referer` header, a `Referer` that is no URL, and an `Origin` that
differs while the `Referer` matches are refused: the `Origin` wins. The check is the pipeline's,
answered before the tenant boundary and before any handler, for a request authenticated by the
cookie; a request with an `Authorization` header is a token's and outside it
([ADR 0035](0035-personal-access-tokens.md) D7, [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md)
D6).)*

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
`POST` and is subject to D1, so a page cannot log a person out. *(Amended 2026-10-03: the login
carries no session, so what it is held to is the origin half of D1 — the `Origin`, or without
one the `Referer`, must be `COWORK_BASE_URL` — and not the custom header. The API document marks
such a route `x-cowork-origin-check`, and the unit test over the document requires the mark on
every public write. Logout is held to both halves.)* *(Amended 2026-10-04: the callback is built
as D5 says, and so is its start, `GET /auth/oidc/login`: both are `GET`s without a session and
outside D1; the start decides nothing a forged link could use, and the callback makes a session only
when the returned `state` is the one sealed — with the nonce and the PKCE verifier — in the
browser's own `__Host-cowork-oidc` cookie, which another site can neither read nor set.)*
*(Amended 2026-10-06, made concrete by the implementer for
[ADR 0071](0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
D2, D3:)* a third route is outside D1 by nature: GitHub's webhook,
`POST /api/v1/tenants/{tenant}/integrations/github/webhook`, a public write that carries no
cookie — one sent is ignored — and whose credential is the HMAC of its body under the tenant's
secret, which no other site can compute. GitHub sends no `Origin`, so it carries no origin check
either; the API document marks it `x-cowork-signed: github` instead, and the unit test over the
document requires every public write to carry one of the two marks, the signed one on that route
alone ([`document_test.go`](../../backend/api/document_test.go)).

**D6 — `COWORK_BASE_URL` is required whenever a cookie login exists** (an issuer or a local
account configured), and it must be the origin the browser sees — behind the Ingress, the
public URL. A mismatch is the first thing the operations page tells an operator to check
when every write answers `403 csrf`. *(Built 2026-10-03: the backend refuses to start without
`COWORK_BASE_URL` while the local administrator is configured, and a value with a path, a query,
a fragment or a user is refused at start. The check fails closed: without an origin to compare
with, no write of a cookie and no login passes — `403 csrf` naming the variable — and reads
still work.)* *(Built 2026-10-04 for the issuer as well: the backend refuses to start without
`COWORK_BASE_URL` while `COWORK_OIDC_ISSUER` is set, whose redirect URI it is.)*

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
  D5) ([docs/security/csrf.md](../security/csrf.md), H-21).
- *(Added 2026-10-03.)* D2 leaves reads unchecked, and two reads write an audit row, as data
  leaving the system must be recorded: an attachment's download and a ticket's Markdown export.
  A link to one of them, followed from another site, carries the `Lax` cookie and records the
  act under the person; it changes no ticket and the answer is unreadable to the other site
  (H-22 of the same page). *(Amended 2026-10-07 by the owner's answer recorded in
  [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D5: the reads that
  record an act are five, and a session's request for one of them that a page on a sibling host
  or of another site makes is refused by its `Sec-Fetch-Site`, `403 csrf`; ~~a link followed from
  another site records the act~~ only in a browser that sends no such header, which H-22 names
  now.)*
- Privacy-hardened browsers that strip both `Origin` and `Referer` on same-origin requests
  are refused by D1; the UI tells the person why. Not verified against any particular
  browser; the integration tier tests the rule, not browsers.

## References

- [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D2 — the cookie this protects
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3, D4 — one origin
- [ADR 0035](0035-personal-access-tokens.md) D7 — tokens are outside this record
- [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) D1, [ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D6 — how the two login routes are protected instead
