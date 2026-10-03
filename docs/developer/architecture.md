# Architecture

What runs where, what a request goes through, and what happens between `cowork serve` and the
first answered request. Read against the tree on 2026-10-03. Everything described here exists;
what is not built is listed at the end.

## Two containers, one origin

```
                          ┌───────────────────────────────────┐
  browser ───────────────►│ cowork-frontend (nginx)   :8080   │
  Claude (PAT) ──────────►│   /            → index.html       │
                          │   /<hashed>.js → immutable        │
                          │   /healthz     → nginx itself     │
                          │   /api/…, /auth/… → proxy ──────────┐
                          └───────────────────────────────────┘ │
                                                                ▼
  kubelet ───────────────►┌───────────────────────────────────┐
  scripts, port-forward ─►│ cowork-backend (Go)       :8080   │──► PostgreSQL 18
                          │   /healthz, /readyz               │      (runtime role; owner role
                          │   /api/v1/…, /auth/…              │       for the migrations)
                          │   everything else → JSON 404      │──► S3-compatible storage
                          └───────────────────────────────────┘      (optional; attachments)
```

The frontend is the entry point and the only Service an Ingress targets; the browser sees one
origin. The backend Service stays cluster-internal for scripts and port-forwards
([ADR 0001] D2–D4). Every route under `/api/v1` except the version and the API document needs a
personal access token or a session cookie ([api.md](api.md#authentication)); the login flows
live at `/auth/…` beside `/api/`; the security architecture is
[docs/security/](../security/README.md).

## Backend startup sequence (`cowork serve`)

[`backend/cmd/cowork/main.go`](../../backend/cmd/cowork/main.go), `runServe`:

1. `config.Load(os.LookupEnv)` reads and validates every `COWORK_*` variable, then
   `requireForServe` adds what only `serve` needs: `COWORK_SESSION_KEY`, and
   `COWORK_DATABASE_OWNER_URL` while `COWORK_MIGRATE_ON_START` is true. `config.Load` reports
   all of its problems together, `requireForServe`'s follow once `Load` passes, and the process
   exits 1 before anything else happens; a secret's value is never in the message.
2. The logger is built from `COWORK_LOG_LEVEL` and `COWORK_LOG_FORMAT` (`log/slog`, JSON by
   default); `SIGINT`/`SIGTERM` are bound to the context.
3. If `COWORK_MIGRATE_ON_START` is true (the default; the chart sets it false and migrates in an
   init container): `store.Migrate` runs as the owner role, with the runtime role's name from
   `COWORK_DATABASE_URL` for the grants ([data-access.md](data-access.md#two-database-roles)).
   A schema ahead of the binary is logged and left alone; a failure ends the process.
4. `store.Open` opens the runtime role's pool on `COWORK_DATABASE_URL` and pings it; `/readyz`
   pings it on every call.
5. `checkDatabase` refuses a runtime role that could bypass row-level security, a dirty schema,
   and pending migrations (`pending migrations: N; run the migration job …`); a schema ahead of
   the binary is served with a warning ([ADR 0057] D3, [ADR 0028]).
6. `bootstrap.Sync` keeps what the configuration says an installation starts with, as the
   runtime role under the advisory lock of the job `bootstrap`: the local administrator —
   created, re-hashed, deactivated, taken over — and, while no tenant exists, the bootstrap
   tenant ([`internal/bootstrap`](../../backend/internal/bootstrap/bootstrap.go),
   [ADR 0032] D2, D6, [ADR 0057] D4). A failure ends the process.
7. `events.New` builds the event hub; `go db.Listen(ctx, hub.Publish, hub.SetUp)` starts the one
   listener of this replica ([events.md](events.md)).
8. `storage.New` builds the object storage client when `COWORK_S3_*` is set; without it a warning
   says uploads are refused ([storage.md](storage.md)).
9. `api.New` loads the embedded API document and builds the router and the generated server
   (it also makes the dummy hash the login verifies unknown usernames against);
   `httpserver.New` wraps it with the health endpoints.
10. `go runJobs` runs the idempotency, session and login expiries at start and every hour
    ([data-access.md](data-access.md#jobs)).
11. `httpserver.ListenAndServe` binds `COWORK_LISTEN_ADDR`, with `hub.Close` registered for the
    shutdown. On a signal every event stream ends at once, the server stops accepting and drains
    in-flight requests for up to `COWORK_SHUTDOWN_TIMEOUT`, then the pool closes and the process
    exits 0 — or 1, logging `server stopped with error`, when the drain outlasts the timeout.

`cowork migrate` is the configuration (with `COWORK_DATABASE_OWNER_URL` required instead of
`requireForServe`), the logger and step 3 alone; it is what the chart's `migrate` init container
runs.

## Backend request path

[`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go), `New`:

```
request ─► withRequestID ─► requestLog ─► recoverer ─► http.ServeMux
                                                         ├─ "GET /healthz"  → {"status":"ok"}
                                                         ├─ "GET /readyz"   → Ready(ctx) == nil ? {"status":"ready"} : 503 not_ready
                                                         ├─ "/healthz", "/readyz" (other methods) → 405, Allow: GET, HEAD
                                                         ├─ "/api/", "/auth/" → the API handler (internal/api)
                                                         └─ "/"             → 404 not_found
```

- **The request id** is a UUIDv7 of the backend's making — an inbound `X-Request-Id` is not
  trusted — answered in `X-Request-Id`, and carried as the `request_id` of a problem body, of the
  log line and of the request's audit rows.
- **The request log** writes one line per request: method, path, status, duration, request id;
  never a body, a header or a query. Its status recorder passes `Flush` through, so the event
  stream is not buffered.
- **The recovery** answers a panic with `500 internal` and logs it under the request id.
- `/readyz` writes the ping's error to the log only: it can name the host and the user.
- `handleGet` registers each health path twice — with `GET` (which matches `HEAD`) and without a
  method — because a wrong method would otherwise fall through to the catch-all and answer `404`;
  `TestKnownPathsRejectOtherMethods` pins it.

Everything under `/api/` runs the API pipeline of
[`internal/api/api.go`](../../backend/internal/api/api.go), `ServeHTTP`:

```
route in the document ─► authenticate ─► session rules ─► tenant boundary ─┬─► timeout ─► body limit ─► validate ─► strict handler
  404 / 405               401 / 403 / 400   403 csrf /      404            │                413          400 / 404
                          (token or         password_change_required       └─► streamEvents: validate ─► serveEvents (no timeout, no limit)
                           session)
```

Each step, and what it answers, is [api.md](api.md#the-pipeline). Every error is an RFC 9457
problem details body written by `problem.Write` ([ADR 0047]).

**Who the client is.** A browser reaches the backend through the frontend's nginx and, with an
Ingress, a controller before it — a cloud load balancer in front of that is a third — and each
appends the address it saw to `X-Forwarded-For`. The backend finds the client by walking that header from the right through the
networks of `COWORK_TRUSTED_PROXIES`, starting at the TCP peer, and uses the address for the
login throttle only ([api.md](api.md#the-pipeline), [the
rule](../security/local-accounts.md#the-client-address); the chain and what to set are
[installation.md](../operations/installation.md#the-client-address-and-the-trusted-proxies)). The
chart's NetworkPolicy keeps every pod but the frontend's away from the backend, because the
walk trusts what a trusted peer says.

## Frontend container

[`frontend/Containerfile`](../../frontend/Containerfile) builds the Angular production bundle
in a Node stage and copies `dist/frontend/browser/` into `nginxinc/nginx-unprivileged`.
[`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template) is
rendered by the image's entrypoint at start with four variables substituted;
`NGINX_ENVSUBST_FILTER` names exactly those four, so nginx's own `$uri`, `$host` and friends stay
intact.

| Variable | Image | Chart |
|---|---|---|
| `BACKEND_URL` | `http://backend:8080` `# default` | the backend Service; never `localhost:8080`, which is nginx itself and would proxy `/api/` back into the proxy |
| `NGINX_LOCAL_RESOLVERS` | the nameservers of `/etc/resolv.conf`, exported by the entrypoint (`NGINX_ENTRYPOINT_LOCAL_RESOLVERS=true`) | — |
| `NGINX_CLIENT_MAX_BODY_SIZE` | `11m` `# default` | the larger of `backend.config.maxJsonBody` and `attachmentMaxBytes`, rounded up to MiB, plus 1 MiB; `0` (no limit) when either is 0 |
| `NGINX_PROXY_READ_TIMEOUT` | `40s` `# default` | `backend.config.requestTimeout` plus ten seconds; `3600s` when it is 0 |

nginx is sized above the backend's limits so the backend answers its own `413` and `504`, with a
request id ([ADR 0039] D3); the chart computes the last two in
[`_helpers.tpl`](../../deploy/helm/cowork/templates/_helpers.tpl). The upstream is a variable
behind a `resolver`, so nginx looks the backend up per request (cached 30 s) rather than once at
start — the frontend starts before the backend Service exists and follows it when its address
changes.

| Path | nginx does |
|---|---|
| `/healthz` | answers `{"status":"ok"}` itself — the frontend's probes, saying nothing about the backend |
| `/api/…` | `location ^~ /api/`, so no static-file rule takes an API path ending in `.png` or `.svg`: `proxy_pass` to `BACKEND_URL` resolved per request, path unchanged, `X-Forwarded-*` set, body size and read timeout from the variables above. What nginx answers itself — `413`, `502`, `503`, `504` — is a static `application/problem+json; charset=utf-8` body without `instance` or `request_id` (`502` is `backend_unreachable`); the backend's own errors pass through ([ADR 0047] D6) |
| `/auth/…` | `location ^~ /auth/`, the same proxy and the same static problem bodies, for the login flows; the cookie passes in both directions |
| `/api/v1/tenants/<slug>/events` | a nested location: unbuffered, uncached, a one-hour read timeout ([events.md](events.md#nginx)) |
| `/favicon.ico`, `/favicon.svg`, `/apple-touch-icon.png` | serves the file with `Cache-Control: no-cache`: the icons come from `public/` and keep their names across builds |
| `*.js`, `*.css`, fonts, images | serves the file with `Cache-Control: public, max-age=31536000, immutable`; the bundle names are hashed |
| everything else | `try_files $uri /index.html` with `Cache-Control: no-store`, so the Angular router resolves deep links and a cached shell never pins old bundle hashes |

The container runs as user 101 with a read-only root filesystem; it writes only under `/tmp`
(pid, temp files) and `/etc/nginx/conf.d` (the rendered configuration), which the chart mounts
as `emptyDir`s made group-writable through `fsGroup: 101`. A `conf.d` that is mounted but not
writable makes the entrypoint log `/etc/nginx/conf.d is not writable` and skip the template:
nginx starts with no server block and answers nothing on 8080. Without the mount, on the
read-only root filesystem, rendering the template fails and the container exits 1. Both were
run against the built image.

## Local development

`make postgres-up` starts PostgreSQL 18 with the development database `cowork`, its owner role
`cowork_owner` and its runtime role `cowork_app`. `make dev` runs all of the following in one terminal, with demo data and the
UI's dev server ([frontend.md](frontend.md#the-development-loop)). `make run` starts the backend on `:8080`:
it migrates as `cowork_owner`, serves as `cowork_app` and makes a throw-away server key unless
`COWORK_SESSION_KEY` is set. `make dev-seed` creates a person, a tenant, an admin membership and
a token, and prints the token once. `make frontend-serve` starts the Angular dev server on
`:4200` with [`frontend/proxy.conf.mjs`](../../frontend/proxy.conf.mjs) forwarding `/api`,
`/auth`, `/healthz` and `/readyz` — the same shape nginx has in the container, and like nginx it
holds no credential. `make dev` ([`hack/dev.sh`](../../hack/dev.sh)) puts it together with the
real login: the backend with the local administrator `dev` and
`COWORK_BASE_URL=https://localhost:4200`, the dev server over HTTPS, the browser signing in as on
an installation ([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D2). The commands are [build-test-lint.md](build-test-lint.md#run-locally).

## What is not built

The identity provider (OIDC, the groups gate and mappings, [ADR 0029], [ADR 0030]) — the login is
local accounts only — and, with it, the groups snapshot of a session; the reactivation of a person
and the list of one's own sessions; the person-level lists, search, saved filters, the
boards and the dashboard; the administration of memberships and of restricted projects' lists;
the MCP server; the score beside the rank, the backlog's drag and the rebalancing of the rank's
keys; deletion and purge; the `/context` export; import; the notification inbox; metrics. The
order in which they come is [docs/planning/project-plan.md](../planning/project-plan.md); each
gets its section here, or a page of its own, when it exists.

[ADR 0001]: ../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md
[ADR 0029]: ../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md
[ADR 0030]: ../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md
[ADR 0032]: ../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md
[ADR 0028]: ../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md
[ADR 0039]: ../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md
[ADR 0047]: ../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md
[ADR 0057]: ../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md
