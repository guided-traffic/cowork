# Trust boundaries of the two containers

What the backend, its migration init container and the frontend trust — the identity provider
among it — whom they answer, and where the credentials they hold live, as built on 2026-10-04.
Once a request is inside a tenant, how it is kept from other tenants and from what it may not see
is [tenancy.md](tenancy.md); what a token or an agent may do is [tokens.md](tokens.md); how a
person logs in, what a session is and what keeps another site from writing with one is
[local-accounts.md](local-accounts.md), [identity-provider.md](identity-provider.md),
[sessions.md](sessions.md) and [csrf.md](csrf.md); what an upload may do is
[attachments.md](attachments.md).

## Components and what they trust

| Component | Trusts | Verified in |
|---|---|---|
| The backend process | Its environment: every `COWORK_*` variable — the runtime role's database URL, the server key, the object storage's access key | [`backend/internal/config/config.go`](../../backend/internal/config/config.go) |
| The backend process | Every TCP peer that reaches `COWORK_LISTEN_ADDR` — the frontend pods, and with `networkPolicy.enabled=false` or a network plugin that does not enforce it anything else in the cluster that reaches the backend Service — for the routes that need no credential: `/healthz`, `/readyz`, `/api/v1/version`, `/api/v1/openapi.json`, `/auth/options`, `/auth/local`, the login, which is origin-checked and throttled ([local-accounts.md](local-accounts.md)), and the identity provider's two browser navigations `/auth/oidc/login` and `/auth/callback`, which make a session only for the browser that holds the login's sealed state cookie ([identity-provider.md](identity-provider.md#the-login)). Every other route under `/api/v1` requires a bearer token or a session cookie, as the API document says per operation | [`backend/internal/api/api.go`](../../backend/internal/api/api.go) `ServeHTTP`, [`authn.go`](../../backend/internal/api/authn.go) `credentialsOf`, `authenticate` |
| The backend process | The row of the presented token — its person, scope, restriction, agent flag and capabilities — or of the presented session cookie: its person, who is a global administrator or must change a temporary password, and for a person of the identity provider their groups as of their last login or refresh. It knows nothing else about the caller | [`authn.go`](../../backend/internal/api/authn.go), [`session.go`](../../backend/internal/api/session.go), [`store/tokens.go`](../../backend/internal/store/tokens.go) `LookupToken`, [`store/sessions.go`](../../backend/internal/store/sessions.go) `LookupSession` |
| The backend process | The identity provider of `COWORK_OIDC_ISSUER`: its discovery document and the endpoints it names, its published keys, and what a verified ID token, a token answer and UserInfo say of a person — the subject, the groups, the name, the address and whether it is verified ([below](#the-identity-provider)) | [`backend/internal/oidc/oidc.go`](../../backend/internal/oidc/oidc.go), [identity-provider.md](identity-provider.md) |
| The migration init container | Its environment: the owner role's URL, and the runtime role's URL, whose user it grants to | [`backend-deployment.yaml`](../../deploy/helm/cowork/templates/backend-deployment.yaml), [`store/migrate.go`](../../backend/internal/store/migrate.go) `Migrate` |
| The frontend (nginx) | `BACKEND_URL` from its environment; every TCP peer that reaches it, which through an Ingress is the internet. It proxies `/api/` and `/auth/` for anyone and passes the `Authorization` and `Cookie` headers — and the backend's `Set-Cookie` — through; it checks nothing | [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template) |
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

Reachability is the cluster's, with one policy of the chart's: both Services are `ClusterIP`
by default, an Ingress, when enabled, routes to the frontend Service, and the chart's
NetworkPolicy ([`networkpolicy.yaml`](../../deploy/helm/cowork/templates/networkpolicy.yaml),
`networkPolicy.enabled`, on by default) admits only the frontend's pods to the backend's pods,
on the backend's port. It exists for the one rule that trusts the network: the backend reads
`X-Forwarded-For` from the proxies in `COWORK_TRUSTED_PROXIES`, and a pod that is no proxy of
ours must not be able to reach the backend and write it. The frontend Service still answers
every pod that reaches it. A network plugin that does not implement NetworkPolicy ignores the
object; Kubernetes says so itself, and says that traffic from the node a pod runs on — the
kubelet's probes — is always allowed. Not verified against a cluster: the policy has been
rendered and linted, not enforced; and `kubectl port-forward`, which reaches the pod through
its own network namespace, is not expected to be blocked either.

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
refresh; the chart's NetworkPolicy restricts no egress, and a policy of the installation's own must
admit the issuer
([installation.md](../operations/installation.md#the-identity-provider)). The issuer never calls
the backend: everything it sends comes through the browser, to `/auth/callback`.

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
secret has no inline path at all.

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
([identity-provider.md](identity-provider.md#what-is-recorded-and-logged)). golang-migrate is handed
an open connection, never the URL ([`migrate.go`](../../backend/internal/store/migrate.go)
`applyMigrations`). A pgx connection error can name the host and the user, not the password;
a malformed URL is reported by pgx with its password masked on a best-effort basis, and pgx
itself states that a malformed string can defeat the masking. Not verified: whether an error
of the storage client can carry the access key id; the secret key never travels, because an
S3 signature does not transmit it.

nginx's access log is the template's own `cowork` format on stdout: the time, the method, the
path without its query, the protocol, the status, the size and the duration — no client
address, no header and no query string, like the backend's request log
([`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template); verified
in the built image). `/healthz` is not logged. nginx's error log is the image's default on
stderr, and its line for a request nginx itself failed carries the query — H-14.

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
limit, 256 MiB by default ([attachments.md](attachments.md) H-12). nginx writes its pid, its
temporary files — request bodies and proxied responses larger than its memory buffers among
them — and the rendered configuration under `/tmp` and `/etc/nginx/conf.d`; the chart mounts
`emptyDir`s there and nothing else is writable. When `backend.config.migrateOnStart` is true
(the default), the init container `migrate` alone holds the owner credential; the serving
container gets `COWORK_MIGRATE_ON_START=false` and refuses to start on pending migrations or
on a runtime role that could bypass row-level security. With the setting false, the chart
renders no owner Secret and no container holds the owner credential, and running
`cowork migrate` is the operator's step — but an inline `database.owner.url` stays in the
release's values, readable with `helm get values`, so leave it empty.
`backend.extraEnv` and `frontend.extraEnv` append variables verbatim: an owner URL added
there reaches the serving container ([tenancy.md](tenancy.md) "The owner credential in the
serving process").

The nginx template is rendered with `envsubst` at start. The image is told to substitute
`BACKEND_URL`, `NGINX_LOCAL_RESOLVERS`, `NGINX_CLIENT_MAX_BODY_SIZE` and
`NGINX_PROXY_READ_TIMEOUT` and nothing else (`NGINX_ENVSUBST_FILTER` in the
[`Containerfile`](../../frontend/Containerfile)), so an unexpected environment variable
cannot change the configuration; the chart computes the last two from the backend's limits
([`_helpers.tpl`](../../deploy/helm/cowork/templates/_helpers.tpl)). All four are trusted as
given: whoever can set the frontend pod's environment can point `/api/` anywhere — and then
receives every bearer token sent through the frontend — or lift the body limit, and whoever
controls the pod's `/etc/resolv.conf` — the cluster DNS — controls where the backend name
resolves to on every request. That is the chart, the kubelet and the cluster administrator.

## What this does not cover

<a id="h-14"></a>
### H-14 — nginx's error log carries the query of a request nginx failed

Live today. When nginx fails a request itself — the backend unreachable (`502`), no answer in
time (`504`), a body over `client_max_body_size` (`413`) — it writes an error line on stderr
with the client address and the whole request line, the query string included. A full-text
search (`?q=`) then lands in the frontend's log and in whatever collects the pod logs; a
request nginx passes on is not logged with its query. The template sets no `error_log`, and
raising its level to `crit` would drop the diagnosis along with the query. Mitigation: treat
the frontend's log as holding search terms, and keep its retention and its readers to those
who may read the tickets.

### The forwarded headers

nginx sets `X-Forwarded-For`, `X-Forwarded-Proto` and `X-Real-IP` in each of its three proxied
locations; a caller that reaches the backend directly can set the same headers to any value.
The trust rule of [ADR 0035](../adr/0035-personal-access-tokens.md) D2 decides what the backend
does with that: `X-Forwarded-For` is read only when the TCP peer is inside
`COWORK_TRUSTED_PROXIES`, from the right, up to the first address that is not a proxy of ours,
and nothing to the left of it is ever read ([local-accounts.md](local-accounts.md) "The client
address"). Two readers use what it finds: the login's throttle and the keyed hash every audit row
of a request carries ([tokens.md](tokens.md#what-is-recorded)). The request log carries no
address. Where the rule is wrong — an empty list, a list too narrow or too wide, a policy that is
not enforced — what it costs is [local-accounts.md](local-accounts.md) H-17, and an audit row's
hash names the wrong client in the same way; `X-Forwarded-Proto` and `X-Real-IP` are read by
nothing.

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
D1). nginx's own hardening beyond `server_tokens off` — a `Content-Security-Policy`,
`X-Content-Type-Options` and frame options for the UI shell — is not configured; the shell
holds no credential today. An attachment download carries its own `nosniff` and `sandbox`
from the backend ([attachments.md](attachments.md)).
