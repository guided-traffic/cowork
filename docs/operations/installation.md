# Installation

cowork is installed with the Helm chart in [`deploy/helm/cowork/`](../../deploy/helm/cowork/).
A release is two Deployments — the backend (the API; its pods migrate the schema in an init
container) and the frontend (nginx with the Angular bundle, proxying `/api/` to the backend).
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
| A PostgreSQL 18 (or newer) database with two roles | yes | the runtime role's URL and the owner role's URL, each from a Secret |
| The server key | yes | a Secret; there is no inline value |
| An S3-compatible bucket with an access key scoped to it | no — without it uploads are refused | endpoint and bucket as values, the key from a Secret, a private authority from a ConfigMap |

Rendering fails, naming the missing value, without a database URL, without an owner URL while
`backend.config.migrateOnStart` is `true` (the default), without `session.existingSecret`, and
with a `storage.endpoint` but no `storage.bucket` or no `storage.existingSecret`.

## The database and its two roles

The schema belongs to one role and the requests run as another
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2).
The tables carry forced row-level security; a role that owns them could switch it off and
grant itself back what was revoked, so the role that serves owns nothing. Any PostgreSQL 18 or
newer will do; the operator creates the database and both roles, cowork creates no role.

| Role | Used by | Requirements |
|---|---|---|
| owner (`cowork_owner` `# example`) | the migrations only: the chart's `migrate` init container, `cowork migrate` | owns every object it creates: it owns the database, or holds `CREATE` on schema `public` (and `CREATE` on the database, unless the extensions exist already — below) |
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

**CloudNativePG.** Its `-app` Secret carries the credentials of the role that owns the
database it bootstraps, with a `uri` key — the owner role's URL, so
`database.owner.existingSecret=<cluster>-app` with `database.owner.existingSecretKey=uri`.
The runtime role is not part of that bootstrap, and the chart reads a URL only (the
component keys of ADR 0058 D3 are not built), so its Secret with a URL key is yours to write.
Not verified against a CloudNativePG cluster in this repository; the Secret's layout is from
that project's documentation.

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
```

| Secret | Values naming it | Key `# default` | Read by |
|---|---|---|---|
| the runtime role's URL | `database.existingSecret` | `database.existingSecretKey`: `databaseUrl` | the backend container; the `migrate` init container reads only the role's name from it |
| the owner role's URL | `database.owner.existingSecret` | `database.owner.existingSecretKey`: `databaseUrl` | the `migrate` init container, nothing else |
| the server key | `session.existingSecret` | `session.keys.key`: `sessionKey` | the backend container |
| the storage access key | `storage.existingSecret` | `storage.keys.accessKeyId`: `accessKeyId`, `storage.keys.secretAccessKey`: `secretAccessKey` | the backend container |

**The server key** is standard base64 of at least 32 random bytes; `openssl rand -base64 32`
makes one. It signs the list cursors, so every replica must hold the same key — they read the
same Secret. Rotating it invalidates the cursors clients hold: the next page they ask for is
`400 invalid_cursor`, and they start the list over. The chart has no inline path for it.

**The owner's credential reaches only the init container.** The serving container gets the
runtime URL alone and `COWORK_MIGRATE_ON_START=false`. An installation that hands the owner URL
to the serving container — through `backend.extraEnv`, for instance — gives a compromised
server process the power to switch row-level security off.

**The inline URLs**, `database.url` and `database.owner.url`, render the Secrets
`<fullname>-database` and `<fullname>-database-owner` for you. Use them for a throw-away
installation only: the values are stored in plain text in the Helm release Secret and shown by
`helm get values`, and the chart prints a warning in its notes for each. When a Secret
reference and its inline URL are both set, the reference wins and the URL is ignored.

**A changed Secret reaches the pods when they start again.** The chart restarts them by
itself only for the inline URLs (a checksum annotation on the pod); after rotating a Secret
you created, run `kubectl -n cowork rollout restart deploy/cowork-backend`.

## Object storage

