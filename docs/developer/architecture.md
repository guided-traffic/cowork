# Architecture

What runs where, what a request goes through, how the two logins make a session, and what happens
between `cowork serve` and the first answered request. Read against the tree on 2026-10-05; the
metrics listener and the instruments on 2026-10-06.
Everything described here exists; what is not built is listed at the end.

## Two containers, one origin

```
  browser ───────┐                          ┌───────────────────────────────────┐
  Claude (PAT) ──┴──► Ingress controller ──►│ cowork-frontend (nginx)   :8080   │
                      (the installation's)  │   /            → index.html       │
                      /     → frontend      │   /<hashed>.js → immutable        │
                      /api/, /auth/         │   /healthz     → nginx itself     │
                          → backend         │   /api/…, /auth/… → 404 problem   │
                              │             └───────────────────────────────────┘
                              ▼
  kubelet ───────────────►┌───────────────────────────────────┐
  scripts, port-forward ─►│ cowork-backend (Go)       :8080   │──► PostgreSQL 18
                          │   /healthz, /readyz               │      (runtime role; owner role
                          │   /api/v1/…, /auth/…              │       for the migrations)
                          │   everything else → JSON 404      │──► S3-compatible storage
  Prometheus ────────────►│                           :8081   │      (optional; attachments)
  (the pod, no Service)   │   /metrics, nothing else          │
                          └───────────────────────────────────┘
                                            │
                                            ├────────────────────► OpenID Connect issuer
                                            │                        (optional; discovery at start,
                                            │                         the code, the groups refresh)
                                            └────────────────────► the chat's model providers
                                                                     (optional; every step of a turn,
                                                                      the picked COWORK_CHAT_<ID>_URL)
```

