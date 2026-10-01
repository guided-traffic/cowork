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

> **Status: skeleton.** The backend, the frontend, the schema migration, the chart and the
> pipeline exist and are tested. Tenants, projects, tickets, users, OIDC and tokens are
> decided in [docs/planning/](docs/planning/) before they are built; the order is
> [the project plan](docs/planning/project-plan.md).

```mermaid
flowchart LR
  B[Browser] --> F
  C[Claude Code<br/>MCP + PAT] --> F
  F[cowork-frontend<br/>nginx + Angular bundle] -->|/api/ proxied| S
  K[kubelet] -->|/healthz /readyz| S
  S[cowork-backend<br/>Go API] --> P[(PostgreSQL 18)]
  S -. migrates on start .-> P
```

## ✨ Key features

- 🧩 **Two containers, one origin** — the Go backend serves the JSON API; the nginx frontend serves the Angular bundle and proxies `/api/` to it, so the browser sees one origin and the Ingress needs one rule.
- 🗄️ **Schema migrations on start** — embedded SQL, applied by the backend before it listens, serialised across replicas with a database advisory lock; switchable off for a migration Job.
- 🐘 **PostgreSQL 18** — `uuidv7()` keys and nothing older than 18.
- ⎈ **One Helm chart** — two hardened Deployments, the database credential from an existing Secret, no RBAC because neither container talks to the Kubernetes API.
- 🧪 **Tested in every layer** — Go unit and integration tiers, Angular unit tests, chart lint and render, one container scan per image, release tooling check; all as `make` targets CI runs unchanged.
- 🆕 **Newest toolchains** — Go 1.27 and Angular 22, moved by Renovate as grouped updates.
- 🗂️ **Documentation with five homes** — decisions in ADRs, work lists in tickets that get archived, one security page per perspective.
- 🧭 **Planned, not guessed** — a question catalog with options and recommendations, worked one decision at a time.

## 📛 Naming conventions

### Environment variables