Attachments live in any S3-compatible store
([ADR 0016](../adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)),
in a bucket of their own, reached with an access key whose policy covers that bucket only —
never the store's root credentials
([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D5).
The backend writes, reads and deletes objects under `<tenant-id>/<attachment-id>`; it never
creates, lists or deletes a bucket, so the bucket exists before the first upload. This policy
was enough against the MinIO of `make minio-up`, with the region left empty:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": ["arn:aws:s3:::cowork/*"]
    }
  ]
}
```

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

The values: `storage.existingSecret=cowork-storage`, `storage.endpoint` (`http://` or
`https://`, host and port; setting it turns the storage on), `storage.bucket`,
`storage.region` (empty lets the client ask the server) and `storage.pathStyle` (`true`, as
MinIO expects; `false` for virtual-host addressing).

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

## Install

```bash
helm repo add cowork https://guided-traffic.github.io/cowork/
helm upgrade --install cowork cowork/cowork --version 0.1.0 --namespace cowork \
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
container in every backend pod, and — only when the values ask for them — the Secrets rendered
from inline URLs, the CA volume and the Ingress. No RBAC objects: neither container talks to
the Kubernetes API. The frontend pod gets `BACKEND_URL` set to the backend Service, and its
body size and read timeout computed from the backend's limits
([runtime.md, what nginx answers itself](runtime.md#what-nginx-answers-itself)).

Verify:

```bash
kubectl -n cowork rollout status deploy/cowork-backend deploy/cowork-frontend
kubectl -n cowork logs deploy/cowork-backend -c migrate   # "database schema is current" with the version
kubectl -n cowork port-forward svc/cowork-frontend 8080:80 &
curl -s localhost:8080/healthz          # {"status":"ok"} — nginx itself
curl -s localhost:8080/api/v1/version   # proxied to the backend
open http://localhost:8080              # the UI shell, with the version in the footer
kubectl -n cowork port-forward svc/cowork-backend 8081:8080 &
curl -s localhost:8081/readyz           # {"status":"ready"} — the backend and its database
```

**Nobody can sign in yet.** No route creates a person, a tenant or a token before the login
exists ([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D6); `make dev-seed` creates them in the development database and is never an installation
step (D7). An installation today answers the health endpoints, `/api/v1/version` and
`/api/v1/openapi.json`, and `401 unauthenticated` on every other route.

## How the schema is migrated

With `backend.config.migrateOnStart: true` (the default), every backend pod starts with the
init container `migrate`: it runs `cowork migrate` as the owner role, granting the runtime
role — named by the runtime URL, which it reads for that name only — what each migration
grants. Pods that start together serialise on a database advisory lock; the first applies,
the rest find the schema current
([ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md) D1).
Then the serving container starts with the runtime URL and `COWORK_MIGRATE_ON_START=false`,
and refuses to serve — exit 1, the pod restarts — a dirty schema or one with pending
migrations: `pending migrations: N; run the migration job (or set COWORK_MIGRATE_ON_START=true)`.
A schema newer than the binary is served, with a warning in the log.

With `backend.config.migrateOnStart: false` there is no init container and the chart needs no
owner credential and renders no owner Secret — leave `database.owner.*` empty: an inline `url`
would still stay in the release's values, readable with `helm get values` — and nothing in the
release migrates. The backend pods refuse to start until
somebody runs `cowork migrate` against the database with `COWORK_DATABASE_OWNER_URL` and
`COWORK_DATABASE_URL` set. The chart's Job mode of ADR 0057 D2 is not built.

A failing migration fails the init container: the pod stays in an `Init:` state and is
retried with back-off, and `kubectl -n cowork logs <pod> -c migrate` shows why. What a failure
leaves behind and how it is repaired: [runtime.md, the migration run](runtime.md#the-migration-run).

## Expose it

`ingress.enabled=true` renders a standard `networking.k8s.io/v1` Ingress named `<fullname>`
that targets the **frontend** Service; the frontend proxies `/api/` to the backend, so one
rule covers the UI and the API. Set `ingress.className`, the host and, for TLS, `ingress.tls`
with a Secret your certificate issuer fills. The event stream and the uploads pass the Ingress
too: the annotations it needs — no buffering, a long read timeout, a body size above the
backend's limits — are in [runtime.md, behind an Ingress](runtime.md#behind-an-ingress). Set
`backend.config.baseURL` to the public URL at the same time; today nothing reads it, and the
features that will (OIDC redirects, links in notifications) need it to be right.

An installation that prefers path routing at the Ingress (`/api` straight to the backend
Service) can write that Ingress itself; both Services exist. It then loses what the frontend's
nginx does for `/api/`: the unbuffered event stream location and the problem bodies for the
errors nginx answers itself. Neither container terminates TLS. Whatever you put in front — an
Ingress controller, a mesh — terminates it; both pods speak plain HTTP on 8080.

## Upgrade

```bash
helm repo update cowork
helm upgrade cowork cowork/cowork --version <new> -n cowork --reuse-values
```

Both images carry the release's version, and the chart of that version names them; set
`backend.image.tag` and `frontend.image.tag` only to pin images apart from the chart, and then
move them together. The new backend pods
apply the pending migrations in their init container before their server starts. **Rolling
back is rolling the image back:** deploy the previous tags and leave the schema where it is.
The previous image's init container finds the schema ahead of it and applies nothing, and its
server serves it with a warning. A migration never removes what the previous release still
reads, which is what makes that safe
([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md));
there is no schema rollback and no `migrate down`.

## Uninstall

```bash
helm uninstall cowork -n cowork
```

The release leaves the database and the bucket untouched. The Secrets and the ConfigMap you
created stay; the Secrets the chart rendered from `database.url` and `database.owner.url` are
removed with the release.

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
