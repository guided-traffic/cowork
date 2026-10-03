---
id: T27
title: nobody can log in to the browser UI — no sessions, no local administrator, no local accounts, no CSRF check, and no route creates a tenant or a token
state: in-progress
severity: high
security: none
threat:
urgency: next         # rule 3: severity high, live — the UI and every real installation are unusable without it
effort: L
blocked-by:
filed-from: T26, the owner's answer of 2026-10-03 (the local login moves from phase 4 into phase 3)
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

On the branch of phase 3, the backend has the sessions, the local administrator, local accounts,
CSRF and the creation routes — `backend/internal/auth`, `internal/api/{session,login,accounts}.go`,
`internal/bootstrap`, migrations 15 and 16, the chart's `localAdmin.*`, `bootstrap.*` and
`auth.*`, and docs/security/{sessions,local-accounts,csrf}.md; the unit and integration tiers,
lint, gosec, vuln and the chart checks pass. The UI has the login page, the
password change, the sign-out and the `401` redirect; `make dev` logs in as the local
administrator `dev` over HTTPS, and the dev proxy holds no token any more.

The owner decided two questions of the build: creating and resetting a local
account are session-only (a leaked admin token must not become access that survives its
revocation), and the login throttle finds the client address through trusted proxies
(`COWORK_TRUSTED_PROXIES`, X-Forwarded-For read from the right) with a NetworkPolicy that admits
only the frontend to the backend. Both are built: `createAccount` and `resetAccountPassword`
declare the session alone, `api/clientaddr.go` walks the header, the chart renders
`networkpolicy.yaml` (`networkPolicy.enabled`, on by default) and `backend.config.trustedProxies`,
and nginx appends the address it saw in all three proxied locations. An IPv6 client counts by
its /64 (`TestAddressHash`).

## Required changes

All of it is decided and unbuilt; the records are the specification. The routes and the
details below are this ticket's design within them, written into the API document first
([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md)).

**Still to do:** Q1 below; the e2e paths of T29; and three small gaps the UI met. The tokens
page, the accounts page and "create the first tenant" are built, reviewed and fixed, and pass in
Chromium and WebKit against the running stack.

The gaps:

- A token names its project restriction by id (`restricted_project_id`) while it names its
  tenant by slug; the tokens page looks the key up per tenant. A `restricted_project` key in
  `Token` removes the lookup.
- The UI cannot learn `COWORK_TOKEN_MAX_LIFETIME`: the form accepts up to the schema's 3650 days
  and the server shortens the request, which the dialog's expiry then shows.
- "Create the first tenant" is offered to a global administrator without a membership, also
  when tenants exist that they do not belong to; no route says whether a tenant exists.

The table below is the shape that was built.

**The routes.** The browser flows live outside `/api/v1`, as ADR 0037 D5 names them, and nginx
and the dev proxy get a location `^~ /auth/` to the backend:

| Route | Who | What |
|---|---|---|
| `GET /auth/options` | anyone | `{"local": bool, "oidc": false}` — what the login page offers (ADR 0033 D8) |
| `POST /auth/local` | anyone, origin-checked | `{"username","password"}` → `200 {"password_change_required": bool}` and the cookie; `401 invalid_credentials`; `429` from the address throttle; `403 not_initialised` for a person who is not a global administrator while no tenant exists (ADR 0032 D5) |
| `POST /auth/logout` | a session, CSRF-checked | deletes the session, clears the cookie, `204` |
| `PUT /api/v1/me/password` | a session only | `{"current_password","new_password"}`; ends the person's other sessions (ADR 0033 D4) |
| `POST /api/v1/me/tokens` | a session only | name, scope, agent flag and capabilities, tenant and project restriction, lifetime within `COWORK_TOKEN_DEFAULT_LIFETIME`/`_MAX_LIFETIME`; the plaintext once (ADR 0035 D4, D5) |
| `POST /api/v1/tenants` | a global administrator, session only | slug and name; the creator's marked grant as `admin` (ADR 0032 D7) |
| `GET/POST /api/v1/tenants/{tenant}/accounts` | the tenant's administrators | local accounts with a grant in the tenant; create with username, display name, a temporary password and the role (ADR 0033 D1, D4) |
| `PUT …/accounts/{username}/password`, `DELETE …/lockout`, `PUT …/deactivation`, `DELETE …/sessions` | the tenant's administrators | reset (temporary), unlock, deactivate (revokes tokens and sessions, ADR 0024 D5), end all sessions (ADR 0031 D4) |

`GET /api/v1/me` adds `global_admin`, `local` and `password_change_required`. The
`sessionCookie` scheme is declared and applied per operation (ADR 0046 D6); the four
session-only routes refuse a token with `403`.

**Security details:**
- The cookie is `__Host-cowork-session` — the `__Host-` prefix enforces ADR 0031 D2's `Secure`,
  `Path=/` and no `Domain` in the browser itself.
- Unknown usernames are throttled and locked like known ones, and every refusal of
  `/auth/local` is the same `401` with the same timing class (Argon2id runs against a dummy hash
  for an unknown name), so neither the answer nor the lockout reveals whether an account exists.
