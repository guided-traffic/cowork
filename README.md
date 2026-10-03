# cowork

[![Build Status](https://github.com/guided-traffic/cowork/actions/workflows/release.yml/badge.svg)](https://github.com/guided-traffic/cowork/actions)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/guided-traffic/cowork/main/.github/badges/coverage.json)](https://github.com/guided-traffic/cowork)
[![Go Report Card](https://goreportcard.com/badge/github.com/guided-traffic/cowork/backend)](https://goreportcard.com/report/github.com/guided-traffic/cowork/backend)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A multi-tenant backlog and kanban board for one person who works on many projects at once,
with an LLM as a co-worker. Tenants keep clients apart, projects group the work, tickets carry
the analysis, the open decisions and the verification, and an LLM such as Claude Code operates
it through the same API people use in the browser — with a personal access token that says who
is accountable.

> **Status: phase 3 in progress — the UI on the core domain.** Tenants, projects and tickets —
> with links, state transitions, open questions, comments, interest, progress, time entries and
> attachments — the audit record and the event stream exist behind a JSON API, tested against
> PostgreSQL 18 and MinIO. A person logs in with a local account: the local administrator the
> installation's Secret names creates the first tenant and the accounts of its people, and each
> person makes their own personal access tokens in the session. There is no identity provider
> yet. What comes next is [the project plan](docs/planning/project-plan.md).

```mermaid
flowchart LR
  B[Browser] --> F
  C[Claude Code<br/>personal access token] --> F
  F[cowork-frontend<br/>nginx + Angular bundle] -->|/api/ proxied| S
  K[kubelet] -->|/healthz /readyz| S
  M[migrate<br/>init container] -->|owner role| P
  S[cowork-backend<br/>Go API] -->|runtime role| P[(PostgreSQL 18)]
  S -->|attachments| O[(S3-compatible<br/>object storage)]
```

## ✨ Key features

- 🧩 **Two containers, one origin** — the Go backend serves the JSON API; the nginx frontend serves the Angular bundle and proxies `/api/` to it, so the browser sees one origin and the Ingress needs one rule.
- 🎫 **Tickets with stable keys** — `acme/COW-42`: five types, a state matrix that asks for reasons and a verification note, four link types with a cycle check on `blocks`, open questions, comments with their history, interest, progress, time entries and attachments.
- 🔑 **Tokens for people and agents** — personal access tokens with a scope and an optional tenant or project restriction, made by the person in a browser session and never by a token; an agent, marked by its token or by `X-Cowork-Agent`, is bound by capabilities and sends an `Idempotency-Key` with every creating `POST`.
- 🔐 **A login that needs no identity provider** — a local administrator kept in step with a Secret, local accounts created by tenant administrators, Argon2id, sessions in the database behind an `HttpOnly` `__Host-` cookie, an account lockout and an address throttle that answer every failure alike, and an origin-plus-header CSRF check on every write of a session.
- 🛡️ **Tenants isolated twice** — every query names its tenant, and forced row-level security under a runtime role that owns nothing backs it; the backend refuses a role that could bypass it.
- 📜 **Contract first** — the OpenAPI 3.1 document in `backend/api/` generates the server, is served at `/api/v1/openapi.json` and validates every request; every error is RFC 9457 problem details with a stable `code`.
- 🧾 **Every act on the record** — an append-only audit record of who did what, with the token and the agent; `ETag` and `If-Match` keep two writers from overwriting each other.
- 📡 **Live updates** — server-sent events per tenant carry keys and versions, never content, filtered by what the reader may see; a reconnect replays what it missed.
- 🗄️ **Migrations under their own role** — embedded SQL applied by an init container as the owner role, serialised across replicas by an advisory lock; the serving container holds only the runtime credential and refuses a schema with pending migrations.
- 🐘 **PostgreSQL 18 and S3** — `uuidv7()` keys and full-text search in PostgreSQL; attachments in any S3-compatible bucket, served only through the backend.
- ⎈ **One Helm chart** — two hardened Deployments, every credential from an existing Secret, nginx sized from the backend's limits, no RBAC because neither container talks to the Kubernetes API.
- 🧪 **Tested in every layer** — Go unit tests; integration and API tests against PostgreSQL 18 and MinIO with every response checked against the API document; Angular unit tests, chart lint and render, one container scan per image, release tooling check; all as `make` targets CI runs unchanged.
- 🆕 **Newest toolchains** — Go 1.27 and Angular 22, moved by Renovate as grouped updates.
- 🗂️ **Documentation with five homes** — decisions in ADRs, work lists in tickets that get archived, one security page per perspective.
- 🧭 **Decided, then built** — every founding question was put to the owner one at a time and became an ADR before the code that depends on it.

## 📛 Naming conventions

### Environment variables

Every backend setting is `COWORK_<NAME>`; the full table is under [Configuration](#configuration).
The frontend container substitutes four variables into its nginx configuration, `BACKEND_URL`,
`NGINX_LOCAL_RESOLVERS`, `NGINX_CLIENT_MAX_BODY_SIZE` and `NGINX_PROXY_READ_TIMEOUT`
([frontend container](#frontend-container)). The integration tier reads
`COWORK_TEST_DATABASE_URL` and `COWORK_TEST_S3_ENDPOINT`, `_ACCESS_KEY_ID`,
`_SECRET_ACCESS_KEY`; `make dev-seed` reads `COWORK_DEV_SEED_DATABASE_URL`.

### Kubernetes objects (Helm chart)

| Object | Name | Notes |
|---|---|---|
| Backend Deployment and Service | `<fullname>-backend` | `<fullname>` is `<release>-cowork`, or the release name itself when it contains `cowork`; `fullnameOverride` replaces it |
| Frontend Deployment and Service | `<fullname>-frontend` | the Ingress targets this Service |
| Ingress | `<fullname>` | only with `ingress.enabled` |
| NetworkPolicy | `<fullname>-backend` | admits ingress to the backend pods from the frontend pods only, on the backend's port; `networkPolicy.enabled`, on by default |
| ServiceAccount | `<fullname>` | shared by both pods, no token mounted |
| Init container of the backend pod | `migrate` | runs `cowork migrate` as the owner role; only with `backend.config.migrateOnStart` |
| Database Secret rendered by the chart | `<fullname>-database`, key `databaseUrl` | only with `database.url` |
| Owner database Secret rendered by the chart | `<fullname>-database-owner`, key `databaseUrl` | only with `database.owner.url` while `backend.config.migrateOnStart` is true |
| Local administrator Secret rendered by the chart | `<fullname>-local-admin`, keys `username` and `password` | only with the inline `localAdmin.username` and `localAdmin.password` |
| Pod annotations of the backend | `checksum/database-secret`, `checksum/database-owner-secret`, `checksum/local-admin-secret` | only with the inline values (the owner's while `migrateOnStart` is true); a changed value rolls the pods |
| CA volume of the backend | `s3-ca`, mounted at `/etc/cowork/s3-ca` | only with `storage.endpoint` and `storage.tls.caConfigMap` |
| Labels | `app.kubernetes.io/name=cowork`, `app.kubernetes.io/instance=<release>`, `app.kubernetes.io/component=backend\|frontend`, `app.kubernetes.io/version`, `app.kubernetes.io/managed-by=Helm`, `helm.sh/chart` | selectors use `name`, `instance` and `component` |
| Container ports | `http`, `8080` on both containers | `backend.containerPort`; the frontend's is fixed by the nginx configuration |

### Keys and identifiers

| Thing | Pattern | Example |
|---|---|---|
| Tenant slug | 2–63 characters of `a-z`, `0-9` and `-`, not starting with `-` | `acme` |
| Project key | 2–10 characters: an upper-case letter, then `A-Z` and `0-9`; no hyphen; never reused in its tenant | `COW` |
| Ticket key | `<tenant-slug>/<PROJECT>-<number>`; the number counts per project from 1, is never reused and ends at 2147483647 | `acme/COW-42` |
| Short ticket key | `<PROJECT>-<number>`, where the path fixes the tenant | `COW-42` |
| Question | its number within the ticket, from 1 | `…/tickets/42/questions/1` |
| Every other id | a UUIDv7 | `0199a3c2-1d2e-7f00-8000-000000000001` |
| Personal access token | `cwk_` and 43 base62 characters; cowork stores its SHA-256 only | — |
| Session cookie | `__Host-cowork-session`, 43 base64url characters (256 random bits); cowork stores its SHA-256 only | `HttpOnly; Secure; SameSite=Lax; Path=/`, no `Domain` |
| Username | 1–63 characters of `a-z`, `0-9`, `.`, `_` and `-`, starting with a letter or a digit; unique in the installation; the identity is `local:<username>` | `ada.lovelace` |
| Local account origin | `config` — the one account `COWORK_LOCAL_ADMIN_*` names — or `tenant` — one a tenant administrator created and that tenant manages | — |
| Agent header | `X-Cowork-Agent: <name>/<model>/<session>`, each part 1–64 printable ASCII characters | `claude-code/opus/7f3a` |
| Request id | `X-Request-Id`, a UUIDv7 the backend makes (an inbound one is ignored); the same value is `request_id` in a problem body and in the request log | — |
| Problem type | `https://cowork.dev/problems/<code, hyphenated>` | `https://cowork.dev/problems/not-found` |
| Attachment object | `<tenant-id>/<attachment-id>` in the configured bucket, derived, never stored | — |
| Event channel | the PostgreSQL `NOTIFY` channel `cowork_events` | — |
| Event names | `ticket.changed` (uploads included), `comment.changed`, `question.changed`, `link.changed`, `interest.changed`; the control events `resync` and `unavailable` | — |

### Development environment

| Thing | Name | Notes |
|---|---|---|
| PostgreSQL container | `cowork-postgres`, `postgres:18` on `localhost:5432` | `make postgres-up`; `POSTGRES_CONTAINER=` and `POSTGRES_PORT=` move it |
| Development database | `cowork`, owned by `cowork_owner`, served as `cowork_app` | created by `make postgres-up`; each password is the role's name |
| MinIO container | `cowork-minio`, `cgr.dev/chainguard/minio` pinned by digest, on `localhost:9000` | `make minio-up`; root keys `cowork` / `cowork-secret`, development values; `MINIO_CONTAINER=` and `MINIO_PORT=` move it |
| Integration run | roles `cowork_it_owner` and `cowork_it_app`, database `cowork_it_<unix-nanoseconds>`, bucket `cowork-it-<unix-nanoseconds>` | one database and one bucket per run; the database is dropped at the end, the bucket stays until `make minio-down` |
| Development seed | person `dev`, tenant `dev`, an admin membership, a token named `dev-seed` | `make dev-seed`; every run prints a new token once |
| Development stack | `make dev`: the backend on `localhost:8080`, the UI on `https://localhost:4200` (self-signed), the local administrator `dev` with the password `dev-only-cowork`, the bucket `cowork-dev`, a second person `sam`, demo projects `COW`, `OPS`, `WEB` | state in `.dev/` (untracked): `token` (the demo data's), `session-key`, `backend.log`, the built `cowork`, and the PrimeUI key in `primeui-license`; `make dev-reset` empties the database |

### Files and images

| Thing | Pattern | Example |
|---|---|---|
| Migration | `backend/internal/store/migrations/NNNNNN_<snake_name>.up.sql`, versions `1..n` without a gap, no down files | `000001_tenants.up.sql` |
| Container images | `guidedtraffic/cowork-backend:<semver>`, `guidedtraffic/cowork-frontend:<semver>` | both carry the release version |
| Helm chart | `cowork` in the repository `https://guided-traffic.github.io/cowork/`, source `deploy/helm/cowork/`; chart version = release version | `helm install cowork cowork/cowork --version 0.1.0` |
| Go module | `github.com/guided-traffic/cowork/backend` | — |
| Angular project | `frontend`, output `frontend/dist/frontend/browser/` | — |
| Ticket (interim, in this repository) | `docs/tickets/NNN-<kebab-slug>.md`, `id: T<n>` | rules in [docs/tickets/README.md](docs/tickets/README.md) |
| API document | `backend/api/openapi.yaml` and one file per path family, bundled by `make generate` into `backend/api/openapi.gen.json` | served at `/api/v1/openapi.json` |

### HTTP

| Path | Backend | Frontend (nginx) |
|---|---|---|
| `/healthz` | liveness | nginx's own health, `{"status":"ok"}` |
| `/readyz` | readiness: a database ping | not proxied: nginx answers it with the UI shell (`index.html`, `200`), which says nothing about the backend — the backend's `/readyz` is reached through the backend Service |
| `/api/v1/…` | the JSON API; errors are RFC 9457 `application/problem+json` with a stable `code` | proxied to the backend, path unchanged; the `413`, `502`, `503` and `504` nginx answers itself are problem bodies without a `request_id` |
| `/auth/options`, `/auth/local`, `/auth/logout` | the browser's login flows, in the API document | proxied like `/api/`, cookies in both directions |
| `/api/v1/tenants/<slug>/events` | the event stream | proxied unbuffered and uncached, with a read timeout of one hour |
| hashed bundles | — | served with `Cache-Control: public, max-age=31536000, immutable` |
| everything else | `404` problem details | `index.html` with `Cache-Control: no-store` |

| Media type | Where |
|---|---|
| `application/json` | request bodies and responses |
| `application/problem+json; charset=utf-8` | every error |
| `multipart/form-data` | an upload: the part `file`, optionally the part `comment_id` |
| `text/csv` | on `Accept: text/csv`: the audit record, the tenant's time entries, the time report |
| `text/markdown; charset=utf-8` | a ticket's canonical Markdown |
| `text/event-stream` | the event stream |

## 📚 Documentation

| Document | What it is for |
|---|---|
| [docs/developer/](docs/developer/README.md) | Contributor entry point and how the code works: layout, package map, architecture, build/test/lint matrix, testing, CI and release, checklists, conventions |
| [docs/operations/](docs/operations/README.md) | Installing and running: the database roles, the Secrets and the object storage; runtime behaviour, the limits, what nginx answers, the event stream behind an Ingress |
| [docs/security/](docs/security/README.md) | The security architecture, one page per perspective; [SECURITY.md](SECURITY.md) to report a vulnerability |
| [docs/adr/](docs/adr/README.md) | Why cowork is the way it is |
| [docs/tickets/](docs/tickets/README.md) | The interim work lists and their rules |
| [docs/planning/](docs/planning/) | The project plan and the VS Code workflow plan — consumed into ADRs and tickets as work proceeds; the question catalog is consumed already |
| [CLAUDE.md](CLAUDE.md) | The working rules for an LLM session in this repository |

## 🚀 Fast start

### Prerequisites

Go 1.27, Node.js 26 with npm, Docker (for the local PostgreSQL and MinIO and the images),
Helm 3 or 4, `openssl`. `make help` lists every target.

### Run it locally

The quickest way to see cowork, with demo data and the UI reloading as you edit:

```bash
make dev                # PostgreSQL, MinIO, the backend, demo data and the UI on https://localhost:4200 — Ctrl-C stops it
```

Sign in as `dev` with the development-only password `dev-only-cowork`. The dev server uses a
self-signed certificate — HTTPS, because Safari stores no `Secure` session cookie from
`http://localhost` — so the browser asks about it once. The parts by hand:

```bash
make postgres-up        # postgres:18 on :5432 — database cowork, roles cowork_owner (migrates) and cowork_app (serves)
make dev-seed           # migrates; then a person, the tenant "dev", an admin membership and a token, printed once
make run                # the backend on :8080: migrates as cowork_owner, serves as cowork_app (text logs)
make frontend-serve     # the Angular dev server on :4200, /api proxied to :8080
```

`make run` needs no server key: it makes a throw-away one per start, so list cursors from an
earlier run answer `invalid_cursor`. `make dev-seed` makes a token: an agent's token with scope
`write` and every capability, so its creating `POST`s carry an `Idempotency-Key`.

```bash
TOKEN=cwk_…             # the token make dev-seed printed
AUTH="Authorization: Bearer $TOKEN"
curl -s localhost:8080/readyz                          # {"status":"ready"}
curl -s -H "$AUTH" localhost:8080/api/v1/me            # the person and the tenant dev
curl -s -H "$AUTH" -H "Idempotency-Key: $(uuidgen)" -H 'Content-Type: application/json' \
  -d '{"key":"COW","name":"cowork"}' localhost:8080/api/v1/tenants/dev/projects
curl -s -H "$AUTH" -H "Idempotency-Key: $(uuidgen)" -H 'Content-Type: application/json' \
  -d '{"type":"task","title":"Try cowork","severity":"low","security":"none","effort":"XS"}' \
  localhost:8080/api/v1/tenants/dev/projects/COW/tickets   # the ticket dev/COW-1
curl -s -H "$AUTH" localhost:8080/api/v1/tickets/dev/COW-1                       # by its key
curl -s -H "$AUTH" localhost:8080/api/v1/tenants/dev/projects/COW/tickets/1/markdown
open https://localhost:4200                            # the UI, after make dev; sign in as dev
```

To try the login — a person in a browser session — start the backend with a local
administrator and the origin the browser sees (`make dev` uses `https://localhost:4200`).
Both variables are required together, the password is at least twelve characters, and the first
thing the administrator does is create the first tenant:

```bash
COWORK_BASE_URL=http://localhost:4200 COWORK_LOCAL_ADMIN_USERNAME=admin \
  COWORK_LOCAL_ADMIN_PASSWORD='a long development password' make run
curl -si -H 'Origin: http://localhost:4200' -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"a long development password"}' localhost:8080/auth/local
# 200 {"password_change_required":false} and  Set-Cookie: __Host-cowork-session=…; HttpOnly; Secure; SameSite=Lax; Path=/
COOKIE='__Host-cowork-session=…'   # the value of that cookie
curl -s -H "Cookie: $COOKIE" localhost:8080/api/v1/me                      # the person; global_admin is true
curl -s -H "Cookie: $COOKIE" -H 'Origin: http://localhost:4200' -H 'X-Requested-With: cowork' \
  -H 'Content-Type: application/json' -d '{"slug":"acme","name":"Acme"}' localhost:8080/api/v1/tenants
```

Every write of a session carries the `Origin` of `COWORK_BASE_URL` and `X-Requested-With: cowork`
([CSRF](docs/security/csrf.md)); a token's request carries neither.

Without object storage the backend refuses uploads (`501 uploads_disabled`). To try
attachments, start MinIO, create a bucket with any S3 client — no target creates one — and
hand the backend its keys:

```bash
make minio-up           # MinIO on :9000, root keys cowork / cowork-secret
mc alias set local http://localhost:9000 cowork cowork-secret && mc mb local/cowork
COWORK_S3_ENDPOINT=http://localhost:9000 COWORK_S3_BUCKET=cowork \
  COWORK_S3_ACCESS_KEY_ID=cowork COWORK_S3_SECRET_ACCESS_KEY=cowork-secret make run
```

### Run the tests

```bash
make test                       # backend unit + frontend unit
make postgres-up minio-up       # the integration tier needs both
make test-integration           # a database and a bucket of its own per run
make lint frontend-lint helm-lint
```

### Build the images

```bash
make docker-build       # guidedtraffic/cowork-backend:latest and guidedtraffic/cowork-frontend:latest
```

### Install on Kubernetes

The database comes first: one PostgreSQL 18 database, an owner role that owns it and a
runtime role that owns nothing — both created by you, as
[installation.md](docs/operations/installation.md#the-database-and-its-two-roles) shows.

```bash
kubectl create namespace cowork
kubectl -n cowork create secret generic cowork-database \
  --from-literal=databaseUrl='postgres://cowork_app:CHANGE-ME@postgres:5432/cowork?sslmode=require'
kubectl -n cowork create secret generic cowork-database-owner \
  --from-literal=databaseUrl='postgres://cowork_owner:CHANGE-ME@postgres:5432/cowork?sslmode=require'
kubectl -n cowork create secret generic cowork-session \
  --from-literal=sessionKey="$(openssl rand -base64 32)"
helm repo add cowork https://guided-traffic.github.io/cowork/
helm upgrade --install cowork cowork/cowork --version 0.1.0 -n cowork \
  --set database.existingSecret=cowork-database \
  --set database.owner.existingSecret=cowork-database-owner \
  --set session.existingSecret=cowork-session
kubectl -n cowork port-forward svc/cowork-frontend 8080:80     # the UI, /api/ proxied
```

The login needs a local administrator and the public URL: a Secret with its username and
password, and the origin the browser shows. The administrator logs in, creates the first tenant
(or `bootstrap.tenant.slug` and `.name` create it at start) and the accounts of its people:

```bash
kubectl -n cowork create secret generic cowork-local-admin \
  --from-literal=username=admin --from-literal=password="$(openssl rand -base64 24)"
helm upgrade cowork cowork/cowork -n cowork --reuse-values \
  --set localAdmin.existingSecret=cowork-local-admin \
  --set backend.config.baseURL=https://cowork.example.com \
  --set bootstrap.tenant.slug=acme --set bootstrap.tenant.name="Acme Corp"
```

Behind the frontend and an Ingress the login throttle has to be told which networks are the
proxies, or it counts one address for every browser:
`--set backend.config.trustedProxies=<the pod network that holds the frontend and the Ingress
controller>` ([the client address](docs/operations/installation.md#the-client-address-and-the-trusted-proxies)).

Attachments need an S3-compatible bucket and three more values; without them uploads are
refused. Without a local administrator nobody can log in. A release publishes the chart and both images (`guidedtraffic/cowork-backend`,
`guidedtraffic/cowork-frontend` on Docker Hub) with one version; the chart's image tags follow
its `appVersion`. Details — the roles, the Secrets,
the object storage, the Ingress annotations, the CloudNativePG note:
[docs/operations/installation.md](docs/operations/installation.md).

<details>
<summary>Upgrade and uninstall</summary>

```bash
helm repo update cowork
helm upgrade cowork cowork/cowork --version <new> -n cowork --reuse-values
helm uninstall cowork -n cowork      # the database and the bucket are left untouched
```

The new backend pods migrate the schema in their init container before the server starts; see
[docs/operations/runtime.md](docs/operations/runtime.md#the-migration-run).

</details>

## 📖 Reference

### Configuration

Every variable is read once at backend start. Invalid values and missing required ones end the
process with exit code 1: every problem the configuration has is listed together, and what only
`serve` or `migrate` needs is checked once the rest passes. An error names the variable and
quotes a rejected setting such as a size or a duration, never the value of a URL, a key or a
secret. Source: [`backend/internal/config/config.go`](backend/internal/config/config.go). A
size is a number of bytes or a number with `KiB`, `MiB` or `GiB`; a duration is a Go duration
(`30s`, `5m`). Where a limit takes `0`, `0` switches it off —
[runtime.md, limits](docs/operations/runtime.md#limits) says what that costs.

**Server and database**

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_DATABASE_URL` | — (required) | `postgres://cowork_app:…@postgres:5432/cowork?sslmode=require` `# example` | The runtime role's connection URL; every request runs as this role. It must not be a superuser, have `BYPASSRLS`, own a relation of the schema or be a member of the owner role — `serve` and `migrate` refuse it otherwise. `pool_max_conns=<n>` in the URL sizes the connection pool. **Security:** a credential: from a Secret. cowork never logs it; a URL pgx cannot parse appears in the startup error with its password masked, which pgx does on a best-effort basis |
| `COWORK_DATABASE_OWNER_URL` | empty `# default` | `postgres://cowork_owner:…@postgres:5432/cowork?sslmode=require` `# example` | The owner role's URL, which the migrations run under; it must name another role than `COWORK_DATABASE_URL`. Required by `cowork migrate`, and by `cowork serve` while `COWORK_MIGRATE_ON_START` is `true`. **Security:** the owner can switch row-level security off, so a serving process that holds this URL loses the second line of tenant isolation against its own compromise. The chart hands it to the `migrate` init container only |
| `COWORK_MIGRATE_ON_START` | `true` `# default` | `true`, `false` | `serve` applies pending migrations before it listens; with `false` it refuses to start while migrations are pending. The chart sets `false` and migrates in an init container |
| `COWORK_SESSION_KEY` | — (required by `serve`) | standard base64 of at least 32 bytes, `openssl rand -base64 32` | The server key; it signs the list cursors and keys the hash of a login's source address. Every replica needs the same key, and a new key invalidates the cursors clients hold (`400 invalid_cursor`). It signs no session: sessions are rows in the database, and a new key logs nobody out. **Security:** a secret: from a Secret, never echoed; the chart has no inline value for it. `make run` makes a throw-away one |
| `COWORK_LISTEN_ADDR` | `:8080` `# default` | `host:port` | The backend listener for API and health |
| `COWORK_LOG_LEVEL` | `info` `# default` | `debug`, `info`, `warn`, `error` | Minimum level |
| `COWORK_LOG_FORMAT` | `json` `# default` | `json`, `text` | `text` for a terminal |
| `COWORK_SHUTDOWN_TIMEOUT` | `15s` `# default` | a positive duration | Drain bound after `SIGTERM`; the event streams end as the drain begins. A drain that outlasts it ends the process with exit 1. Keep it below the pod's grace period |
| `COWORK_BASE_URL` | empty `# default` | `https://cowork.example.com` `# example` | The public URL, as the browser shows it: an origin — `http` or `https`, a host, an optional port, no path, no query. **Required while `COWORK_LOCAL_ADMIN_USERNAME` is set.** The CSRF check compares the `Origin` (or `Referer`) of every write of a session, and of the login, with it exactly ([CSRF](docs/security/csrf.md)); a URL the browser does not show makes every such write `403 csrf`, and without one no write of a cookie passes. **Security:** nothing secret; a wrong value locks the browser out, never lets another origin in |

**Login, sessions and accounts** ([ADR 0031](docs/adr/0031-server-side-sessions-in-an-httponly-cookie.md),
[0032](docs/adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md),
[0033](docs/adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md),
[0035](docs/adr/0035-personal-access-tokens.md) D4, [0037](docs/adr/0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md)).
Chart values are in [Helm chart values](#helm-chart-values); the pages are
[sessions](docs/security/sessions.md), [local accounts](docs/security/local-accounts.md) and
[CSRF](docs/security/csrf.md).

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_LOCAL_ADMIN_USERNAME` | empty `# default` | `admin` `# example` | With `COWORK_LOCAL_ADMIN_PASSWORD`: the one account the configuration keeps, a global administrator — it creates tenants and holds no role in any until it grants itself one — and a full account (`local:<username>`). Synchronised at every start, after the migrations, under an advisory lock: created; re-hashed with all its sessions ended when the password changed; deactivated, its tokens revoked and its sessions ended, when both variables are empty; never deleted. Both set or both empty — one alone refuses the start, naming the missing variable. Needs `COWORK_BASE_URL`. 1–63 characters of `a-z`, `0-9`, `.`, `_`, `-`, starting with a letter or a digit |
| `COWORK_LOCAL_ADMIN_PASSWORD` | empty `# default` | at least `COWORK_PASSWORD_MIN_LENGTH` characters | The account's password, read as it is — spaces included. **Security:** a secret: from a Secret, never echoed or logged; anyone who can read the pod spec or the Secret can read it. A leaked one stays valid until the Secret is rotated **and** the backend restarted; that rotation also unlocks a locked administrator and ends its sessions. The account's password cannot be changed in the UI: this is its source |
| `COWORK_BOOTSTRAP_TENANT_SLUG` | empty `# default` | `acme` `# example` | With `COWORK_BOOTSTRAP_TENANT_NAME`: while no tenant exists, a start creates this tenant and gives the local administrator a marked grant as its `admin`; once a tenant exists these variables do nothing, whatever they say. Both or neither, and only with the local administrator |
| `COWORK_BOOTSTRAP_TENANT_NAME` | empty `# default` | `Acme Corp` `# example` | The tenant's name, 1–200 characters |
| `COWORK_PASSWORD_MIN_LENGTH` | `12` `# default` | `8` to `1024` | The shortest password of a local account, counted in characters; the policy is length only — no character classes, no history — and it holds for the local administrator too. Below `8` the start is refused |
| `COWORK_LOGIN_LOCKOUT` | `window` `# default` | `window`, `admin` | `window`: a username locked by failures is free again when the 15-minute window passes. `admin`: it stays locked until a tenant administrator unlocks it (`DELETE …/accounts/{username}/lockout`) — or, for the local administrator, until the Secret is rotated and the backend restarted. **Security:** `admin` lets anyone who knows a username keep its account locked |
| `COWORK_LOGIN_MAX_FAILURES` | `5` `# default` | a count; `0` never locks | Failed attempts of one username within fifteen minutes that lock it — a username nobody has too, so neither the answer nor the lock says whether an account exists |
| `COWORK_LOGIN_ADDRESS_LIMIT` | `20` `# default` | a count; `0` disables | Login attempts of one client address — an IPv6 client by its /64 — within a minute before `429 too_many_attempts`. The client address is the TCP peer's unless the peer is inside `COWORK_TRUSTED_PROXIES` ([H-17](docs/security/local-accounts.md#h-17)); with that list empty, behind the frontend's nginx the peer is nginx and the limit holds for the whole installation |
| `COWORK_TRUSTED_PROXIES` | empty `# default` | comma-separated CIDRs, IPv4 and IPv6; `10.244.0.0/16,fd00:10:244::/48` `# example` | The networks of the proxies in front of the backend. The client address is found by walking `X-Forwarded-For` from the right: from the TCP peer, while the current address is inside these networks the entry to its left becomes the current one; the first address outside them is the client, and nothing to its left is read. Empty: the peer is the client and the header is never read. A single host is `/32` or `/128`; an entry that is no CIDR refuses the start, naming the variable and that entry. **Security:** name the proxies and no more — a client inside a trusted network chooses its own address, which defeats the throttle and lets it fill another client's bucket; an empty list leaves one address for the whole installation ([installation.md](docs/operations/installation.md#the-client-address-and-the-trusted-proxies)) |
| `COWORK_SESSION_LIFETIME` | `12h` `# default` | a positive duration | The absolute lifetime of a session; it is also the cookie's `Max-Age` |
| `COWORK_SESSION_IDLE` | `2h` `# default` | a positive duration | How long a session may lie unused; a request within it extends the session up to the lifetime. The idle clock moves at most once a minute |
| `COWORK_TOKEN_DEFAULT_LIFETIME` | `2160h` (90 days) `# default` | a positive duration, not above the maximum | The lifetime of a token whose creator named none |
| `COWORK_TOKEN_MAX_LIFETIME` | `8760h` (one year) `# default` | a positive duration | The longest lifetime a token may have; a longer request is shortened to it and the answer says what the token got |

**Limits** ([ADR 0039](docs/adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md))

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_MAX_JSON_BODY` | `1MiB` `# default` | a size; `0` disables | A larger JSON body is `413 payload_too_large` |
| `COWORK_REQUEST_TIMEOUT` | `30s` `# default` | a duration, not negative; `0` disables | A request still running is cancelled and answered `504 timeout`; the event stream is exempt |
| `COWORK_MAX_PAGE_SIZE` | `200` `# default` | a count; `0` disables | The largest page a list returns; a larger `limit` is clamped, not refused |
| `COWORK_MAX_QUERY_LENGTH` | `256` `# default` | a count of characters; `0` disables | A longer full-text query `q` is `400 validation_failed` |
| `COWORK_ATTACHMENT_MAX_BYTES` | `10MiB` `# default` | a size; `0` disables | The largest upload; above it `413`, before anything is stored. Uploads are buffered in memory: with `0` one upload at a time is read whole, whatever its size ([docs/security/attachments.md](docs/security/attachments.md#h-12)) |
| `COWORK_ATTACHMENT_MAX_PER_TICKET` | `100` `# default` | a count; `0` disables | The attachments one ticket takes; one more is `409 attachment_limit` |

**Object storage** ([ADR 0016](docs/adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)) —
the endpoint, the bucket and both keys together, or none of them. Without them uploads answer
`501 uploads_disabled` and the log warns at start; the other three are read only with them.

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_S3_ENDPOINT` | empty `# default` | `https://s3.example.com` `# example` | `http://` or `https://`, a host and an optional port. Not contacted at start; the first upload is the test |
| `COWORK_S3_BUCKET` | empty `# default` | `cowork` `# example` | The bucket; it must exist — the backend never creates one |
| `COWORK_S3_ACCESS_KEY_ID` | empty `# default` | `cowork-app` `# example` | The access key's id |
| `COWORK_S3_SECRET_ACCESS_KEY` | empty `# default` | — | **Security:** a secret, never echoed; the key of a policy that reaches this bucket only, never root credentials |
| `COWORK_S3_REGION` | empty `# default` | `eu-central-1` `# example` | Empty lets the client ask the server |
| `COWORK_S3_USE_PATH_STYLE` | `true` `# default` | `true`, `false` | Path-style addressing, as MinIO expects; `false` for virtual-host style |
| `COWORK_S3_CA` | empty `# default` | `/etc/cowork/s3-ca/ca.crt` `# example` | A PEM file of a private authority, trusted in addition to the system's |

**Event stream** ([ADR 0054](docs/adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md))

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_SSE_REPLAY_WINDOW` | `5m` `# default` | a duration, not negative | How long a replica keeps events for a reconnect's `Last-Event-ID`; beyond it the stream starts with `resync`. `0` keeps no replay |
| `COWORK_SSE_MAX_STREAMS_PER_PERSON` | `10` `# default` | a count; `0` disables | The streams one person holds on one replica; one more closes the oldest with `event: unavailable` |

#### Frontend container

The image's entrypoint substitutes these four variables into the nginx configuration, and
nothing else:

| Variable | Image default | The chart sets it to | Meaning |
|---|---|---|---|
| `BACKEND_URL` | `http://backend:8080` | the backend Service, `http://<fullname>-backend:8080` | Where nginx proxies `/api/`; resolved per request (cached 30 s), so the frontend starts before the backend |
| `NGINX_LOCAL_RESOLVERS` | the nameservers of `/etc/resolv.conf`, exported by the entrypoint (`NGINX_ENTRYPOINT_LOCAL_RESOLVERS=true`) | — | The DNS servers of that lookup |
| `NGINX_CLIENT_MAX_BODY_SIZE` | `11m` | the larger of `backend.config.maxJsonBody` and `attachmentMaxBytes`, rounded up to MiB, plus 1 MiB; `0` (no limit) when either is `0` | nginx's body limit, kept above the backend's so the backend answers its own `413` |
| `NGINX_PROXY_READ_TIMEOUT` | `40s` | `backend.config.requestTimeout` plus 10 s; `3600s` when it is `0` | nginx's read timeout on `/api/`, kept above the backend's so the backend answers its own `504`; the event stream has an hour of its own |

### CLI (backend)

| Command | Does |
|---|---|
| `cowork serve` | Load the configuration (`COWORK_SESSION_KEY` required, `COWORK_DATABASE_OWNER_URL` too while migrating on start); migrate unless `COWORK_MIGRATE_ON_START=false`; connect as the runtime role; refuse a runtime role that could bypass row-level security, a dirty schema and pending migrations; synchronise the local administrator and the bootstrap tenant under an advisory lock; listen until `SIGINT`/`SIGTERM` |
| `cowork migrate` | Load the configuration (`COWORK_DATABASE_URL` names the runtime role the migrations grant to, `COWORK_DATABASE_OWNER_URL` is the role they run as); apply pending migrations; exit 0. Exit 1 on a dirty or failing schema or a runtime role that could bypass row-level security |
| `cowork version` | Print `cowork <version> (commit <sha>, built <epoch>)` |
| `cowork help` | Print the usage (also `-h`, `--help`) |

Exit codes: `0` success, `1` configuration or runtime error, `2` unknown command or no command.

### API (backend)

The contract is the OpenAPI 3.1 document in [`backend/api/`](backend/api/openapi.yaml)
([ADR 0046](docs/adr/0046-spec-first-the-openapi-document-is-the-contract.md)); the backend
serves it at `/api/v1/openapi.json` with `info.version` set to its own version, and validates
every request against it. What this section says in one line per route, the document says in
full.

- **Authentication.** Every route under `/api/v1/` except `version` and `openapi.json` takes
  one of two credentials, and the document says which per operation (`bearerToken`,
  `sessionCookie`). A personal access token, `Authorization: Bearer cwk_…`, is for scripts and
  agents; the session cookie `__Host-cowork-session` of `POST /auth/local` is for the browser. A
  request with an `Authorization` header is a token's, whatever cookie it carries. Without a
  valid credential the answer is `401` (`unauthenticated`, `token_expired`, `token_revoked`) with
  `WWW-Authenticate: Bearer realm="cowork"`. Six routes take a **session only** and answer a
  token `403 session_required`: creating a token, a tenant or a local account, resetting or
  changing a password, logging out ([ADR 0035](docs/adr/0035-personal-access-tokens.md) D5,
  [ADR 0033](docs/adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)
  D1, D5). A **write of a session** must come
  from `COWORK_BASE_URL` — its `Origin`, or without one its `Referer` — and carry
  `X-Requested-With: cowork`, else `403 csrf`; a token's writes need neither
  ([CSRF](docs/security/csrf.md)). A session whose account has a temporary password can only read
  `GET /api/v1/me`, change the password and log out; everything else is
  `403 password_change_required`. `X-Cowork-Agent: <name>/<model>/<session>` marks a request as an
  agent's; a token with the agent flag makes it one with or without the header.
- **Tenants.** A route under `/api/v1/tenants/{tenant}` answers `404 not_found` alike for an
  unknown slug, a tenant the person does not belong to and a token restricted to another.
- **Writes.** An entity's `ETag` is its version; an overwriting write needs it in `If-Match`
  (`428` without, `412` with a stale one). An `Idempotency-Key` (a UUID) makes a creating
  `POST` safe to retry for 24 hours — the same request replays the stored answer, another one
  is `422`; an agent's creating `POST` must carry one.
- **Lists.** `limit` (default 50, clamped to `COWORK_MAX_PAGE_SIZE`) and `cursor`, from the
  previous page's `next_cursor`. The ticket lists and the tenant's time entries also take
  numbered pages, `page` and `per_page` (`25`, `50`, `100`), with a total, up to row 10 000;
  the ticket lists answer `304` to an unchanged page's weak `ETag` in `If-None-Match`. A query
  parameter the route does not declare is `400`; a path parameter that cannot name anything is
  `404`.
- **Every response** carries `Cache-Control: no-store` (the event stream `no-cache`) and
  `X-Request-Id`. An error is `application/problem+json`
  ([RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)): `type`, `title`, `status`, `detail`,
  `instance`, the stable `code` (snake_case, [the table below](#problem-codes)), the
  `request_id` and, for invalid fields, `errors[]` with a `pointer` each — for example
  `{"type":"https://cowork.dev/problems/not-found","title":"Not found","status":404,"detail":"no such tenant","instance":"/api/v1/tenants/acme/projects","code":"not_found","request_id":"0199a3c2-1d2e-7f00-8000-0000000000ff"}`.

| Method and path | Answers |
|---|---|
| `GET /healthz` | `200 {"status":"ok"}` — the process serves; no authentication |
| `GET /readyz` | `200 {"status":"ready"}`, or `503 not_ready` when the database does not answer (the error goes to the log); no authentication |
| `GET /api/v1/version` | `200 {"version":"…","commit":"…","build_time":"…"}`; no authentication |
| `GET /api/v1/openapi.json` | the API document; no authentication |
| a known path with another method | `405 method_not_allowed`, `Allow` names the methods the API document declares there; the document declares no `HEAD`, so `HEAD` on the API is `405` (the health endpoints answer it) |
| any other path | `404 not_found`, `detail: no route <METHOD> <path>` |
| `GET /auth/options` | `200 {"local": bool, "oidc": false, "password_min_length": int}` — what the login page offers: the local form while an active local account exists, and the minimum password length every password form follows (`COWORK_PASSWORD_MIN_LENGTH`); no authentication |
| `POST /auth/local` | `{"username","password"}` → `200 {"password_change_required": bool}` and the session cookie; every failure is `401 invalid_credentials`, the same answer in the same time for an unknown username, a wrong password, a locked or a deactivated account; `429 too_many_attempts` from the address throttle; `403 not_initialised` for a person who is not a global administrator while no tenant exists; `403 csrf` unless the `Origin` is `COWORK_BASE_URL`; no authentication |
| `POST /auth/logout` | a session, CSRF-checked: ends it, clears the cookie, `204` |
| `GET /api/v1/me` | the calling person and their memberships, whether they are a global administrator (`global_admin`), have a local account (`local`) and must change a temporary password (`password_change_required`) |
| `PUT /api/v1/me/password` | a session only: `{"current_password","new_password"}`; the current password counts like a login attempt towards the lockout; the new one meets `COWORK_PASSWORD_MIN_LENGTH` and differs; every other session of the account ends; `204`. Not for the local administrator, whose password is the configuration's (`403 forbidden`) |
| `GET /api/v1/me/tokens` | the person's tokens, revoked and expired ones included — metadata only |
| `POST /api/v1/me/tokens` | a session only: `{"name","scope"}` and optionally `agent`, `capabilities`, `tenant`, `project`, `lifetime_days`; `201` with the token **and its plaintext, once** — a replay for an `Idempotency-Key` answers without it. The lifetime defaults to `COWORK_TOKEN_DEFAULT_LIFETIME` and is shortened to `COWORK_TOKEN_MAX_LIFETIME`; an agent token has at most `write` scope and every capability when `capabilities` is left out — an empty list is none, the baseline only |
| `DELETE /api/v1/me/tokens/{token_id}` | revoke one; a token may always revoke itself, another needs `write`, an agent revokes only its own |
| `POST /api/v1/tenants` | a global administrator, session only: `{"slug","name"}` → `201`; the creator becomes the tenant's first administrator by a marked grant, in the same transaction; `409 tenant_slug_taken` |
| `GET /api/v1/tickets/{tenant}/{key}` | a ticket by its short key, `<PROJECT>-<number>` — the body and `ETag` of its own route |

The routes of a tenant start with `/api/v1/tenants/{tenant}`, written `…` below; `…/{number}`
stands for `/api/v1/tenants/{tenant}/projects/{project}/tickets/{number}`.

<details>
<summary>Local accounts — 6 routes</summary>

For the tenant's administrators with `admin` scope, never an agent. **Creating an account and
resetting a password take a browser session only** — a token, an administrator's included, is
`403 session_required` — because an account or a password made with a leaked token would outlive
its revocation; the other four routes work with an `admin`-scope token. A tenant manages the local
accounts **its own administrators created** and no others: another tenant's account, a global
administrator and the local administrator answer `404 not_found`, like a username nobody has.
An administrator's own account is off limits for a password reset, an unlock and a deactivation
(`403 forbidden`).

| Method and path | Does |
|---|---|
| `GET …/accounts` | the accounts the tenant manages, by person id: username, display name, role, `password_change_required`, `locked`, `deactivated_at` |
| `POST …/accounts` | a session only: create one: `{"username","display_name","temporary_password","role"}`; the person changes the password at the first login; a marked grant with that role; `409 username_taken` (the installation has one namespace) |
| `PUT …/accounts/{username}/password` | a session only: set a new temporary password; every session of the account ends |
| `DELETE …/accounts/{username}/lockout` | forget the failures and the lock of the username |
| `PUT …/accounts/{username}/deactivation` | deactivate: no login, tokens revoked, sessions ended, the person and their acts stay |
| `DELETE …/accounts/{username}/sessions` | end every session of the account, at once; tokens are not affected |

</details>

<details>
<summary>Tenant and projects — 10 routes</summary>

| Method and path | Does |
|---|---|
| `GET …` | the tenant and its settings |
| `PATCH …` | change the name or the settings — an administrator with `admin` scope, never an agent; `If-Match` |
| `GET …/members` | the members and their roles |
| `GET …/audit` | the audit record, newest first, for administrators; filters `actor`, `token`, `action`, `entity_type`, `from`, `to`; CSV on `Accept: text/csv` |
| `GET …/events` | the event stream of the changes the caller may see ([runtime.md](docs/operations/runtime.md#the-event-stream)) |
| `GET …/projects` | the projects the caller can see, by key; `include_archived` |
| `POST …/projects` | create one — `write`; a member while the tenant allows it, an administrator always, an agent with `create-project` |
| `GET …/projects/{project}` | one project |
| `PATCH …/projects/{project}` | change its name or description — an administrator with `admin` scope; `If-Match` |
| `PUT …/projects/{project}/archive` | archive it — an administrator, never an agent; it keeps its tickets and refuses new ones |

</details>

<details>
<summary>Tickets — 19 routes</summary>

| Method and path | Does |
|---|---|
| `GET …/tickets` | the tenant's tickets across its projects, newest first; filters `project`, `state`, `type`, `severity`, `security`, `urgency`, `effort`, `assignee`, `reporter`, `parent`, `progress_min`, `progress_max`, `opened_after`, `opened_before`, `updated_after`, `updated_before`, `q`, `include_terminal`, `blocked`, `has_open_questions`, `interest` ([ADR 0049](docs/adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md)) |
| `GET …/projects/{project}/tickets` | the project's tickets in its rank: the ranked by their key, then the unranked — done and dropped, and open ones a release before the rank filed — by number ([ADR 0014](docs/adr/0014-rank-is-the-decision-score-is-the-warning.md)); the same filters but `project`; a cursor from before the rank is `400 invalid_cursor` |
| `POST …/projects/{project}/tickets` | file a ticket (`type`, `title`, `severity`, `security`, `effort`); its number is the project's next, its rank the bottom |
| `GET …/{number}` | one ticket |
| `PATCH …/{number}` | change its fields; `If-Match` |
| `PUT …/{number}/body` | replace its body as a whole; `If-Match` |
| `PUT …/{number}/urgency-override` | override the derived urgency with a reason, until an input of the derivation changes; an agent needs `override-urgency`; `If-Match` |
| `DELETE …/{number}/urgency-override` | withdraw the override; `If-Match` |
| `PUT …/{number}/confidential` | set or lift the confidential flag — an administrator with `admin` scope, never an agent; lifting needs a reason; `If-Match` |
| `POST …/{number}/transitions` | move it to another state; `from` must be the current state, else `409 state_conflict`; done needs a verification note, and over open prerequisites it is `409 open_prerequisites` unless a person overrides with a reason; an agent needs `decide`, `close` or `drop` for those moves; done and dropped take the rank away, a reopen ranks the ticket at the bottom |
| `PUT …/{number}/rank` | place it directly after or before another open ticket of the project, `{"after": n}` or `{"before": n}`: one key written between the neighbour's and the next one's on that side, those the caller cannot see counted, recorded as `ranked` with the neighbour, the version raised — the key itself is never shown, the list's order is the rank; a ticket already there among those the caller can see is `200` unchanged; no `If-Match` — the last move wins; an agent needs `rank`; a done or dropped ticket or neighbour is `409 state_conflict`, a neighbour the caller cannot see the `400` of one that does not exist |
| `GET …/{number}/links` | its links in both directions |
| `PUT …/{number}/links/{type}/{other}` | link it, as the source, to `other` (a short key): `blocks`, `relates-to`, `duplicates`, `found-in`; `201` new, `200` existing; a `blocks` cycle is `409 link_cycle` |
| `DELETE …/{number}/links/{type}/{other}` | remove the link; `204` also when there was none |
| `GET …/{number}/interest` | who holds a stake in it |
| `PUT …/{number}/interest` | set the caller's own stake; `201` new, `200` otherwise |
| `DELETE …/{number}/interest` | remove the caller's own stake |
| `GET …/{number}/markdown` | its canonical Markdown, `text/markdown`; the `ETag` is its version; every call is recorded |
| `GET …/{number}/activity` | every recorded act on it, from the audit record |

</details>

<details>
<summary>Questions and comments — 12 routes</summary>

| Method and path | Does |
|---|---|
| `GET …/{number}/questions` | its questions, by number |
| `POST …/{number}/questions` | ask one; the person asked must be able to see the ticket — without one the question is open to the tenant |
| `GET …/{number}/questions/{question}` | one question |
| `PATCH …/{number}/questions/{question}` | edit it while open — the asker; `If-Match` |
| `PUT …/{number}/questions/{question}/answer` | answer it, or change one's answer — a person decides; an agent with `record-answer` writes down its person's answer; `If-Match` once answered |
| `PUT …/{number}/questions/{question}/withdrawal` | withdraw an open question — the asker |
| `GET …/{number}/comments` | the comment thread, oldest first (`order=desc` for newest) |
| `POST …/{number}/comments` | comment |
| `GET …/{number}/comments/{comment}` | one comment |
| `PATCH …/{number}/comments/{comment}` | edit it — its author, or the person whose agent wrote it; the old text is kept; `If-Match` |
| `GET …/{number}/comments/{comment}/revisions` | a comment's earlier texts, oldest first; empty once withdrawn |
| `PUT …/{number}/comments/{comment}/withdrawal` | withdraw it: the text is hidden, the entry stays — its author, their person, or an administrator |

</details>

<details>
<summary>Time — 8 routes</summary>

| Method and path | Does |
|---|---|
| `GET …/{number}/time-entries` | the ticket's time entries the caller may see, and their sum |
| `POST …/{number}/time-entries` | book the caller's own time — a member, never an agent; a locked day is `409 period_locked` |
| `GET …/{number}/time-entries/{entry}` | one entry |
| `PATCH …/{number}/time-entries/{entry}` | correct it — its author; the previous values are kept; `If-Match` |
| `PUT …/{number}/time-entries/{entry}/void` | void it — its author; kept, and left out of every sum |
| `GET …/{number}/time-entries/{entry}/revisions` | its previous values |
| `GET …/time-entries` | the tenant's entries the caller may see, newest first; filters `from`, `to`, `project`, `ticket`, `person`, `include_voided`; CSV on `Accept: text/csv` |
| `GET …/time-report` | minutes summed per `ticket`, `project`, `person` or `tenant` (`group_by`) over a period; CSV on `Accept: text/csv` |

</details>

<details>
<summary>Attachments — 4 routes</summary>

| Method and path | Does |
|---|---|
| `GET …/{number}/attachments` | the ticket's attachments |
| `POST …/{number}/attachments` | upload a file to the ticket, or to one of its comments: `multipart/form-data` with `file` and optionally `comment_id`; the type is detected from the bytes — PNG, JPEG, GIF, WebP, PDF, UTF-8 text, SVG — anything else is `415`; `501 uploads_disabled` without object storage |
| `GET …/{number}/attachments/{attachment}` | its metadata |
| `GET …/{number}/attachments/{attachment}/content` | its bytes, with `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox`; raster images inline, everything else as a download; the `ETag` is the SHA-256 of the bytes; every `200` is recorded |

</details>

#### Problem codes

Generated by `make generate` from [`backend/internal/problem`](backend/internal/problem/problem.go);
every error body carries one of these as `code`.

<!-- problem-codes:start -->
| `code` | Status | Meaning |
|---|---|---|
| `validation_failed` | 400 | The request does not match the API document, or a field breaks a rule; `errors[]` names each field |
| `idempotency_key_required` | 400 | An agent's `POST` that creates something came without an `Idempotency-Key`; a transition needs none (docs/adr/0045 D2, D3) |
| `invalid_cursor` | 400 | The cursor was altered, belongs to another list, or comes from another installation |
| `page_too_deep` | 400 | A numbered page beyond the depth cap; follow the cursor instead |
| `unauthenticated` | 401 | No token or session, a malformed one, or one cowork does not know; a session that expired or was ended answers the same |
| `token_expired` | 401 | The token is past its expiry (docs/adr/0035 D4) |
| `token_revoked` | 401 | The token was revoked, or its person deactivated (docs/adr/0035 D6) |
| `invalid_credentials` | 401 | The local login failed: the same answer, in the same time, for an unknown username, a wrong password, a locked or a deactivated account (docs/adr/0033 D6) |
| `forbidden` | 403 | The person's role does not allow the act (docs/adr/0034) |
| `insufficient_scope` | 403 | The token's scope does not reach the act (docs/adr/0035 D3) |
| `agent_forbidden` | 403 | The act is on the agent hard-off list or needs a capability the token lacks; `detail` names which (docs/adr/0043 D5) |
| `session_required` | 403 | The route is for a person in a browser session; a personal access token cannot call it (docs/adr/0035 D5) |
| `password_change_required` | 403 | The session's account has a temporary password, which has to be changed before anything else (docs/adr/0033 D4) |
| `not_initialised` | 403 | The installation has no tenant yet and the person is not a global administrator (docs/adr/0032 D5) |
| `csrf` | 403 | A cookie-authenticated write, or the login, did not come from COWORK_BASE_URL or lacks `X-Requested-With: cowork` (docs/adr/0037 D1) |
| `not_found` | 404 | No such route, or a tenant, project or ticket the caller cannot see — the answer does not say which (docs/adr/0047 D5) |
| `method_not_allowed` | 405 | The path exists with other methods; `Allow` names them |
| `username_taken` | 409 | The installation has a person with this username; usernames are unique (docs/adr/0033 D2) |
| `tenant_slug_taken` | 409 | The installation has a tenant with this slug; slugs are never reused (docs/adr/0005 D4) |
| `project_key_taken` | 409 | The tenant has a project with this key; keys are never reused (docs/adr/0007 D4) |
| `project_archived` | 409 | An archived project refuses new tickets (docs/adr/0006 D4) |
| `state_conflict` | 409 | The ticket is not in the state the request assumed, or its state does not allow the change; `errors[]` names the current state (docs/adr/0045 D2) |
| `parent_cycle` | 409 | The new parent is the ticket itself or one of its descendants (docs/adr/0008 D2) |
| `link_cycle` | 409 | The blocks link would close a cycle of prerequisites (docs/adr/0012 D4) |
| `open_prerequisites` | 409 | Tickets that block this one are not done or dropped; `errors[]` lists them, and a person may override with a reason (docs/adr/0012 D7) |
| `period_locked` | 409 | The day lies on or before the tenant's time_locked_until: the period is closed to new, changed and voided entries (docs/adr/0017 D8) |
| `attachment_limit` | 409 | The ticket holds as many attachments as COWORK_ATTACHMENT_MAX_PER_TICKET allows (docs/adr/0016 D6) |
| `uploads_disabled` | 501 | The installation has no object storage configured; attachments cannot be uploaded (docs/adr/0016 D1) |
| `precondition_failed` | 412 | The `If-Match` version is stale; the response carries the current `ETag` and `errors[]` the current values (docs/adr/0050 D5) |
| `payload_too_large` | 413 | The body is larger than the configured limit (docs/adr/0039 D2) |
| `unsupported_media_type` | 415 | The body's type is not one the route accepts |
| `idempotency_mismatch` | 422 | The `Idempotency-Key` was used before with a different request (docs/adr/0045 D4) |
| `precondition_required` | 428 | An overwriting write came without `If-Match` (docs/adr/0050 D3) |
| `too_many_attempts` | 429 | More login attempts from this address within a minute than COWORK_LOGIN_ADDRESS_LIMIT allows; `Retry-After` says how long to wait (docs/adr/0033 D6) |
| `internal` | 500 | Something failed inside cowork; the `request_id` finds it in the log |
| `not_ready` | 503 | The backend cannot reach its database |
| `timeout` | 504 | The request took longer than the configured limit (docs/adr/0039 D2) |
| `backend_unreachable` | 502 | The frontend's proxy could not reach the backend; answered by nginx without a request id (docs/adr/0047 D6) |
<!-- problem-codes:end -->

### Helm chart values

Source: [`deploy/helm/cowork/values.yaml`](deploy/helm/cowork/values.yaml). Every value below is the default.

```yaml
nameOverride: ""
fullnameOverride: ""
imagePullSecrets: []
serviceAccount:
  create: true
  annotations: {}
  name: ""
  automountServiceAccountToken: false # neither pod talks to the Kubernetes API
database:                             # the runtime role: owns nothing, held to row-level security
  existingSecret: ""                  # preferred: the URL never enters the release
  existingSecretKey: databaseUrl
  url: ""                             # renders <fullname>-database; plain text in the release Secret and in `helm get values`
  owner:                              # the role the migrations run as; only the migrate init container reads it
    existingSecret: ""                # preferred; this or url is required while backend.config.migrateOnStart is true
    existingSecretKey: databaseUrl
    url: ""                           # renders <fullname>-database-owner while migrateOnStart is true; plain text in the release either way
session:                              # COWORK_SESSION_KEY, from a Secret only
  existingSecret: ""                  # required: rendering fails without it
  keys:
    key: sessionKey
  lifetime: 12h                       # COWORK_SESSION_LIFETIME, the absolute lifetime of a browser session
  idle: 2h                            # COWORK_SESSION_IDLE, how long a session may lie unused
localAdmin:                           # the local administrator of docs/adr/0032: a global administrator kept in step with this Secret at every start
  existingSecret: ""                  # preferred; with the inline values empty there is no local administrator and nobody can log in
  keys:
    username: username                # COWORK_LOCAL_ADMIN_USERNAME
    password: password                # COWORK_LOCAL_ADMIN_PASSWORD
  username: ""                        # renders <fullname>-local-admin; plain text in the release and in `helm get values`
  password: ""                        # both inline values or neither; the existing Secret wins; needs backend.config.baseURL
bootstrap:
  tenant:                             # created at start while no tenant exists; once one does, these do nothing; needs localAdmin
    slug: ""                          # COWORK_BOOTSTRAP_TENANT_SLUG
    name: ""                          # COWORK_BOOTSTRAP_TENANT_NAME; both or neither
auth:
  local:
    passwordMinLength: 12             # COWORK_PASSWORD_MIN_LENGTH, characters; below 8 the backend refuses to start
    lockout: window                   # COWORK_LOGIN_LOCKOUT: window | admin
    maxFailures: 5                    # COWORK_LOGIN_MAX_FAILURES within fifteen minutes; 0 never locks
    addressLimit: 20                  # COWORK_LOGIN_ADDRESS_LIMIT attempts per client address (IPv6: per /64) and minute; 0 disables
storage:                              # S3-compatible object storage; without an endpoint uploads are refused
  existingSecret: ""                  # required with an endpoint: the key of a bucket-scoped policy, never root
  keys:
    accessKeyId: accessKeyId          # COWORK_S3_ACCESS_KEY_ID
    secretAccessKey: secretAccessKey  # COWORK_S3_SECRET_ACCESS_KEY
  endpoint: ""                        # COWORK_S3_ENDPOINT, e.g. https://s3.example.com; setting it turns the storage on
  bucket: ""                          # COWORK_S3_BUCKET; required with an endpoint
  region: ""                          # COWORK_S3_REGION, set only when non-empty; empty asks the server
  pathStyle: true                     # COWORK_S3_USE_PATH_STYLE
  tls:
    caConfigMap: ""                   # ConfigMap with a private authority's PEM, mounted at /etc/cowork/s3-ca
    keys:
      ca: ca.crt                      # the ConfigMap's key; COWORK_S3_CA=/etc/cowork/s3-ca/<key>
networkPolicy:                        # which pods may reach the backend
  enabled: true                       # ingress to the backend pods from the frontend pods only; enforced only by a network plugin that implements NetworkPolicy
ingress:                              # targets the frontend Service
  enabled: false
  className: ""
  annotations: {}                     # the event stream and uploads need some: docs/operations/runtime.md
  hosts:
    - host: cowork.example.com        # example
      paths:
        - path: /
          pathType: Prefix
  tls: []
backend:
  replicaCount: 1                     # every pod migrates in its init container; they serialise on an advisory lock
  image:                              # the migrate init container runs the same image
    repository: guidedtraffic/cowork-backend
    pullPolicy: IfNotPresent
    tag: ""                           # default: the chart appVersion
  containerPort: 8080                 # COWORK_LISTEN_ADDR=":8080"
  service:
    type: ClusterIP
    port: 8080
  config:
    migrateOnStart: true              # the migrate init container; false: nothing migrates — run `cowork migrate` yourself
    logLevel: info                    # COWORK_LOG_LEVEL, also for the init container
    logFormat: json                   # COWORK_LOG_FORMAT, also for the init container
    shutdownTimeout: 15s              # COWORK_SHUTDOWN_TIMEOUT; keep below terminationGracePeriodSeconds
    baseURL: ""                       # COWORK_BASE_URL, set only when non-empty: the origin the browser shows; required with localAdmin
    trustedProxies: ""                # COWORK_TRUSTED_PROXIES, set only when non-empty: the networks of the proxies in front of the backend, comma-separated CIDRs
    maxJsonBody: 1048576              # COWORK_MAX_JSON_BODY, bytes; 0 disables
    attachmentMaxBytes: 10485760      # COWORK_ATTACHMENT_MAX_BYTES, bytes; 0 disables (nginx then has no body limit either)
    attachmentMaxPerTicket: 100       # COWORK_ATTACHMENT_MAX_PER_TICKET; 0 disables
    requestTimeout: 30                # COWORK_REQUEST_TIMEOUT, seconds; 0 disables; the event stream is exempt
    maxPageSize: 200                  # COWORK_MAX_PAGE_SIZE; 0 disables
    maxQueryLength: 256               # COWORK_MAX_QUERY_LENGTH, characters; 0 disables
    sseReplayWindow: 5m               # COWORK_SSE_REPLAY_WINDOW
    sseMaxStreamsPerPerson: 10        # COWORK_SSE_MAX_STREAMS_PER_PERSON, per replica; 0 disables
  extraEnv: []                        # appended verbatim to the backend container's env
  podAnnotations: {}
  podLabels: {}
  podSecurityContext:                 # distroless nonroot user
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    fsGroup: 65532
    seccompProfile:
      type: RuntimeDefault
  securityContext:                    # the binary writes nothing to disk; the init container has the same
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    capabilities:
      drop: [ALL]
  resources:                          # the init container has the same
    limits:
      memory: 256Mi
    requests:
      cpu: 50m
      memory: 128Mi
  probes:
    startup:                          # /healthz; the migration ran before, in the init container
      periodSeconds: 5
      failureThreshold: 36
    liveness:                         # /healthz
      periodSeconds: 10
    readiness:                        # /readyz, a database ping
      periodSeconds: 10
  terminationGracePeriodSeconds: 30
  nodeSelector: {}
  tolerations: []
  affinity: {}
frontend:
  replicaCount: 1
  image:
    repository: guidedtraffic/cowork-frontend
    pullPolicy: IfNotPresent
    tag: ""                           # default: the chart appVersion
  containerPort: 8080                 # fixed by the nginx configuration in the image
  service:
    type: ClusterIP
    port: 80
  extraEnv: []                        # the chart sets BACKEND_URL, NGINX_CLIENT_MAX_BODY_SIZE, NGINX_PROXY_READ_TIMEOUT
  podAnnotations: {}
  podLabels: {}
  podSecurityContext:                 # nginx-unprivileged user
    runAsNonRoot: true
    runAsUser: 101
    runAsGroup: 101
    fsGroup: 101
    seccompProfile:
      type: RuntimeDefault
  securityContext:                    # emptyDirs at /tmp and /etc/nginx/conf.d are all nginx writes
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    capabilities:
      drop: [ALL]
  resources:
    limits:
      memory: 64Mi
    requests:
      cpu: 10m
      memory: 32Mi
  probes:
    liveness:                         # /healthz, answered by nginx
      periodSeconds: 10
    readiness:
      periodSeconds: 10
  terminationGracePeriodSeconds: 30
  nodeSelector: {}
  tolerations: []
  affinity: {}
```

Rendering fails, naming the value, without `database.existingSecret` or `database.url`;
without `database.owner.existingSecret` or `database.owner.url` while
`backend.config.migrateOnStart` is `true`; without `session.existingSecret`; with a
`storage.endpoint` but no `storage.bucket` or no `storage.existingSecret`; with one of
`localAdmin.username` and `localAdmin.password` alone, or a local administrator without
`backend.config.baseURL`; and with `bootstrap.tenant.slug` and `.name` apart or without a local
administrator. `helm lint` reports
these as info lines, `helm template` and `helm install` fail. Where a Secret reference and its
inline URL are both set, the reference wins and the URL is ignored.

The modes that change what is exposed:

- **Secret references or inline URLs.** With `database.existingSecret` and
  `database.owner.existingSecret` no credential enters the release. `database.url` and
  `database.owner.url` are for throw-away installations: each is stored in plain text in the
  Helm release Secret and returned by `helm get values`, and the chart's notes warn for each.
  The owner URL is the worse one to inline — its role can switch row-level security off.
- **The owner credential reaches the `migrate` init container only.** The serving container
  gets the runtime URL and `COWORK_MIGRATE_ON_START=false`; never add the owner URL to
  `backend.extraEnv`. With `migrateOnStart: false` the chart needs no owner credential and
  renders no owner Secret — an inline `database.owner.url` still stays in the release values,
  readable with `helm get values`, so leave it empty — and nothing migrates: the backend
  refuses to start until `cowork migrate` has run.
- **The server key has no inline path.** One Secret gives every replica the same key;
  rotating it invalidates the cursors clients hold.
- **The local administrator: a Secret or inline values.** `localAdmin.existingSecret` names a
  Secret whose keys `localAdmin.keys.username` and `.password` hold the account; the chart never
  sees the password. `localAdmin.username` and `.password` render `<fullname>-local-admin` for
  a throw-away installation: plain text in the release Secret and in `helm get values`, like
  `database.url`, with a warning in the notes. The account follows the Secret at every start —
  a changed value reaches the pods when they restart; rotate the Secret **and** restart to end a
  leaked password ([operations](docs/operations/installation.md#the-local-administrator)).
- **`backend.config.trustedProxies` and the NetworkPolicy.** Empty, the login throttle counts the
  frontend pod as the one client of every browser; set to the proxies' networks it counts the
  browser. Too wide a list lets a client choose its address. The NetworkPolicy
  (`networkPolicy.enabled`, on by default) admits only the frontend pods to the backend, so no
  other pod can write `X-Forwarded-For` to it; it also refuses an in-cluster script or an Ingress
  that calls the backend Service directly, and a network plugin without NetworkPolicy support
  ignores it. The chart's notes warn about the empty list and about the policy switched off
  while the list is set ([operations](docs/operations/installation.md#the-client-address-and-the-trusted-proxies)).
- **The storage key comes from a Secret only**; endpoint, bucket and region are plain values.
  Without `storage.endpoint` the backend runs without object storage and refuses uploads.
- **`0` in `backend.config`** switches a backend limit off and opens nginx along with it —
  `maxJsonBody: 0` leaves nginx without a body limit, `requestTimeout: 0` gives it an hour's
  read timeout. No production values file should carry one
  ([runtime.md, limits](docs/operations/runtime.md#limits)). `attachmentMaxBytes: 0` removes the
  upload maximum and nginx's body limit with it; one upload at a time is then read whole
  ([attachments.md H-12](docs/security/attachments.md#h-12)).

## 🛠 Development

```bash
make help                 # every target, grouped
make dev                  # the whole stack with demo data; the UI on :4200 with live reload
make generate             # after a change to backend/api/, the SQL queries or the problem catalogue; CI fails on drift
make frontend-generate    # after make generate changed the API document: the Angular client; CI fails on drift
make lint cyclo gosec vuln
make postgres-up minio-up # what the integration tier needs
make test test-integration
make frontend-lint frontend-test-coverage frontend-build
make build                # bin/cowork and frontend/dist/frontend/browser
make docker-build         # both images, from backend/Containerfile and frontend/Containerfile
```

[docs/developer/](docs/developer/README.md) has the layout, the package map, the architecture,
the build and test matrix, the checklists, the toolchain versions and the CI and release
process.

## 📄 License

[Apache License 2.0](LICENSE).

The UI uses PrimeNG, PrimeIcons and `@primeuix/themes`, which since version 22 are under the
PrimeUI License: free under its Community License with a key, which the released images carry.
A build without a key works and shows PrimeNG's license notice; a fork needs a key of its own
([ADR 0052](docs/adr/0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md)
D9).
