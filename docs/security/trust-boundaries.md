# Trust boundaries of the two containers

What the backend, its migration init container and the frontend trust — the identity provider
and the chat's model among it — whom they answer, and where the credentials they hold live, as
built on 2026-10-04. Once a request is inside a tenant, how it is kept from other tenants and from
what it may not see is [tenancy.md](tenancy.md); what a token or an agent may do is
[tokens.md](tokens.md); how a person logs in, what a session is and what keeps another site from
writing with one is [local-accounts.md](local-accounts.md), [identity-provider.md](identity-provider.md),
[sessions.md](sessions.md) and [csrf.md](csrf.md); what an upload may do is
[attachments.md](attachments.md); what the chat in the UI may do, and what of a tenant reaches its
model, is [chat.md](chat.md).

## Components and what they trust

| Component | Trusts | Verified in |
|---|---|---|
| The backend process | Its environment: every `COWORK_*` variable — the runtime role's database URL, the server key, the object storage's access key | [`backend/internal/config/config.go`](../../backend/internal/config/config.go) |
| The backend process | Every TCP peer that reaches `COWORK_LISTEN_ADDR` — the Ingress controller's pods, and every other pod of the cluster that reaches the backend Service, since the chart ships no NetworkPolicy — for the routes that need no credential: `/healthz`, `/readyz`, `/api/v1/version`, `/api/v1/openapi.json`, `/auth/options`, `/auth/local`, the login, which is origin-checked and throttled ([local-accounts.md](local-accounts.md)), and the identity provider's two browser navigations `/auth/oidc/login` and `/auth/callback`, which make a session only for the browser that holds the login's sealed state cookie ([identity-provider.md](identity-provider.md#the-login)). Every other route under `/api/v1` requires a bearer token or a session cookie, as the API document says per operation | [`backend/internal/api/api.go`](../../backend/internal/api/api.go) `ServeHTTP`, [`authn.go`](../../backend/internal/api/authn.go) `credentialsOf`, `authenticate` |
| The backend process | The row of the presented token — its person, scope, restriction, agent flag and capabilities — or of the presented session cookie: its person, who is a global administrator or must change a temporary password, and for a person of the identity provider their groups as of their last login or refresh. It knows nothing else about the caller | [`authn.go`](../../backend/internal/api/authn.go), [`session.go`](../../backend/internal/api/session.go), [`store/tokens.go`](../../backend/internal/store/tokens.go) `LookupToken`, [`store/sessions.go`](../../backend/internal/store/sessions.go) `LookupSession` |
| The backend process | The identity provider of `COWORK_OIDC_ISSUER`: its discovery document and the endpoints it names, its published keys, and what a verified ID token, a token answer and UserInfo say of a person — the subject, the groups, the name, the address and whether it is verified ([below](#the-identity-provider)) | [`backend/internal/oidc/oidc.go`](../../backend/internal/oidc/oidc.go), [identity-provider.md](identity-provider.md) |
| The backend process | The chat's providers at their `COWORK_CHAT_<ID>_URL`, each with what a turn that picked it sends it — nothing it answers: its text goes to the person as text and its tool calls are requests the API judges as the person's agent's ([below](#the-chats-provider)) | [`backend/internal/llm`](../../backend/internal/llm/llm.go), [chat.md](chat.md) |
| The migration init container | Its environment: the owner role's URL, and the runtime role's URL, whose user it grants to | [`backend-deployment.yaml`](../../deploy/helm/cowork/templates/backend-deployment.yaml), [`store/migrate.go`](../../backend/internal/store/migrate.go) `Migrate` |
| The frontend (nginx) | Nothing from its environment: its configuration is a file in the image. Every TCP peer that reaches it, which through an Ingress is the internet; it serves the UI's files to anyone, answers `/api/` and `/auth/` with a `404` problem, proxies nothing and reaches no backend | [`frontend/nginx/default.conf`](../../frontend/nginx/default.conf) |
| The Ingress controller (the installation's) | What the cluster administrator configures it with. It routes `/api/` and `/auth/` to the backend Service and everything else to the frontend Service for anyone, as the chart's Ingress says, and passes the `Authorization` and `Cookie` headers — and the backend's `Set-Cookie` — through; it checks nothing of cowork's. The trust rule for forwarded addresses needs it to write the address it saw as the last entry of `X-Forwarded-For`; ingress-nginx writes it in place of what the client sent | [`ingress.yaml`](../../deploy/helm/cowork/templates/ingress.yaml); ingress-nginx v1.15.1 in a kind cluster, 2026-10-04 ([installation.md](../operations/installation.md#expose-it)) |
| The backend | `X-Forwarded-For`, and only from a TCP peer inside `COWORK_TRUSTED_PROXIES` — empty by default, and then never: the client address of a login is the first address, walking the header from the right, that is not a proxy of ours ([local-accounts.md](local-accounts.md) "The client address", H-17). `X-Forwarded-Proto` and `X-Real-IP` are read by nothing | [`backend/internal/api/clientaddr.go`](../../backend/internal/api/clientaddr.go) `clientAddress` |
| The database | Two roles: the owner role, which owns every object and runs the migrations, and the runtime role the server connects as, which owns nothing and is subject to forced row-level security | [`store/migrate.go`](../../backend/internal/store/migrate.go), [`store/roles.go`](../../backend/internal/store/roles.go), [tenancy.md](tenancy.md) "Two database roles" |
| The object storage | The access key pair the backend presents | [`backend/internal/storage/storage.go`](../../backend/internal/storage/storage.go) |
| The kubelet | `/healthz` and `/readyz` on the backend, `/healthz` on the frontend, unauthenticated | the chart's probes |

## What a network peer can do

Without a credential: the UI shell, nginx's `/healthz`, the version (`/api/v1/version`), the
API document (`/api/v1/openapi.json`), what the login page offers (`GET /auth/options`: whether
an active local account exists, whether the identity provider is offered and its display name,
the minimum password length), the login itself (`POST /auth/local`), which answers every
failure alike and counts and locks by the username it was given ([local-accounts.md](local-accounts.md)),
and the identity provider's start and callback (`GET /auth/oidc/login`, `GET /auth/callback`),
which answer redirects and make a session only out of a code the issuer gave for the browser's own
sealed state ([identity-provider.md](identity-provider.md)).
Through the backend Service, from inside the cluster, additionally the backend's `/healthz` and
`/readyz`; `/readyz` says whether the database answers and nothing more — the ping's error,
which can name the host and the user, goes to the log only
([`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go)
`handleReadyz`). Every other route answers `401 unauthenticated` with
`WWW-Authenticate: Bearer realm="cowork"` before any tenant is looked up, so an anonymous
caller learns nothing about which tenants exist. An unknown path answers `404` and a known
path with the wrong method `405`, also before authentication; the route table is the
published API document anyway.

With a token: what the token's person may do in the tenants the token reaches, narrowed by
the token's scope and restriction and, for an agent, by the agent rules
([tokens.md](tokens.md)); nothing of a tenant the person is not a member of, and nothing
inside one that the person may not see ([tenancy.md](tenancy.md)). A token is a bearer
credential: the backend cannot tell its person from someone who copied it. With a session
cookie: what the person's role allows, with no agent rule and no scope, for writes only from
the installation's own origin ([sessions.md](sessions.md), [csrf.md](csrf.md)); a session is
a bearer credential too ([sessions.md](sessions.md) H-15).

What a token buys before the handler checks the role, the scope and the agent rules: the
tenant boundary, and the request's validation against the API document, which reads a JSON
body — at most `COWORK_MAX_JSON_BODY`, 1 MiB by default — into memory, within the request
timeout. An upload's body is read only by its handler, after those checks and inside the
upload budget ([attachments.md](attachments.md) H-12): the validator neither reads a multipart
body nor runs its own security check, which would read every body first
([`api/validate.go`](../../backend/internal/api/validate.go) `unsecured`;
`TestAnUploadIsRefusedBeforeItsBodyIsRead`). How many requests a token sends at once is not
bounded ([ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D1).

Reachability is the cluster's: both Services are `ClusterIP` by default; an Ingress, when
enabled, routes `/api/` and `/auth/` to the backend Service and everything else to the frontend
Service ([`ingress.yaml`](../../deploy/helm/cowork/templates/ingress.yaml),
[ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3); and the chart ships no NetworkPolicy, so every pod of the cluster reaches both Services unless
a policy of the cluster's says otherwise. That matters for the one rule that trusts the network:
the backend reads `X-Forwarded-For` from the peers in `COWORK_TRUSTED_PROXIES`, and a pod inside
those networks that reaches the backend chooses its client address
([local-accounts.md](local-accounts.md#h-17) H-17; verified in a kind cluster on 2026-10-04). With
the list empty — the default — no peer is trusted, and a pod that reaches the backend is its own
client. Which pods a policy should admit is
[installation.md](../operations/installation.md#network-policies-are-the-clusters).

## The identity provider

With `COWORK_OIDC_ISSUER` set, the backend is a relying party of one issuer, and the issuer is
trusted for what it is asked: who a person is (the issuer and the subject), which groups they are
in — which decides whether they get in, whether they administer the installation, and through the
tenants' mappings which tenants they belong to in which role — their name, their e-mail address and
whether the issuer verified it ([identity-provider.md](identity-provider.md)). Whoever can change a
person's groups at the issuer changes what they may do in cowork, from their next login or refresh;
whoever controls the issuer's signing keys or its token endpoint can be anybody.

What is checked rather than trusted: the discovery document's `issuer` against the configured string,
exactly; an ID token's signature against the keys the issuer publishes, with an asymmetric algorithm,
its issuer, its audience, its authorized party, its expiry and its nonce; that UserInfo and a
refreshed ID token speak of the same subject; the `state` the browser brings back against the sealed
cookie the login began with. The issuer is reached over TLS — `http://` only on a loopback host —
and so is every endpoint its discovery names, or the start is refused; an end-session endpoint that
fails the rule is dropped. The backend's client follows no redirect of the issuer's, reads at most
1 MiB of any answer, and writes no answer's body into an error
([`oidc/client.go`](../../backend/internal/oidc/client.go)). The backend calls out to the issuer at
every start (discovery), at a login (the token endpoint, the keys, UserInfo) and at every groups
refresh; the chart ships no NetworkPolicy, and a policy of the cluster's that restricts the
backend's egress must admit the issuer
([installation.md](../operations/installation.md#the-identity-provider)). The issuer never calls
the backend: everything it sends comes through the browser, to `/auth/callback`.

## The chat's provider

With `COWORK_CHAT_PROVIDERS` set, the backend calls the model of the provider a turn picked for the
chat in the UI
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)).
A provider is a boundary the other way round from the issuer: it is trusted with what a turn sends
it — the instructions, the conversation, and every tool's answer of the turn, which is the text of
the tenant's tickets — and with nothing it answers. Its text reaches the person as text; its tool
calls are requests of the person's session marked as the chat's agent, which the API judges like any
agent's, with the capabilities the person chose; a refusal is an answer the model reads. Every
configured provider may receive what the person can read in the turn's tenant, confidential tickets
included — a risk the owner accepted; the operator's list of providers is the only gate
([chat.md](chat.md#what-reaches-a-provider), H-37).

What is checked rather than trusted: the address is configuration only, never a request's, and
`https://` or `http://` on a host of the operator's own network by its name
([chat.md H-41](chat.md#h-41)); the gateway follows no redirect, gives up on a provider that has not
begun to answer in two minutes or stays silent for ninety seconds, reads bounded answers, and keeps
the provider's error message out of what the person sees and out of the log but for a clip without
the key ([`llm/client.go`](../../backend/internal/llm/client.go)). The backend calls out to the
provider at every call of the model in a turn, never at start; the chart ships no NetworkPolicy, and a
policy of the cluster's that restricts the backend's egress must admit the provider and DNS
([docs/operations/chat.md](../operations/chat.md#in-the-chart)). The provider never calls the
backend.

## Where the credentials live

| Credential | Source in the chart | Held by |
|---|---|---|
| The runtime role's URL, `COWORK_DATABASE_URL` | `database.existingSecret` (preferred), or `database.url` rendered into a release Secret | the serving container, and the migration init container, which needs the role's name for the grants |
| The owner role's URL, `COWORK_DATABASE_OWNER_URL` | `database.owner.existingSecret` (preferred), or `database.owner.url` rendered into a release Secret | the migration init container only, and only while `backend.config.migrateOnStart` is true |
| The server key, `COWORK_SESSION_KEY` | `session.existingSecret` only; the chart fails without it | the serving container |
| The storage access key, `COWORK_S3_ACCESS_KEY_ID` and `COWORK_S3_SECRET_ACCESS_KEY` | `storage.existingSecret` only, required with `storage.endpoint` | the serving container |
| The local administrator, `COWORK_LOCAL_ADMIN_USERNAME` and `COWORK_LOCAL_ADMIN_PASSWORD` | `localAdmin.existingSecret` (preferred; the key names are values), or `localAdmin.username` and `localAdmin.password` rendered into a release Secret | the serving container; the account follows it at every start ([local-accounts.md](local-accounts.md) H-20) |
| The identity provider's client secret, `COWORK_OIDC_CLIENT_SECRET` | `auth.oidc.existingSecret` only — there is no inline value — under `auth.oidc.keys.clientSecret`; the client id is a value, or from the same Secret under `auth.oidc.keys.clientId` | the serving container, which sends it to the issuer's token endpoint |
| The issuer's refresh tokens | not in the chart; sealed in `sessions.refresh_token_sealed` under a key derived from the server key ([identity-provider.md](identity-provider.md#what-cowork-keeps-of-the-issuers-tokens)) | the issuer; whoever holds the database, the server key and the client secret ([identity-provider.md](identity-provider.md#h-27) H-27) |
| A chat provider's API key, `COWORK_CHAT_<ID>_API_KEY` | that provider's `chat.providers[].existingSecret` only — one Secret per provider, there is no inline value — under its `keys.apiKey`; required for kind `anthropic`, optional for `openai` | the serving container, which sends it to that provider's host and to no other ([chat.md](chat.md#what-reaches-a-provider)) |
| A login's state, nonce and PKCE verifier | not in the chart; the cookie `__Host-cowork-oidc`, sealed under a key derived from the server key, for ten minutes | the browser that began the login |
| Personal access tokens | not in the chart; the database holds their SHA-256 ([tokens.md](tokens.md)) | whoever holds one |
| Session cookies | not in the chart; the database holds their SHA-256 ([sessions.md](sessions.md)) | the browser that logged in, and whoever steals the cookie |
| Local passwords | not in the chart (but the local administrator's); the database holds Argon2id hashes ([local-accounts.md](local-accounts.md)) | the person, and the administrator who set a temporary one |

Each Secret value reaches its container through `secretKeyRef`
([`backend-deployment.yaml`](../../deploy/helm/cowork/templates/backend-deployment.yaml)); the
frontend container holds none of them. With an `existingSecret` the chart never sees the
value. The inline `database.url`, `database.owner.url` and `localAdmin.username` with
`localAdmin.password` put the credential in plain text
into a release Secret and into `helm get values`; the chart notes warn at install time
([`NOTES.txt`](../../deploy/helm/cowork/templates/NOTES.txt)). The identity provider's client
secret and the chat providers' API keys have no inline path at all.

The server key is one secret with six uses, each under a key derived from it by HKDF-SHA256 with a
label of its own, so no two uses share a key:

| Use | Label | Whoever holds the server key |
|---|---|---|
| signing the list cursors ([`api/cursor.go`](../../backend/internal/api/cursor.go)) | `cowork cursor v1` | forges a cursor, which moves a page's position inside a list its caller reads anyway, under the same predicates |
| sealing a rank position in a cursor | `cowork cursor position v1`, `cowork cursor nonce v1` | reads a rank key the cursor carries |
| keying the fingerprint of an idempotent request ([`api/server.go`](../../backend/internal/api/server.go) `newFingerprintKey`) | `cowork idempotency fingerprint v1` | tests a guessed request body — a temporary password among them — against a fingerprint kept for a day |
| hashing a login's client address for the throttle ([`api/login.go`](../../backend/internal/api/login.go) `newAddressKey`) | `cowork login address v1` | tests a guessed address against `login_attempts`, which keeps its rows fifteen minutes |
| hashing the client address of an audit row (`newSourceKey`) | `cowork audit address v1` | reverses every IPv4 hash in the record ([tokens.md](tokens.md#h-30) H-30) |
| sealing a login's state cookie and a session's refresh token ([`auth/seal.go`](../../backend/internal/auth/seal.go)) | `cowork oidc login v1`, `cowork oidc refresh token v1` | opens the stored refresh tokens ([identity-provider.md](identity-provider.md#h-27) H-27) |

It signs no session: a session is a random value and a row. There is one key and no previous one
kept beside it, so rotating it invalidates the cursors clients hold and the stored fingerprints,
fails the logins in flight, gives every address another hash, and ends each session of the identity
provider that holds a refresh token at its next refresh — a session of the local login, and one
without a refresh token, stays.

The backend does not log a credential. An error about a secret variable names the variable,
never its value ([`config.go`](../../backend/internal/config/config.go) `Load`); the request
log carries method, path, status, duration and request id — no header, so no token, no body
and no query ([`server.go`](../../backend/internal/httpserver/server.go) `requestLog`); a
refused dead token is logged by its id and the reason, never the token
([`authn.go`](../../backend/internal/api/authn.go) `recordRefusal`); a login through the identity
provider logs no code and no token of the issuer, and stores none but the sealed refresh token
([identity-provider.md](identity-provider.md#what-is-recorded-and-logged)); a failure of the chat's
provider is logged as a clip of its message with the configured key taken out, and no turn's
messages are logged at all ([chat.md](chat.md#what-is-recorded-and-logged), H-42). golang-migrate is handed
an open connection, never the URL ([`migrate.go`](../../backend/internal/store/migrate.go)
`applyMigrations`). A pgx connection error can name the host and the user, not the password;
a malformed URL is reported by pgx with its password masked on a best-effort basis, and pgx
itself states that a malformed string can defeat the masking. Not verified: whether an error
of the storage client can carry the access key id; the secret key never travels, because an
S3 signature does not transmit it.

nginx's access log is the configuration's own `cowork` format on stdout: the time, the method, the
path without its query, the protocol, the status, the size and the duration — no client
address, no header and no query string, like the backend's request log
([`frontend/nginx/default.conf`](../../frontend/nginx/default.conf); verified in the built
image). `/healthz` is not logged. nginx's error log is the image's default on stderr, and its line
for a request nginx refuses itself carries the request line, the query included — H-14. The
Ingress controller's logs are the installation's: a controller that logs the request line — the
default access log of ingress-nginx does — carries the client address and the query of every
request, H-14 as well.

## The pods

The backend image is `gcr.io/distroless/static-debian12:nonroot`, one static binary, no
shell; the chart runs it as UID/GID 65532, and the migration init container runs the same
image with the same security context. The frontend image is
`nginxinc/nginx-unprivileged:1.31-alpine`, which has a shell; the chart runs it as UID/GID
101. Both run with `runAsNonRoot`, a read-only root filesystem, all capabilities dropped,
`allowPrivilegeEscalation: false`, the `RuntimeDefault` seccomp profile, and a ServiceAccount
whose token is not mounted ([`values.yaml`](../../deploy/helm/cowork/values.yaml),
`backend.podSecurityContext`, `frontend.podSecurityContext`, the two `securityContext`s,
`serviceAccount.automountServiceAccountToken`). The chart renders no Role, ClusterRole or
binding: neither container has a Kubernetes API client.

The backend writes no files: an upload is buffered in memory under the container's memory
limit, 256 MiB by default ([attachments.md](attachments.md) H-12). nginx writes its pid and its
temporary files under `/tmp`; the chart mounts an `emptyDir` there and nothing else is writable.
It proxies nothing, so no request body and no answer of the backend passes through it — an
upload is buffered, if at all, by the Ingress controller. When `backend.config.migrateOnStart` is true
(the default), the init container `migrate` alone holds the owner credential; the serving
container gets `COWORK_MIGRATE_ON_START=false` and refuses to start on pending migrations or
on a runtime role that could bypass row-level security. With the setting false, the chart
renders no owner Secret and no container holds the owner credential, and running
`cowork migrate` is the operator's step — but an inline `database.owner.url` stays in the
release's values, readable with `helm get values`, so leave it empty.
`backend.extraEnv` and `frontend.extraEnv` append variables verbatim: an owner URL added
there reaches the serving container ([tenancy.md](tenancy.md) "The owner credential in the
serving process").

The frontend's configuration is a file in the image, owned by root
([`Containerfile`](../../frontend/Containerfile)): nothing is substituted at start, so no
environment variable changes it, and it resolves no name. Whoever can change the image, or mount a
volume over `/etc/nginx/conf.d`, changes what the UI is, its content-security policy included —
the image's publisher and whoever may edit the Deployment. Every bearer token and session cookie
passes the Ingress controller, not the frontend: whoever may change the Ingress object or the
controller's configuration can send `/api/` or `/auth/` anywhere and receive what passes — the
cluster administrator and whoever may write Ingress objects in the namespace.

## The shell's content-security policy

The frontend's nginx sends one `Content-Security-Policy` with everything it serves of the UI —
`index.html` for every path the router owns, the hashed bundles and fonts, the icons
([`frontend/nginx/default.conf`](../../frontend/nginx/default.conf) `$ui_csp`; an
`add_header` in a location replaces the server's, so each of the three locations adds it). It exists
since the chat put a model's output into the page
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)
D6), as the second line behind the panel that shows that output as text
([chat.md](chat.md#what-the-panel-shows)).

| Directive | Allows | Why |
|---|---|---|
| `default-src 'self'` | the origin, for whatever no other directive names | — |
| `script-src 'self'` | the bundle's files; no inline script, no `eval` | the production build inlines no critical CSS for it: the inliner loads the stylesheet through an inline `onload` handler (`"inlineCritical": false` in [`angular.json`](../../frontend/angular.json)) |
| `style-src 'self' 'unsafe-inline'` | the stylesheet, and `<style>` elements and `style` attributes in the page | PrimeNG writes its theme, and Angular its components' styles, as `<style>` elements at run time, which no hash fixed at build time covers |
| `img-src 'self' data: blob:` | the origin's images and the `data:` and `blob:` URLs the page makes | — |
| `font-src 'self' data:` | the self-hosted fonts and icons | — |
| `connect-src 'self'` | requests to the origin only — `/api/`, `/auth/`, the event stream, the chat's stream | `fetch`, `EventSource` and sockets of the page reach no other host; a top-level navigation is outside what a policy governs |
| `frame-ancestors 'none'` | no page may frame the UI | clickjacking |
| `base-uri 'self'`, `form-action 'self'`, `object-src 'none'` | no `<base>` of another origin, no form posting elsewhere, no plugin | — |

`'unsafe-inline'` for styles is what the policy concedes. Markup from people's texts reaches the page
only as the server rendered and sanitised it, which carries no `style` attribute and no `<style>`
element, and through Angular's sanitiser ([rendered-markdown.md](rendered-markdown.md)); should other
markup ever reach the page, it could restyle the page — hide a control, imitate one — but not load
anything from another origin, because `img-src`, `font-src` and `connect-src` keep every request on
the origin, and run no script. An image in a rendered text is an attachment of the origin, which
`img-src 'self'` admits. The
PrimeUI license is checked in the page, offline, and needs no source of its own. The answers of
`/api/` and `/auth/` carry no policy of the shell's — they are no documents — and an attachment's
content carries its own `sandbox` ([attachments.md](attachments.md)). Verified on 2026-10-04 against
the production bundle behind nginx with this policy, in Chromium and WebKit, in both colour schemes,
with the API mocked: no violation was reported while the shell, the settings and the chat panel ran
a turn; and again on 2026-10-04, after the routing moved to the Ingress, in Chromium behind the
Ingress stand-in and behind ingress-nginx with the real backend: no violation through the login,
the tenant page and the backlog. Since 2026-10-06 the end-to-end tier watches the policy on two
paths over the built images behind the Ingress stand-in, in Chromium and WebKit and both colour
schemes, and fails on any refusal: a ticket's page whose body renders headings, a table, code, a link,
its own image and hostile lines ([`rendered.spec.ts`](../../frontend/e2e/rendered.spec.ts)), and the
search from the top bar through the results of a tenant and of every tenant of the person to a hit's
comment ([`search.spec.ts`](../../frontend/e2e/search.spec.ts)); no violation is reported on either.
Not verified: the other pages under the policy — the suite's other paths run under it but do not
watch for a refusal — a page that needs another source fails in the browser with a violation in the
console, and nginx has no unit test.

## What this does not cover

<a id="h-14"></a>
### H-14 — The Ingress controller's log, and nginx's error log, carry a request's query

Live today. Every request passes the Ingress controller, and what it logs is the installation's,
not cowork's: a controller that writes the request line into its access log writes the query string
with it, so a full-text search (`?q=`) and the identity provider's `code` and `state` on
`/auth/callback` land in the controller's log and in whatever collects it. ingress-nginx's default
access log does exactly that (`log_format upstreaminfo`, with the client address and `"$request"`,
verified in v1.15.1's generated configuration on 2026-10-04). The frontend's nginx logs no query in
its access log, but when it refuses a request itself — a body above its 1 MiB limit on a path that
reached it by mistake — it writes an error line on stderr with the client address and the whole
request line; it proxies nothing, so no failed proxying is logged there any more. The configuration
sets no `error_log`, and raising its level to `crit` would drop the diagnosis along with the query.
Mitigation: treat the controller's log and the frontend's error log as holding search terms and
one-time login codes, and keep their retention and their readers to those who may read the
tickets; where the controller's log format is configurable, leave the query out of it.

### The forwarded headers

The Ingress controller writes `X-Forwarded-For` — the trust rule needs the address it saw as the
header's last entry, which ingress-nginx writes in place of what the client sent — and may set
`X-Forwarded-Proto` and `X-Real-IP`; a caller that reaches the backend directly can set the same
headers to any value.
The trust rule of [ADR 0035](../adr/0035-personal-access-tokens.md) D2 decides what the backend
does with that: `X-Forwarded-For` is read only when the TCP peer is inside
`COWORK_TRUSTED_PROXIES`, from the right, up to the first address that is not a proxy of ours,
and nothing to the left of it is ever read ([local-accounts.md](local-accounts.md) "The client
address"). Two readers use what it finds: the login's throttle and the keyed hash every audit row
of a request carries ([tokens.md](tokens.md#what-is-recorded)). The request log carries no
address. Where the rule is wrong — an empty list, a list too narrow or too wide, a trusted network
that holds pods no policy of the cluster's keeps away from the backend — what it costs is
[local-accounts.md](local-accounts.md) H-17, and an audit row's hash names the wrong client in the
same way; `X-Forwarded-Proto` and `X-Real-IP` are read by nothing.

### The identity provider's own controls

How a person proves who they are to the issuer — a second factor, a password policy, a lockout —
who is in which group and who may change that, how long the issuer's own session and its refresh
tokens live, and how it signs its tokens are the issuer's and its operators'; cowork takes the
issuer's word ([above](#the-identity-provider)) and verifies none of it. Where the issuer has a
second factor, a person who logs in through it has one; the local login has none
([local-accounts.md](local-accounts.md#h-16) H-16).

### The database's own controls

TLS to the database (`sslmode`), backups, encryption at rest, who else may connect, and the
roles' attributes beyond what the start-up check verifies ([tenancy.md](tenancy.md) "Two
database roles") — all the database's, none enforced by cowork. The URLs are passed through
as given. Who else may connect matters for the event channel: [tenancy.md](tenancy.md) H-4.

### The transport in front of the pods

Both containers speak plain HTTP. TLS, client certificates, IP allow-lists and rate limits
are the Ingress controller's or the mesh's; cowork has no request budget
([ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D1). nginx's own hardening beyond `server_tokens off` and the shell's content-security policy
([above](#the-shells-content-security-policy)) — `X-Content-Type-Options`, a `Referrer-Policy`,
`Strict-Transport-Security` for the UI shell — is not configured; HSTS belongs to whatever
terminates TLS in front. An attachment download carries its own `nosniff` and `sandbox`
from the backend ([attachments.md](attachments.md)).
