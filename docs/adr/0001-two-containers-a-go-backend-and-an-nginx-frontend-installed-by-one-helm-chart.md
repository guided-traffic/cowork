# ADR 0001: Two Containers — a Go Backend That Migrates PostgreSQL 18 on Start and an nginx Frontend That Serves the Angular UI ~~and Proxies the API~~, the Ingress Routing the API to the Backend — Installed by One Helm Chart

## Status

Accepted, amended 2026-10-01 (D5: no down files — the owner's answer to the catalog question
"down migrations?", whose operating rule
[ADR 0028](0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) records),
amended 2026-10-02 (D5, D7, D8: migrations run under a separate owner role, see
[ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2; D3: four
substituted nginx variables, see [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D3), amended 2026-10-03 (D3: `/auth/` is proxied too, and the chart admits only the frontend's pods
to the backend), amended 2026-10-04 by the owner (D3: the Ingress routes `/api/` and `/auth/` to the
backend Service and everything else to the frontend Service, the frontend's nginx serves the UI only
and never reaches the backend, and the chart ships no NetworkPolicy — network policies are the
cluster administrator's; D7: the frontend takes no configuration; D8: its one `emptyDir` is `/tmp`;
the path routing this record had rejected is the decision now, and the nginx proxy the rejected
alternative, because it was an extra hop behind the reverse proxy the Ingress already is), amended
2026-10-06 by the owner (D3, the Alternatives and the Residual risks: the chart offers the Ingress
only and no route of the Gateway API; an installation on a Gateway writes its own `HTTPRoute`),
amended 2026-10-07 (D8: the backend container's `GOMEMLIMIT` is its memory limit).
Date: 2026-09-29. The stack was set by the owner in the founding brief; the cut into
two containers and the "latest release" policy (D2, D9) are the owner's explicit instructions
of the same day, given after a first skeleton had embedded the UI into the Go binary — that
shape is recorded under *Alternatives Considered*. The remaining shape rules were chosen while
building and are recorded here so they can be argued with.

**Partly built.** Verified in the working tree on 2026-09-29:

- D1: [`backend/go.mod`](../../backend/go.mod) declares Go 1.27.2; [`frontend/`](../../frontend/)
  is an Angular 22.2 workspace (`frontend/package.json`).
- D2, D3: [`backend/Containerfile`](../../backend/Containerfile) builds the distroless backend
  image; [`frontend/Containerfile`](../../frontend/Containerfile) builds the Angular bundle and
  puts it into `nginxinc/nginx-unprivileged` with ~~`frontend/nginx/default.conf.template`~~
  [`frontend/nginx/default.conf`](../../frontend/nginx/default.conf) *(a plain file since
  2026-10-04)*. Both
  images were run together, read-only, against a local PostgreSQL 18: ~~the frontend started
  with no backend present and answered `/api/` with `502`; once the backend appeared,
  `/api/v1/version` was proxied without a restart;~~ `/healthz` answered by nginx; a deep link
  resolved to `index.html` with `Cache-Control: no-store`; a hashed bundle with `immutable`.
- D4: [`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go)
  serves health, version and a JSON 404 for everything else; the tests pin the 404 and the 405.
- D5: `cowork serve` calls `store.Migrate` before it listens unless
  `COWORK_MIGRATE_ON_START=false` ([`backend/cmd/cowork/main.go`](../../backend/cmd/cowork/main.go)).
- D6: migration `000001_tenants` uses `uuidv7()`; the integration test asserts
  `server_version_num >= 180000`.
- D7: every backend setting is a `COWORK_*` variable
  ([`backend/internal/config/config.go`](../../backend/internal/config/config.go)); ~~the
  frontend takes `BACKEND_URL`~~ *(since 2026-10-04 the frontend takes none)*.
- D8: [`deploy/helm/cowork/`](../../deploy/helm/cowork/) renders two Deployments, two
  Services, an optional Ingress ~~on the frontend Service~~ *(since 2026-10-04 routing `/api/` and
  `/auth/` to the backend Service and the rest to the frontend Service)* and an optional database
  Secret.
- Phase 2, verified 2026-10-02: both images rebuilt and run together read-only with the
  migration as the owner role and the server as the runtime role; ~~the frontend answered a body
  above the backend's limit with the backend's problem and one above its own with its static
  problem body, a stopped backend with the `502` problem, an API path ending in `.png` reached
  the backend,~~ a 10 MiB upload passed, the event stream passed unbuffered, and `SIGTERM` ended
  an open stream at once *(the struck checks were of the frontend's proxy, gone since
  2026-10-04)*.
- D3 as amended 2026-10-04, verified the same day: the chart renders the Ingress with `/api/` and
  `/auth/` (`Prefix`) to `<fullname>-backend` and `/` to `<fullname>-frontend` for every host, and
  no NetworkPolicy (`make helm-lint helm-template` with every `ci/` file; an old values file with a
  host's `paths` and `networkPolicy.enabled` renders the same). Both images rebuilt and run
  read-only on one Docker network behind the Ingress stand-in of
  [`hack/ingress/default.conf`](../../hack/ingress/default.conf), the frontend with nothing but
  `/tmp` writable, no environment of its own, and started before the backend: through the
  stand-in the shell and a deep link came with `no-store` and the shell's policy, a hashed bundle
  with `immutable`, `/healthz` from nginx, and `/api/v1/version`, `/auth/options` and
  `/auth/callback` from the backend; the local administrator signed in through the login page in
  Chromium, the tenant's event stream stayed open past the stand-in's forty-second read timeout on
  its heartbeats and delivered a ticket's event 50 ms after the write, a JSON body above the
  backend's limit got the backend's `413` with its request id, a 10 MiB upload passed, and
  `SIGTERM` ended an open stream at once. The frontend alone answered `/api/` and `/auth/` — a
  path ending in `.png` and a body of 2 MiB among them — with the `404` problem, which the UI
  shows on its page. In a throwaway kind cluster (Kubernetes v1.36.1) behind ingress-nginx
  v1.15.1, the chart installed with its Ingress: the API server took the rendered paths, the
  frontend pod ran under the chart's security context with nothing but its `/tmp` volume, and the
  controller routed `/api/`, `/auth/` and `/auth/callback` to the backend and the rest to the
  frontend; with the controller's defaults a 2 MiB body got its own `413` page, with
  `proxy-body-size: 11m` the backend's `413` problem; the event stream delivered an event within
  50 ms and stayed open past a read timeout of 60 s and of 40 s, response buffering forced on
  included; a forged `X-Forwarded-For` through the controller moved no throttle bucket, while a
  pod calling the backend Service directly chose a new one with every forged header; no backend
  pod gave the controller's `503` page; the UI signed in and ran live in Chromium.

**Open:** everything that makes this a product — tenants, projects, tickets, users,
authentication, authorization, the API, the MCP interface — is not decided by this record. ~~Those decisions are the question catalog in
`docs/planning` and become ADRs of their own.~~ *(Amended 2026-10-06, the catalog's tombstone
deleted with its directory; no rule changes:)* those decisions were the founding question catalog,
which became ADR 0004 to ADR 0073
([ADR 0074](0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)).

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

**D3 — ~~The frontend is the entry point.~~ The Ingress is the entry point** *(amended
2026-10-04 by the owner; the rule it replaces is struck through below)*. For every host it
serves, the chart's Ingress routes `/api/` and `/auth/` (`pathType: Prefix`) to the backend
Service and `/` (`Prefix`) to the frontend Service; the paths are the chart's, not values. The
browser still sees one origin. The frontend's nginx serves the UI and nothing else: the bundle,
every path the router owns resolved to `index.html` (never cached, because it names the bundle
hashes), hashed bundles as immutable, its own `/healthz`, and the shell's `Content-Security-Policy`
([ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)
D6). It never reaches the backend: a request for `/api/` or `/auth/` that reaches it all the same —
an Ingress that sends every path to the frontend — is a `404` problem that names the cause, never
the shell. Its configuration is a plain file in the image,
[`frontend/nginx/default.conf`](../../frontend/nginx/default.conf), and nothing is substituted at
start. The Ingress controller is the one hop in front of the backend: its body limit and read
timeout are the installation's to set and must sit above the backend's limits
([ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md) D3
as amended), and the backend reads `X-Forwarded-For` from the controller's networks when
`COWORK_TRUSTED_PROXIES` names them ([ADR 0035](0035-personal-access-tokens.md) D2). The chart
ships no NetworkPolicy: who may reach the backend's and the frontend's pods is the cluster
administrator's policy — with `COWORK_TRUSTED_PROXIES` set, a pod inside those networks that
reaches the backend chooses its client address, and only such a policy keeps it from doing so
([docs/security/local-accounts.md](../security/local-accounts.md#h-17) H-17). The backend Service
serves scripts, tokens and a port-forward inside the cluster as before; a port-forward to the
frontend alone serves the UI without its API. *(Amended 2026-10-06 by the owner.)* The chart offers
the Ingress only and no route of the Gateway API: an installation on a Gateway leaves the Ingress
off and writes its own `HTTPRoute` with the same three path rules, outside the chart.

~~nginx serves the bundle, resolves every path the
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
tried in a cluster. A network plugin that does not implement NetworkPolicy ignores the object.)*~~

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

**D7 — Backend configuration is `COWORK_*` environment variables and nothing else~~; the
frontend's is `BACKEND_URL`~~.** *(Amended 2026-10-04: the frontend takes no configuration; its
nginx configuration is a file in the image, D3.)* No configuration files, no flags. `config.Load` reports every
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
service account token; ~~the frontend gets `emptyDir` volumes at `/tmp` and
`/etc/nginx/conf.d`, which is all nginx writes~~ *(amended 2026-10-04: the frontend gets an
`emptyDir` at `/tmp`, which is all nginx writes; its configuration is part of the image, which a
volume at `/etc/nginx/conf.d` would hide)*. There is no RBAC: neither container talks to
the Kubernetes API. *(Amended 2026-10-07: the backend container's `GOMEMLIMIT` is its memory
limit, in bytes, through the downward API — the Go runtime's soft limit, so that the collector works
harder as the process nears the limit, before the kernel kills the container
([docs/operations/runtime.md](../operations/runtime.md#memory)). It is no `COWORK_*` variable of
D7: the Go runtime reads it, not the configuration.)*

**D9 — Both toolchains track the newest stable release.** Go 1.27.1 and Angular 22.2 at the
time of writing; Renovate moves the Go version as one grouped change across `backend/go.mod`,
the `Containerfile` and the workflow, and the Angular packages as one grouped change. A
version that lags the newest release is a defect to fix, not a pin to keep. The one bound is
what the framework supports: TypeScript stays inside Angular's peer range.

## Consequences

- Two images per release, two rollouts, one chart version; a UI change ships without a Go
  build and the other way round.
- ~~One nginx hop in front of the API, and an nginx configuration to keep correct: the cache
  headers, the proxy headers, the template variable.~~ *(Amended 2026-10-04: no hop of ours in
  front of the API — the Ingress controller is the one hop — and an nginx configuration to keep
  correct for the UI alone: the cache headers, the shell's policy, the `404` for an API path that
  reaches it.)* Its correctness is proven by running the
  image, not by a unit test *(since 2026-10-04 behind the Ingress stand-in of
  [`hack/ingress/default.conf`](../../hack/ingress/default.conf))*.
- *(Added 2026-10-04.)* The controller's limits — its body size and its read timeout — are the
  installation's to set, in the controller's own settings, because the chart does not know
  which controller it is; what the controller answers itself, a `502` without a ready backend pod
  or its own `413` and `504`, is the controller's page, not a problem body
  ([ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D6 as amended).
- *(Added 2026-10-04.)* Without an Ingress there is no one origin: a port-forward to the
  frontend alone serves the UI without its API, and the chart's notes say so; a port-forward to
  the backend serves the API to scripts and tokens.
- *(Added 2026-10-04.)* The chart keeps no pod away from the backend any more. With
  `COWORK_TRUSTED_PROXIES` set, a pod inside its networks that reaches the backend chooses its
  client address for the login throttle and the audit's source hash; a policy of the cluster's that
  admits only the Ingress controller is what closes that
  ([docs/security/local-accounts.md](../security/local-accounts.md#h-17) H-17).
- Local development mirrors production: `ng serve` proxies `/api` to the backend ~~exactly as
  nginx does~~ *(amended 2026-10-04: as the Ingress routes it — the dev server's proxy is the
  developer's stand-in for the Ingress)* (`frontend/proxy.conf.json`, since 2026-10-03
  `frontend/proxy.conf.mjs`).
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
- ~~**Ingress path routing (`/api` → backend Service, `/` → frontend Service) instead of the
  nginx proxy.** No extra hop, and both Services exist, so an installation can still do it.
  Not the default because a port-forward to the frontend alone would then reach no API, and
  because the origin question (cookies, CORS) is settled once by proxying.~~ *(Amended
  2026-10-04: this is the decision now, D3, the owner's. A port-forward to the frontend alone
  reaches no API, as this entry said; the origin question is settled by the Ingress just as well,
  which serves one origin.)*
- **The frontend's nginx as the entry point, proxying `/api/` and `/auth/` to the backend
  Service** — D3 from 2026-09-29 to 2026-10-04, rejected by the owner on 2026-10-04. One Service
  for the Ingress, and a port-forward to the frontend that served the UI with its API; but an
  extra hop behind the reverse proxy the Ingress already is, with a second body limit, read
  timeout, buffering rule and set of error bodies to keep in step with the backend's, a resolver
  and four substituted variables, and a NetworkPolicy of the chart's to keep other pods from
  writing `X-Forwarded-For` to the backend.
- **An `HTTPRoute` of the Gateway API in the chart** — beside the Ingress, one of the two switched
  on (the recommendation), or in place of it — rejected by the owner on 2026-10-06. Kubernetes
  retired ingress-nginx in March 2026 ([installation.md](../operations/installation.md#expose-it)),
  and the route would have been a small template with the same three rules. The chart keeps one
  route to template and verify, and an installation on a Gateway writes its own.
- **Two origins with CORS.** More configuration in both halves for no benefit at this scale.
- **A Go static file server instead of nginx.** A second Go program to maintain for what
  nginx does by default.
- **Migrations in an init container or a Helm hook Job.** Cleaner separation of the DDL role
  and one migration run per rollout. Lost for the first version because it adds a second
  execution path to test; D5 keeps the door open.
- **A Postgres subchart.** Would make the demo install one command shorter and every
  production install wrong.

## Residual risks

- ~~Not verified in a cluster:~~ *(Verified 2026-10-04 in a kind cluster, see the Status:)* the
  frontend pod under the chart's `readOnlyRootFilesystem` with
  ~~the two `emptyDir` mounts~~ *(since 2026-10-04 its one `emptyDir` mount, `/tmp`)*. The
  ~~mounts are~~ mount is writable for user 101 only because the chart sets
  `fsGroup: 101`, which the kubelet applies to `emptyDir` volumes; verified with
  `docker run --read-only --tmpfs /tmp:uid=101,gid=101 --user 101:101` *(until 2026-10-04 with
  ~~`--tmpfs /etc/nginx/conf.d:gid=101,mode=2775`~~ as well)*, which models that. ~~A plain `--tmpfs` owned by root is not writable and the
  entrypoint then skips the template — the first run of the image found exactly that.~~
- ~~The runtime resolver is the cluster DNS from `/etc/resolv.conf`; `ipv6=off` and a 30 s
  cache are set in the template. A cluster whose DNS answers only over IPv6 is not covered.~~
  *(Gone 2026-10-04: the frontend resolves no backend.)*
- *(Added 2026-10-04.)* Verified against one controller only, ingress-nginx v1.15.1 in a kind
  cluster: no other controller, no Gateway API implementation, no cloud load balancer in front, no
  TLS at the Ingress, no production cluster.
- *(Added 2026-10-06.)* The chart offers no route of the Gateway API (D3). An installation on a
  Gateway writes and keeps its own `HTTPRoute`, which no release of the chart updates; one that
  stays on the retired ingress-nginx because the chart offers no route runs a controller without
  security fixes ([installation.md](../operations/installation.md#expose-it)).
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
- [`backend/Containerfile`](../../backend/Containerfile), [`frontend/Containerfile`](../../frontend/Containerfile), [`frontend/nginx/default.conf`](../../frontend/nginx/default.conf)
- [`deploy/helm/cowork/`](../../deploy/helm/cowork/), its [`templates/ingress.yaml`](../../deploy/helm/cowork/templates/ingress.yaml) — the routing of D3
- [`hack/ingress/default.conf`](../../hack/ingress/default.conf) — the stand-in for the Ingress when the two images run together
- [ADR 0003](0003-test-and-ci-policy.md) — the tiers that prove the above