Every backend setting is `COWORK_<NAME>`; the full table is under [Configuration](#configuration).
The frontend container takes one variable, `BACKEND_URL`.

### Kubernetes objects (Helm chart)

| Object | Name | Notes |
|---|---|---|
| Backend Deployment and Service | `<fullname>-backend` | `<fullname>` is `<release>-cowork`, or `cowork` when the release name contains it; `fullnameOverride` replaces it |
| Frontend Deployment and Service | `<fullname>-frontend` | the Ingress targets this Service |
| ServiceAccount | `<fullname>` | shared by both pods, no token mounted |
| Database Secret rendered by the chart | `<fullname>-database`, key `databaseUrl` | only with `database.url` |
| Labels | `app.kubernetes.io/name=cowork`, `app.kubernetes.io/instance=<release>`, `app.kubernetes.io/component=backend\|frontend`, `app.kubernetes.io/version`, `app.kubernetes.io/managed-by=Helm`, `helm.sh/chart` | selectors use `name`, `instance` and `component` |
| Container ports | `http`, `8080` on both containers | `backend.containerPort`; the frontend's is fixed by the nginx configuration |

### Files and images

| Thing | Pattern | Example |
|---|---|---|
| Migration | `backend/internal/store/migrations/NNNNNN_<snake_name>.up.sql`, versions `1..n` without a gap, no down files | `000001_tenants.up.sql` |
| Container images | `guidedtraffic/cowork-backend:<semver>`, `guidedtraffic/cowork-frontend:<semver>` | both carry the release version |
| Helm chart | `cowork`, `deploy/helm/cowork/` | `helm install cowork deploy/helm/cowork` |
| Go module | `github.com/guided-traffic/cowork/backend` | — |
| Angular project | `frontend`, output `frontend/dist/frontend/browser/` | — |
| Ticket (interim, in this repository) | `docs/tickets/NNN-<kebab-slug>.md`, `id: T<n>` | rules in [docs/tickets/README.md](docs/tickets/README.md) |
| Ticket key in cowork | undecided | question Q-A4 in [the catalog](docs/planning/questions.md) |

### HTTP

| Path | Backend | Frontend (nginx) |
|---|---|---|
| `/healthz` | liveness | nginx's own health, `{"status":"ok"}` |
| `/readyz` | readiness: a database ping | not served (backend Service only) |
| `/api/v1/…` | the JSON API; errors are RFC 9457 `application/problem+json` with a stable `code` | proxied to the backend, path unchanged |
| hashed bundles | — | served with `Cache-Control: public, max-age=31536000, immutable` |
| everything else | `404` JSON | `index.html` with `Cache-Control: no-store` |

## 📚 Documentation

| Document | What it is for |
|---|---|
| [docs/developer/](docs/developer/README.md) | Contributor entry point and how the code works: layout, package map, architecture, build/test/lint matrix, testing, CI and release, checklists, conventions |
| [docs/operations/](docs/operations/README.md) | Installing and running: installation, runtime behaviour |
| [docs/security/](docs/security/README.md) | The security architecture, one page per perspective; [SECURITY.md](SECURITY.md) to report a vulnerability |
| [docs/adr/](docs/adr/README.md) | Why cowork is the way it is |
| [docs/tickets/](docs/tickets/README.md) | The interim work lists and their rules |
| [docs/planning/](docs/planning/) | The question catalog, the project plan, the VS Code workflow plan — consumed into ADRs and tickets as work proceeds |
| [CLAUDE.md](CLAUDE.md) | The working rules for an LLM session in this repository |

## 🚀 Fast start

### Prerequisites

Go 1.27, Node.js 26 with npm, Docker (for the local PostgreSQL and the images), Helm 3 or 4.
`make help` lists every target.

### Run it locally

```bash
make postgres-up        # postgres:18 on localhost:5432, user/password/db = cowork
make run                # backend: migrates, then serves on :8080 (text logs)
make frontend-serve     # frontend: Angular dev server on :4200, /api proxied to :8080
curl -s localhost:8080/readyz          # {"status":"ready"}
curl -s localhost:4200/api/v1/version  # through the dev-server proxy
open http://localhost:4200
```

### Run the tests

```bash
make test               # backend unit + frontend unit
make test-integration   # against the container from make postgres-up
make lint frontend-lint helm-lint
```

### Build the images

```bash
make docker-build       # guidedtraffic/cowork-backend:latest and guidedtraffic/cowork-frontend:latest
```

### Install on Kubernetes

```bash
kubectl -n cowork create secret generic cowork-database \
  --from-literal=databaseUrl='postgres://cowork:CHANGE-ME@postgres:5432/cowork?sslmode=require'
helm upgrade --install cowork deploy/helm/cowork -n cowork --create-namespace \
  --set backend.image.repository=<registry>/cowork-backend --set backend.image.tag=<tag> \
  --set frontend.image.repository=<registry>/cowork-frontend --set frontend.image.tag=<tag> \
  --set database.existingSecret=cowork-database
kubectl -n cowork port-forward svc/cowork-frontend 8080:80     # the UI, /api/ proxied
```

There is no published image or chart repository yet. Details, Ingress and the CloudNativePG
note: [docs/operations/installation.md](docs/operations/installation.md).

<details>
<summary>Upgrade and uninstall</summary>

```bash
helm upgrade cowork deploy/helm/cowork -n cowork --reuse-values \
  --set backend.image.tag=<new> --set frontend.image.tag=<new>
helm uninstall cowork -n cowork      # the database is left untouched
```

The new backend pod migrates the schema before it listens; see
[docs/operations/runtime.md](docs/operations/runtime.md).

</details>

## 📖 Reference

### Configuration

Every variable is read once at backend start; an invalid value or a missing required one ends
the process with every problem listed. Source: [`backend/internal/config/config.go`](backend/internal/config/config.go).

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_DATABASE_URL` | — (required) | `postgres://user:pass@host:5432/db?sslmode=require` | The PostgreSQL 18 connection URL. The role owns the schema: migrations run under it. Never log it, never put it in Helm values outside a throw-away install |
| `COWORK_LISTEN_ADDR` | `:8080` `# default` | `host:port` | The backend listener for API and health |
| `COWORK_MIGRATE_ON_START` | `true` `# default` | `true`, `false` | Apply pending migrations before listening. `false` for installations that run `cowork migrate` in a Job |
| `COWORK_LOG_LEVEL` | `info` `# default` | `debug`, `info`, `warn`, `error` | Minimum level |
| `COWORK_LOG_FORMAT` | `json` `# default` | `json`, `text` | `text` for a terminal |
| `COWORK_SHUTDOWN_TIMEOUT` | `15s` `# default` | a positive Go duration | Drain bound after `SIGTERM`; keep it below the pod's grace period |
| `COWORK_BASE_URL` | empty `# default` | `https://cowork.example.com` `# example` | The public URL. Read by nothing today; OIDC redirects will need it |

The frontend container reads one variable:

| Variable | Default | Meaning |
|---|---|---|
| `BACKEND_URL` | `http://localhost:8080` `# default` (image); the backend Service `# chart` | Where nginx proxies `/api/`. Rendered into the configuration at start and resolved per request through the cluster DNS; besides it only `NGINX_LOCAL_RESOLVERS`, set by the image entrypoint, is substituted |

### CLI (backend)

| Command | Does |
|---|---|
| `cowork serve` | Load configuration, migrate (unless disabled), connect, listen until `SIGINT`/`SIGTERM` |
| `cowork migrate` | Load configuration, apply pending migrations, exit 0; exit 1 on a dirty or failing schema |
| `cowork version` | Print `cowork <version> (commit <sha>, built <epoch>)` |
| `cowork help` | Print the usage |

Exit codes: `0` success, `1` configuration or runtime error, `2` unknown command or no command.

### API (backend)

| Method and path | Status | Body |
|---|---|---|
| `GET /healthz` | `200` | `{"status":"ok"}` — the process serves |
| `GET /readyz` | `200` / `503` | `{"status":"ready"}` / problem details with `code: not_ready` and the ping error as `detail` |
| `GET /api/v1/version` | `200` | `{"version":"…","commit":"…","buildTime":"…"}` |
| other methods on those paths | `405` | `Allow: GET, HEAD`, problem details with `code: method_not_allowed` |
| any other path | `404` | problem details with `code: not_found` and `detail: no route <METHOD> <path>` |

Every response carries `Content-Type: application/json; charset=utf-8` and
`Cache-Control: no-store`. An error body is `application/problem+json`
([RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)): `type`, `title`, `status`, `detail`,
`instance`, plus the stable `code` (snake_case) — for example
`{"type":"https://cowork.dev/problems/not-found","title":"Not found","status":404,"detail":"no route GET /x","instance":"/x","code":"not_found"}`.

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
database:
  existingSecret: ""                  # preferred: the URL never enters the release
  existingSecretKey: databaseUrl
  url: ""                             # renders <fullname>-database; plain text in the release Secret and in `helm get values`
ingress:                              # targets the frontend Service
  enabled: false
  className: ""
  annotations: {}
  hosts:
    - host: cowork.example.com        # example
      paths:
        - path: /
          pathType: Prefix
  tls: []
backend:
  replicaCount: 1                     # several replicas migrate under one advisory lock
  image:
    repository: guidedtraffic/cowork-backend
    pullPolicy: IfNotPresent
    tag: ""                           # default: the chart appVersion
  containerPort: 8080                 # COWORK_LISTEN_ADDR=":8080"
  service:
    type: ClusterIP
    port: 8080
  config:
    migrateOnStart: true              # COWORK_MIGRATE_ON_START
    logLevel: info                    # COWORK_LOG_LEVEL
    logFormat: json                   # COWORK_LOG_FORMAT
    shutdownTimeout: 15s              # COWORK_SHUTDOWN_TIMEOUT; keep below terminationGracePeriodSeconds
    baseURL: ""                       # COWORK_BASE_URL, set only when non-empty
  extraEnv: []                        # appended verbatim to the container env
  podAnnotations: {}
  podLabels: {}
  podSecurityContext:                 # distroless nonroot user
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    fsGroup: 65532
    seccompProfile:
      type: RuntimeDefault
  securityContext:                    # the binary writes nothing to disk
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    capabilities:
      drop: [ALL]
  resources:
    limits:
      memory: 256Mi
    requests:
      cpu: 50m
      memory: 128Mi
  probes:
    startup:                          # /healthz; covers the migration run
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
  extraEnv: []                        # BACKEND_URL is set by the chart to the backend Service
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

Exactly one of `database.existingSecret` and `database.url` must be set; rendering fails
otherwise (`helm lint` reports it as an info line, `helm template` and `helm install` fail).
Security note on `database.url`: the credential is stored in plain text in the Helm release
Secret and returned by `helm get values`; the chart's notes warn when it is set.

## 🛠 Development

```bash
make help                 # every target, grouped
make lint cyclo gosec vuln
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
