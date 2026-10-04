# ADR 0058: PostgreSQL and Object Storage Are External; the Chart Takes Secret and ConfigMap References With Configurable Keys; Example Manifests Are Syntax-Checked, the Tests Are What Is Verified

## Status

Accepted, amended 2026-10-01 the same day (D1, D2: example manifests are provided after
all, syntax-checked, with the tests remaining what is verified), amended 2026-10-02 (D3, D4,
D5: the owner role of ADR 0021 D2 has its own credential), amended 2026-10-04 (D3: the chat's
key row). Decided by the owner as the
answer to the catalog questions "how is PostgreSQL provided?" and "how is the object storage
provided?", taken together: both external, the chart consuming references, over optional
subcharts and over an umbrella chart; the owner first declined example manifests and then
allowed them as proposed. The owner's condition: because the Secrets and ConfigMaps come from
operators an administrator cannot always shape, every key name the chart reads must be
configurable. D3–D5 are the design for that condition and were not objected to. Amended
2026-10-04 (D3: the OIDC client's row as built), and again on 2026-10-04 by the owner's answers on
the chat recorded in [ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)
(D3: the chat's providers are a list, and each provider's key comes from a Secret of its own; built
the same day).

**Partly built** (phase 2, 2026-10-02): D1, D3 and D5 in part — the database and owner Secrets
by URL key (`database.existingSecret`, `database.owner.existingSecret`), the session key
Secret, the storage credentials Secret with literal endpoint values and the CA ConfigMap, and
the role and bucket requirements on the operations page. Not built: D3's component keys and
`existingConfigMap` sources, D4's composed URL, D1's and D2's example manifests. Since phase 4
(2026-10-04) the identity provider's client Secret as well (D3, amended); since phase 5
(2026-10-04) the chat's API key (D3, the row added), checked by the chart's `cowork.chatEnabled`
helper and rendered in `ci/chat-values.yaml` — since the owner's answers of 2026-10-04 one key per
provider of `chat.providers`, two providers in that values file.

## Context

[ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D8 keeps stateful systems out of the application chart; [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
added an S3-compatible store; [ADR 0038](0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D2 runs both locally from `compose.yaml`; [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D2 constrains the database role. What the chart receives in a real cluster is whatever the
database operator or the platform team produces: CloudNativePG writes `uri`, `host`,
`user`, `password`, `dbname` into its `-app` Secret; another operator writes `connectionString`;
a platform team hands out host and port in a ConfigMap and the password in a Secret. A chart
that insists on one key name, or on a URL where only components exist, forces an
administrator to copy Secrets by hand — which the owner rules out. The owner also rules out
maintaining example manifests for the external systems: the project's responsibility ends at
its tests.

## Decision

**D1 — Both systems are external and the chart ships neither.** No PostgreSQL subchart, no
MinIO subchart, no umbrella chart, no operator resources inside the chart. The operations
page states "any PostgreSQL 18 or newer, any S3-compatible object store" and the role and
bucket requirements below. *(Amended:)* **Example manifests live in `deploy/examples/`**: a
CloudNativePG `Cluster` with `bootstrap.initdb` creating the `cowork` role with the
attributes of D5 and the extensions, and a MinIO bucket with a dedicated access key and a
bucket-scoped policy (as a MinIO Operator `Tenant` resource and, separately, as `mc`
commands for an existing MinIO). Each file says in its first lines that it is an example to
copy and adapt, not a supported deployment, and the operations page links to them.

**D2 — The tests are what is verified; the examples are syntax-checked.** The integration
and end-to-end tiers run against `postgres:18` and MinIO service containers with the
development `compose.yaml` ([ADR 0038](0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D5), and what those files configure is verified on every push. *(Amended:)* The example
manifests are validated in CI with `kubeconform` against the operators' CRD schemas (a
`make examples-lint` target in the `helm` job), which proves they parse against the versions
pinned there and nothing more; the operations page says so, and names the operator versions
the examples were written against.

**D3 — Every externally sourced value is a reference with configurable keys.** The chart's
pattern for each of them:

| Value | Reference | Keys (each configurable, with a default) |
|---|---|---|
| database | `database.existingSecret` | either `keys.url` (default `databaseUrl`) **or** the component keys `keys.host`, `keys.port`, `keys.name`, `keys.user`, `keys.password`, `keys.sslmode`; `database.existingConfigMap` may supply the non-secret components |
| database owner *(added 2026-10-02, [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2)* | `database.owner.existingSecret` | the same key set as the database row; read only by the migration run (the init container or the Job of [ADR 0057](0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)), never by the serving container |
| object storage credentials | `storage.existingSecret` | `keys.accessKeyId`, `keys.secretAccessKey` |
| object storage endpoint | `storage.existingConfigMap` or literal values | `keys.endpoint`, `keys.bucket`, `keys.region`, `keys.pathStyle`; `storage.tls.caConfigMap` + `keys.ca` for a private authority |
| OIDC client ([ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)) | ~~`oidc.existingSecret`~~ `auth.oidc.existingSecret` *(amended 2026-10-04)* | ~~`keys.clientId`, `keys.clientSecret`; issuer and scopes as values or `oidc.existingConfigMap` keys~~ *(amended 2026-10-04: `auth.oidc.keys.clientSecret`, default `clientSecret`, required with `auth.oidc.issuer` — the client secret has no inline value at all —; `auth.oidc.keys.clientId`, empty by default, reads the client id from the same Secret instead of the value `auth.oidc.clientId`; the issuer, the scopes, the groups claim, the gate, the administrator group, the refresh interval and the display name are values; no `existingConfigMap`)* |
| local administrator ([ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)) | `localAdmin.existingSecret` | `keys.username`, `keys.password` |
| session key ([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md)) | `session.existingSecret` | `keys.key` |
| the chat's API key *(added 2026-10-04, [ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md) D3)* | ~~`chat.existingSecret`~~ *(amended again 2026-10-04)* each provider's own `chat.providers[].existingSecret` | ~~`chat.keys.apiKey`, default `apiKey`, read into `COWORK_CHAT_API_KEY`; required with `chat.provider: anthropic`~~ that provider's `keys.apiKey`, default `apiKey`, read into `COWORK_CHAT_<ID>_API_KEY`; required for a provider of kind `anthropic`, optional for `openai` (LM Studio takes none); no inline value at all — an `apiKey` in a provider's entry fails the rendering. ~~The provider, its URL, the model, `inside`,~~ A provider's id, name, kind, URL and model and the turn's timeout and steps are values, rendered only while `chat.providers` lists one |

Where an earlier record allows a value rendered from the values file (`database.url`,
`localAdmin.password`), that stays as the throw-away path with its warning; the reference
is the production path.

**D4 — The backend accepts the database as a URL or as components.** `COWORK_DATABASE_URL`
stays; alternatively `COWORK_DATABASE_HOST`, `_PORT`, `_NAME`, `_USER`, `_PASSWORD`,
`_SSLMODE` are read and composed by `config.Load`, so a Secret that has no URL key needs no
copying and no init container. Both set is a configuration error; the password is
URL-escaped when composed. *(Added 2026-10-02: the owner credential of ADR 0021 D2 takes the
same two shapes, `COWORK_DATABASE_OWNER_URL` or the `COWORK_DATABASE_OWNER_*` components.)* The chart maps whichever key set the values name onto whichever
variable set.

**D5 — Requirements the operations page states, not provisions:** ~~a database role that is
not a superuser and has no `BYPASSRLS` ([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D2), owns its database, and may create extensions `unaccent`, `pg_trgm`, `btree_gin`
([ADR 0025](0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md)) — or
the installation creates them beforehand;~~ *(amended 2026-10-02: two roles, ADR 0021 D2 — an
owner role that owns the database — or holds `CREATE` on schema `public`, which since
PostgreSQL 15 only the database's owner has by default, and `CREATE` on the database for the
extensions `unaccent`, `pg_trgm`, `btree_gin`, unless the installation creates them
beforehand; and a
runtime role with `LOGIN` that is not a superuser, has no `BYPASSRLS`, owns nothing and is
not a member of the owner role; the operator creates both);* a bucket of its own with an access key whose policy
reaches that bucket only, never root credentials.

## Consequences

- An administrator points the chart at what the operator produced — by name and by key —
  and copies nothing.
- Backup, high availability and upgrades of both systems belong to their operators; cowork's
  second line is its export ([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
  D4).
- D4 is a backend change: `config.Load` grows the component variables and the composition,
  with tests; the README's table lists both forms.
- The chart's values file grows a `keys` block per reference; `ci/*-values.yaml` cover a
  URL Secret, a component Secret with a ConfigMap, and the throw-away paths.
- A fresh cluster becomes: apply the two examples (adapted), then `helm install`. The
  examples carry Renovate comments for the operator versions so `kubeconform`'s schemas and
  the pinned versions move together.
- The examples are a maintenance surface of their own: an operator's CRD change breaks
  `make examples-lint`, which is intended — it is the moment to update the example or to say
  it is stale.

## Alternatives Considered

- **Optional subcharts** (PostgreSQL, MinIO behind `enabled` flags). One command with a
  database; stateful systems inside an application chart, no HA, no backup, a demo path that
  ends up in production. Lost (and already lost by ADR 0001 D8 for the database).
- **An umbrella chart with operator resources.** One command without polluting the
  application chart; a second chart to maintain, coupled to operator CRD versions. Lost.
- **Maintained example manifests in `deploy/examples/`** — the recommendation. Copyable,
  lint-checked references for CloudNativePG and MinIO. First declined by the owner as
  outside the project's responsibility, then adopted in the limited form of D1 and D2:
  syntax-checked examples, not verified deployments.

## Residual risks

- D4's composition from components has to escape the password correctly; a test with a
  password containing `@`, `:`, `/` and `%` is the proof.
- The examples are checked for syntax, not run; a semantic mistake in them surfaces at the
  installation that copies them, where the start-up checks of ADR 0021 D2 and ADR 0025
  (extensions) refuse to run and name the cause. D5 is the checklist behind the examples.

## References

- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D8 — no stateful system in the chart
- [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md) — the object store
- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2, [ADR 0025](0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md) — the role and the extensions
- [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md), [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md), [ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md) — the other secrets the pattern covers
- [ADR 0038](0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md) D5 — the tests as the reference
- [`deploy/helm/cowork/values.yaml`](../../deploy/helm/cowork/values.yaml), [`backend/internal/config/config.go`](../../backend/internal/config/config.go) — where D3 and D4 land
