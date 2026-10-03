# ADR 0001: Two Containers — a Go Backend That Migrates PostgreSQL 18 on Start and an nginx Frontend That Serves the Angular UI and Proxies the API — Installed by One Helm Chart

## Status

Accepted, amended 2026-10-01 (D5: no down files, see
[ADR 0028](0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)),
amended 2026-10-02 (D5, D7, D8: migrations run under a separate owner role, see
[ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2; D3: four
substituted nginx variables, see [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D3), amended 2026-10-03 (D3: `/auth/` is proxied too, and the chart admits only the frontend's pods
to the backend). Date:
2026-09-29. The stack was set by the owner in the founding brief; the cut into
two containers and the "latest release" policy (D2, D9) are the owner's explicit instructions
of the same day, given after a first skeleton had embedded the UI into the Go binary — that
shape is recorded under *Alternatives Considered*. The remaining shape rules were chosen while
building and are recorded here so they can be argued with.

**Partly built.** Verified in the working tree on 2026-09-29:

- D1: [`backend/go.mod`](../../backend/go.mod) declares Go 1.27.1; [`frontend/`](../../frontend/)
  is an Angular 22.2 workspace (`frontend/package.json`).
- D2, D3: [`backend/Containerfile`](../../backend/Containerfile) builds the distroless backend
  image; [`frontend/Containerfile`](../../frontend/Containerfile) builds the Angular bundle and
  puts it into `nginxinc/nginx-unprivileged` with
  [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template). Both
  images were run together, read-only, against a local PostgreSQL 18: the frontend started
  with no backend present and answered `/api/` with `502`; once the backend appeared,
  `/api/v1/version` was proxied without a restart; `/healthz` answered by nginx; a deep link
  resolved to `index.html` with `Cache-Control: no-store`; a hashed bundle with `immutable`.
- D4: [`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go)
  serves health, version and a JSON 404 for everything else; the tests pin the 404 and the 405.
- D5: `cowork serve` calls `store.Migrate` before it listens unless
  `COWORK_MIGRATE_ON_START=false` ([`backend/cmd/cowork/main.go`](../../backend/cmd/cowork/main.go)).
- D6: migration `000001_tenants` uses `uuidv7()`; the integration test asserts
  `server_version_num >= 180000`.
- D7: every backend setting is a `COWORK_*` variable
  ([`backend/internal/config/config.go`](../../backend/internal/config/config.go)); the
  frontend takes `BACKEND_URL`.
- D8: [`deploy/helm/cowork/`](../../deploy/helm/cowork/) renders two Deployments, two
  Services, an optional Ingress on the frontend Service and an optional database Secret.
- Phase 2, verified 2026-10-02: both images rebuilt and run together read-only with the
  migration as the owner role and the server as the runtime role; the frontend answered a body
  above the backend's limit with the backend's problem and one above its own with its static
  problem body, a stopped backend with the `502` problem, an API path ending in `.png` reached
  the backend, a 10 MiB upload passed, the event stream passed unbuffered, and `SIGTERM` ended
  an open stream at once.

**Open:** everything that makes this a product — tenants, projects, tickets, users,
authentication, authorization, the API, the MCP interface — is not decided by this record. Those decisions are the question catalog in
[docs/planning/questions.md](../planning/questions.md) and become ADRs of their own.

## Context

The owner works in many repositories in parallel, with an LLM (Claude Code in VS Code) as a
co-worker, and loses the overview of what is open where. The tickets of those repositories are
Markdown files today. The tool that replaces them has to be small enough for one person to
run and change, has to be driven by an LLM as a first-class user, and has to keep the tickets
of different clients apart.

The founding brief fixes the stack: a Go backend, an Angular frontend, PostgreSQL 18 or newer
with an automatic schema migration when the pod starts, a Helm chart as the installation, OIDC
for people and personal access tokens for the LLM. The owner then fixed the cut: the backend
lives in `backend/`, the frontend in `frontend/`, each is its own container, and both start on
the newest release of their toolchain.

## Decision

**D1 — The backend is Go in `backend/`, the frontend is Angular in `frontend/`, the database
is PostgreSQL 18 or newer.** The Go module is `github.com/guided-traffic/cowork/backend`; the
Angular workspace's project is `frontend`.

**D2 — Two images, two Deployments, one chart.** `cowork-backend` (distroless, `cowork serve`,
the JSON API and the health endpoints on `:8080`) and `cowork-frontend` (nginx-unprivileged,
the production bundle, `:8080`). The chart deploys both under one release and one version.

**D3 — The frontend is the entry point.** nginx serves the bundle, resolves every path the
router owns to `index.html` (never cached, because it names the bundle hashes), serves hashed
bundles as immutable, answers its own `/healthz`, and proxies `/api/` to the backend Service.
The backend URL is one environment variable, `BACKEND_URL`, rendered into the nginx
configuration at container start; the image substitutes that variable and the resolver list
the entrypoint reads from `/etc/resolv.conf`, and no other (`NGINX_ENVSUBST_FILTER`)
*(amended 2026-10-02: and the body size and the read timeout, `NGINX_CLIENT_MAX_BODY_SIZE` and
`NGINX_PROXY_READ_TIMEOUT`, which the chart sets from the backend's limits — four variables;
`/api/` is a `^~` prefix so no static-file rule takes an API path)* *(amended 2026-10-03: the
browser's login routes, `/auth/options`, `/auth/local` and `/auth/logout`, are proxied exactly
like the API by a second `^~` prefix, `/auth/`, with the same settings and problem bodies; the
OIDC callback will be a path under it)*. nginx
resolves the backend per request through that resolver, so the frontend starts before the
backend Service exists and follows it when its address changes. The Ingress targets the
frontend Service only. The backend Service exists beside it for scripts, tokens and a
port-forward; nothing outside the cluster needs it. *(Amended 2026-10-03: the chart's
NetworkPolicy, `networkPolicy.enabled`, on by default, admits ingress to the backend's pods from
the frontend's pods only, on the backend's port — the backend reads `X-Forwarded-For` from the
networks of `COWORK_TRUSTED_PROXIES` ([ADR 0035](0035-personal-access-tokens.md) D2), and no other
pod may write it. A script inside the cluster therefore goes through the frontend Service, which
proxies `/api/` with the same tokens; the kubelet's probes come from the pod's node, which
Kubernetes always allows; a port-forward reaches the pod through its own network namespace, not
tried in a cluster. A network plugin that does not implement NetworkPolicy ignores the object.)*

**D4 — The backend serves no UI.** A path it does not know is a JSON `404`; a known path with
the wrong method is a `405` with `Allow`. The browser talks to one origin, so there is no CORS
and a session cookie is first-party.

**D5 — The backend migrates the schema on start, and can be told not to.** Migrations are
~~pairs of `NNNNNN_<name>.up.sql` / `.down.sql` files~~ *(amended 2026-10-01: `.up.sql`
files only, ADR 0028)* embedded into the binary and applied by golang-migrate before the
listener opens *(amended 2026-10-02: under the owner role of ADR 0021 D2, from
`COWORK_DATABASE_OWNER_URL`; in the chart by an init container, [ADR 0057](0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)
D1)*. Several replicas may start at once; golang-migrate
serialises them with a PostgreSQL advisory lock. `COWORK_MIGRATE_ON_START=false` skips the
run and `cowork migrate` applies it on demand, so an installation can move the migration into
a Job later without a code change. A dirty schema version is an error the backend refuses to
start on; it is not repaired automatically.

**D6 — Minimum server version 18, and the schema may rely on it.** `uuidv7()` is the default
primary key generator. The integration tier asserts the server version.

**D7 — Backend configuration is `COWORK_*` environment variables and nothing else; the
frontend's is `BACKEND_URL`.** No configuration files, no flags. `config.Load` reports every
invalid value at once and `COWORK_DATABASE_URL` is ~~the only required one~~ *(amended
2026-10-02: required, together with `COWORK_DATABASE_OWNER_URL` wherever migrations run —
`cowork migrate`, and `cowork serve` with `COWORK_MIGRATE_ON_START=true`)*. The chart maps its
values onto these variables one to one.

**D8 — The Helm chart is the installation, brings no database, and hardens both pods.** The
connection URL comes from `database.existingSecret` (preferred) or is rendered from
`database.url` for throw-away installations *(amended 2026-10-02: so does the owner role's,
from `database.owner.existingSecret` or `database.owner.url`, read by the migration's init
container only)*. Each pod runs as its image's non-root user
(65532 for distroless, 101 for nginx-unprivileged) with a read-only root filesystem, all
capabilities dropped, no privilege escalation, the `RuntimeDefault` seccomp profile and no
service account token; the frontend gets `emptyDir` volumes at `/tmp` and
`/etc/nginx/conf.d`, which is all nginx writes. There is no RBAC: neither container talks to
the Kubernetes API.

**D9 — Both toolchains track the newest stable release.** Go 1.27.1 and Angular 22.2 at the
time of writing; Renovate moves the Go version as one grouped change across `backend/go.mod`,
the `Containerfile` and the workflow, and the Angular packages as one grouped change. A
version that lags the newest release is a defect to fix, not a pin to keep. The one bound is
what the framework supports: TypeScript stays inside Angular's peer range.

## Consequences

- Two images per release, two rollouts, one chart version; a UI change ships without a Go
  build and the other way round.
- One nginx hop in front of the API, and an nginx configuration to keep correct: the cache
  headers, the proxy headers, the template variable. Its correctness is proven by running the
  image, not by a unit test.
- Local development mirrors production: `ng serve` proxies `/api` to the backend exactly as
  nginx does (`frontend/proxy.conf.json`, since 2026-10-03 `frontend/proxy.conf.mjs`).
- Migrations run under the application's database role, so that role needs DDL rights. A
  split into a migration role and a runtime role is possible later through D5's off switch and
  a Job.
- A failing migration keeps every backend replica from starting; the startup probe allows for
  a slow one, not for a broken one. That is intended.
- D9 means a Go or Angular release can land on a Tuesday and CI must absorb it; the grouped
  Renovate rules and the required gates are what make that safe.

## Alternatives Considered

- **One Go binary with the UI embedded (`go:embed`) — built first, rejected by the owner.**
  One image and one release unit, no nginx, no second Containerfile. The owner decided on two
  containers; what that buys: the standard Angular delivery, independent scaling and rollout
  of UI and API, and no Go build for a UI change. The embedded variant's SPA handler and its
  tests were removed in the same change.
- **Ingress path routing (`/api` → backend Service, `/` → frontend Service) instead of the
  nginx proxy.** No extra hop, and both Services exist, so an installation can still do it.
  Not the default because a port-forward to the frontend alone would then reach no API, and
  because the origin question (cookies, CORS) is settled once by proxying.
- **Two origins with CORS.** More configuration in both halves for no benefit at this scale.
- **A Go static file server instead of nginx.** A second Go program to maintain for what
  nginx does by default.
- **Migrations in an init container or a Helm hook Job.** Cleaner separation of the DDL role
  and one migration run per rollout. Lost for the first version because it adds a second
  execution path to test; D5 keeps the door open.
- **A Postgres subchart.** Would make the demo install one command shorter and every
  production install wrong.

## Residual risks

- Not verified in a cluster: the frontend pod under the chart's `readOnlyRootFilesystem` with
  the two `emptyDir` mounts. The mounts are writable for user 101 only because the chart sets
  `fsGroup: 101`, which the kubelet applies to `emptyDir` volumes; verified with
  `docker run --read-only --tmpfs /tmp:uid=101,gid=101 --tmpfs /etc/nginx/conf.d:gid=101,mode=2775
  --user 101:101`, which models that. A plain `--tmpfs` owned by root is not writable and the
  entrypoint then skips the template — the first run of the image found exactly that.
- The runtime resolver is the cluster DNS from `/etc/resolv.conf`; `ipv6=off` and a 30 s
  cache are set in the template. A cluster whose DNS answers only over IPv6 is not covered.
- Not verified: the advisory-lock behaviour under two backend replicas starting at the same
  second against the same fresh database.
- Not verified: `pgx` executing a multi-statement migration file through the simple protocol.
  Every migration so far is one statement.
- The distroless backend has no shell; a debugging session inside the pod needs an ephemeral
  container. nginx-unprivileged has one.

## References

- [`backend/cmd/cowork/main.go`](../../backend/cmd/cowork/main.go) — the `serve` and `migrate` commands
- [`backend/internal/config/config.go`](../../backend/internal/config/config.go) — the configuration surface
- [`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go) — the handler
- [`backend/internal/store/migrate.go`](../../backend/internal/store/migrate.go) — the migration run
- [`backend/Containerfile`](../../backend/Containerfile), [`frontend/Containerfile`](../../frontend/Containerfile), [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template)
- [`deploy/helm/cowork/`](../../deploy/helm/cowork/)
- [ADR 0003](0003-test-and-ci-policy.md) — the tiers that prove the above
