# Runtime behaviour

What the two cowork pods do between being scheduled and serving, how they hold a request to
its limits, what reaches a client from nginx and what from the backend, how the event stream
behaves behind proxies, and how the pods behave under the probes, on shutdown and in their
logs. The variables named here are explained one by one in
[README.md, Configuration](../../README.md#configuration).

## The backend

`cowork serve` is the container command. In order:

1. **Configuration.** Every `COWORK_*` variable is read. Invalid values and a missing
   `COWORK_DATABASE_URL` end the process with exit code 1 and one message that lists them
   all; `serve`'s own requirements — `COWORK_SESSION_KEY`, and `COWORK_DATABASE_OWNER_URL`
   while `COWORK_MIGRATE_ON_START` is `true` — are listed the same way once the rest is valid.
   These messages go to stderr as `cowork: …`, before the log exists. They name the variable
   and quote a rejected setting such as a size or a duration — never the value of a URL, a key
   or a secret.
2. **The migration run**, unless `COWORK_MIGRATE_ON_START=false`; then the log says
   `migrations skipped on start`. The chart always sets `false`: its pods migrate in an init
   container before the server starts. See below.
3. **The connection pool** is opened as the runtime role and pinged. A database that cannot
   be reached ends the process with exit code 1; the pod restarts and tries again, which is the
   intended behaviour while a database is still coming up.
4. **The database check.** A runtime role that could bypass row-level security is refused
   ([installation.md](installation.md#the-database-and-its-two-roles)), and so is a schema
   that is dirty or has pending migrations —
   `pending migrations: N; run the migration job (or set COWORK_MIGRATE_ON_START=true)`. Each
   ends the process with `database check failed` and exit code 1. A schema newer than the
   binary is served, with the warning `database schema is ahead of this binary; serving it`.
5. **The bootstrap**: the local administrator the variables name is created or brought in step,
   and the bootstrap tenant is created while no tenant exists — as `system:bootstrap`, under an
   advisory lock that makes replicas wait for each other
   ([installation.md](installation.md#the-local-administrator)). With neither variable set it
   deactivates an account it kept before and otherwise does nothing; a failure ends the process
   with `bootstrap failed` and exit code 1.
6. **The event listener** starts: one connection of its own, outside the pool, listening on
   the channel `cowork_events`; it reconnects by itself when the connection drops
   ([the event stream](#the-event-stream)).
7. **The object storage** client is set up when the `COWORK_S3_*` variables are set; it does
   not contact the storage. Without them the log warns `no object storage configured;
   attachments cannot be uploaded` ([attachments](#attachments)).
8. **The listener** opens on `COWORK_LISTEN_ADDR` and the log says `listening` with the
   address, the version and the commit.

From then on each replica, at start and once an hour, removes the idempotency records older
than a day, the sessions past their absolute or their idle limit, and the login's failed
attempts and ended locks older than fifteen minutes; each job holds a transaction-level
advisory lock of its own that lets one replica at a time do it, and the log says
`job removed expired rows` with the job and the count when there were any. A session past a
limit is refused at its next request whether or not the job has run; the job only keeps the
table small.

## The migration run

The schema lives in the binary as numbered SQL files, forward only. A run applies every file
newer than the version recorded in the `schema_migrations` table, each file in one
transaction, as the **owner role** of `COWORK_DATABASE_OWNER_URL`. The runtime role — the user
named in `COWORK_DATABASE_URL` — receives what each file grants it, and is checked before and
after the run. Several runs at once take a PostgreSQL advisory lock in turn; the first applies,
the rest log `database schema is current` with `applied=0`.

Where a run happens:

- **In the chart:** the `migrate` init container of every backend pod runs `cowork migrate`;
  the server container starts after it succeeded and never migrates
  ([installation.md](installation.md#how-the-schema-is-migrated)).
- **`cowork migrate`**, run by hand or by a Job you write, with both URLs — what an
  installation with `backend.config.migrateOnStart: false` has to do before the pods start.
- **`cowork serve` itself**, when `COWORK_MIGRATE_ON_START=true` and the owner URL is set —
  `make run` does this. A serving process that holds the owner credential keeps row-level
  security against defects but not against its own compromise; it is a development
  convenience, never the chart's way.

What stops a run:

- **A migration fails.** The version is recorded as *dirty*, the process exits 1, and every
  further run and every `cowork serve` refuses with `schema version N is dirty: a previous
  migration failed halfway and needs a manual repair`. The repair is manual on purpose. A file
  runs in one transaction, so a failed file left nothing behind: fix the cause, then set the
  version back to the last good one, `UPDATE schema_migrations SET version = <N-1>, dirty = false`,
  and the next run applies file N again. Clearing the flag alone
  (`UPDATE schema_migrations SET dirty = false`) records file N as applied; do that only after
  completing its work by hand. Nothing repairs automatically. An example: the owner could not
  create an extension and left version 8 dirty; an administrator created the extensions and
  set the version to 7, and the next run applied 8 to 14.
- **The owner role lacks a privilege** — the two messages are in
  [installation.md](installation.md#the-database-and-its-two-roles).
- **The runtime role** is a superuser, has `BYPASSRLS`, is the owner role itself or does not
  exist — the run refuses before it applies anything. A runtime role that owns relations of
  the schema, or, on an empty database, is a member of the owner role, is found after the run,
  with the migrations applied; exit 1 either way.
- **The database is older than PostgreSQL 18.** The first migration uses `uuidv7()`, which
  does not exist before 18; the failure surfaces as a dirty version 1.

A database whose version is newer than the binary — an image rolled back over a newer schema —
is not an error: the run applies nothing and logs
`database schema is ahead of this binary; nothing applied`, and `serve` serves it.

### Probes

| Probe | Path | Answers | The chart's default |
|---|---|---|---|
| startup | `/healthz` | 200 as soon as the listener is open. In the chart the migration ran before, in the init container, so this covers connecting and the database check | every 5 s, up to 36 failures — three minutes |
| liveness | `/healthz` | 200 while the process serves; says nothing about the database | every 10 s |
| readiness | `/readyz` | 200 when a ping on the connection pool succeeds; otherwise 503 `not_ready` with the detail `the database does not answer` — the ping's error goes to the log (`not ready`), never into the body | every 10 s |

A pod whose database goes away stays alive and leaves the Service endpoints until the
database is back; it is not restarted for it. While no backend endpoint is ready, the
frontend still serves the UI and answers `/api/` requests with its own `502`
([what nginx answers itself](#what-nginx-answers-itself)). The object storage is not probed.

### Shutdown

On `SIGTERM` the server stops accepting connections and, at the same moment, ends every open
event stream with `event: unavailable`, so the clients reconnect elsewhere instead of holding
the drain open; it finishes in-flight requests for up to `COWORK_SHUTDOWN_TIMEOUT` (default
15s), closes the pool and exits 0 — or 1, logging `server stopped with error`, when the drain
does not finish in time. With a stream open, the process stopped in well under a second in
the run of both images; no test measures it. Keep the timeout below the pod's `terminationGracePeriodSeconds` (chart
default 30s); otherwise the kubelet kills what the server was still draining.

### Log

One line per request — `method`, `path`, `status`, `duration` and the `request_id` that the
response carries in `X-Request-Id` and in every problem body — and the lifecycle events named
above, at `COWORK_LOG_LEVEL` (default `info`) in the format `COWORK_LOG_FORMAT` (default
`json`; `text` for a terminal). Request bodies, query strings and headers are not logged; an
event stream's line is written when the stream ends, with its whole duration. The lines worth
an alert or a look:

| Message | Level | Meaning |
|---|---|---|
| `request failed` | error | a request answered `500 internal`; the line carries the error and the request id the client saw |
| `panic` | error | a handler panicked; the client got `500 internal` |
| `not ready` | warn | `/readyz` failed its ping; the line carries the error |
| `the event listener lost its connection` | warn | event streams are refused until it reconnects ([the event stream](#the-event-stream)) |
| `slow query` | warn | a query took longer than 500 ms; the line names the query, never its arguments |
| `token refused` | info | a presented token was expired or revoked; the line names the token id and the reason |
| `client addresses are read through trusted proxies` | info | at start, when `COWORK_TRUSTED_PROXIES` is set; the line lists the networks as parsed |
| `the local administrator is created`, `… is in step with the configuration`, `… is deactivated: the configuration no longer names it`, `the bootstrap tenant is created` | info | the start's bootstrap changed something; the line names the username or the slug, never the password. Nothing is logged when nothing changed |
| `a stored password hash cannot be verified` | error | an account's hash is damaged or foreign; the login answers its person like a wrong password, and the line carries the request id |
| `job removed expired rows`, `job failed` | info, error | the hourly jobs ([above](#the-backend)) |
| `no object storage configured; attachments cannot be uploaded` | warn | at start, without `COWORK_S3_*` |
| `database schema is ahead of this binary; …` | warn | an image rollback over a newer schema |

There is no metrics endpoint yet; [ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md)
decides one, and it is not built.

## The login

Behaviour an operator meets once people log in. The mechanisms are in
[docs/security](../security/local-accounts.md); the settings are in
[README.md, Configuration](../../README.md#configuration).

**`403 csrf` on every write, with the UI otherwise working** is the first thing to check:
`COWORK_BASE_URL` must be exactly the origin the browser shows — scheme, host and port, no
path. The backend compares the `Origin` header (or the `Referer`) of every write of a session
and of the login with it, and the `detail` of the problem says which half failed: the origin,
or the `X-Requested-With` header the UI sets. Common causes: the URL was changed in the
Ingress and not in `backend.config.baseURL`; `http` where the browser shows `https`; a
`www.` host. Without a `COWORK_BASE_URL` no write of a cookie passes at all; the backend
refuses to start without one while the local administrator is configured
([CSRF](../security/csrf.md)). Tokens are not affected: a script's token request carries no
cookie and no check.

**The browser keeps no session** — the login answers `200` and the next request is `401`: the
cookie is `Secure` and has the `__Host-` prefix, so a browser stores it only over HTTPS (or on
`localhost`). A page reached over plain `http://` on another host cannot log in; terminate TLS
in front of the frontend.

**Sessions** live in the database: a restart of every pod ends none of them, and a changed
server key neither. The absolute lifetime is `COWORK_SESSION_LIFETIME` (12 hours), the idle
limit `COWORK_SESSION_IDLE` (2 hours); a request moves the idle clock at most once a minute.
An administrator ends an account's sessions with `DELETE …/accounts/{username}/sessions`;
a person's other sessions end when they change their password.

**Failed logins.** Every refusal is `401 invalid_credentials`, and a username nobody has is
counted and locked like one somebody has, so the answer does not help a guesser. Five failures
of a username within fifteen minutes lock it — until the window passes, or until a tenant
administrator unlocks it with `COWORK_LOGIN_LOCKOUT=admin`; the local administrator is
recovered by rotating its Secret and restarting
([installation.md](installation.md#the-local-administrator)). More than
`COWORK_LOGIN_ADDRESS_LIMIT` (20) attempts a minute from one client address — an IPv6 client by
its /64 — are `429` with
`Retry-After: 60`. The client address is the TCP peer's unless the peer is inside
`COWORK_TRUSTED_PROXIES`, in which case it is the first address of `X-Forwarded-For`, from the
right, that is not a proxy of ours. **With the list empty — the default — the address behind the
frontend is nginx's**, so the limit is one for the whole installation and one client's failing
logins can use it up for everybody; a list that is too wide lets a client choose its address
([H-17](../security/local-accounts.md#h-17);
[installation.md](installation.md#the-client-address-and-the-trusted-proxies) says what to set).
`COWORK_LOGIN_ADDRESS_LIMIT=0` and `COWORK_LOGIN_MAX_FAILURES=0` switch the throttle and the
lockout off. The failures, the locks and the unlocks are audit rows of the system actor
`system:login` — installation-level, readable in the database; no route shows them yet.

**The request log** carries the login like any request — method, path, status, duration, the
request id — and never the username, the password or the cookie.

## Limits

What a single request may cost is bounded by configuration
([ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md));
there are no request budgets — a client in a loop shows in the audit record and is stopped by
revoking its token. Each limit below is a variable and, in the chart, a `backend.config` value.

| Variable | Default | Beyond it | `0` |
|---|---|---|---|
| `COWORK_MAX_JSON_BODY` | `1MiB` | `413 payload_too_large` — before reading when the declared length is larger, while reading otherwise | no limit |
| `COWORK_ATTACHMENT_MAX_BYTES` | `10MiB` | `413` before anything is stored; the upload's body may be 64 KiB larger, for the multipart framing | no limit: one upload at a time is buffered whole, so a single upload can exhaust the container's memory ([attachments.md H-12](../security/attachments.md#h-12)) |
| `COWORK_ATTACHMENT_MAX_PER_TICKET` | `100` | `409 attachment_limit` | no limit |
| `COWORK_REQUEST_TIMEOUT` | `30s` | the handler's context is cancelled, `504 timeout`; the event stream is exempt | no limit |
| `COWORK_MAX_PAGE_SIZE` | `200` | a larger `limit` is clamped, not refused (without `limit` a page has 50) | no clamp |
| `COWORK_MAX_QUERY_LENGTH` | `256` characters | a longer full-text `q` is `400 validation_failed` | no limit of its own; the API document still caps `q` at 4096 characters |
| `COWORK_SSE_MAX_STREAMS_PER_PERSON` | `10`, per replica | the next stream closes the person's oldest with `event: unavailable` | no limit |
| `COWORK_SSE_REPLAY_WINDOW` | `5m` | a reconnect beyond the window starts with `event: resync` | no replay: a reconnect that missed anything starts with `resync` |

Two bounds are fixed: a numbered page that would end past row 10 000 (`page` × `per_page`)
is `400 page_too_deep`, and the uploads the backend holds in memory at once are bounded by a
64 MiB budget ([installation.md, resources](installation.md#resources-and-scheduling)).

**`0` belongs in no production values file.** A disabled body limit lets one request hold
unbounded memory; a disabled timeout lets one slow request hold its connection and its
database transaction for as long as it runs; a disabled stream limit lets one person hold any
number of streams, each with a buffer of its own. Nothing warns when a limit is `0` — not the
log, not the chart, which also opens nginx up in step (`0` body size, an hour's read timeout).
The chart's own defaults set none.

## What nginx answers itself

The frontend's nginx sits in front of every `/api/` request with two limits the chart derives
from the backend's: its body size is the larger of `maxJsonBody` and `attachmentMaxBytes`,
rounded up to whole MiB, plus 1 MiB (`11m` with the defaults), and its read timeout is
`requestTimeout` plus ten seconds (`40s`). So a request over a backend limit reaches the
backend and gets the backend's answer; nginx answers only what the backend never sees:

| Status | When | Body |
|---|---|---|
| `413` | the body is larger than nginx's limit | `code: payload_too_large`, detail `the request is larger than the proxy passes` |
| `502` | the backend cannot be reached — down, not ready, not resolvable | `code: backend_unreachable` |
| `503` | nginx generates one itself — mapped for completeness; nothing in this configuration is expected to | `code: not_ready` |
| `504` | the backend did not answer within nginx's read timeout | `code: timeout` |

These are static `application/problem+json` bodies from the nginx configuration: no
`instance`, no `request_id`, and no `X-Request-Id` header — that absence tells a client the
answer came from the proxy. Every answer the backend gives, its own `413`, `503` and `504`
included, passes through untouched (nginx does not intercept upstream errors) and carries the
request id.

## The event stream

`GET /api/v1/tenants/{tenant}/events` is a server-sent event stream of the tenant's changes
that the caller may see
([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)).
Each event carries a key, a version and the kind of change, never content:

```text
id: 01a0fe6c-90fc-7dde-81b5-3f0380e67ac4
event: ticket.changed
data: {"key":"dev/COW-1","version":2,"kind":"transitioned"}
```

How it behaves, as somebody running it sees it:

- **Every replica hears every change.** Each backend replica holds one database connection
  that listens on `cowork_events`; a change is published when its transaction commits, never
  when it rolls back. A client can reconnect to any replica.
- **A heartbeat every 20 seconds** (`: heartbeat`, an SSE comment) keeps proxies from closing
  a quiet stream and checks the token and the membership again; the stream ends when either
  is gone.
- **Reconnects replay.** A client that reconnects with `Last-Event-ID` gets what it missed
  while the event is still within `COWORK_SSE_REPLAY_WINDOW` on that replica; otherwise the
  stream starts with `event: resync` and the client refetches its lists.
- **A slow client is dropped, never waited for.** A stream that falls 256 events behind gets
  `event: resync` and is closed.
- **The listener's connection drops:** the log says `the event listener lost its connection`;
  new streams are refused with `503 not_ready` (`the event stream is unavailable; poll the
  lists`) while it reconnects with a growing pause of up to 30 seconds; once it is back, every
  open stream gets `event: resync` and is closed, so the clients reconnect and refetch.
- **The stream is exempt from `COWORK_REQUEST_TIMEOUT`**, and a shutdown ends it at once
  ([shutdown](#shutdown)).
- The backend answers with `Content-Type: text/event-stream`, `Cache-Control: no-cache` and
  `X-Accel-Buffering: no`. The frontend's nginx has a location of its own for the path:
  buffering and caching off, HTTP/1.1, a read timeout of one hour.

### Behind an Ingress

Whatever stands in front of the frontend Service has to pass the stream through unbuffered
and keep it open, and has to let uploads through. The frontend's nginx does not forward the
backend's `X-Accel-Buffering` header to it — nginx keeps `X-Accel-*` headers to itself — so
the Ingress needs its own settings. For ingress-nginx:

```yaml
ingress:
  annotations:
    nginx.ingress.kubernetes.io/proxy-buffering: "off"      # the event stream, unbuffered
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"  # seconds; as the frontend's stream location
    nginx.ingress.kubernetes.io/proxy-body-size: "11m"      # at least the frontend's NGINX_CLIENT_MAX_BODY_SIZE
```

Without the body size, an upload larger than the controller's default (1 MiB for
ingress-nginx) gets the controller's own `413` page instead of a problem body. **Streams that
die every minute** — or events that arrive late and in bursts — are the symptom of a proxy in
front that buffers the response or cuts it at a read timeout: set the two stream annotations
above, or their equivalent on another controller or load balancer. Not verified against an
ingress-nginx in this repository; the annotations and the default are from that project's
documentation.

## Attachments

Without object storage (no `COWORK_S3_*`), `POST …/attachments` answers
`501 uploads_disabled`; the lists and the metadata of attachments still answer, and a download
answers `404` saying the installation has no object storage. With storage configured, a
download whose object is missing from the bucket — a restore that brought the database back
without the bytes — answers `404` saying so, and an object store that refuses the backend shows
as `500 internal` with a `request failed` log line naming the storage's error. The storage is
checked by nothing at start; the first upload is the test. Setting it up:
[installation.md, object storage](installation.md#object-storage).

## The frontend

The image is `nginxinc/nginx-unprivileged` with the Angular bundle and one configuration
template. At start the image entrypoint renders the template into
`/etc/nginx/conf.d/default.conf` with four variables and nothing else: `BACKEND_URL` (the
chart sets it to the backend Service, `http://<fullname>-backend:8080`; the image's default,
`http://backend:8080`, is a name a plain `docker run` must provide), the cluster
nameservers from `/etc/resolv.conf` as `NGINX_LOCAL_RESOLVERS`, and the body size
`NGINX_CLIENT_MAX_BODY_SIZE` and read timeout `NGINX_PROXY_READ_TIMEOUT` that the chart
computes from the backend's limits (the image's defaults, `11m` and `40s`, match the backend's
defaults). nginx listens on 8080 as user 101. The backend name is resolved per request (cached
30 s), so the frontend pod starts and becomes ready whether or not the backend exists yet;
`/api/` answers `502` until it does. `frontend.extraEnv` exists for the entrypoint's own
switches (`NGINX_ENTRYPOINT_QUIET_LOGS`, for instance).

What nginx does with a request:

| Path | Behaviour |
|---|---|
| `/healthz` | `{"status":"ok"}` from nginx itself — the frontend's liveness and readiness probes; it says nothing about the backend |
| `/api/v1/tenants/<slug>/events` | proxied unbuffered and uncached, with a read timeout of one hour |
| `/api/…` | proxied to `BACKEND_URL` with the path unchanged and `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Real-IP` set; the errors nginx answers itself are problem bodies ([above](#what-nginx-answers-itself)). An API path that ends like a static file (`….png`) still goes to the backend |
| `/auth/…` | the same, for the login flows: `/auth/options`, `/auth/local`, `/auth/logout`; the cookie and the backend's `Set-Cookie` pass through, and the errors nginx answers itself are the same problem bodies |
| hashed bundles (`*.js`, `*.css`, fonts, images) | served with `Cache-Control: public, max-age=31536000, immutable` |
| everything else | `index.html` with `Cache-Control: no-store` — the Angular router resolves the path |

The pod runs with a read-only root filesystem; the chart mounts `emptyDir`s at `/tmp` and
`/etc/nginx/conf.d`, which is all nginx writes, and `fsGroup: 101` is what makes them
writable for the nginx user. If `conf.d` is mounted but not writable the entrypoint logs
`/etc/nginx/conf.d is not writable`, skips the template, and nginx serves nothing on 8080 —
the readiness probe then never passes, which is the symptom to look for. Without the mount,
on the read-only root filesystem, rendering fails with `can't create
/etc/nginx/conf.d/default.conf: Read-only file system` and the container exits 1. Both were
run against the built image. Its log is nginx's
access log on stdout — time, method, path without its query, status, size, duration — and its
error log on stderr, whose line for a request nginx failed carries the query
([trust-boundaries.md H-14](../security/trust-boundaries.md#h-14)). On `SIGTERM` the image's nginx exits within its
grace period; there is no draining beyond nginx's own.
