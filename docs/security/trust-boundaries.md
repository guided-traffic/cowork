# Trust boundaries of the skeleton

What the two containers as they exist on 2026-09-29 trust, whom they answer, and where the one
credential they hold lives. This page will be rewritten when authentication exists; a reader
who wants the intended model — tenants, OIDC groups, personal access tokens — finds the open
decisions in [docs/planning/questions.md](../planning/questions.md), not here, because nothing
of it is built.

## Components and what they trust

| Component | Trusts | Verified in |
|---|---|---|
| The backend process | Its environment: every `COWORK_*` variable, above all `COWORK_DATABASE_URL` | [`backend/internal/config/config.go`](../../backend/internal/config/config.go) |
| The backend process | Every TCP peer that reaches `COWORK_LISTEN_ADDR` — the frontend pod, and anything else in the cluster that can reach the backend Service | [`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go): no handler checks an identity |
| The frontend (nginx) | `BACKEND_URL` from its environment; every TCP peer that reaches it, which through the Ingress is the internet | [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template): `/api/` is proxied for anyone |
| The backend | The `X-Forwarded-*` headers nginx sets — and any a caller sets when it reaches the backend Service directly, because the backend does not distinguish the two | the template sets them; nothing in the backend reads them yet |
| The database | The role in the URL, with DDL rights | [`backend/internal/store/migrate.go`](../../backend/internal/store/migrate.go) runs the migrations under it |
| The kubelet | `/healthz` and `/readyz` on the backend, `/healthz` on the frontend, unauthenticated | the chart's probes |

## What a network peer can do today

Through the frontend: read the UI shell, nginx's `/healthz`, and everything under `/api/` the
backend offers — today `/api/v1/version` (the version, the commit, the build time). Through the
backend Service, from inside the cluster: additionally `/readyz`, which reveals whether the
database answers and the ping error text when it does not. Nothing writes to the database
through the API yet, so a peer cannot change data. Reachability is the only control, and it is
the cluster's, not cowork's: the chart ships no NetworkPolicy, the backend Service is
`ClusterIP`, and the frontend Service is `ClusterIP` unless an Ingress is enabled.

## Where the credential lives

The database URL, password included, is one environment variable in the backend container,
read from a Secret through `secretKeyRef`
([`deploy/helm/cowork/templates/backend-deployment.yaml`](../../deploy/helm/cowork/templates/backend-deployment.yaml)).
The frontend container never sees it.
With `database.existingSecret` the chart never sees the value. With `database.url` the value
is in the Helm release Secret and in `helm get values` — the chart notes say so at install
time, and the operations page tells you not to do this outside a throw-away installation.

The process does not log the URL: the configuration error path names the variable, not its
value ([`config.go`](../../backend/internal/config/config.go), `Load`), and the request log
carries method, path, status and duration ([`server.go`](../../backend/internal/httpserver/server.go),
`requestLog`). A pgx connection error can include the host and the user; it does not include
the password. Not verified: every error path of golang-migrate; its errors wrap pgx's.

## The pods

The backend image is `gcr.io/distroless/static-debian12:nonroot`, one static binary, no
shell; the chart runs it as UID/GID 65532. The frontend image is
`nginxinc/nginx-unprivileged:1.30-alpine`, which has a shell; the chart runs it as UID/GID 101.
Both run with `runAsNonRoot`, a read-only root filesystem, all capabilities dropped,
`allowPrivilegeEscalation: false`, the `RuntimeDefault` seccomp profile, and a ServiceAccount
whose token is not mounted ([`values.yaml`](../../deploy/helm/cowork/values.yaml),
`backend.podSecurityContext`, `frontend.podSecurityContext`, the two `securityContext`s,
`serviceAccount.automountServiceAccountToken`). The backend writes no files. nginx writes its
pid and temp files under `/tmp` and the rendered configuration under `/etc/nginx/conf.d`; the
chart mounts `emptyDir`s there and nothing else is writable. The chart renders no Role,
ClusterRole or binding: neither container has a Kubernetes API client.

The nginx template is rendered with `envsubst` at start. The image is told to substitute
`BACKEND_URL` and `NGINX_LOCAL_RESOLVERS` and nothing else (`NGINX_ENVSUBST_FILTER` in the
[`Containerfile`](../../frontend/Containerfile)), so an unexpected environment variable cannot
change the configuration. Both are trusted as given: whoever can set the frontend pod's
environment can point `/api/` anywhere, and whoever controls the pod's `/etc/resolv.conf` —
the cluster DNS — controls where the backend name resolves to on every request. That is the
chart, the kubelet and the cluster administrator.

## What this does not cover

<a id="h-1"></a>
### H-1 — There is no authentication and no tenancy

Live today. Any peer that can reach either port is every user, and the frontend proxies
`/api/` to the backend for anyone. The endpoints that exist expose nothing beyond version and
liveness, so the exposure today is information (version, commit, and through the backend
Service the database reachability), not data. The gap closes when the authentication decisions
in the planning catalog become ADRs and code; until then an installation must not be reachable
from anything it does not trust, and the operations page says to keep it behind an Ingress you
control or a port-forward.

### The forwarded headers

nginx sets `X-Forwarded-For`, `X-Forwarded-Proto` and `X-Real-IP`; a caller that reaches the
backend Service directly can set the same headers to any value, and the backend has no way to
tell. Nothing reads them today. When something does (audit, rate limits, redirect URLs), it
must trust them only from the frontend — a NetworkPolicy that admits only the frontend pods to
the backend, or a header the frontend strips and re-sets — and the page of that mechanism has
to say which.

### The database's own controls

TLS to the database (`sslmode`), the role's privileges beyond what the migrations need,
backups, encryption at rest — all the database's, none verified or enforced by cowork. The
URL is passed through as given.

### The transport in front of the pods

Both containers speak plain HTTP. TLS, client certificates, IP allow-lists and rate limits are
the Ingress controller's or the mesh's. nginx's own hardening beyond `server_tokens off` — a
`Content-Security-Policy`, `X-Content-Type-Options`, frame options — is not configured yet;
it belongs with the authentication work, when the UI has something worth protecting.
