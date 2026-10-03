# Trust boundaries of the two containers

What the backend, its migration init container and the frontend trust, whom they answer, and
where the credentials they hold live, as built on 2026-10-02. Once a request is inside a
tenant, how it is kept from other tenants and from what it may not see is
[tenancy.md](tenancy.md); what a token or an agent may do is [tokens.md](tokens.md); what an
upload may do is [attachments.md](attachments.md).

## Components and what they trust

| Component | Trusts | Verified in |
|---|---|---|
| The backend process | Its environment: every `COWORK_*` variable — the runtime role's database URL, the server key, the object storage's access key | [`backend/internal/config/config.go`](../../backend/internal/config/config.go) |
| The backend process | Every TCP peer that reaches `COWORK_LISTEN_ADDR` — the frontend pod and anything else in the cluster that reaches the backend Service — for the routes that need no token: `/healthz`, `/readyz`, `/api/v1/version`, `/api/v1/openapi.json`. Every other route under `/api/v1` requires a bearer token | [`backend/internal/api/api.go`](../../backend/internal/api/api.go) `requiresBearer`, [`authn.go`](../../backend/internal/api/authn.go) `authenticate` |
| The backend process | The row of the presented token: its person, scope, restriction, agent flag and capabilities. It knows nothing else about the caller | [`authn.go`](../../backend/internal/api/authn.go), [`store/tokens.go`](../../backend/internal/store/tokens.go) `LookupToken` |
| The migration init container | Its environment: the owner role's URL, and the runtime role's URL, whose user it grants to | [`backend-deployment.yaml`](../../deploy/helm/cowork/templates/backend-deployment.yaml), [`store/migrate.go`](../../backend/internal/store/migrate.go) `Migrate` |
| The frontend (nginx) | `BACKEND_URL` from its environment; every TCP peer that reaches it, which through an Ingress is the internet. It proxies `/api/` for anyone and passes the `Authorization` header through; it checks nothing | [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template) |
| The backend | The `X-Forwarded-*` headers nginx sets — and any a caller sets when it reaches the backend Service directly; nothing in the backend reads them | the template sets them; no backend code reads them |
| The database | Two roles: the owner role, which owns every object and runs the migrations, and the runtime role the server connects as, which owns nothing and is subject to forced row-level security | [`store/migrate.go`](../../backend/internal/store/migrate.go), [`store/roles.go`](../../backend/internal/store/roles.go), [tenancy.md](tenancy.md) "Two database roles" |
| The object storage | The access key pair the backend presents | [`backend/internal/storage/storage.go`](../../backend/internal/storage/storage.go) |
| The kubelet | `/healthz` and `/readyz` on the backend, `/healthz` on the frontend, unauthenticated | the chart's probes |

## What a network peer can do

