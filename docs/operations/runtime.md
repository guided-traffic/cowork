# Runtime behaviour

What the two cowork pods do between being scheduled and serving, how they hold a request to
its limits, what reaches a client from the backend, from the Ingress controller and from nginx,
how the event stream behaves behind proxies, and how the pods behave under the probes, on shutdown and in their
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
5. **The identity provider**, when `COWORK_OIDC_ISSUER` is set: the backend fetches
   `<issuer>/.well-known/openid-configuration` — thirty seconds at most, ten per call, no redirect
   followed, at most 1 MiB read. The `issuer` the document names must be the configured string
   exactly; its authorization, token, keys and UserInfo endpoints must be `https`, or `http` on a
   loopback host; and it must name a signature algorithm cowork verifies — an asymmetric one, or
   none, which means `RS256`. A provider that fails any of this ends the process with
   `identity provider discovery failed` and exit code 1, the error naming the issuer and the rule
   and never the client secret or the answer; in the chart the pod restarts with back-off and tries
   again. An end-session endpoint that fails the rule is dropped with the warning `the issuer's
   end_session_endpoint is dropped: a logout ends no session at the issuer`. Discovered, the log
   says `identity provider discovered` with the issuer, the number of allowed groups, whether an
   administrator group is set, the refresh interval, the groups' maximum age for a token and whether
   an address without the issuer's word on it is trusted; a gate that names no group warns `the
   identity provider's gate admits nobody …`, and the login page then offers no button
   ([installation.md](installation.md#the-identity-provider)).
6. **The bootstrap**: the local administrator the variables name is created or brought in step,
   and the bootstrap tenant is created while no tenant exists — with the administrator group
   mapped to its `admin` role when `COWORK_ADMIN_GROUP` is set — as `system:bootstrap`, under an
   advisory lock that makes replicas wait for each other
   ([installation.md](installation.md#the-local-administrator)). With neither variable set it
   deactivates an account it kept before and otherwise does nothing; a failure ends the process
   with `bootstrap failed` and exit code 1.
7. **The event listener** starts: one connection of its own, outside the pool, listening on
   the channel `cowork_events`; it reconnects by itself when the connection drops
   ([the event stream](#the-event-stream)).
8. **The object storage** client is set up when the `COWORK_S3_*` variables are set; it does
   not contact the storage. Without them the log warns `no object storage configured;
   attachments cannot be uploaded` ([attachments](#attachments)).
9. **The chat's providers**, when `COWORK_CHAT_PROVIDERS` is set: a gateway per provider is set up,
   the log says `the chat talks to a model` once per provider with its id, kind and model, and
   `the chat's limits`; it does not contact a provider, so a wrong URL or key shows at the first turn
   that picks it ([the chat's stream](#the-chats-stream), [chat.md](chat.md)).
10. **The listener** opens on `COWORK_LISTEN_ADDR` and the log says `listening` with the
    address, the version and the commit.

From then on each replica, at start and once an hour, removes the idempotency records older
than a day, the sessions past their absolute or their idle limit, the login's failed
attempts and ended locks older than fifteen minutes, and the notifications read more than ninety
days ago — an unread one stays ([ADR 0020](../adr/0020-notifications-are-an-in-app-inbox-per-person.md)
D6) —, and purges the tickets deleted more than thirty days ago, up to 200 a run, with their
attachments' objects once the purge committed
([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2); each job holds a transaction-level
advisory lock of its own that lets one replica at a time do it, and the log says
`job removed expired rows` with the job and the count when there were any — the purge says
`ticket purged` with each key first. A session past a
limit is refused at its next request whether or not the job has run; the job only keeps the
table small. A purge is irreversible; an installation that must keep a deleted ticket longer has
no setting for it yet.

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
frontend still serves the UI, and the Ingress controller answers `/api/` and `/auth/` itself, with
a page of its own ([what answers what](#what-answers-what)). The object storage and the identity
provider are not probed: the provider is asked at start and at a login or a refresh, never by a
probe.

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
| `token refused` | info | a presented token was expired, revoked, or — its person one of the identity provider's — outside the provider's gate, of another issuer than the configured one, or judged by groups older than `COWORK_OIDC_GROUPS_MAX_AGE` (`not_allowed`); the line names the token id and the reason |
| the identity provider's lines | info, warn, error | discovery at start, failed logins, the groups refresh, the token gate ([below](#the-login-through-the-identity-provider)) |
| `client addresses are read through trusted proxies` | info | at start, when `COWORK_TRUSTED_PROXIES` is set; the line lists the networks as parsed |
| `the local administrator is created`, `… is in step with the configuration`, `… is deactivated: the configuration no longer names it`, `the bootstrap tenant is created` | info | the start's bootstrap changed something; the line names the username or the slug, never the password. Nothing is logged when nothing changed |
| `a stored password hash cannot be verified` | error | an account's hash is damaged or foreign; the login answers its person like a wrong password, and the line carries the request id |
| `job removed expired rows`, `job failed` | info, error | the hourly jobs ([above](#the-backend)) |
| `ticket purged` | info | the purge job removed a ticket deleted thirty days ago; the line names its key and how many attachments it had |
| `an attachment object of a purged ticket could not be removed`, `a purged ticket had attachments, and no object storage is configured to remove them from` | error, warn | a purge — the job's or an administrator's — committed and an object stays in the bucket that no row names; the line names the ticket and the object key, which the operator may remove by hand |
| `the chat's provider failed`, `a turn of the chat failed` | warn, error | a turn of the chat ended on its provider — the kind, the status and a clip of the provider's message without the key — or on anything else ([the chat's stream](#the-chats-stream)) |
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
refuses to start without one while the local administrator or an identity provider is configured
([CSRF](../security/csrf.md)). Tokens are not affected: a script's token request carries no
cookie and no check.

**The browser keeps no session** — the login answers `200` and the next request is `401`: the
cookie is `Secure` and has the `__Host-` prefix, so a browser stores it only over HTTPS (or on
`localhost`). A page reached over plain `http://` on another host cannot log in; terminate TLS
at the Ingress.

**Sessions** live in the database: a restart of every pod ends none of them. A changed server key
ends none of the local login's, and each session of the identity provider that holds a refresh
token at its next refresh, whose sealed token no longer opens — no previous key is kept to open it,
so each such person logs in again once their session's refresh is due; what else a change of the key
does is in [installation.md](installation.md#the-secrets). The absolute lifetime is
`COWORK_SESSION_LIFETIME` (12 hours), the idle limit `COWORK_SESSION_IDLE` (2 hours); a request
moves the idle clock at most once a minute. An administrator ends a local account's sessions with
`DELETE …/accounts/{username}/sessions`; a person's other sessions end when they change their
password. A person of the identity provider has neither: their sessions end at the limits, or at a
refresh when the issuer refuses the refresh token or the gate no longer admits them
([below](#the-login-through-the-identity-provider)).

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
Ingress is the controller pod's**, so the limit is one for every browser behind it and one client's
failing logins can use it up for everybody; a list that is too wide, or one that names a pod
network other pods can reach the backend from, lets a client choose its address
([H-17](../security/local-accounts.md#h-17);
[installation.md](installation.md#the-client-address-and-the-trusted-proxies) says what to set).
`COWORK_LOGIN_ADDRESS_LIMIT=0` and `COWORK_LOGIN_MAX_FAILURES=0` switch the throttle and the
lockout off. The failures, the locks and the unlocks are audit rows of the system actor
`system:login` — installation-level, readable in the database; no route shows them yet.

**The request log** carries the login like any request — method, path, status, duration, the
request id — and never the username, the password or the cookie.

## The login through the identity provider

Behaviour an operator meets with `COWORK_OIDC_ISSUER` set. The mechanism and what it leaves open
are [identity-provider.md](../security/identity-provider.md); setting it up is
[installation.md](installation.md#the-identity-provider).

**A login that fails lands on the login page**, `/login?error=<code>`, which says why in a sentence
of its own; the reason is in the backend's log, never on the page:

| Code | Means | Where to look |
|---|---|---|
| `oidc_unavailable` | no provider is configured, or its gate names no group | `COWORK_OIDC_ALLOWED_GROUPS`, `COWORK_ADMIN_GROUP`; the start's warning |
| `oidc_failed` | the login could not complete: the state cookie was missing or older than ten minutes, the issuer answered with an error, the code was refused, an answer of the issuer redirected or exceeded 1 MiB, the ID token did not verify, the groups claim had a shape that is no list of names, or the login could not be stored | `a login through the identity provider did not succeed` (info) with the request id, the code, the reason and the error — for the token endpoint its status and OAuth error code, never the answer's body; `a login through the identity provider failed` (error) when storing it failed |
| `not_allowed` | the person is outside the gate — none of their groups is allowed or the administrator group — or deactivated | the person's groups at the issuer, the claim's name (`COWORK_OIDC_GROUPS_CLAIM`), the installation-level `login_refused` row, whose note says which |
| `not_initialised` | no tenant exists, and the person is not in the administrator group | the first tenant: a global administrator creates it, or `bootstrap.tenant` with `auth.oidc.adminGroup` |

Common causes of `oidc_failed`: the browser spent more than ten minutes at the issuer; a second
login started in the same browser replaced the first one's state cookie; the browser dropped the
state cookie, which is `Secure` like the session cookie, so a page reached over plain `http://` that
is not `localhost` loses it; the replicas hold different server keys, so one cannot open what
another sealed. A redirect URI the issuer has not registered stops the login at the issuer, on its
own error page, before cowork sees anything.

**A person who belongs nowhere.** A login that passes the gate makes the person, but gives them a
membership only where a tenant maps one of their groups; a person in no mapped group logs in to an
empty start page until an administrator adds them by the address the issuer sends
(`POST …/members`).

**The groups refresh.** Every `COWORK_OIDC_GROUPS_REFRESH` (15 minutes) one request of a provider
session claims the refresh and asks the issuer for the person's groups again with the session's
refresh token — that request waits, twenty seconds at most for all the calls, while the session's
other requests are served on the groups it holds — and an open event stream does the same at its
heartbeat. No database connection is held while the issuer is asked. What shows:

| Log line | Level | Means |
|---|---|---|
| `session groups refresh`, with `ended` and `reason` | info | a refresh ended the session — the gate no longer admits the person (`gate`), or the issuer refused (`identity-provider`) — or could not reach the issuer (`the issuer could not be reached`), or found another request holding the refresh or the session gone; the line names the person's id, never a token |
| `the issuer refused a session's refresh` | info | an OAuth error answer about the person — a spent, revoked or expired refresh token, a refused grant — or a refreshed ID token that did not verify or named another subject; that session ended, and its person logs in again |
| `the issuer refuses cowork's client; check COWORK_OIDC_CLIENT_ID and COWORK_OIDC_CLIENT_SECRET` | error | `invalid_client` or `unauthorized_client`: the client is not what the issuer has — a secret rotated at the issuer and not in the Secret, say. Sessions are served on the groups they hold and retried every minute, and no refresh reads groups until it is fixed ([H-24](../security/identity-provider.md#h-24)) |
| `the issuer could not refresh a session's groups; serving it and trying again later` | warn | no answer, a timeout, a `5xx`, a `429`, a temporary OAuth error, keys the issuer could not serve: the session is served on the groups it holds and tries again a minute later ([H-24](../security/identity-provider.md#h-24)) |
| `a session's refresh token does not open; the session ends` | warn | the server key changed since the session's login |
| `a session of a person the configured issuer does not name ended` | info | a person of another issuer, or of a provider no longer configured, used a session: every session of theirs ended |
| `the issuer gave no refresh token: …` | warn, once per process | the scopes lack `offline_access`, or the issuer gives none: sessions never read the groups anew ([H-25](../security/identity-provider.md#h-25)) |
| `the issuer's refresh carries no groups claim, …` | warn, once per process | a refresh reads no groups: the same ([H-25](../security/identity-provider.md#h-25)) |
| `the session's groups refresh failed`, `the token gate failed` | error | the database failed during a refresh or a token's gate check; the request answered `500 internal` |

**Tokens of the provider's persons.** A token whose person the gate no longer admits — judged on the
person's groups as of their last login or refresh, every 15 minutes, and at once when the person is
not the configured issuer's — answers `401 not_allowed` and is logged as `token refused` with that
reason; it is not revoked and works again once the person is admitted. So does a token whose
person's groups were last read longer ago than `COWORK_OIDC_GROUPS_MAX_AGE` (a week by default), with
a detail that says to sign in to the browser once: a person who uses tokens only must sign in that
often, and is judged on groups up to that old meanwhile
([H-23](../security/identity-provider.md#h-23)).

**While the issuer is down**, running pods go on: a session whose refresh is due is served on its
groups and retried every minute — the one request that asks waits up to twenty seconds, the others
are served at once — and a new login through the provider fails (`oidc_failed`). A pod that starts
during the outage does not start at all, its discovery failing. The local administrator and the
local accounts log in meanwhile, on the pods that run.

**Logout** sends the browser on to the issuer's end-session endpoint where its discovery names one
that passed the endpoint rule. Dex names none, so a person who logs out of cowork stays logged in at
Dex ([H-28](../security/identity-provider.md#h-28)).

**The record.** A refused login is an installation-level `login_refused` row of
`system:identity-provider`; so are the persons it makes and changes and the sessions it ends. The
memberships it derives are rows of their tenants, in the tenant's audit view; the installation-level
rows are readable in the database only.

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
| `COWORK_REQUEST_TIMEOUT` | `30s` | the handler's context is cancelled, `504 timeout`; the event stream is exempt, and a turn of the chat after its body is read | no limit |
| `COWORK_MAX_PAGE_SIZE` | `200` | a larger `limit` is clamped, not refused (without `limit` a page has 50) | no clamp |
| `COWORK_MAX_QUERY_LENGTH` | `256` characters | a longer full-text `q` — of a ticket list or of a search — is `400 validation_failed` | no limit of its own; the API document still caps `q` at 4096 characters |
| `COWORK_SSE_MAX_STREAMS_PER_PERSON` | `10`, per replica | the next stream closes the person's oldest with `event: unavailable` | no limit |
| `COWORK_SSE_REPLAY_WINDOW` | `5m` | a reconnect beyond the window starts with `event: resync` | no replay: a reconnect that missed anything starts with `resync` |
| `COWORK_CHAT_TURN_TIMEOUT` | `5m` | a turn of the chat ends with the `error` event `timeout` | no limit |
| `COWORK_CHAT_MAX_STEPS` | `8` | the turn ends after that many calls of the model; a new message goes on | no limit |
| `COWORK_CHAT_TURNS_PER_PERSON` | `2`, per replica | one more turn of the same person is `429 chat_busy` | no limit |

Two bounds are fixed: a numbered page that would end past row 10 000 (`page` × `per_page`)
is `400 page_too_deep`, and the uploads the backend holds in memory at once are bounded by a
64 MiB budget ([installation.md, resources](installation.md#resources-and-scheduling)).

**`0` belongs in no production values file.** A disabled body limit lets one request hold
unbounded memory; a disabled timeout lets one slow request hold its connection and its
database transaction for as long as it runs; a disabled stream limit lets one person hold any
number of streams, each with a buffer of its own. Nothing warns when a limit is `0` — not the
log, not the chart, whose notes then ask the Ingress controller for no body limit and an hour's
read timeout in step. The chart's own defaults set none.

## What answers what

The Ingress routes `/api/` and `/auth/` to the backend and everything else to the frontend
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3), so an answer comes from one of three places:

| From | When | Body |
|---|---|---|
| the backend | everything that reaches it — its own `413` above `COWORK_MAX_JSON_BODY` or the upload limit, its `503 not_ready`, its `504 timeout` included | a problem with `instance` and `request_id`, and `X-Request-Id` |
| the Ingress controller | what never reaches the backend: no backend pod ready (`502` or `503`), a body above the controller's limit (`413`), no answer within its read timeout (`504`) | the controller's own page, not a problem body — ingress-nginx's, in the run of [installation.md](installation.md#expose-it), is HTML. The UI shows a `502`, `503` or `504` without a problem body as the backend out of reach — "The backend cannot be reached: The Ingress answered 503: no backend took the request. cowork tries again on its own." — and another status without one as an unexpected answer that names it |
| the frontend's nginx | a request for `/api/` or `/auth/` that reaches the frontend — an Ingress that sends every path there | `404` with a static problem, `not_found`, whose detail is "the frontend serves the UI only; the Ingress must route /api/ and /auth/ to the backend Service", without `instance` and `request_id` — a body above nginx's own 1 MiB limit gets the same |

So a request over a backend limit gets the backend's answer only while the controller's limits sit
above the backend's: a body size of at least the larger of `maxJsonBody` and `attachmentMaxBytes`,
rounded up to whole MiB, plus 1 MiB (`11m` with the defaults), and a read timeout of at least
`requestTimeout` plus ten seconds (`40` seconds). The chart sets neither — it does not know the
controller — and its notes print both; [installation.md, expose it](installation.md#expose-it)
has what any controller must do, a worked example for a cluster that still runs the retired
ingress-nginx, and what was verified with it. The absence of `request_id`
and of the `X-Request-Id` header tells a client that the answer did not come from the backend.

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

A change of who belongs to the tenant or who sees a project is `membership.changed`, with the
tenant's slug and the ids of what changed — `tenant`, `person_id`, `project_id`, `mapping_id` — and
reaches every member, the
administrators only, or the administrators and the person it names, by what it is
([tenancy.md](../security/tenancy.md#the-event-stream-carries-what-its-subscriber-could-read)).
A project's rank sorted by the score is `project.changed`, with the project's key and the kind and
no version, and reaches whoever sees the project.

Opened with `?me=true` — as the browser always opens it — it is the person-level stream: it carries
the events of every tenant the person belongs to, each as that tenant's own stream would judge it,
over one connection whatever the number of tenants, and `inbox.changed` with `data: {"unread": n}`,
the person's unread notifications in every tenant, when it opens and once a burst of changes of the
inbox is over (a tenth of a second). A reconnect replays the gap of every tenant it follows; the
count carries no `id:` and is not replayed, and the browser reloads its person-level pages when the
stream opens. Every tenant it follows costs one database transaction when it opens, at every
heartbeat and on every act that changes what the person may see there
([tenancy.md](../security/tenancy.md#the-person-level-stream)).

How it behaves, as somebody running it sees it:

- **Every replica hears every change.** Each backend replica holds one database connection
  that listens on `cowork_events`; a change is published when its transaction commits, never
  when it rolls back. A client can reconnect to any replica.
- **A heartbeat every 20 seconds** (`: heartbeat`, an SSE comment) keeps proxies from closing
  a quiet stream and checks the token and the membership again; the stream ends when either
  is gone.
- **A change of who sees what reaches the open streams at once.** A project created, a membership,
  a mapping, a restriction or an access entry changed makes every open stream of the tenant read
  the person's role and the projects they see again — two small queries per stream, once or twice for a
  burst of such changes — before it passes its next event; the heartbeat repeats it for a change
  made in the database past the API.
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
  `X-Accel-Buffering: no`. The Ingress routes the stream to the backend like any `/api/` path; the
  frontend's nginx never sees it.

### Behind an Ingress

The Ingress controller is the one proxy in front of the backend, and it has to pass the streams
through unbuffered and keep them open — the event stream, and the stream of a turn of the chat
([below](#the-chats-stream)) — and has to let uploads through. Both streams come with
`X-Accel-Buffering: no`, so a controller that honours that header — nginx does — buffers neither,
and one that does not must be told not to buffer `text/event-stream`. Both send something at least
every twenty seconds, so any read timeout above that keeps them open. The body limit and the read
timeout the backend's own answers need are in [installation.md, expose it](installation.md#expose-it),
with a worked example for a cluster that still runs ingress-nginx, which is retired and not for a
new installation. Verified on 2026-10-04 with ingress-nginx v1.15.1 in a kind cluster: the stream
held open past a read timeout of 60 s and of 40 s, and unbuffered with `proxy-buffering: "on"`
forced. **Streams that die every minute** — or events that arrive late and in bursts — are the
symptom of a proxy in front that buffers the response or cuts it at a read timeout below twenty
seconds: a controller that does not honour `X-Accel-Buffering`, or a load balancer before it,
needs its own way to pass `text/event-stream` unbuffered.

## The chat's stream

`POST /api/v1/tenants/{tenant}/chat` runs one turn of the chat in the UI and answers it as
server-sent events ([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md);
setting the chat up is [chat.md](chat.md)). What an operator meets:

- **Refusals before the stream** are problem answers like any: `409 chat_unavailable` where the
  installation configures no provider, `429 chat_busy` past `COWORK_CHAT_TURNS_PER_PERSON`,
  `403 session_required` for a token, `403 csrf`, `400 validation_failed` for a conversation the
  model could not read in its place, one past its bounds or a provider the installation does not
  list, `413` past `COWORK_MAX_JSON_BODY`.
- **Once the stream has begun** the answer is `200` with `Content-Type: text/event-stream` and
  `X-Accel-Buffering: no`, every event flushed as it is written; a failure is the `error` event, a
  problem body with the request id — `chat_provider_failed`, `timeout`, `not_ready`, `internal` —
  and the turn ends with `done` in every case it ends by itself; a turn the person stopped
  (`DELETE …/chat/turns`, [chat.md, Stop](chat.md#stop)) ends with `done` and the reason `stopped`,
  and no `error` event.
- **A comment, `: keep-alive`, after ten seconds without an event**, so no proxy closes a turn while
  the model thinks. The Ingress routes the turn to the backend like any `/api/` path: a controller
  that honours the backend's `X-Accel-Buffering: no` passes it unbuffered, and the comments keep it
  inside any read timeout above ten seconds ([above](#behind-an-ingress)). Not run through a
  controller: the event stream was, with the same header.
- **The turn is exempt from `COWORK_REQUEST_TIMEOUT`**, which bounds reading its body; it is bounded
  by `COWORK_CHAT_TURN_TIMEOUT` and `COWORK_CHAT_MAX_STEPS` ([limits](#limits)).
- **Each tool call is a request of its own** through the whole server, so the request log has a line
  per call besides the turn's own line, which is written when the turn ends, with its whole duration.
- **A turn is never repeated by the client**: a repeated turn would repeat its acts. A stream that
  ends without `done` was cut — the network, a proxy, a shutdown — and the acts its results reported
  have happened.

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

The image is `nginxinc/nginx-unprivileged` with the Angular bundle and one configuration file,
[`frontend/nginx/default.conf`](../../frontend/nginx/default.conf), copied into
`/etc/nginx/conf.d/default.conf` at build time. Nothing in it is substituted at start and the image
reads no variable of cowork's: the frontend serves the UI and reaches no backend
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3). nginx listens on 8080 as user 101, and the pod starts and becomes ready whether or not the
backend exists. `frontend.extraEnv` exists for the entrypoint's own switches
(`NGINX_ENTRYPOINT_QUIET_LOGS`, for instance).

What nginx does with a request:

| Path | Behaviour |
|---|---|
| `/healthz` | `{"status":"ok"}` from nginx itself — the frontend's liveness and readiness probes; it says nothing about the backend |
| `/api/…`, `/auth/…` | the Ingress routes these to the backend, so they reach the frontend only by mistake — an Ingress that sends every path here, a port-forward to the frontend. nginx then answers `404` with a static problem whose detail names the cause ([what answers what](#what-answers-what)), a path ending in `.png` and a body above its 1 MiB limit alike; the UI shows that detail on its page |
| hashed bundles (`*.js`, `*.css`, fonts, images) | served with `Cache-Control: public, max-age=31536000, immutable` and the shell's `Content-Security-Policy` |
| `/favicon.ico`, `/favicon.svg`, `/apple-touch-icon.png` | served with `Cache-Control: no-cache` and the shell's `Content-Security-Policy` |
| everything else | `index.html` with `Cache-Control: no-store` and the shell's `Content-Security-Policy` — the Angular router resolves the path |

The policy keeps every script, style sheet, font, image and request of the UI on its own origin and
runs no inline script ([trust-boundaries.md](../security/trust-boundaries.md#the-shells-content-security-policy)).
A page that broke under it shows a violation in the browser's console, never in nginx's log.

The pod runs with a read-only root filesystem; the chart mounts an `emptyDir` at `/tmp`, which is
all nginx writes — its pid and temporary files —, and `fsGroup: 101` is what makes it writable for
the nginx user. The configuration is part of the image: a volume mounted over `/etc/nginx/conf.d`
hides it, and nginx then starts with no server and answers nothing on 8080 — the readiness probe
never passes, which is the symptom to look for. The chart's mounts were run in a kind cluster,
the volume over `/etc/nginx/conf.d` against the built image. Its log is nginx's access log on
stdout — time, method,
path without its query, status, size, duration — and its error log on stderr
([trust-boundaries.md H-14](../security/trust-boundaries.md#h-14)). On `SIGTERM` the image's nginx
exits within its grace period; there is no draining beyond nginx's own.