- Argon2id with the parameters recorded on the security page (at least OWASP's 19 MiB, two
  iterations, one lane); a password in no log, no audit row, no error text (ADR 0033 D4).
- A new session id at every login, the old one deleted (ADR 0031 D5); the session table holds
  the hash of the cookie only (D1).
- The origin check of ADR 0037 D1 compares against `COWORK_BASE_URL` exactly; with `make dev`
  that is `http://localhost:4200`, the origin the browser sees through the dev proxy.

1. **Sessions** ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md)): the
   table (not tenant-bound, ADR 0021 D6's list), the opaque cookie with only its hash stored,
   `HttpOnly; Secure; SameSite=Lax; Path=/`, the absolute and idle lifetimes with their variables
   and the expiry job, revocation by delete, the one resolver for cookies and tokens (D6), the
   recorded acts without the session id (D7). The OIDC parts of D1 (groups snapshot, refresh
   token) wait for phase 4.
2. **The local administrator** ([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
   D1–D3, D5–D8): `COWORK_LOCAL_ADMIN_USERNAME`/`_PASSWORD`, synced at every start under an
   advisory lock, a global administrator; the init state ("create the first tenant"); the
   optional bootstrap tenant; the chart's `localAdmin.existingSecret` with configurable keys.
3. **Local accounts** ([ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)):
   created by administrators with a marked grant, Argon2id, the length-only policy
   (`COWORK_PASSWORD_MIN_LENGTH`), the temporary password changed at first login, reset by an
   administrator, the lockout and the per-address throttle (`COWORK_LOGIN_LOCKOUT`), D8's login
   page rule.
4. **CSRF** ([ADR 0037](../adr/0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md)):
   the origin check against `COWORK_BASE_URL` and `X-Requested-With: cowork` on every unsafe
   cookie request (the frontend's interceptor sends it since T28); `COWORK_BASE_URL` required
   whenever a cookie login exists (D6).
5. **Creation routes:** a tenant by a global administrator, its creator its first administrator
   (ADR 0005 D5, ADR 0032 D7); a token by its person in a session, plaintext shown once
   (ADR 0035 D5).
6. **The UI:** the login page, the first-login password change, "create the first tenant", the
   person's tokens (create, list, revoke), the administrator's local accounts (create, reset,
   unlock, deactivate). A `401` takes the browser to the login page
   ([ADR 0053](../adr/0053-signals-and-services-no-store-framework.md) D5).
7. **The development start:** `make dev` with `COWORK_LOCAL_ADMIN_*` and the real login; the
   dev proxy's token, its proxy code and ADR 0038 D2's interim wording go.
8. **Tests:** integration tests for every rule above, with two identities — among them that a
   session survives a backend restart and dies on logout; the e2e tier of T29 logs in through
   the page.
9. **Documentation:** security pages for sessions, local accounts and CSRF (each ending with
   `## What this does not cover`, the missing second factor as an `H-<n>` gap, ADR 0033 D7); the
   README reference for every new variable, value, route and problem code; the operations pages
   for the local administrator and its recovery; the Status of ADR 0005, 0031, 0032, 0033, 0035,
   0037 and 0038.

## Verified

- Playwright against `make dev`: Chromium and WebKit are sent to the login, log in as
  `dev`, keep `__Host-cowork-session` (Secure, HttpOnly, Lax), open the live stream, write a
  comment through the session (the CSRF check passes from `https://localhost:4200`), sign out and
  are sent to the login again. Over plain `http://localhost` WebKit dropped the cookie, which is
  why development serves HTTPS.

## Open questions

### Q1: Does the chart keep other pods away from the frontend?

The backend has to trust the frontend's pods as hops, and their addresses come from the pod
network at every start, so `COWORK_TRUSTED_PROXIES` holds that network — and with it every pod.
A pod that reaches the frontend directly writes `X-Forwarded-For` itself, nginx appends the
pod's trusted address, and the walk reads what the pod wrote: it chooses its throttle bucket.
The chart's NetworkPolicy guards the backend, not the frontend (docs/security/local-accounts.md
H-17).

- **(a) Documentation only:** the operator writes a policy of their own; H-17 stays as it is.
- **(b) nginx decides the client (`real_ip`) and sends one address:** a fifth substituted nginx
  variable for the controller's networks. It helps only where the Ingress controller has a
  narrower range than the pod network (host network, an external balancer); a controller that
  runs as an ordinary Deployment gets the same pod-network entry, and the hole moves one hop
  out.
- **(c) An opt-in frontend NetworkPolicy:** `networkPolicy.frontendFrom`, a list of
  NetworkPolicyPeers (typically the Ingress controller's namespace); when set, the chart
  renders a second policy that admits only those peers to the frontend port. It cannot be on
  by default, because the chart does not know where the controller runs.

Recommended: **(c)** — the only option that closes the hole in the common topology (the
controller as a Deployment in the pod network), with the mechanism the chart already has, and
nothing changes for an installation that leaves it empty.

**Answer:** _open_
