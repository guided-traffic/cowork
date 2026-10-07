# Installation

cowork is installed with the Helm chart in [`deploy/helm/cowork/`](../../deploy/helm/cowork/).
A release is two Deployments — the backend (the API; its pods migrate the schema in an init
container) and the frontend (nginx with the Angular bundle, and nothing else) — and, when enabled,
an Ingress that routes `/api/` and `/auth/` to the backend and everything else to the frontend
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3).
The chart brings neither the database nor the object storage
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D8,
[ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D1):
the installation provides them, and the chart takes references to them.

**Each release publishes the chart and both images with one version**
([`.github/workflows/build.yml`](../../.github/workflows/build.yml)): the chart to the Helm
repository `https://guided-traffic.github.io/cowork/`, the images to Docker Hub as
`guidedtraffic/cowork-backend` and `guidedtraffic/cowork-frontend`. The chart's image tags
default to its `appVersion`, so a chart version brings its own images. The first release is
`0.1.0`. Installing from the checked-out tree, with images you built with `make docker-build`,
works the same way with `deploy/helm/cowork` and the image values set. The values reference is
[README.md, Helm chart values](../../README.md#helm-chart-values).

## What the installation provides

| Thing | Needed | How the chart takes it |
|---|---|---|
| A PostgreSQL 18 (or newer) database with two roles | yes | the runtime role's connection and the owner role's, each from a Secret — a URL, or its components with the location from a ConfigMap if you like ([the Secrets](#the-secrets)) |
| The server key | yes | a Secret; there is no inline value |
| A local administrator, an identity provider, or both, and the public URL | yes, to log in at all — without one of the two nobody can | the administrator's username and password from a Secret ([below](#the-local-administrator)); the provider's issuer and client id as values, its client secret from a Secret ([below](#the-identity-provider)); `backend.config.baseURL` for either |
| An S3-compatible bucket with an access key scoped to it | no — without it uploads are refused | endpoint and bucket as values or from a ConfigMap, the key from a Secret, a private authority from a ConfigMap |

[Example manifests](#example-manifests) for CloudNativePG and for MinIO show one way to provide the
database and the bucket; they are checked for syntax only.
| A model the chat in the UI talks to | no — without it there is no chat | the provider, its URL and the model as values, its API key from a Secret ([below](#the-chat)) |
| A Prometheus — with the Prometheus Operator's CRDs for the monitors and the alerts — and a Grafana with the dashboard sidecar | no — the metrics port is on either way, until `metrics.enabled: false` | `metrics.*` switches each resource on; nothing of it is a credential ([metrics.md](metrics.md)) |

Rendering fails, naming the missing value, without a database connection, without an owner
connection while the release migrates — `backend.config.migrateOnStart` `true` (the default), or
`migrations.mode: job` —, with `migrations.mode: job` and an inline `database.url`,
`database.owner.url` or local administrator ([job mode](#job-mode)), with a `migrations.mode` other
than `onStart` and `job`, with a database role's `keys.url` empty and no key for its host, name,
user or password, with an `existingConfigMap` beside a URL, without `session.existingSecret`, with
a `storage.endpoint` or a `storage.existingConfigMap` but no bucket or no `storage.existingSecret`, with a local
administrator or an `auth.oidc.issuer` but no `backend.config.baseURL`, with an issuer but no
client id or no client secret, with a group in `auth.oidc.allowedGroups` that holds a comma, with
a chat provider in `chat.providers` whose id is not one or repeats, whose kind is neither `openai` nor
`anthropic`, without `url` or `model`, of kind `anthropic` without `existingSecret`, or with an
inline `apiKey`,
and with half of what belongs together —
`localAdmin.username` without `.password`, `bootstrap.tenant.slug` without `.name`, a bootstrap
tenant with neither a local administrator nor `auth.oidc.adminGroup`.

## The database and its two roles

The schema belongs to one role and the requests run as another
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2).
The tables carry forced row-level security; a role that owns them could switch it off and
grant itself back what was revoked, so the role that serves owns nothing. Any PostgreSQL 18 or
newer will do; the operator creates the database and both roles, cowork creates no role.

| Role | Used by | Requirements |
|---|---|---|
| owner (`cowork_owner` `# example`) | the migrations only: the chart's `migrate` init container or migration Job, `cowork migrate` | owns every object it creates: it owns the database, or holds `CREATE` on schema `public` (and `CREATE` on the database, unless the extensions exist already — below) |
| runtime (`cowork_app` `# example`) | `cowork serve`, every request | `LOGIN`; not a superuser, no `BYPASSRLS`, owns nothing in the schema, is not a member of the owner role. The migrations grant it what it needs, table by table |

As an administrative role:

```sql
-- every name and password is an example
CREATE ROLE cowork_owner LOGIN PASSWORD 'CHANGE-ME';
CREATE ROLE cowork_app LOGIN PASSWORD 'CHANGE-ME' NOSUPERUSER NOBYPASSRLS;
CREATE DATABASE cowork OWNER cowork_owner;
REVOKE CONNECT ON DATABASE cowork FROM PUBLIC;
GRANT CONNECT ON DATABASE cowork TO cowork_owner, cowork_app;
```

**The owner role.** Owning the database is the simple case: since PostgreSQL 15, by default
only the database's owner may create objects in schema `public`, and the owner may create the three
extensions the schema needs — `unaccent`, `pg_trgm` and `btree_gin`, created by the eighth
migration with `CREATE EXTENSION IF NOT EXISTS`. An owner role that does not own the database
needs `GRANT CREATE ON DATABASE cowork TO cowork_owner` (for the extensions) and, connected to
the database, `GRANT CREATE ON SCHEMA public TO cowork_owner`. If the installation creates the
extensions beforehand (`CREATE EXTENSION unaccent;` and the same for `pg_trgm` and
`btree_gin`, in the cowork database), `CREATE` on schema `public` is enough. All three
arrangements were run against `postgres:18` with a non-superuser owner. The symptoms of a
missing privilege in the migration log:

- `permission denied for schema public` — the owner may not create in `public`; the run
  stops before any file is applied.
- `permission denied to create extension "unaccent"` — the eighth migration fails and leaves
  schema version 8 *dirty*; the repair is in
  [runtime.md, the migration run](runtime.md#the-migration-run).

**The runtime role** is checked, not trusted. `cowork migrate` checks it before and after the
run, `cowork serve` before it listens, and both exit 1 with
`refusing a runtime role that could bypass row-level security (docs/adr/0021 D2): …` and the
reasons — `is a superuser`, `has BYPASSRLS`, `owns relations of the schema`,
`is a member of the owner role`. Two more refusals: one role for both URLs
(`the runtime role "…" is the owner role: migrations need a separate owner role`), and a
runtime role that does not exist (`the runtime role "…" does not exist; the operator creates it`).

**`REVOKE CONNECT … FROM PUBLIC`.** Every committed change to a ticket is published on the
PostgreSQL channel `cowork_events` for the event stream
([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md) D4).
PostgreSQL lets any role that can connect to a database `LISTEN` on any of its channels — no
grant and no row-level security applies. A role with no privilege at all, connected to the
cowork database, received every payload: the act's id and kind, the tenant's and the
project's ids, the ticket key and version, the confidential flag, the assignee's and the
reporter's ids — never a title or a body. PostgreSQL grants `CONNECT` to `PUBLIC` by default,
so on a server that other applications share, every one of their roles can read along.
Revoking it and granting `CONNECT` to the two roles leaves the channel to them and to
superusers; both roles kept working with it in place. cowork does not check this at start.
The gap, and what stays open after the revocation, is
[H-4 in the tenancy security page](../security/tenancy.md#h-4).

**CloudNativePG.** Its `-app` Secret carries the credentials of the role that owns the database it
bootstraps — the owner role — under the keys `username`, `user`, `password`, `host`, `port`,
`dbname` and `uri` among others (CloudNativePG 1.30.1's source); its `uri` names no `sslmode`. So
`database.owner.existingSecret=<cluster>-app` with `database.owner.keys.url=uri` reads its URL, and
with `database.owner.keys.url` empty its components, the chart's default key names being
CloudNativePG's. The runtime role is not part of that bootstrap:
[`deploy/examples/cloudnative-pg-cluster.yaml`](../../deploy/examples/cloudnative-pg-cluster.yaml)
creates it and keeps its password from a `kubernetes.io/basic-auth` Secret, and reads both roles'
location from a ConfigMap of its own, which is how `sslmode` gets set. CloudNativePG serves TLS
with a certificate of its own authority; the chart mounts no authority for the database, so
`require` — encrypted, the server not verified — is the strongest mode it can use with it. Not
verified against a CloudNativePG cluster in this repository: the example is checked for syntax
([example manifests](#example-manifests)).

## The Secrets

Create them before the release; every key name is configurable.

```bash
kubectl create namespace cowork
kubectl -n cowork create secret generic cowork-database \
  --from-literal=databaseUrl='postgres://cowork_app:CHANGE-ME@postgres.cowork.svc:5432/cowork?sslmode=require'
kubectl -n cowork create secret generic cowork-database-owner \
  --from-literal=databaseUrl='postgres://cowork_owner:CHANGE-ME@postgres.cowork.svc:5432/cowork?sslmode=require'
kubectl -n cowork create secret generic cowork-session \
  --from-literal=sessionKey="$(openssl rand -base64 32)"
kubectl -n cowork create secret generic cowork-local-admin \
  --from-literal=username=admin --from-literal=password="$(openssl rand -base64 24)"
kubectl -n cowork create secret generic cowork-oidc \
  --from-literal=clientSecret='CHANGE-ME'     # with an identity provider: the secret it issued for cowork's client
kubectl -n cowork create secret generic cowork-chat \
  --from-literal=apiKey='CHANGE-ME'           # with a chat provider that takes a key: its API key
```

| Secret | Values naming it | Key `# default` | Read by |
|---|---|---|---|
| the runtime role's connection | `database.existingSecret` | `database.keys.url`: `databaseUrl` — or, with it empty, the components below | the backend container; the migration run — the `migrate` init container or the migration Job — for the role's name, and the Job for the bootstrap |
| the owner role's connection | `database.owner.existingSecret` | `database.owner.keys.url`: `databaseUrl` — or the components | the migration run, nothing else |
| the server key | `session.existingSecret` | `session.keys.key`: `sessionKey` | the backend container |
| the local administrator | `localAdmin.existingSecret` | `localAdmin.keys.username`: `username`, `localAdmin.keys.password`: `password` | the backend container; the account follows it at every start |
| the identity provider's client | `auth.oidc.existingSecret` | `auth.oidc.keys.clientSecret`: `clientSecret`; `auth.oidc.keys.clientId`: empty — set, it reads the client id from the Secret too, in place of `auth.oidc.clientId` | the backend container |
| the storage access key | `storage.existingSecret` | `storage.keys.accessKeyId`: `accessKeyId`, `storage.keys.secretAccessKey`: `secretAccessKey` | the backend container |
| a chat provider's API key, one Secret per provider | `chat.providers[].existingSecret` | that entry's `keys.apiKey`: `apiKey` | the backend container, which sends it to that provider |

**A database role as components.** A Secret that holds no URL — what an operator or a platform
team hands out — is read key by key: set the role's `keys.url` to empty, and the backend composes
the URL from `COWORK_DATABASE_HOST`, `_PORT`, `_NAME`, `_USER`, `_PASSWORD` and `_SSLMODE` (the
owner's under `COWORK_DATABASE_OWNER_`), escaping the password
([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D4).
The user and the password always come from the Secret; the location — host, port, name, sslmode —
from `existingConfigMap` when you name one, from the Secret otherwise. Every key name is a value:

```yaml
database:
  existingSecret: cowork-db-runtime        # example: a kubernetes.io/basic-auth Secret
  existingConfigMap: cowork-db-location    # example: optional, the location
  keys:
    url: ""                                # empty: the components, whatever existingSecretKey says
    host: host                             # default
    port: port                             # default; empty: not read, the port is 5432
    name: dbname                           # default
    user: username                         # default
    password: password                     # default
    sslmode: sslmode                       # example; default empty: not read, the driver's default prefer
  owner:                                   # the same keys, read by the migration run only
    existingSecret: cowork-db-app          # example: CloudNativePG's Secret of the owner
    existingConfigMap: cowork-db-location  # example
    keys:
      url: ""
      sslmode: sslmode                     # example
```

A key that is named and missing from its Secret or ConfigMap stops the pod with
`CreateContainerConfigError`, as a missing Secret does — never a silent fallback. Name
`sslmode` (`require` or stronger) for a database outside the pod network. `existingSecretKey`, the
URL's key under its earlier name, is still read and wins over `keys.url` while it names a key.

**The server key** is standard base64 of at least 32 random bytes; `openssl rand -base64 32`
makes one. It signs the list cursors, keys the hashes of a client's address — the login
throttle's and the audit rows' — and the fingerprints of idempotent requests, and seals the identity
provider's login state and refresh tokens, so every replica must hold the same key — they read the
same Secret. The chart has no inline path for it.

**Rotating the server key.** Write the new key into the Secret and restart the backend
(`kubectl -n cowork rollout restart deploy/cowork-backend`); every replica must have the new key
before it serves. cowork keeps one key and no previous one to open what the old key sealed
([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D1), so the change does this,
once:

- **Each session of the identity provider that holds a refresh token ends** at its next groups
  refresh, its sealed token no longer opening — `revoked` with the cause `identity-provider`, and
  the warning `a session's refresh token does not open; the session ends` — and its person signs in
  again. The change fails closed. A session of an issuer that gave no refresh token goes on.
- **A login through the provider under way** at the moment fails with `oidc_failed`: its state
  cookie was sealed under the old key. The person starts it again.
- **The list cursors clients hold** stop working: the next page they ask for is
  `400 invalid_cursor`, and they start the list over.
- **The login throttle's count of an address starts over**: the address is hashed under a key
  derived from the server key. The lockout of a username is kept by the username and stays.
- **The audit rows' source hashes** before and after the change cannot be compared: one address
  gets another hash.
- **An idempotent request retried across the change** — its key stored before it, the retry within
  the key's twenty-four hours — no longer matches its stored fingerprint, an HMAC under a key derived
  from the server key, and is refused with `422 idempotency_mismatch`, neither replayed nor run a
  second time. A client that retries a creation across a rotation reads the list to see whether it
  happened.
- **The sessions of the local login survive**, and so does every personal access token: a session
  row and a token are found by the SHA-256 of their value, which no key enters.

Whoever holds the old key and a copy of the database taken before the change can still open the
refresh tokens sealed in that copy
([identity-provider.md](../security/identity-provider.md#h-27) H-27;
[trust-boundaries.md](../security/trust-boundaries.md#where-the-credentials-live)).

**The owner's credential reaches only the migration run** — the `migrate` init container, or in
[job mode](#job-mode) the migration Job. The serving container gets the runtime role's connection
alone and `COWORK_MIGRATE_ON_START=false`. An installation that hands the owner URL to the serving
container — through `backend.extraEnv`, for instance — gives a compromised server process the power
to switch row-level security off.

**The inline values**, `database.url`, `database.owner.url` and `localAdmin.username` with
`localAdmin.password`, render the Secrets `<fullname>-database`, `<fullname>-database-owner` and
`<fullname>-local-admin` for you; the identity provider's client secret and the chat's API key have
no inline value and come from `auth.oidc.existingSecret` and each provider's
`chat.providers[].existingSecret` only
([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D3). Use them for
a throw-away installation only: the values are
stored in plain text in the Helm release Secret and shown by `helm get values`, and the chart
prints a warning in its notes for each. When a Secret reference and its inline value are both
set, the reference wins and the value is ignored. [Job mode](#job-mode) takes none of them.

**A changed Secret reaches the pods when they start again.** The chart restarts them by
itself only for the inline values (a checksum annotation on the pod); after rotating a Secret
you created, run `kubectl -n cowork rollout restart deploy/cowork-backend`.

## The local administrator

The login needs an account to log in with
([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)).
The chart's local administrator is that account, kept in step with a Secret: name the Secret
with `localAdmin.existingSecret` (its keys `username` and `password`, or the names you set in
`localAdmin.keys`), and set `backend.config.baseURL` to the public URL, as the browser shows it
(`https://cowork.example.com`; the backend refuses to start with a local administrator and no
base URL, and the chart refuses to render). A cookie is only stored by a browser over HTTPS —
or on `localhost` — so the URL is the one your TLS-terminating Ingress serves.

```bash
# 0.4.0 is an example: the first release whose Ingress routes the API to the backend, or a later one
helm upgrade --install cowork cowork/cowork --version 0.4.0 -n cowork --reuse-values \
  --set localAdmin.existingSecret=cowork-local-admin \
  --set backend.config.baseURL=https://cowork.example.com \
  --set bootstrap.tenant.slug=acme --set bootstrap.tenant.name="Acme Corp"   # optional
```

What happens at every start of a backend pod, after the migrations and under an advisory lock
(so replicas that start together agree), as the actor `system:bootstrap`:

| The Secret says | The start does |
|---|---|
| a username and password, no such account yet | creates a **global administrator** with that username and password: it creates tenants and holds no role in any until it grants itself one |
| the same, and the password differs from the stored hash | stores the new hash, **ends every session** of the account and forgets its failed logins and its lock |
| the same, and the account was deactivated | reactivates it |
| a username that a tenant's administrator already gave to an account | takes the account over: the configured password, no session, no token, and no tenant manages it any more |
| another username than the account kept before | deactivates the old account — its tokens revoked, its sessions ended — and creates the new one |
| both values empty or the values removed | deactivates the account, revokes its tokens, ends its sessions; the person and what they did stay, nothing is deleted |
| one value empty and the other set | refuses the start, naming the missing variable — never a value |

A start that finds everything as the Secret says changes nothing. The password must have at
least `auth.local.passwordMinLength` characters (12 by default, 8 at the lowest); a shorter one
refuses the start naming `COWORK_LOCAL_ADMIN_PASSWORD`, never the password. In
[job mode](#job-mode) the migration Job runs the same synchronisation after the schema step, before
a pod of the release or upgrade starts, so the account and the first tenant exist once the Job is
done; the pods still run it at their start and find it in step.

**The first tenant.** While no tenant exists, only a global administrator may log in; anyone
else with the right password gets `403 not_initialised` and no session. The local administrator
logs in and creates the first tenant (`POST /api/v1/tenants`) — it becomes its first
administrator by a marked grant — or `bootstrap.tenant.slug` and `.name` have the start create
it and grant the administrator. Once a tenant exists the bootstrap values do nothing, whatever
they say. The administrator of a tenant then creates the accounts of its people
(`POST …/accounts`, [README, API](../../README.md#api-backend)): there is no registration and
no invitation link, and no e-mail, so a forgotten password is an administrator's reset. Creating
an account and resetting a password take a **browser session**: a script with an administrator's
token is `403 session_required` on both, so that a leaked token cannot leave an account or a
password behind it ([local-accounts.md](../security/local-accounts.md)). Listing the accounts,
unlocking one, deactivating one and ending its sessions work with an `admin`-scope token.

**Recovering the local administrator** — its password leaked, or the account is locked
(`COWORK_LOGIN_LOCKOUT=admin`, or an attacker who keeps failing the logins): rotate the Secret
**and restart the backend pods**. The environment is read once, at start, so a changed Secret
does nothing until the pods restart; the start then stores the new password, ends every session
of the account and forgets the lock. A leaked password stays valid until both steps are done.

**What it can and cannot do through the UI.** The local administrator's password changes only
where it comes from: `PUT /api/v1/me/password` is refused for it (`403`, naming
`COWORK_LOCAL_ADMIN_PASSWORD`), because the next start would put the configured password back.
To switch the account off, empty the Secret's values and restart; the account is deactivated — in
every tenant at once, without the check that keeps a tenant's last administrator: a tenant whose
only administrator it is keeps none who can log in, so first grant each of its tenants another
administrator ([H-32](../security/local-accounts.md#h-32)).
Where an identity provider with a second factor does the work, keep it switched off: a local
account has no second factor ([H-16](../security/local-accounts.md#h-16)).

## The identity provider

cowork logs people in through any OpenID Connect provider as a standard relying party: the
authorization code flow with PKCE, the provider's discovery document, the ID token's signature
checked against the provider's published keys, `state` and `nonce` checked
([ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D1). Nothing provider-specific is assumed but the name of the claim that carries the groups. One
installation has one provider (D5); the local administrator can work beside it, or be switched
off ([above](#the-local-administrator)). What cowork checks of the provider, what its groups
decide and what that leaves open is
[docs/security/identity-provider.md](../security/identity-provider.md).

**At the provider**, register a confidential client — one with a client secret; a public client
is not supported — for the authorization code flow, with this redirect URI, exactly as the browser
shows the UI (scheme, host, port):

```
https://cowork.example.com/auth/callback       # example: <backend.config.baseURL>/auth/callback
```

The Ingress of [Expose it](#expose-it) routes `/auth/` to the backend, so its rule carries the
callback. The provider must be reachable from the backend pods: the backend fetches the discovery
document, the keys, the tokens and UserInfo from it. The chart ships no NetworkPolicy; a policy of
the cluster's that restricts the backend's egress must admit the provider.

**In cowork**, the client secret goes into a Secret ([the Secrets](#the-secrets)), the rest into
values:

```yaml
backend:
  config:
    baseURL: https://cowork.example.com             # example; required with an issuer
auth:
  oidc:
    issuer: https://login.example.com/realms/acme    # example; setting it turns the login on
    clientId: cowork                                 # example
    existingSecret: cowork-oidc                      # example; key auth.oidc.keys.clientSecret, default clientSecret
    allowedGroups: [cowork-users]                    # example: the gate
    adminGroup: cowork-admins                        # example: the global administrators
    displayName: Acme SSO                            # example: the login page offers "Sign in with Acme SSO"
    scopes: openid profile email groups offline_access   # default
    groupsClaim: groups                              # default
    groupsRefresh: 15m                               # default
    groupsMaxAge: 168h                               # default: longer than groupsRefresh
    emailTrusted: false                              # default
```

Each value is one `COWORK_OIDC_*` variable (`adminGroup` is `COWORK_ADMIN_GROUP`), rendered only
while `auth.oidc.issuer` is set; the list is
[README.md, Helm chart values](../../README.md#helm-chart-values). The issuer must be written as
the provider names itself in its discovery document (`issuer`), trailing slash and all: every ID
token carries that string and is checked against it.

**A provider that cannot be reached refuses the start.** The backend fetches
`<issuer>/.well-known/openid-configuration` at every start and does not start without it, as with
an invalid setting: the pod exits and is restarted with back-off (ADR 0029 D4). An outage of the
provider leaves the running pods serving and stops every pod that starts during it — a rolling
upgrade, a rescheduled pod. The local administrator is no way around that, because a backend that
does not start serves nobody. To run without the provider, empty `auth.oidc.issuer` and upgrade:
the other `auth.oidc` values may stay, they are rendered only with an issuer, and the local
administrator and the local accounts are then the only way in. Every person of the provider is
outside the gate from then on: their sessions end at their next request and their tokens are
refused, until the same issuer is configured again.

**The discovery must hold to cowork's rules**, or the start is refused the same way: the document
at that URL answers `200` directly — a redirect is not followed — and within 1 MiB; its
authorization, token, keys and UserInfo endpoints are `https`, or `http` on a loopback host as for a
development issuer; and it names a signature algorithm cowork verifies, an asymmetric one, or none,
which means `RS256`. An `end_session_endpoint` over plain `http` on another host is dropped with a
warning, and a logout then ends no session at the provider. The error in the log names the issuer
and the rule it failed.

**The groups claim.** The gate and the group mappings work on group names, read from the claim
`auth.oidc.groupsClaim` — in the ID token, or in the UserInfo answer when the token has none; a
claim that is one string is one group (ADR 0029 D2). A name is matched exactly, case and all, so
it is whatever the provider writes there: a name, a path, an id. Look at a token of your provider
before you write the allow-list. A provider that leaves the claim out — some do for a person in
many groups — leaves that person without groups, and so outside the gate; have the provider send
the groups cowork needs. A group whose name holds a comma cannot be in the allow-list: the comma
separates the groups in `COWORK_OIDC_ALLOWED_GROUPS`, and the chart refuses it.

**The allow-list and the administrator group** are the gate
([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D1): a person logs in through the provider only when their groups include one of
`auth.oidc.allowedGroups` or `auth.oidc.adminGroup`, whatever the tenants map. Both empty admits
nobody (D8): the login page shows no provider button, and the chart's notes warn. The members of
`auth.oidc.adminGroup` are global administrators — they create tenants, each of which they then
administer by a grant, and hold no role in a tenant they were not given: they see every tenant, and
in one without a role its members, mappings and settings, and grant themselves a role there in the
UI — recorded in the tenant's audit, and how a tenant that lost its last administrator gets one
again; no route deletes a tenant yet
([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2) — so whoever may change that group at the provider administers the installation. Which
tenant a person belongs to, and in which role, is not configuration: a global administrator who
administers a tenant maps groups to its roles, and its administrators grant roles to people by hand
and remove mappings, in the UI (ADR 0030 D2, D3, D7). Every tenant shares the provider's groups, so
a tenant's administrator who is not a global administrator makes no mapping and changes none; such a
tenant gets a new mapping once one of its administrators grants a global administrator the `admin`
role there, or a global administrator grants it to themselves.

**The first tenant.** While no tenant exists, only global administrators log in — the local
administrator and the members of `auth.oidc.adminGroup`; anyone else behind the gate is sent back
to the login page as "not initialised"
([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
D5). With `bootstrap.tenant` and `auth.oidc.adminGroup` set, the start creates the tenant and maps
the administrator group to its `admin` role, so the installation needs no local administrator at
all (D6).

**`offline_access`, and why the default asks for it.** A browser session re-reads the person's
groups from the provider every `auth.oidc.groupsRefresh` and applies the gate and the mappings
again (ADR 0030 D5): a person who left the allowed groups is logged out at their next request
after that, and a person who left a mapped group loses what it gave. The refresh needs a refresh
token, and providers commonly issue one only to a client that asks for `offline_access` — the Dex
below does exactly that. Without one, a session never reads the groups anew: it is judged on the
groups of the person's last login until it ends, and the backend logs a warning. While the
provider cannot be reached, a session is served on the groups it has and the refresh is tried
again a minute later; when the provider refuses the refresh token, the session ends. When it
refuses cowork's own client — a client secret rotated at the provider and not yet in the Secret —
the sessions are served as during an outage and the log says so at error level, so put the new
secret into the Secret, and restart, as soon as the provider has it. A personal access token is checked against the groups of its
person's last login or session refresh, not against the provider: removing a person from a group
at the provider reaches their tokens at their next browser login or session refresh, and at the
latest when those groups are older than `auth.oidc.groupsMaxAge` — a week by default: from then on
the person's tokens are `401 not_allowed`, with a detail that says to sign in to the browser once,
until they do ([H-23](../security/identity-provider.md#h-23)). A person who works with tokens only —
an agent's, a script's — signs in to the browser at least that often; a shorter maximum age cuts a
removed person off sooner and asks everyone to sign in more often. Behind the gate,
which tenants a person belongs to follows their groups at every login and refresh as well: a
person in no mapped group logs in to no tenant until an administrator grants them one.

**Granting by e-mail address, and `emailTrusted`.** An administrator grants a role by the address
the provider asserted at the person's last login. By default only an address the provider marked
verified (`email_verified: true`) finds the person. A provider that sends no `email_verified`
claim — Entra is one: it sends none, and its optional `xms_edov` claim, which says whether the
address's domain is verified, is not read by cowork — leaves every address unmarked, so its people
are found by no address and are admitted by a group mapping instead; set `auth.oidc.emailTrusted: true` only when the provider's
addresses are issued by its administrators, not chosen by its users: where a person can choose an
address unverified, they can take a colleague's and be granted the colleague's role
([H-26](../security/identity-provider.md#h-26)). An address the provider marked unverified never
matches, whatever the setting. What the
operator sees of all this is [runtime.md](runtime.md#the-login-through-the-identity-provider).

**Logging out** ends the cowork session. When the provider's discovery document names an
`end_session_endpoint` that passed the rule above, the browser is sent there as well, with
`post_logout_redirect_uri=<backend.config.baseURL>/login` — register that URI at the provider if
it asks for post-logout redirect URIs. Dex names none.

### The reference fixture: a minimal Dex

The login is developed and tested against Dex — the release `DEX_IMAGE` in the
[`Makefile`](../../Makefile) pins — in its minimal configuration,
[`hack/dex/config.yaml`](../../hack/dex/config.yaml): one static client, four static users with
their groups, no connectors, nothing stored beyond the process (ADR 0029 D3). `make dex-up` runs
it on `localhost:5556`, published on the loopback address only (`CONTAINER_BIND`); `make dev` and
the integration tier log in through it. It is the proof
that the flow works with a standard provider, not a recommendation for running one: its client
secret and passwords are public in this repository, and it forgets every token when it stops.
The parts the values above meet:

```yaml
issuer: http://localhost:5556/dex              # = auth.oidc.issuer
oauth2:
  skipApprovalScreen: true                     # no consent page between the login and the redirect
staticClients:
  - id: cowork                                 # = auth.oidc.clientId
    secret: cowork-dev-dex-secret              # = the client secret; development-only
    redirectURIs:
      - https://localhost:4200/auth/callback   # = <backend.config.baseURL>/auth/callback of make dev
staticPasswords:
  - email: ada@example.com
    groups: [cowork-admins, cowork-users]      # what the groups claim carries
```

**Verified on 2026-10-04** against that Dex, v2.45.1, with a script that plays the browser:
discovery; the code flow with PKCE through Dex's login form; an ID token with `email`, `name` and
`groups` (and no `preferred_username`), the same groups in UserInfo; a refresh token only when
`offline_access` is asked for, replaced at every refresh; an unregistered redirect URI refused; no
`end_session_endpoint`. The chart's half — the environment, the Secret reference, the
refusals and the notes — was rendered and linted. The backend's half is what the integration tier
asserts against that Dex and against an issuer in the test's own process, which can be made to do
what Dex cannot: the gate and its four users (`TestLoginThroughDex`), the first-tenant rule
(`TestInitStateThroughDex`), leaving the allow-list (`TestLeavingTheAllowList`), the refresh, its
refusals, an unreachable issuer, a refused client and a refresh that holds no connection while it
waits (`TestRefreshFollowsTheIssuersGroups`, `TestARefusedRefreshTokenEndsTheSession`,
`TestAnUnreachableIssuerLeavesTheSessionServed`, `TestAClientTheIssuerRefusesIsTheConfigurationsError`,
`TestARefreshWaitsForNoOneElse`), a person of another issuer
(`TestAPersonOfAnotherIssuerIsOutsideTheGate`), the logout at an issuer that names an end-session
endpoint (`TestLogoutAtTheIssuer`) and the bootstrap tenant of the administrator group
(`TestBootstrapSeedsTheAdministratorGroupsMapping`); the refusal to start on an issuer that cannot
be discovered, or whose discovery breaks the rules above, is a unit test (`TestDiscoveryFailure`,
`TestDiscoveryHoldsTheIssuerToItsRules`).
**Not verified here:** an issuer other than Dex and the test's own; the start's refusal through the
chart, on a cluster. The login page in a real browser — the local form, a temporary password, the
sign-in through Dex — is walked by the end-to-end tier against the built images
([testing.md](../developer/testing.md#end-to-end-tests)).

## Object storage

Attachments live in any S3-compatible store
([ADR 0016](../adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)),
in a bucket of their own, reached with an access key whose policy covers that bucket only —
never the store's root credentials
([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D5).
The backend writes, reads and deletes objects under `<tenant-id>/<attachment-id>`, and lists the
objects under each tenant's prefix once a day for the consistency check
([backups.md](backups.md#the-consistency-check)); it never creates or deletes a bucket, so the
bucket exists before the first upload. This policy was enough against the MinIO of
`make minio-up`, with the region left empty:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": ["arn:aws:s3:::cowork/*"]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": ["arn:aws:s3:::cowork"]
    }
  ]
}
```

**`s3:ListBucket` is the consistency check's.** Without it every upload and download works and the
check fails at its listing — the log says `job failed` with `consistency-check` and `Access Denied`,
and [`CoworkJobFailing`](metrics.md#coworkjobfailing) fires. With it the key can list every
tenant's object keys, which it could not before: a key that leaks alone then reads every object, not
only those whose keys the database names
([attachments.md H-70](../security/attachments.md#h-70)). Checked on 2026-10-07 against that MinIO
with a key of this policy and with one without its second statement: the listing was refused
without it and answered with it, another bucket's listing was refused either way, and a missing
object's `HEAD` and `GET` answered `404` either way. AWS S3 documents a `403` instead of the `404`
for a missing object to a key without `s3:ListBucket`, which a download of a lost file would then
answer as `500 internal` instead of the `404` that names the cause — not verified here.

With the MinIO client, against an existing MinIO, by its administrator — cowork never sees the
administrator's keys (names and secrets are examples):

```bash
mc alias set minio https://minio.example.com <admin-access-key> <admin-secret-key>
mc mb minio/cowork
mc admin policy create minio cowork-attachments cowork-policy.json   # the policy above
mc admin user add minio cowork-app 'CHANGE-ME'
mc admin policy attach minio cowork-attachments --user cowork-app
kubectl -n cowork create secret generic cowork-storage \
  --from-literal=accessKeyId=cowork-app --from-literal=secretAccessKey='CHANGE-ME'
```

[`deploy/examples/minio-bucket.sh`](../../deploy/examples/minio-bucket.sh) is these commands as a
script, with a generated key; it was run once against the MinIO of `make minio-up`
([example manifests](#example-manifests)).

The values: `storage.existingSecret=cowork-storage`, `storage.endpoint` (`http://` or
`https://`, host and port; setting it turns the storage on), `storage.bucket`,
`storage.region` (empty lets the client ask the server) and `storage.pathStyle` (`true`, as
MinIO expects; `false` for virtual-host addressing).

**From a ConfigMap.** Where the store's location is handed out in a ConfigMap — one the platform
team keeps for every application, say — name it in `storage.existingConfigMap`: each of the four
values is then read from it under its key in `storage.keys` (`endpoint`, `bucket`, `region`,
`pathStyle` `# default`), and a value whose key you set to empty stays the literal value, so a
shared ConfigMap combines with a bucket of your own. Naming the ConfigMap turns the storage on as
the endpoint does. The region and the path style may be missing from it — the backend then asks the
server for the region and addresses by path —, the endpoint and the bucket may not: a missing key
stops the pod with `CreateContainerConfigError`.

```yaml
storage:
  existingSecret: cowork-storage   # example: the bucket-scoped key, still from a Secret
  existingConfigMap: platform-s3   # example
  keys:
    bucket: ""                     # empty: the value below
  bucket: cowork                   # example
```

**A private certificate authority:** put its PEM into a ConfigMap and name it.

```bash
kubectl -n cowork create configmap cowork-s3-ca --from-file=ca.crt=./ca.pem
# values: storage.tls.caConfigMap=cowork-s3-ca (key storage.tls.keys.ca, default ca.crt)
```

The chart mounts it read-only at `/etc/cowork/s3-ca` and points `COWORK_S3_CA` at the key;
the backend trusts that authority in addition to the system's.

**Without storage**, uploads answer `501 uploads_disabled`; lists and metadata of attachments
still answer, and the backend logs `no object storage configured; attachments cannot be
uploaded` once at start. The backend does not contact the storage at start and `/readyz` does
not check it: a wrong endpoint, key or bucket shows on the first upload, as
`500 internal` and a `request failed` log line with the storage's error
(`put object: Access Denied.` for a bucket the key does not reach).

## Example manifests

[`deploy/examples/`](../../deploy/examples/) shows one way to provide what the chart takes
references to ([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
D1, D2). **They are examples to copy and adapt, not supported deployments, and they are checked for
syntax only**: `make examples-lint`, in CI's `helm` job, validates them against the
CustomResourceDefinitions of the operator releases they name, which proves that they parse against
those releases and nothing more. Each file names, in its first lines, what it was written against
and what it makes, and the chart's values for it.

| File | Written against | What it makes |
|---|---|---|
| [`cloudnative-pg-cluster.yaml`](../../deploy/examples/cloudnative-pg-cluster.yaml) | CloudNativePG 1.30.1, PostgreSQL 18.6 | a `Cluster` whose `initdb` bootstrap makes the database, the owner role, the runtime role with the attributes [above](#the-database-and-its-two-roles), the three extensions and `CONNECT` for the two roles only; the runtime role's `basic-auth` Secret; a ConfigMap with the location. Never applied to a cluster here |
| [`minio-tenant.yaml`](../../deploy/examples/minio-tenant.yaml) | the MinIO Operator v7.1.1 | a `Tenant` with one pool and the bucket — and no user: the operator gives every user of its `users` field the policy `consoleAdmin`, an administrator of the whole store. Never applied to a cluster here |
| [`minio-bucket.sh`](../../deploy/examples/minio-bucket.sh) | `mc` RELEASE.2025-08-13T08-35-41Z | the bucket, the bucket-scoped policy, the access key with that policy alone and the Secret `cowork-storage`, for an existing MinIO or the Tenant above. Run once on 2026-10-06 against the MinIO of `make minio-up`: the key put, read and deleted objects in its bucket and was refused listing it, another bucket and the administration |

**MinIO is archived.** The repositories of the MinIO Operator, the MinIO server and `mc` are
archived on GitHub, and the server image the operator defaults to, `minio/minio`, can no longer
be pulled (checked 2026-10-06): no fix follows for any of them. Do not install the operator or
MinIO for a new installation; the two MinIO files are for a cluster that runs them already, and
the Tenant names Chainguard's build of the server, which this repository's tests run and which
was not tried with the operator. cowork needs no MinIO: any S3-compatible store with a bucket and
a key scoped to it will do ([object storage](#object-storage)).

## The chat

The assistant at the right edge of the UI talks to a model the backend calls for the person
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)).
`chat.providers` turns it on: a list of providers the person picks from, the first the default, each
with an id, a name, a kind — `openai` for OpenAI Chat Completions, which LM Studio, Ollama and vLLM
serve as well, or `anthropic` —, the base URL and the model; a key, where a provider takes one, comes
from a Secret of that provider's own ([the Secrets](#the-secrets)):

```yaml
chat:
  providers:                             # default []: no chat
    - id: ollama                         # example: COWORK_CHAT_OLLAMA_*
      name: Ollama                       # example; the id when empty
      kind: openai                       # example
      url: http://ollama.ai.svc:11434/v1 # example: https://, or http:// on a host of your own network
      model: openai/gpt-oss-20b          # example: the model's name at the provider
      existingSecret: ""                 # required for anthropic; the key under keys.apiKey, default apiKey
```

**Every provider receives what the chat reads, in every tenant.** No tenant is asked: every member
has the chat once a provider is listed, and a provider receives what the chat reads for its person,
confidential tickets included — the owner's decision, with the risk accepted
([chat.md H-37](../security/chat.md#h-37)). List a hosted provider only where every tenant's data may
go to it ([chat.md, adding a hosted provider](chat.md#adding-a-hosted-provider)).

**The backend's pods reach the providers.** The browser never does. The chart ships no
NetworkPolicy; a policy of the cluster's that restricts the backend's egress must admit the
providers and DNS. The stream of a turn passes the Ingress like the event stream, unbuffered
([runtime.md, behind an Ingress](runtime.md#behind-an-ingress)).

LM Studio, OpenAI and Anthropic one by one, the context LM Studio loads a model with, the limits
and what the log says are [chat.md](chat.md).

## Install

```bash
helm repo add cowork https://guided-traffic.github.io/cowork/
# 0.4.0 is an example: the first release whose Ingress routes the API to the backend, or a later one
helm upgrade --install cowork cowork/cowork --version 0.4.0 --namespace cowork \
  --set database.existingSecret=cowork-database \
  --set database.owner.existingSecret=cowork-database-owner \
  --set session.existingSecret=cowork-session \
  --set storage.existingSecret=cowork-storage \
  --set storage.endpoint=https://s3.example.com --set storage.bucket=cowork
```

The last two lines are optional; leave them out and uploads are refused. What the release
contains: the Deployments `<fullname>-backend` and `<fullname>-frontend` — `<fullname>` is
`<release>-cowork`, or the release name itself when it contains `cowork`, so `cowork-backend`
and `cowork-frontend` for the release `cowork` — a Service for each
(backend on 8080, frontend on 80), one ServiceAccount without an API token, the `migrate` init
container in every backend pod — or, in [job mode](#job-mode), the hook Job `<fullname>-migrate`
in its place —, the container port `metrics` (8081) on the backend pods, which no Service lists
([metrics.md](metrics.md)), and — only when the values ask for them — the Secrets rendered from
inline URLs, the CA volume, the Ingress ([Expose it](#expose-it)) and the monitoring resources of
`metrics.*`. No NetworkPolicy
([network policies are the cluster's](#network-policies-are-the-clusters)) and no RBAC objects:
neither container talks to the Kubernetes API. The frontend pod takes no configuration: its
nginx serves the UI from a file in the image and reaches no backend.

Verify:

```bash
kubectl -n cowork rollout status deploy/cowork-backend deploy/cowork-frontend
kubectl -n cowork logs deploy/cowork-backend -c migrate   # "database schema is current" with the version; job mode: the Job is deleted once it succeeded
kubectl -n cowork port-forward svc/cowork-backend 8081:8080 &
curl -s localhost:8081/readyz           # {"status":"ready"} — the backend and its database
curl -s localhost:8081/api/v1/version   # the backend's version
kubectl -n cowork port-forward svc/cowork-frontend 8080:80 &
curl -s localhost:8080/healthz          # {"status":"ok"} — nginx itself
curl -s localhost:8080/api/v1/version   # a 404 problem: the frontend serves no API, the Ingress routes it
kubectl -n cowork port-forward deploy/cowork-backend 18081:8081 &
curl -s localhost:18081/metrics | grep '^cowork_migrations'   # the schema version, dirty 0: the metrics port of a pod
# once the Ingress is set up (below):
curl -s https://cowork.example.com/api/v1/version   # the backend's version, through the Ingress
open https://cowork.example.com                     # the UI shell, with the version in the footer
```

The UI needs the Ingress, or a route of your own with the same paths: a port-forward to the
frontend alone serves the shell without its API, and the page then shows "Not found — the
frontend serves the UI only; the Ingress must route /api/ and /auth/ to the backend Service" with
"backend unreachable" in the footer — what an Ingress that sends every path to the frontend shows
as well.

**Log in.** Without a local administrator or an identity provider nobody can: an installation
answers the health endpoints, the version, the API document, what the login page offers and the
login, and `401 unauthenticated` on every other route. The UI's login page shows the form while
an active local account exists — the local administrator ([above](#the-local-administrator)) and
the accounts tenants create, `POST /auth/local` of [README, API](../../README.md#api-backend) —
and "Sign in with" the provider's `auth.oidc.displayName` while a provider is configured and its
gate names a group ([the identity provider](#the-identity-provider)). `make dev-seed` creates a
person, a tenant and a token in the development database and is never an installation step
([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D7).

## How the schema is migrated

`migrations.mode` chooses where the migrations run
([ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)):
`onStart`, the default, in an init container of every backend pod; or `job`, in one Helm hook Job
before every install and upgrade. In both, the serving container never migrates: it starts with
the runtime role's connection and `COWORK_MIGRATE_ON_START=false`, and refuses to serve — exit 1,
the pod restarts — a dirty schema or one with pending migrations:
`pending migrations: N; run the migration job (or set COWORK_MIGRATE_ON_START=true)`. A schema
newer than the binary is served, with a warning in the log.

### On start

With `backend.config.migrateOnStart: true` (the default), every backend pod starts with the
init container `migrate`: it runs `cowork migrate` as the owner role, granting the runtime
role — named by the runtime role's connection, which it reads for that name only — what each
migration grants. Pods that start together serialise on a database advisory lock; the first
applies, the rest find the schema current (D1).

With `backend.config.migrateOnStart: false` there is no init container and the chart needs no
owner credential and renders no owner Secret — leave `database.owner.*` empty: an inline `url`
would still stay in the release's values, readable with `helm get values` — and nothing in the
release migrates. The backend pods refuse to start until somebody runs `cowork migrate` against
the database with the owner's connection and the runtime role's set (`COWORK_DATABASE_OWNER_URL`
and `COWORK_DATABASE_URL`, or their components).

A failing migration fails the init container: the pod stays in an `Init:` state and is
retried with back-off, and `kubectl -n cowork logs <pod> -c migrate` shows why. What a failure
leaves behind and how it is repaired: [runtime.md, the migration run](runtime.md#the-migration-run).

### Job mode

With `migrations.mode: job` the chart renders the Job `<fullname>-migrate`, a Helm hook
(`pre-install,pre-upgrade`, weight `-10`): Helm runs it, and waits for it, before it installs or
upgrades anything else of the release, so the migration is one visible step, once per rollout, and
a failed one fails the release before a new pod starts (D2). The backend pods have no init
container; `backend.config.migrateOnStart` is not read.

```yaml
migrations:
  mode: job
database:
  existingSecret: cowork-database          # example: required in job mode
  owner:
    existingSecret: cowork-database-owner  # example: required in job mode
localAdmin:
  existingSecret: cowork-local-admin       # example: required in job mode with a local administrator
```

- **Secret references only.** The Job runs before the Secrets the chart renders from inline values
  exist, and before a changed inline value reaches its Secret on an upgrade, so job mode takes no
  `database.url`, no `database.owner.url` and no inline local administrator: rendering fails on
  each, naming the value. ConfigMaps are yours as well and exist before the release.
- **The bootstrap follows the migration.** After the schema step the Job synchronises the local
  administrator and creates the bootstrap tenant, with the administrator group's mapping, as a pod
  does at its start (D4; [the local administrator](#the-local-administrator)) — it reads the local
  administrator's Secret, `bootstrap.tenant`, `backend.config.baseURL`, the password policy and, with
  an `auth.oidc.adminGroup`, the issuer and that group. It is given no client secret, no server key
  and no storage key. The pods still run the bootstrap at their start, so rotating the local
  administrator's Secret and restarting them works as in the other mode.
- **One attempt, bounded by Helm's `--timeout`** (five minutes by default). A migration that fails
  fails the release: `kubectl -n cowork logs job/cowork-migrate` shows why — a failed Job stays
  until the next attempt replaces it, a succeeded one is deleted. A Helm that gives up waiting marks
  the release failed and leaves the Job running; for a migration that takes longer, give the
  upgrade a longer `--timeout`. A dirty schema is repaired by a person in either mode (D7,
  [runtime.md](runtime.md#the-migration-run)).
- **It runs as the namespace's `default` ServiceAccount** — the one the chart creates does not
  exist before the release — or as `serviceAccount.name` when you set `serviceAccount.create: false`,
  with no token mounted, the backend's image, security contexts, resources and scheduling.

**Switching modes** is an upgrade: from `onStart` to `job` the upgrade's Job migrates before the
new pods, which come without the init container; from `job` to `onStart` the new pods migrate in
their init container again, and no Job runs. Mind the references first — job mode refuses inline
values.

### Argo CD and Flux

Neither controller was run with cowork; these notes are written from their documentation — Argo
CD's Helm user guide and sync phases of v3.5.3, and Flux's `HelmRelease` reference of
helm-controller v1.6.5 — and say what job mode needs of each (D6).

**Argo CD** renders the chart with `helm template` and maps Helm's hook annotations onto its own:
`pre-install` and `pre-upgrade` are its `PreSync` phase, `helm.sh/hook-weight` its
`argocd.argoproj.io/sync-wave`, and the Job's `hook-delete-policy` its delete policies, which Argo
CD evaluates by its own sync phases. So the Job needs no annotation of Argo CD's and gets none from
the chart. What differs from Helm:

- Argo CD cannot tell an install from an upgrade: **every sync runs the Job**, the migration and the
  bootstrap. Both find nothing to do when nothing changed; the Job's pod holds the owner's credential
  for that time at every sync.
- **A `PreSync` hook runs before the application's manifests are applied** — Secrets included —,
  which is why job mode takes `existingSecret` references only: the Secrets the Job reads must exist
  before the sync, made outside the application or by one that syncs before it.
- **Adding an Argo CD hook annotation to any resource of the application makes Argo CD ignore every
  Helm hook**, the Job's included: then add `argocd.argoproj.io/hook: PreSync` to the Job yourself,
  through a post-renderer or a patch — the chart has no value for it.
- A selective sync runs no hook.

**Flux**'s helm-controller runs Helm's install and upgrade actions, chart hooks included unless
told otherwise, so the Job runs as with Helm. In the `HelmRelease`:

- leave `spec.install.disableHooks` and `spec.upgrade.disableHooks` at `false`, their default: with
  either `true` the Job never runs, and the pods refuse to start on pending migrations;
- `spec.timeout` (five minutes by default), or `spec.install.timeout` and `spec.upgrade.timeout`, is
  how long the controller waits for the Job: give a long migration more;
- `spec.install.remediation.retries` (default `0`) retries a failed install, uninstalling between
  attempts; `spec.upgrade.remediation` (default `0` retries, strategy `rollback`) rolls back a failed
  upgrade. A rollback runs no pre-upgrade hook: the pods of the release before keep serving, which a
  migration that failed in its own transaction leaves them able to do, and a migration that left its
  version dirty stops every pod that starts afterwards until a person repairs it (D7). The
  `RetryOnFailure` strategy retries the upgrade, the Job included, without remediating;
- `spec.upgrade.preserveValues` re-uses the values of the last release; whether it then takes the
  new chart's defaults, as `--reset-then-reuse-values` does, or the old release's, as
  `--reuse-values` does ([upgrade](#upgrade)), its reference does not say — not verified here.

## Expose it

`ingress.enabled=true` renders a standard `networking.k8s.io/v1` Ingress named `<fullname>` with
three paths for every host of `ingress.hosts`
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3):

```yaml
- path: /api/      # the API → Service <fullname>-backend, port http (8080)
  pathType: Prefix
- path: /auth/     # the browser's login flows, the identity provider's /auth/callback among them → <fullname>-backend
  pathType: Prefix
- path: /          # the UI → Service <fullname>-frontend, port http (80)
  pathType: Prefix
```

The paths are the chart's: a host names nothing but `host`, and the `paths` an older values file
gives a host are ignored. Set `ingress.className`, the hosts and, for TLS, `ingress.tls` with a
Secret your certificate issuer fills. Set `backend.config.baseURL` to the public URL at the same
time: it is the origin the CSRF check compares every write of a session with, so it must be
exactly what the browser shows — scheme, host, port, no path — and a mismatch is `403 csrf` on
every write ([runtime.md, the login](runtime.md#the-login)).

**The controller stands in front of the backend, and its settings are yours.** The frontend's
nginx proxies nothing: what the controller lets through reaches the backend, and what it refuses
never does. The chart does not know which controller runs and sets none of its settings; set them
in `ingress.annotations` or in the controller's own configuration. What any controller must do:

| What | Must be | Otherwise |
|---|---|---|
| request body limit | above the backend's larger limit: `max(maxJsonBody, attachmentMaxBytes)` rounded up to MiB, plus 1 MiB — `11m` with the defaults; none when either is `0`. The chart's notes print the figure | an upload or a body above the controller's limit gets the controller's own `413` page, not the backend's problem |
| read timeout | above `backend.config.requestTimeout` plus 10 s — `40` seconds with the default; an hour when it is `0`. The chart's notes print the figure. The two streams send something at least every twenty seconds — the event stream's heartbeat, the chat's comment every ten — so they stay open within any timeout above that | a slow request gets the controller's `504` page instead of the backend's `504` problem with its request id; below twenty seconds the streams are cut |
| buffering of `text/event-stream` | off. The backend answers the event stream and a turn of the chat with `X-Accel-Buffering: no`; a controller that honours that header — nginx does — needs no setting, one that does not must be told not to buffer | events arrive late and in bursts, a turn's text all at once at its end |
| `X-Forwarded-For` | the address the controller saw as the header's last entry, written in place of the client's header or appended to it | `backend.config.trustedProxies` finds the wrong client ([below](#the-client-address-and-the-trusted-proxies)) |

**ingress-nginx is retired**: Kubernetes ended it in March 2026 — no releases, no bug fixes and no
security fixes since ([the announcement](https://kubernetes.io/blog/2025/11/11/ingress-nginx-retirement/),
[the statement](https://kubernetes.io/blog/2026/01/29/ingress-nginx-statement/)). Do not install it
for a new installation.

**A worked example, for a cluster that still runs ingress-nginx.** Its own defaults are a body
limit of `1m`, a read timeout of 60 seconds, no response buffering, and `X-Forwarded-For` set to the
address it saw; so for the backend's defaults the body limit is the annotation it needs, and the
read timeout one once `requestTimeout` is above 50 seconds:

```yaml
ingress:
  enabled: true
  className: nginx                                       # example
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: 11m     # example: the figure for the default limits
    nginx.ingress.kubernetes.io/proxy-read-timeout: "40" # example: the figure for the default requestTimeout
  hosts:
    - host: cowork.example.com                           # example
  tls:
    - secretName: cowork-tls                             # example
      hosts: [cowork.example.com]
```

**Verified on 2026-10-04** with ingress-nginx v1.15.1 in a kind cluster, the chart installed with
its Ingress: the controller routed `/api/`, `/auth/` and `/auth/callback` to the backend and the
rest to the frontend; its generated configuration had `client_max_body_size 1m`,
`proxy_read_timeout 60s` and `proxy_buffering off` by default, and a 2 MiB body got the
controller's `413` page until `proxy-body-size: 11m` let it through to the backend's own `413`
problem; the event stream delivered an event within 50 ms and stayed open past a read timeout of
60 s and of 40 s on its heartbeats — with `proxy-buffering: "on"` forced as well, so the backend's
header alone keeps it unbuffered. The chat's stream carries the same header and was not run through
the controller. No other controller was tried here: give yours the four settings above in its own
terms.

**What the controller answers itself is its page, not a problem body:** a `502` or `503` while no
backend pod is ready — ingress-nginx answered `503 Service Temporarily Unavailable` in the run
above, the Ingress stand-in of local runs `502` —, its `413` above its body limit, its `504` past
its read timeout. The UI shows a `502`, `503` or `504` without a problem body as the backend out of
reach — "The backend cannot be reached: The Ingress answered 503: no backend took the request.
cowork tries again on its own." — and any other status without one as an unexpected answer that
names the status; the backend's own errors always come as problem bodies with a request id
([ADR 0047](../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D6).

**The chart offers the Ingress only, no route of the Gateway API**
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3). On a cluster that serves through a Gateway, leave `ingress.enabled` false and write an
`HTTPRoute` of your own, attached to your Gateway, with the three path rules below; the chart
renders none and the release does not manage it. No Gateway API implementation has been tried with
cowork.

**Without the chart's Ingress** — an Ingress, an `HTTPRoute` or a mesh route of your own — route
the same three paths on one host: `/api/` and `/auth/` to `<fullname>-backend:8080`, everything
else to `<fullname>-frontend:80`. A request for `/api/` or `/auth/` that reaches the frontend all
the same answers `404` with a problem whose detail says so ("the frontend serves the UI only; the
Ingress must route /api/ and /auth/ to the backend Service"), and the UI shows that detail on its
page. Neither container terminates TLS. Whatever you put in front — an Ingress controller, a mesh —
terminates it; both pods speak plain HTTP on 8080.

## The client address and the trusted proxies

The login throttle counts attempts per **client address**, and every audit row of a request
carries a keyed hash of it; the backend has to be told how to find it. A browser reaches the
backend through the Ingress controller — a cloud load balancer in front of that is a second hop —
and each proxy writes the address it saw into `X-Forwarded-For`:

```
browser ──▶ Ingress controller ──▶ backend
 203.0.113.9   sees 203.0.113.9       sees 10.244.0.7 (the controller pod)
               sends  203.0.113.9
```

The backend's TCP peer is the controller pod; the header says who was before it. With
`backend.config.trustedProxies` (`COWORK_TRUSTED_PROXIES`) set to the networks of the proxies —
here `10.244.0.0/16`, the pod network the controller runs in — the backend walks the header from
the right: the peer is trusted, so it takes `203.0.113.9`; that is not, so it is the client.
Entries to the left of the client were written by the client and are never read; an entry that is
no address stops the walk at the hop before it.
[local-accounts.md](../security/local-accounts.md#the-client-address) has the rule and its tests.

**The controller has to write the address it saw as the last entry of `X-Forwarded-For`** — in
place of the client's header, or appended to it: either way the walk stops at that entry, and what
the client wrote to its left is never read. A controller that passed the client's header on
unchanged would let every client choose its address through it. ingress-nginx writes the address
it saw in place of what the client sent (`X-Forwarded-For $remote_addr` in its generated
configuration, with `use-forwarded-headers` `"false"`, its default), so a forged header does not
reach the backend through it. **Verified on 2026-10-04** with v1.15.1 in a kind cluster, the backend trusting the pod network and allowing
three attempts a minute: four logins through the controller, each with another forged
`X-Forwarded-For`, were throttled at the fourth — one bucket; the Ingress stand-in of local runs
does the same.

**What to set.** `trustedProxies` is the networks of the *proxies* — the Ingress controller's
pods, and a load balancer that adds its own hop — and no more:

```yaml
backend:
  config:
    baseURL: https://cowork.example.com
    trustedProxies: "10.244.0.0/16"   # example: the pod network of a cluster whose Ingress controller runs in it
```

Take the real ranges from your cluster — the pod CIDR (`kubectl get nodes -o
jsonpath='{.items[*].spec.podCIDR}'` lists the per-node ranges on a cluster whose node
controller allocates them; a network plugin with its own address management keeps them
elsewhere), or the nodes' addresses for a controller on the host network, and, behind a load
balancer that adds its own hop, that hop's address too. IPv4 and IPv6 are separate entries; a
single host is `/32` or `/128`. The list is validated at start: an entry that is no CIDR refuses
the start, naming `COWORK_TRUSTED_PROXIES` and that entry only, and the start logs the networks it
parsed (`client addresses are read through trusted proxies`). The frontend's pods are no hop: they
proxy nothing.

The controller has to see the browser's address to pass it on. Behind a cloud load balancer or a
`NodePort`, Kubernetes may rewrite the source address before the controller sees it — its
documentation says it is not defined whether that happens before or after NetworkPolicy
processing — and then the "client" is the node or the balancer. `externalTrafficPolicy: Local`,
the PROXY protocol, or a controller told to read the forwarded header of a layer-7 balancer in front
of it are the ways; they are the controller's to configure, and the balancer then belongs in
`trustedProxies` as well.

**What goes wrong:**

| The list | What happens |
|---|---|
| empty (the default) | the client is the controller pod, for every browser behind it: `auth.local.addressLimit` attempts a minute shared by all of them, one bucket per controller pod. One client's failures use it up for everybody. The chart's notes say so while a local administrator is set |
| too narrow — the controller's network missing | the walk stops at the controller: the same as empty |
| too wide — `0.0.0.0/0`, a whole private range, a network that holds clients | a client inside it is a hop itself, the walk goes on into the entries it wrote, and it chooses its own address: it dodges the throttle by changing the address with every attempt, and fills another client's bucket to keep that person from logging in |
| a pod network other workloads share, with their access to the backend | a pod that calls the backend Service directly is a trusted peer, and the walk reads what it wrote: it chooses its address for the throttle and the audit's source hash. Verified as above: a pod calling the backend Service with a forged header per attempt was never throttled, and the same pod without one was at its fourth. A narrower list helps only where the controller's addresses are narrower than the pod network (the host network, a fixed range); otherwise a network policy of the cluster's that admits only the controller to the backend's pods is what closes it — the chart ships none ([H-17](../security/local-accounts.md#h-17)) |

**Check it.** With the list set, fail the login more often than `auth.local.addressLimit` in a
minute from one machine: that machine gets `429`, and a login from another machine in the same
minute still works. If both get `429`, the walk stopped at a proxy that is not in the list.

### Network policies are the cluster's

The chart renders no NetworkPolicy
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3): who may reach which pod is the cluster administrator's policy, and the chart does not know
where your controller runs. What cowork's pods need, for a policy you write:

- **The backend's pods**, port 8080: the Ingress controller's pods, for `/api/` and `/auth/`; and
  whatever else calls the API inside the cluster — a script, an agent — with a token. With
  `trustedProxies` set, this is the policy that keeps every other pod from choosing its address
  (H-17 above). Out of them: the database, the object storage, the identity provider, the chat's
  providers, and DNS.
- **The frontend's pods**, port 8080: the Ingress controller's pods. The frontend reaches nothing.
- **The kubelet's probes** of both pods come from the node, which Kubernetes always admits.
- **The backend's pods**, port 8081 (`metrics.port`): the monitoring namespace, for Prometheus, and
  nothing else — the port answers `/metrics` without authentication; with
  `frontend.metrics.exporter.enabled`, **the frontend's pods**, port 9113, likewise. The rule and
  why it goes beside the others are [metrics.md](metrics.md#the-network-policy-for-the-port).

A policy is enforced by a network plugin that implements NetworkPolicy and by nothing else;
Kubernetes says that creating one without such a plugin "will have no effect". Nothing of this was
run against a plugin that enforces policies.

## Upgrade

The procedure — a backup first, the order the migrations and the pods go in, how to read where
the schema stands, a rollback and a dirty schema — is [upgrade.md](upgrade.md). Here are the
commands and what each release changes.

```bash
helm repo update cowork
helm upgrade cowork cowork/cowork --version <new> -n cowork --reset-then-reuse-values
```

**`--reset-then-reuse-values`** (Helm 3.14 or newer, and Helm 4) keeps the values you set and takes
the new chart's defaults for every other. **`--reuse-values`** takes the defaults of the chart the
release was installed with instead — Helm puts the old release's values in place of the new chart's
`values.yaml` (`reuseValues` in Helm's `pkg/action/upgrade.go`, 3.21.3 and 4.3.0 alike) — so a
value a later release adds is missing. The chart reads every value added since 0.7.0 with its
default when it is missing, so `--reuse-values` from 0.7.0 renders as `--reset-then-reuse-values`
does; the charts before it did not, and an upgrade from 0.2.0 to 0.7.0 with `--reuse-values` fails
to render at `chat.providers` (checked 2026-10-06). Keeping your own values file and passing it with `-f` works as well.

Both images carry the release's version, and the chart of that version names them; set
`backend.image.tag` and `frontend.image.tag` only to pin images apart from the chart, and then
move them together — across the release below, the frontend image and the chart move together as
well. The pending migrations are applied before a new server starts: in `onStart` mode by the
init container of each new backend pod, in [job mode](#job-mode) by the Job, before Helm changes
anything else of the release. **Rolling back is rolling the image back:** deploy the previous tags
and leave the schema where it is. The previous image's migration run — its init container, or the
Job of that upgrade — finds the schema ahead of it and applies nothing, and its server serves it
with a warning; `helm rollback` runs no Job at all. A migration never removes what the previous
release still reads, which is what makes that safe
([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md));
there is no schema rollback and no `migrate down`.

**The release with the migration Job and the component keys**
([ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)
D2, [ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
D3, both amended 2026-10-06) changes nothing for an installation that sets nothing new: every
values file of the chart's `ci/` renders the same manifests, byte for byte, with the new defaults and
with the values of 0.7.0 that `--reuse-values` keeps. `database.existingSecretKey` and
`database.owner.existingSecretKey` keep working — their default is empty now, and `keys.url`
carries `databaseUrl`. Switching to [job mode](#job-mode) or to the components is an upgrade with
the new values, after their Secrets and ConfigMaps exist.

**The release whose Ingress routes the API to the backend**
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3, amended 2026-10-04) changes what stands in front of the backend:

- **Set the controller's limits before the upgrade** ([Expose it](#expose-it)): the Ingress sends
  `/api/` and `/auth/` to the backend itself now, and a controller whose body limit is below the
  backend's answers a larger upload with its own `413` page — ingress-nginx's default limit is
  1 MiB.
- **The upgrade deletes the chart's NetworkPolicy** `<fullname>-backend`. With `trustedProxies`
  set, a policy of the cluster's that admits only the controller to the backend's pods is what
  keeps other pods from choosing their client address ([network policies are the
  cluster's](#network-policies-are-the-clusters)). A script that went through the frontend
  Service calls the backend Service now: the frontend answers `/api/` with a `404` problem.
- **`trustedProxies` names the controller's networks only**: the frontend's pods are no hop any
  more, so a list that named their range for them alone can be narrowed.
- **The chart and the frontend image of that release go together.** The new image's
  configuration lives in `/etc/nginx/conf.d`, where an older chart mounts an empty volume: the
  pod then answers nothing and never gets ready. An older image renders its configuration into
  `/etc/nginx/conf.d` at start, which the new chart leaves on the read-only root filesystem: the
  container exits 1. Both measured with the images on 2026-10-04. So pin no `frontend.image.tag`
  apart from the chart across this release, and roll back with `helm rollback`, chart and images
  together, not the frontend image alone.
- **An Ingress of your own** that sent every path to the frontend Service needs the two backend
  paths of [Expose it](#expose-it).
- `networkPolicy.enabled` and a host's `paths` are no values any more; `--reuse-values` carries
  them along, and nothing reads them.

**The release whose API names the horizon by its word**
([ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D1 and
[ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4, amended 2026-10-05) keeps the names before beside the new ones, so a `cowork-mcp` of the release
before keeps working against it. Two things to know:

- **Upgrade the installation before the people's `cowork-mcp`.** A `cowork-mcp` of this release
  calls `setHorizon`, which the release before does not serve, and refuses every tool against it,
  naming the operation.
- **A rollback to 0.4** is unsupported since 0.5.0 ([SECURITY.md](../../SECURITY.md)); a migration
  keeps only the release directly before it working
  ([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D3).

**The release that drops the horizon's names before**
([ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D1 and
[ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4, amended 2026-10-06) is the contract of the one above. Its migration rewrites every stored
`override-urgency` to `set-horizon` and a saved filter's `urgency` to `horizon`, and its API knows the
new names only. Two things to know:

- **A `cowork-mcp` of 0.4 does not work against it**, and is unsupported
  ([SECURITY.md](../../SECURITY.md)): its token reads without the capability it looks for, and its
  routes for the horizon are gone. Every person's `cowork-mcp` of 0.5 keeps working.
- **A rollback to 0.5 over it is safe** — over it alone, not over the release that narrows the
  checks, below. 0.5 reads everything the migration leaves, and the checks of that release and the
  one after it, 0.6 and 0.7, take the `override-urgency` that 0.5 writes beside `set-horizon` into
  every agent token it makes and every chat capability set it stores; once the image goes forward
  again, 0.6 and 0.7 read such a set without the old name. One thing is lost in that window: a
  saved filter 0.5 stores with the key `urgency` — which 0.5 still takes — loses that condition
  under 0.6 and 0.7, which read it as a filter without a horizon, until the migration of the release
  that narrows the checks rewrites it to `horizon`. Read from the code of 0.5.1, not run.

**The release that narrows the capability checks**
([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4 and [ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D1, as amended
2026-10-06) completes the contract above. Its migration rewrites again what a rollback to 0.5 left —
every stored `override-urgency` to `set-horizon`, and a saved filter's `urgency` to `horizon`, which
gives the filter back the condition the window lost, unless its conditions were saved again
meanwhile — and then the checks of the tokens' and the chat's capability sets refuse
`override-urgency`. Two things to know:

- **A rollback to the release directly before it is safe**: no release from 0.6.0 on writes the
  old name.
- **A rollback to 0.5 over it is not.** 0.5 writes `override-urgency` beside `set-horizon` into
  every agent token it makes and every chat capability set it stores that holds `set-horizon`; the
  checks refuse that write, so making such a token or choosing such a set fails. 0.5 is
  unsupported ([SECURITY.md](../../SECURITY.md)). Read from the code of 0.5.1, not run.

**The release that brings the metrics**
([ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md))
opens a port on the backend pods with the upgrade itself: `metrics.enabled` is `true` by default,
so the new pods answer `/metrics` on 8081 without authentication to every pod that reaches them
([metrics.md](metrics.md#the-network-policy-for-the-port)). Add the policy for the port before, or
upgrade with `--set metrics.enabled=false` until it is in place. Nothing else changes: the
monitoring resources stay off until they are switched on, and the backend Service keeps its one
port.

**The release that brings the consistency check**
([ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4) lists the bucket once a day, at the first start and then in the hour after 03:00 UTC:

- **Grant the storage key `s3:ListBucket` on the bucket before the upgrade**
  ([object storage](#object-storage)). Without it uploads and downloads go on, and the check fails
  every hour with `job failed` and [`CoworkJobFailing`](metrics.md#coworkjobfailing).
- **Its migration** adds two tables and two audit actions and lets the job read every tenant;
  a rollback to the release before serves over it ([upgrade.md](upgrade.md#rolling-back)).
- **With `metrics.prometheusRule.enabled`** the rule gains `CoworkAttachmentsOutOfStep`, which waits
  `metrics.prometheusRule.restoreWindow`, a day by default ([metrics.md](metrics.md#coworkattachmentsoutofstep)).
  The first check may find what a restore or a failed removal left in the past, and the alert then
  fires a day later — the tenants' administrators settle it on their settings page.

## Uninstall

```bash
helm uninstall cowork -n cowork
```

The release leaves the database and the bucket untouched. The Secrets and the ConfigMaps you
created stay; the Secrets the chart rendered from `database.url`, `database.owner.url`, the
inline local administrator are removed with the release. In job mode a migration Job that failed
stays as well — a hook is not part of the release; Helm deleted a succeeded one already —:
`kubectl -n cowork delete job cowork-migrate`. The client
at the identity provider stays registered until you remove it there.

## Resources and scheduling

The backend defaults ask for 50m CPU and 128Mi memory and cap memory at 256Mi; the frontend
for 10m and 32Mi, capped at 64Mi. Both are idle at a fraction of that today and will be
revisited once there is a workload. Uploads are what moves the backend's memory: a file is
held in memory while it is checked and stored, and the backend lets 64 MiB divided by
`backend.config.attachmentMaxBytes` uploads in at a time, at least one — six with the 10 MiB
default; the rest wait. From 64 MiB on it is one upload at a time, holding up to the whole
maximum: raise the memory limit before raising the maximum.

Each backend replica holds its connection pool to PostgreSQL — pgx's default size is the
larger of 4 and the number of CPUs the process sees; `pool_max_conns=<n>` in the runtime URL
sets it — plus one connection for the event listener. `nodeSelector`, `tolerations` and
`affinity` are per component and passed through verbatim.
