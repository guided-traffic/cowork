# Cross-site writes: the CSRF check

What keeps another site from writing with a person's session, what is checked and what is
not, and what the check depends on, as built on 2026-10-04. The session it protects is
[sessions.md](sessions.md); how a request is authenticated at all is
[trust-boundaries.md](trust-boundaries.md).

## The rule

`SameSite=Lax` on the session cookie keeps a cross-site form from carrying it on a `POST`, and
the record says in the same breath that `Lax` alone is not CSRF protection for an API over
which tickets are deleted
([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) Consequences,
[ADR 0037](../adr/0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md)).
So on every `POST`, `PUT`, `PATCH` and `DELETE` of a request **authenticated by the session
cookie**, the backend requires two things (D1;
[`api/session.go`](../../backend/internal/api/session.go) `csrf`, called from `sessionRules`
in the pipeline):

1. The `Origin` header — or, when there is none, the `Referer` — equals `COWORK_BASE_URL`
   exactly: scheme, host and port. A request with neither header is refused, not waved
   through; so is an `Origin` of `null`, a second `Origin` or `Referer` header, a `Referer`
   that is no URL, and an `Origin` that differs while a `Referer` matches — the `Origin` wins.
2. The header `X-Requested-With: cowork` is present with exactly that value.

Either missing or wrong is `403` with the code `csrf`, answered before the tenant boundary and
before any handler. `COWORK_BASE_URL` is read as an origin: the scheme and host lower-cased, and
a port that is the scheme's default (`443` for `https`) dropped, as a browser writes the
`Origin` header; a URL with a path, a query, a fragment or a user is refused at start
(`config.Origin`). `TestCSRFRule` runs the table of headers; `TestSessionWritesAreCSRFChecked`
runs it through the whole server and asserts that a refused write changed nothing.

## What is outside it, and why

| Request | Checked | Why |
|---|---|---|
| `GET`, `HEAD`, `OPTIONS` | no | a read never mutates (D2) — see [H-22](#h-22) |
| a token's request | no | an `Authorization` header makes it a token's; it carries no cookie, and a page of another site cannot set that header without a preflight the backend does not answer ([ADR 0035](../adr/0035-personal-access-tokens.md) D7) |
| `POST /auth/local` | the origin half only | no session yet carries the check, so the `Origin` or `Referer` must still be `COWORK_BASE_URL` — a cross-site login attempt is refused (D5); the custom header is not required, because the login is public and not a write of a session |
| `GET /auth/oidc/login`, `GET /auth/callback` | no | the identity provider's browser navigations, which carry no session (D5): the callback makes one only when the `state` the issuer returns is the one sealed in the browser's own `__Host-cowork-oidc` cookie, which another site can neither read nor set, so a page of another site cannot log a person in as someone else — a link to the start logs a person in as themselves at most ([identity-provider.md](identity-provider.md#the-login)) |
| `POST /auth/logout` | both halves | a page must not be able to log a person out (D5) |
| a tool call of the chat in the UI | both halves, which the backend writes itself | the call is a request the backend makes in its own process with the person's cookie ([chat.md](chat.md)); it sets `Origin` to `COWORK_BASE_URL` and `X-Requested-With: cowork` because the turn that makes it, `POST …/chat`, passed the check as a write of the session — a page of another site cannot start a turn, and so cannot make a call |

**The check fails closed.** Without a `COWORK_BASE_URL` there is no origin to compare with, so
no write of a cookie and no login passes — `403 csrf` naming the variable; the backend refuses
to start without it while the local administrator or an identity provider is configured (D6).
Reads still work.

**There is no CORS.** The backend sends no `Access-Control-*` header and no configuration turns
one on (D3). A cross-origin `fetch` that sets `X-Requested-With` triggers a preflight nobody
answers; a cross-site form can submit but cannot set the header; that is why a stateless
check is complete under one origin. Putting the UI on another origin later is a decision of
its own, which would bring a synchroniser token with it.

## What it depends on

- **`COWORK_BASE_URL` is the origin the browser shows**: behind an Ingress, the public URL. A
  value the browser does not send makes every write of a session `403 csrf` — the first thing
  to look at when the UI loads but nothing saves
  ([runtime.md](../operations/runtime.md#the-login)). A wrong value locks the browser out; it
  never lets another origin in.
- **One origin.** The UI and the API are one origin behind the Ingress, which routes `/api/` and
  `/auth/` to the backend and the rest to the frontend
  ([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
  D3, D4); the controller passes `Origin`, `Referer` and `X-Requested-With` through — ingress-nginx
  v1.15.1 did on 2026-10-04, when a session's writes passed the check through it in a kind cluster.
  A controller configured to drop or rewrite one of them makes every write of a session
  `403 csrf`.
- **The frontend's interceptor** sets `X-Requested-With: cowork` on every request of the
  `HttpClient` ([`frontend/src/app/core/http.ts`](../../frontend/src/app/core/http.ts); D4). The
  chat's turn is a `fetch` — the `HttpClient` waits for a whole body, and the turn is a stream —
  and sets the same header from the same constant (`requestedWithHeader`); the browser adds the
  `Origin` to that `POST` itself.

Not verified against any particular browser: the tests send the headers a browser sends and
run no browser. A privacy-hardened browser that strips both `Origin` and `Referer` on
same-origin requests is refused by the rule; the UI tells the person why.

## GitHub's webhook is outside the check

`POST /api/v1/tenants/{tenant}/integrations/github/webhook` is a public write that carries no
cookie — a cookie sent with it is never looked at — and whose credential is the HMAC of its body
under the tenant's secret, which no other site can compute
([ADR 0037](../adr/0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md)
D5 as amended 2026-10-06). GitHub sends no `Origin`, so it is not held to the origin half either: the
API document marks it `x-cowork-signed` instead of `x-cowork-origin-check`, and the unit test over the
document holds that mark to this route alone. A page of another site can post to it, as any client
can, and gets `404` or `401` like anybody without the secret ([github-webhook.md](github-webhook.md)).

## What this does not cover

<a id="h-21"></a>
### H-21 — Code running inside the page passes the check

Live by construction. The check defends against requests made by other sites, not against
script inside the origin: an XSS in the page, or a browser extension, sets the header and
sends the cookie, and acts as the person within the page's reach. What limits that is the
sanitiser of rendered Markdown
([ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6,
which is not built yet — the UI renders no Markdown, and the chat shows a model's output as text),
the shell's content-security policy, which runs the bundle's scripts and no other since 2026-10-04
([trust-boundaries.md](trust-boundaries.md#the-shells-content-security-policy)), the attachment
delivery's `sandbox` and `nosniff` ([attachments.md](attachments.md)), the `HttpOnly` cookie that
such a script cannot carry away, and `PUT /api/v1/me/password` asking for the current password,
which a script in the page does not know and which counts toward the lockout when guessed. A
browser extension runs outside the page's policy.

<a id="h-22"></a>
### H-22 — Two reads write an audit row, and a link can trigger them

Live today. The rule leaves reads unchecked because a read mutates nothing (D2), and two
routes record an act on a read, as data leaving the system must be recorded
([ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D5): an
attachment's bytes (`downloaded`) and a ticket's Markdown export (`exported`). A page of
another site that gets a person to follow a link to one of them makes a top-level `GET` that
carries the `Lax` cookie, so the act is recorded under the person — a row in an append-only
table, not a change to any ticket, and the response is unreadable to the other site. An
`<img>` or a `fetch` from another site does not carry the cookie. If an attacker choosing the
audit record's content matters to an installation, the answer is a decision on these two
routes — a header they require too — not a setting.
