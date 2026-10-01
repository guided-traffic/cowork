# Architecture

What runs where, what a request goes through, and what happens between `cowork serve` and the
first answered request. Read against the tree on 2026-09-29. Everything described here exists;
what is planned is marked as such.

## Two containers, one origin

```
                          ┌───────────────────────────────────┐
  browser ───────────────►│ cowork-frontend (nginx)   :8080   │
  Claude (PAT) ──────────►│   /            → index.html       │
                          │   /<hashed>.js → immutable        │
                          │   /healthz     → nginx itself     │
                          │   /api/…       → proxy ─────────────┐
                          └───────────────────────────────────┘ │
                                                                ▼
  kubelet ───────────────►┌───────────────────────────────────┐
  scripts, port-forward ─►│ cowork-backend (Go)       :8080   │
                          │   /healthz, /readyz               │──► PostgreSQL 18
                          │   /api/v1/…                       │
                          │   everything else → JSON 404      │
                          └───────────────────────────────────┘
```

The frontend is the entry point and the only Service an Ingress targets; the browser sees one
origin. The backend Service stays cluster-internal for scripts and port-forwards
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D2–D4). **No authentication exists yet** — see
[docs/security/trust-boundaries.md](../security/trust-boundaries.md).

## Backend startup sequence (`cowork serve`)

[`backend/cmd/cowork/main.go`](../../backend/cmd/cowork/main.go), `runServe`:

1. `config.Load(os.LookupEnv)` — every `COWORK_*` variable is read and validated; all errors
   are reported together and the process exits 1 before anything else happens.
2. The logger is built from `COWORK_LOG_LEVEL` and `COWORK_LOG_FORMAT` (`log/slog`, JSON by
   default).
3. `SIGINT`/`SIGTERM` are bound to the context.
4. If `COWORK_MIGRATE_ON_START` is true (default): `store.Migrate` opens the database through
   `database/sql`, takes golang-migrate's advisory lock, applies every pending `up` migration
   and logs the resulting version. A dirty version or a failed migration ends the process; the
   startup probe in the chart then reports the pod as not started.
5. `store.Connect` opens the `pgxpool.Pool` and pings it. This pool is what `/readyz` pings on
   every call.
6. `httpserver.New` assembles the handler; `httpserver.ListenAndServe` binds
   `COWORK_LISTEN_ADDR`.
7. On a signal the server stops accepting, drains in-flight requests for up to
   `COWORK_SHUTDOWN_TIMEOUT`, closes the listener and the pool, and exits 0.

`cowork migrate` is steps 1, 2 and 4 alone.

## Backend request path

[`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go), `New`:

```
request ─► requestLog ─► http.ServeMux
                           ├─ "GET /healthz"          → {"status":"ok"}
                           ├─ "GET /readyz"           → Options.Ready(ctx) == nil ? 200 : 503 problem+json not_ready
                           ├─ "GET /api/v1/version"   → {"version","commit","buildTime"}
                           ├─ "/healthz", "/readyz", "/api/v1/version"  (no method) → 405, Allow: GET, HEAD
                           └─ "/"                     → 404 problem+json not_found
```

The mux uses Go 1.22 method patterns. `handleGet` registers each path twice — once with `GET`
(which also matches `HEAD`) and once without a method — because a request with the wrong
method would otherwise fall through to the catch-all and answer `404` instead of `405`. The
test `TestKnownPathsRejectOtherMethods` pins that.

**Errors on the API** are RFC 9457 problem details
([ADR 0047](../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md)):
`application/problem+json` with `type` (`https://cowork.dev/problems/<code>`), `title`,
`status`, `detail`, `instance` and the stable `code`; `writeProblem` in
[`server.go`](../../backend/internal/httpserver/server.go) is the one place that writes
them. `request_id` and the field-level `errors[]` arrive with the request-id and validation
middlewares.

## Frontend container

[`frontend/Containerfile`](../../frontend/Containerfile) builds the Angular production bundle
in a Node stage and copies `dist/frontend/browser/` into `nginxinc/nginx-unprivileged`.
[`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template) is
rendered by the image's entrypoint at start with `${BACKEND_URL}` and
`${NGINX_LOCAL_RESOLVERS}` (the nameservers of `/etc/resolv.conf`) substituted;
`NGINX_ENVSUBST_FILTER` names exactly those two, so nginx's own `$uri`, `$host` and friends
stay intact. The upstream is a variable behind a `resolver`, so nginx looks the backend up per
request (cached 30 s) rather than once at start — the frontend starts before the backend
Service exists and follows it when its address changes.

| Path | nginx does |
|---|---|
| `/healthz` | answers `{"status":"ok"}` itself — the frontend's probes, saying nothing about the backend |
| `/api/…` | `proxy_pass` to `BACKEND_URL` resolved per request, path unchanged, `X-Forwarded-*` set; `502` while no backend answers |
| `*.js`, `*.css`, fonts, images | serves the file with `Cache-Control: public, max-age=31536000, immutable`; the names are hashed |
| everything else | `try_files $uri /index.html` with `Cache-Control: no-store`, so the Angular router resolves deep links and a cached shell never pins old bundle hashes |

The container runs as user 101 with a read-only root filesystem; it writes only under `/tmp`
(pid, temp files) and `/etc/nginx/conf.d` (the rendered configuration), which the chart mounts
as `emptyDir`s made group-writable through `fsGroup: 101`. Without a writable `conf.d` the
entrypoint skips the template and nginx starts with no server block — silently answering
nothing on 8080.

## Local development

`make run` starts the backend on `:8080` against `make postgres-up`; `make frontend-serve`
starts the Angular dev server on `:4200` with [`frontend/proxy.conf.json`](../../frontend/proxy.conf.json)
forwarding `/api`, `/healthz` and `/readyz` — the same shape nginx has in the container, so
what works in development works behind nginx.

## What is planned and not built

The domain (tenants beyond the table, projects, tickets, links, questions), authentication
(OIDC sessions, personal access tokens), authorization (tenant membership, roles), the API
under `/api/v1/`, the MCP interface for Claude, the audit log. Each is a decision in
[docs/planning/questions.md](../planning/questions.md) before it is code, and each gets its own
section here when it exists.