Without a token: the UI shell, nginx's `/healthz`, the version (`/api/v1/version`) and the API
document (`/api/v1/openapi.json`). Through the backend Service, from inside the cluster,
additionally the backend's `/healthz` and `/readyz`; `/readyz` says whether the database
answers and nothing more — the ping's error, which can name the host and the user, goes to
the log only ([`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go)
`handleReadyz`). Every other route answers `401 unauthenticated` with
`WWW-Authenticate: Bearer realm="cowork"` before any tenant is looked up, so an anonymous
caller learns nothing about which tenants exist. An unknown path answers `404` and a known
path with the wrong method `405`, also before authentication; the route table is the
published API document anyway.

With a token: what the token's person may do in the tenants the token reaches, narrowed by
the token's scope and restriction and, for an agent, by the agent rules
([tokens.md](tokens.md)); nothing of a tenant the person is not a member of, and nothing
inside one that the person may not see ([tenancy.md](tenancy.md)). A token is a bearer
credential: the backend cannot tell its person from someone who copied it.

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

Reachability is the cluster's, not cowork's: the chart ships no NetworkPolicy, both Services
are `ClusterIP` by default, and an Ingress, when enabled, routes to the frontend Service.
Unless the cluster's own policies say otherwise, both Services answer every pod that reaches
them.

## Where the credentials live

| Credential | Source in the chart | Held by |
|---|---|---|
| The runtime role's URL, `COWORK_DATABASE_URL` | `database.existingSecret` (preferred), or `database.url` rendered into a release Secret | the serving container, and the migration init container, which needs the role's name for the grants |
| The owner role's URL, `COWORK_DATABASE_OWNER_URL` | `database.owner.existingSecret` (preferred), or `database.owner.url` rendered into a release Secret | the migration init container only, and only while `backend.config.migrateOnStart` is true |
| The server key, `COWORK_SESSION_KEY` | `session.existingSecret` only; the chart fails without it | the serving container |
| The storage access key, `COWORK_S3_ACCESS_KEY_ID` and `COWORK_S3_SECRET_ACCESS_KEY` | `storage.existingSecret` only, required with `storage.endpoint` | the serving container |
| Personal access tokens | not in the chart; the database holds their SHA-256 ([tokens.md](tokens.md)) | whoever holds one |

Each Secret value reaches its container through `secretKeyRef`
([`backend-deployment.yaml`](../../deploy/helm/cowork/templates/backend-deployment.yaml)); the
frontend container holds none of them. With an `existingSecret` the chart never sees the
value. The inline `database.url` and `database.owner.url` put the credential in plain text
into a release Secret and into `helm get values`; the chart notes warn at install time
([`NOTES.txt`](../../deploy/helm/cowork/templates/NOTES.txt)). The server key signs the list
cursors with a key derived from it ([`backend/internal/api/cursor.go`](../../backend/internal/api/cursor.go)):
whoever holds it can forge a cursor, which moves a page's position inside a list its caller
reads anyway, under the same predicates. Rotating the key invalidates the cursors clients
hold.

The backend does not log a credential. An error about a secret variable names the variable,
never its value ([`config.go`](../../backend/internal/config/config.go) `Load`); the request
log carries method, path, status, duration and request id — no header, so no token, no body
and no query ([`server.go`](../../backend/internal/httpserver/server.go) `requestLog`); a
refused dead token is logged by its id and the reason, never the token
([`authn.go`](../../backend/internal/api/authn.go) `recordRefusal`). golang-migrate is handed
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

<a id="h-1"></a>
### H-1 — There is no login and no session

Live today. No route creates a person, a tenant, a membership or a token, and the runtime
role has no grant to insert one; the test fixture and `make dev-seed` write them over an
administrative database connection, past row-level security
([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D6, D7). On an installation a token therefore exists only because someone holding an
administrative database credential wrote one — and that credential writes one for any
person, with any scope. The browser UI cannot authenticate: it calls `/api/v1/version` and
nothing else. Every caller of the API is a script or an agent holding a bearer token, and
whoever holds a token acts as its person within its scope, restriction and lifetime
([tokens.md](tokens.md)). Both Services answer every pod in the cluster, and the routes that
need no token answer anyone who reaches them. The gap closes with the login and the sessions
of [ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
and [ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md); until then keep an
installation reachable only from what you trust — behind an Ingress you control, or through a
port-forward — and treat the administrative database credential as the key to every account.

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

nginx sets `X-Forwarded-For`, `X-Forwarded-Proto` and `X-Real-IP`; a caller that reaches the
backend Service directly can set the same headers to any value, and the backend has no way to
tell. Nothing reads them: the request log carries no address, and the audit record carries no
address hash, which [ADR 0035](../adr/0035-personal-access-tokens.md) D2 ties to a trust rule
for forwarded addresses that does not exist yet. When something reads them, it must trust
them only from the frontend — a NetworkPolicy that admits only the frontend pods to the
backend, or a header the frontend strips and re-sets — and the page of that mechanism has to
say which.

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