The Ingress is the entry point: for every host it routes `/api/` and `/auth/` to the backend
Service and everything else to the frontend Service, so the browser sees one origin, and the
frontend's nginx serves the UI and never reaches the backend ([ADR 0001] D2–D4;
[`ingress.yaml`](../../deploy/helm/cowork/templates/ingress.yaml)). The backend Service also serves
scripts and port-forwards inside the cluster. Every route under `/api/v1` except the version, the API document and the
schema of `.cowork.yaml` needs a personal access token or a session cookie
([api.md](api.md#authentication)); the login flows live at `/auth/…` beside `/api/`. During a
login through the identity provider the browser goes to the issuer and comes back to
`/auth/callback`; the backend itself calls the issuer at start, at a login and at a session's
groups refresh, and the issuer never calls the backend ([the two logins](#the-two-logins)). With
chat providers configured, the backend calls the picked provider's model at every step of a turn of
the chat in the UI — the browser never does — and the model's tool calls come back into the backend's own handler
([a turn of the chat](#a-turn-of-the-chat)). A second listener, `COWORK_METRICS_ADDR` (`:8081`),
answers Prometheus at `/metrics` without authentication; it is on the pod and on no Service, and
shares the API listener's lifecycle ([metrics.md](metrics.md)). The security architecture is
[docs/security/](../security/README.md).

A third program runs on a person's machine, not in the cluster: `cowork-mcp`, the MCP server
Claude Code starts over stdio and the command its hooks run. It is a client of `/api/v1` with
the person's token, like a script — no path to the database, nothing the API does not allow
([mcp.md](mcp.md), [ADR 0040](../adr/0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)).

## Backend startup sequence (`cowork serve`)

[`backend/cmd/cowork/main.go`](../../backend/cmd/cowork/main.go), `runServe`:

1. `config.Load(os.LookupEnv)` reads and validates every `COWORK_*` variable — a database role's
   components composed into its URL in [`config/database.go`](../../backend/internal/config/database.go),
   so everything after reads `DatabaseURL` and `DatabaseOwnerURL` whichever way they came —, then
   `requireForServe` adds what only `serve` needs: `COWORK_SESSION_KEY`, the owner role's
   connection while `COWORK_MIGRATE_ON_START` is true, and with an issuer the client's id and
   secret (`OIDC.RequireClient`). `config.Load` reports
   all of its problems together, `requireForServe`'s follow once `Load` passes, and the process
   exits 1 before anything else happens; a secret's value is never in the message.
2. The logger is built from `COWORK_LOG_LEVEL` and `COWORK_LOG_FORMAT` (`log/slog`, JSON by
   default); `SIGINT`/`SIGTERM` are bound to the context. `metricsOf` makes the registry of the
   instruments, or none while `COWORK_METRICS_ADDR` is empty; it goes to the store, the hub, the API
   and the server below ([metrics.md](metrics.md)).
3. If `COWORK_MIGRATE_ON_START` is true (the default; the chart sets it false and migrates in an
   init container or a Job): `store.Migrate` runs as the owner role, with the runtime role's name from
   `COWORK_DATABASE_URL` for the grants ([data-access.md](data-access.md#two-database-roles)).
   A schema ahead of the binary is logged and left alone; a failure ends the process.
4. `store.Open` opens the runtime role's pool on `COWORK_DATABASE_URL` and pings it; `/readyz`
   pings it on every call.
5. `checkDatabase` refuses a runtime role that could bypass row-level security, a dirty schema,
   and pending migrations (`pending migrations: N; run the migration job …`); a schema ahead of
   the binary is served with a warning ([ADR 0057] D3, [ADR 0028]).
6. `discoverIssuer`, when `COWORK_OIDC_ISSUER` is set: `oidc.Discover` fetches the discovery
   document within thirty seconds through the issuer client — no redirect, at most 1 MiB — holds it
   to the rules — the issuer it names must be the configured string exactly, the authorization,
   token, keys and UserInfo endpoints `https` or loopback `http`, an asymmetric signature algorithm
   among those it names — and keeps the endpoints, the verifier over cowork's key set and the
   `end_session_endpoint`, which it drops with a warning when it breaks the rule; a failure ends the
   process (`identity provider discovery failed`). The result is `api.OIDCOptions`: the provider,
   the gate, the refresh interval, the display name ([ADR 0029] D1, D4).
7. `bootstrap.Sync` keeps what the configuration says an installation starts with, as the
   runtime role under the advisory lock of the job `bootstrap`: the local administrator —
   created, re-hashed, deactivated, taken over — and, while no tenant exists, the bootstrap
   tenant with the local administrator's grant and the administrator group's mapping
   ([`internal/bootstrap`](../../backend/internal/bootstrap/bootstrap.go),
   [ADR 0032] D2, D6, [ADR 0057] D4). A failure ends the process.
8. `events.New` builds the event hub; `go db.Listen(ctx, hub.Publish, hub.SetUp)` starts the one
   listener of this replica ([events.md](events.md)).
9. `storage.New` builds the object storage client when `COWORK_S3_*` is set; without it a warning
   says uploads are refused ([storage.md](storage.md)).
   `chatOf` builds a gateway per provider when `COWORK_CHAT_PROVIDERS` is set — it contacts no
   provider —
   and `api.ChatOptions` with the signal context, which ends the running turns at a shutdown, and a
   function that returns the root handler of step 10, which the turns' tool calls go through
   ([chat.md](chat.md)).
10. `api.New` loads the embedded API document and builds the router and the generated server
    (it also makes the dummy hash the login verifies unknown usernames against, and derives from
    the server key the keys of the cursors, the fingerprints, the two address hashes, the two
    sealers of the identity provider and the sealer of the tenants' GitHub webhook secrets);
    `httpserver.New` wraps it with the health endpoints.
11. `go runJobs` runs the idempotency, session, login, notification, GitHub delivery and import expiries and
    the purge of the tickets deleted thirty days ago at start and every hour, and asks as often whether the daily
    consistency check of the attachments is due ([data-access.md](data-access.md#jobs),
    [storage.md](storage.md#the-consistency-check)).
12. `serve` binds `COWORK_LISTEN_ADDR` and, unless it is off, `COWORK_METRICS_ADDR` — both before
    either serves, so a taken port refuses the start — and `httpserver.ServeAll` serves the two with
    one lifecycle, `hub.Close` registered for the API listener's shutdown. On a signal every event
    stream ends at once, both listeners stop accepting and drain in-flight requests for up to
    `COWORK_SHUTDOWN_TIMEOUT`, then the pool closes and the process exits 0 — or 1, logging
    `server stopped with error`, when a drain outlasts the timeout or a listener fails, which stops
    the other.

`cowork migrate` is the configuration (with the owner role's connection required instead of
`requireForServe`), the logger and step 3 — and, with `COWORK_MIGRATE_BOOTSTRAP=true`, step 7's
bootstrap after it, on a runtime-role pool of its own that `bootstrapAfterMigration` opens and
closes ([ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)
D4). The chart's `migrate` init container runs it without the switch, the migration Job of
`migrations.mode: job` with it. It discovers no issuer: neither ever reaches the identity provider,
and the Job reads the issuer and the administrator group without the client secret.

## Backend request path

[`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go), `New`:

```
request ─► withRequestID ─► instrument ─► requestLog ─► recoverer ─► http.ServeMux
                                                                      ├─ "GET /healthz"  → {"status":"ok"}
                                                                      ├─ "GET /readyz"   → Ready(ctx) == nil ? {"status":"ready"} : 503 not_ready
                                                                      ├─ "/healthz", "/readyz" (other methods) → 405, Allow: GET, HEAD
                                                                      ├─ "/api/", "/auth/" → the API handler (internal/api)
                                                                      └─ "/"             → 404 not_found
```

- **The request id** is a UUIDv7 of the backend's making — an inbound `X-Request-Id` is not
  trusted — answered in `X-Request-Id`, and carried as the `request_id` of a problem body, of the
  log line and of the request's audit rows.
- **The instruments** count the request in flight while it runs and then record it by its route —
  the pattern the API document or the mux names, `unmatched` when none does —, its method, its
  status and its duration ([metrics.md](metrics.md)); without a registry the step is left out.
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
route in the document ─► authenticate ─► session rules ─► tenant boundary ─┬─► timeout ─► body limit ─► validate ─┬─► strict handler
  404 / 405               401 / 403 / 400   403 csrf /      404            │                413          400 / 404 │
                          (token or         agent_forbidden /              │                                       └─► runChatTurn: serveChat
                           session)         password_change_required       │                                           (the turn's own limits; a stream)
                                                                           └─► streamEvents: validate ─► serveEvents (no timeout, no limit)

receiveGitHubWebhook (public, x-cowork-signed): route ─► webhookTenant (the tenant and its secret; 404) ─► timeout ─► body limit (413)
                                                ─► validate the headers, not the body ─► serveGitHubWebhook: signature (401) ─► 400 / 415
                                                ─► the delivery kept a day (200 again) ─► 202
```

Each step, and what it answers, is [api.md](api.md#the-pipeline) — GitHub's webhook, which takes
no person's credential and meets no boundary, is [api.md](api.md#githubs-webhook). Every error is an
RFC 9457 problem details body written by `problem.Write` ([ADR 0047]).

**Authentication may call the identity provider.** For a session of the identity provider whose
groups are older than `COWORK_OIDC_GROUPS_REFRESH`, `authenticateSession` runs the groups refresh
before anything else of the request — before the session rules, the boundary and the timeout. The
one request that claims it waits for the issuer, up to twenty seconds on a context of its own, with
no database connection or lock held; the session's other requests are served on its groups
meanwhile ([the groups refresh](#the-groups-refresh-in-the-request-path)). For a token of a person
of the provider whose last gate check is older than the interval, `authenticateToken` runs the token
gate, which reads the database only. Both may write, in transactions of their own, before the
request's handler has run.

**Who the client is.** A browser reaches the backend through the Ingress controller — a cloud
load balancer in front of that is a second hop — and each writes the address it saw into
`X-Forwarded-For` (ingress-nginx in place of what the client sent). The backend finds the client by walking that header from the right through the
networks of `COWORK_TRUSTED_PROXIES`, starting at the TCP peer, and uses the address for the
login throttle and for the keyed hash every audit row of the request carries
([api.md](api.md#the-pipeline), [the
rule](../security/local-accounts.md#the-client-address); the chain and what to set are
[installation.md](../operations/installation.md#the-client-address-and-the-trusted-proxies)). The
walk trusts what a trusted peer says, and the chart ships no NetworkPolicy: with the list naming a
network other pods live in, keeping them away from the backend is the cluster's policy
([H-17](../security/local-accounts.md#h-17)).

### A turn of the chat

A turn is one request that makes more requests of the same server
([chat.md](chat.md), [ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)):

```
POST …/chat (session) ─► the pipeline above ─► serveChat: availability · provider · Check · capabilities
                                                  · chat_busy + the registry ─► 200 text/event-stream
   ─► chat.Run, up to COWORK_CHAT_MAX_STEPS, within COWORK_CHAT_TURN_TIMEOUT, ended by a shutdown or a stop:
        llm.Provider.Complete ──► the picked provider (text streamed out as `text`)
        each tool call, at once: tools.Tool.Call ─► apigen client ─► chat.Loopback (the turn's tenant only)
                                         ─► the root handler: request id ─► request log ─► recoverer ─► the pipeline
                                            (cookie + X-Cowork-Agent: chat/<model>/<conversation> ─► an agent's request
                                             holding the person's chat capabilities)
                                         ─► `tool_result`
   ─► `error` (a failure) ─► `done`: the messages the turn added, and why — `stopped` after a stop

DELETE …/chat/turns (session) ─► StopChatTurns ─► stopTurns: cancel the person's turns in the tenant
                                                   on this replica ─► 204 once they have ended
```

The outer request holds its connection, one database-free goroutine for its comments and, while the
model writes, one connection to the provider; each tool call is a request of its own with its own
request id, log line, transaction and audit rows, and the event streams hear its acts like any.

## The two logins

Both make the same kind of session — a random cookie value, its SHA-256 in `sessions` — and both
replace the session the request presented ([sessions](../security/sessions.md)).

```
local:   POST /auth/local ─► LoginLocal: origin check, address throttle, one Argon2id
                             ─► store.RecordLoginAttempt (the username's lock) ─► store.CreateSession ─► 200 + cookie

identity provider:
  GET /auth/oidc/login ─► LoginOidc: state, nonce, PKCE verifier sealed into __Host-cowork-oidc ─► 302 to the issuer
       (silent=true, the login page's own attempt: prompt=none, and the issuer's error ─► /login?error=login_required)
       … the browser at the issuer …
  GET /auth/callback?code&state ─► OidcCallback: open the cookie (≤ 10 min), compare the state
       ─► oidc.Provider.Exchange: the code with the verifier and the client secret; the ID token verified,
          its nonce compared; the groups from the token, else UserInfo
       ─► store.CompleteOIDCLogin, one transaction of system:identity-provider under the person's lock:
          the gate, deactivated, the init state ─► the person kept or made ─► memberships derived
          in every tenant ─► the session, with its groups and the sealed refresh token
       ─► 303 to return_to + the session cookie, the state cookie cleared — or 303 to /login?error=<code>
```

| Piece | Where |
|---|---|
| The relying party: discovery, the authorization URL, the exchange and the ID token's verification, the refresh grant, UserInfo, the groups claim, the end-session URL | [`internal/oidc`](../../backend/internal/oidc/oidc.go) over go-oidc v3 and `golang.org/x/oauth2`; its own discovery, key set ([`keys.go`](../../backend/internal/oidc/keys.go)) and HTTP client — no redirect, at most 1 MiB, the endpoint rule ([`client.go`](../../backend/internal/oidc/client.go)) |
| The start and the callback, the state cookie, `return_to` | [`internal/api/oidc.go`](../../backend/internal/api/oidc.go) |
| The gate and its issuer check, the groups refresh's claim, question and answer, the token gate, what an event stream's heartbeat checks | [`internal/api/identity.go`](../../backend/internal/api/identity.go) |
| The transactions that decide — a login, a refresh's claim and its answer, a token's gate check, the end of a person's sessions — and the derivation of memberships | [`internal/store/identity.go`](../../backend/internal/store/identity.go) ([data-access.md](data-access.md#the-identity-providers-transactions)) |
| AES-256-GCM sealing under keys derived from the server key | [`internal/auth/seal.go`](../../backend/internal/auth/seal.go) |
| The variables, the issuer rule | [`internal/config/oidc.go`](../../backend/internal/config/oidc.go) |

### The groups refresh in the request path

```
authenticateSession ─► LookupSession ─► sessionLive ─► checkProviderSession (method oidc):
   not the configured issuer's person ─► store.EndProviderSessions: every session of the person ends
   ─► store.RefreshDue (groups older than the interval, no retry or lease pending)
   ─► refreshSession
         ─► store.ClaimSessionRefresh: one short transaction as the person moves refresh_retry_at 30 s ahead
            where due and free ─► not claimed: served on the session's groups, nothing waits
         ─► askIssuer, no connection or lock held, a context the request's end does not cancel, 20 s:
            open the sealed refresh token ─► oidc.Provider.Refresh (the token endpoint; the refreshed ID token,
            else UserInfo) ─► read / judged / refused / unreachable
         ─► store.ApplySessionRefresh, 10 s: a transaction of system:identity-provider, the person's lock, the
            session's row FOR UPDATE, the lease still ours? ─► read: groups stored unless the person's are newer,
            memberships derived while admitted; judged: the session row only; outside the gate every session of
            the person deleted; refused: this session deleted; unreachable: retry in a minute
   ─► ended: 401 like any ended session ─► otherwise LookupSession again (the administrator flag may have changed)
   ─► not a write the CSRF check refuses (movesIdleClock)? ─► TouchSession ─► the principal
```

An event stream checks the same at its heartbeat (`streamStillAdmitted`), without touching the idle
clock. The token gate is shorter: `authenticateToken` → revoked or expired? → `tokenGate`: not the
configured issuer's person, or one whose groups are older than `COWORK_OIDC_GROUPS_MAX_AGE`
(`groupsTooOld`) → `401 not_allowed` at once; else `store.GateDue` →
`store.CheckTokenGate`, which judges the person's stored groups against the gate as configured —
`401 not_allowed`, or the check stamped and the memberships derived. What each outcome means for
the person, and what it leaves open, is
[identity-provider.md](../security/identity-provider.md#the-groups-refresh).

## Frontend container

[`frontend/Containerfile`](../../frontend/Containerfile) builds the Angular production bundle
in a Node stage and copies `dist/frontend/browser/` into `nginxinc/nginx-unprivileged`, with
[`frontend/nginx/default.conf`](../../frontend/nginx/default.conf) over the base image's default
server at `/etc/nginx/conf.d/default.conf`, owned by root. Nothing is substituted at start — the
image sets no variable of its own, and none changes the configuration — and nginx resolves no name:
the frontend serves the UI and nothing else ([ADR 0001] D3).

| Path | nginx does |
|---|---|
| `/healthz` | answers `{"status":"ok"}` itself — the frontend's probes, saying nothing about the backend |
| `/api/…`, `/auth/…` | `location ^~ /api/` and `^~ /auth/`, so no static-file rule takes such a path: `return 404`, and `error_page 404 413 =404 @misrouted` sends that and a body above nginx's 1 MiB limit to one named location, which answers a static `application/problem+json; charset=utf-8` body — `not_found`, the detail naming the cause, without `instance` or `request_id` ([ADR 0047] D6) — under an empty `types {}`, so a path ending in `.png` is not typed `image/png`. The Ingress routes these paths to the backend; they reach the frontend only by mistake |
| `/favicon.ico`, `/favicon.svg`, `/apple-touch-icon.png` | serves the file with `Cache-Control: no-cache`: the icons come from `public/` and keep their names across builds; the shell's `Content-Security-Policy` |
| `*.js`, `*.css`, fonts, images | serves the file with `Cache-Control: public, max-age=31536000, immutable`; the bundle names are hashed; the shell's `Content-Security-Policy` |
| everything else | `try_files $uri /index.html` with `Cache-Control: no-store`, so the Angular router resolves deep links and a cached shell never pins old bundle hashes; the shell's `Content-Security-Policy` ([chat.md](chat.md#the-content-security-policy)) |

A second server listens on `127.0.0.1:8082` with nginx's `stub_status` at `/stub_status` and a `404`
for every other path, without an access log: the counts the exporter sidecar of
`frontend.metrics.exporter.enabled` reads, on the pod's loopback address, which nothing outside the
pod reaches ([ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md)
D7; [docs/operations/metrics.md](../operations/metrics.md#nginxs-numbers)).

The container runs as user 101 with a read-only root filesystem; it writes only under `/tmp`
(pid, temp files), which the chart mounts as an `emptyDir` made group-writable through
`fsGroup: 101`. A volume over `/etc/nginx/conf.d` would hide the configuration: nginx then starts
with no server and answers nothing on 8080. Both were run against the built image, and the chart's
mounts in a kind cluster.

## Local development

`make postgres-up` starts PostgreSQL 18 with the development database `cowork`, its owner role
`cowork_owner` and its runtime role `cowork_app`. `make dev` runs all of the following in one terminal, with demo data and the
UI's dev server ([frontend.md](frontend.md#the-development-loop)). `make run` starts the backend on `:8080`:
it migrates as `cowork_owner`, serves as `cowork_app` and makes a throw-away server key unless
`COWORK_SESSION_KEY` is set. `make dev-seed` creates a person, a tenant, an admin membership and
a token, and prints the token once. `make frontend-serve` starts the Angular dev server on
`:4200` with [`frontend/proxy.conf.mjs`](../../frontend/proxy.conf.mjs) forwarding `/api`,
`/auth`, `/healthz` and `/readyz` to the backend — the developer's stand-in for the Ingress, which
routes `/api/` and `/auth/` the same way on an installation; like the Ingress it holds no
credential. Runs of the two images get a stand-in of their own,
[`hack/ingress/default.conf`](../../hack/ingress/default.conf)
([build-test-lint.md](build-test-lint.md#run-the-images-together)). `make dex-up` starts the minimal Dex of
[`hack/dex/config.yaml`](../../hack/dex/config.yaml) on `localhost:5556`, and `make dev-up` all
three containers, each published on the loopback address only (`CONTAINER_BIND`). `make dev`
([`hack/dev.sh`](../../hack/dev.sh)) puts it together with the real logins: the backend with the
local administrator `dev`, Dex as its identity provider and `COWORK_BASE_URL=https://localhost:4200`,
the dev server over HTTPS, the browser signing in as on an installation — with the form, or with
*Sign in with Dex* as one of its four users
([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D2) — and, when LM Studio answers on `localhost:1234` with the model `COWORK_DEV_CHAT_MODEL`
(`qwen/qwen3-30b-a3b-2507` `# default`), the chat in the UI talking to it as its one provider,
`lmstudio`. The commands are [build-test-lint.md](build-test-lint.md#run-locally).

## What is not built

The reactivation of a person, the deactivation of a person of the identity provider, and the list
of one's own sessions; a global administrator's reading of the installation-level audit rows and
the deletion of a tenant ([ADR 0034] D2); the
revocation of a refresh token at the issuer when a session ends; the deletion of a project; the age
of the last export among the metrics of
[ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4 ([metrics.md](metrics.md#the-consistency-family)). The work lists in
[docs/tickets/](../tickets/README.md) say in which order they come; each gets its section here, or a
page of its own, when it exists.

[ADR 0001]: ../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md
[ADR 0029]: ../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md
[ADR 0030]: ../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md
[ADR 0032]: ../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md
[ADR 0034]: ../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md
[ADR 0028]: ../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md
[ADR 0047]: ../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md
[ADR 0057]: ../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md
