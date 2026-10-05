# cowork

[![Build Status](https://github.com/guided-traffic/cowork/actions/workflows/release.yml/badge.svg)](https://github.com/guided-traffic/cowork/actions)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/guided-traffic/cowork/main/.github/badges/coverage.json)](https://github.com/guided-traffic/cowork)
[![Go Report Card](https://goreportcard.com/badge/github.com/guided-traffic/cowork/backend)](https://goreportcard.com/report/github.com/guided-traffic/cowork/backend)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A multi-tenant backlog and kanban board for one person who works on many projects at once,
with an LLM as a co-worker. Tenants keep clients apart, projects group the work, tickets carry
the analysis, the open decisions and the verification, and an LLM such as Claude Code operates
it through the same API people use in the browser — with a personal access token that says who
is accountable.

> **Status: phase 4 — the login through an identity provider — and phase 5 — the LLM interface and
> the chat in the UI — released as `0.3.0`; phase 3 — the UI on the core domain — in progress, its
> parts released with `0.2.0` and `0.3.0`.** Tenants,
> projects and tickets — with links, state transitions, open questions, comments, interest,
> progress, time entries and attachments — the audit record and the event stream exist behind a
> JSON API, tested against PostgreSQL 18, MinIO and Dex. A person logs in through any OpenID Connect
> provider, whose groups decide who gets in and — mapped per tenant — in which role, or with a local
> account: the local administrator the installation's Secret names, or the provider's administrator
> group, creates the first tenant, its administrators add its people, and each person makes their
> own personal access tokens in the session. Claude Code works on the backlog through `cowork-mcp`,
> an MCP server with hooks that runs on the person's machine
> ([Claude Code](docs/operations/claude-code.md)); in the browser an assistant works on the same
> tools as the person's agent, with a model of the providers the operator lists — LM Studio on the
> operator's machine, a server of the OpenAI format or Anthropic — and the capabilities the person
> chooses for it; its acts run at once, and Stop ends a turn at once
> ([the chat](docs/operations/chat.md)). What comes next is
> [the project plan](docs/planning/project-plan.md).

```mermaid
flowchart LR
  B[Browser] --> G[Ingress]
  B -.->|login redirects| I[OpenID Connect<br/>identity provider]
  C[Claude Code] -->|stdio| X[cowork-mcp<br/>personal access token]
  X -->|HTTPS| G
  G -->|/| F[cowork-frontend<br/>nginx + Angular bundle]
  G -->|/api/ /auth/| S
  K[kubelet] -->|/healthz /readyz| S
  M[migrate<br/>init container] -->|owner role| P
  S[cowork-backend<br/>Go API] -->|runtime role| P[(PostgreSQL 18)]
  S -->|attachments| O[(S3-compatible<br/>object storage)]
  S -->|discovery, code, refresh| I
  S -->|the chat's model calls| L[LLM providers<br/>OpenAI format or Anthropic]
```

## ✨ Key features

- 🧩 **Two containers, one origin** — the Go backend serves the JSON API, the nginx frontend serves the Angular bundle and nothing else, and the chart's Ingress routes `/api/` and `/auth/` to the backend and the rest to the frontend, so the browser sees one origin.
- 🎫 **Tickets with stable keys** — `acme/COW-42`: five types, a state matrix that asks for reasons and a verification note, four link types with a cycle check on `blocks` and the prerequisite tree they make, open questions, comments with their history, interest, progress, time entries and attachments.
- 🔑 **Tokens for people and agents** — personal access tokens with a scope and an optional tenant or project restriction, made by the person in a browser session and never by a token; an agent, marked by its token or by `X-Cowork-Agent`, is bound by capabilities and sends an `Idempotency-Key` with every creating `POST`.
- 💬 **An assistant in the browser** — a chat panel whose model works on the same tools as the person's agent, with the capabilities the person chooses (by default not deciding, closing, dropping or recording answers); the providers are a list in the chart, the person picks one, and Stop ends a turn at once.
- 🤖 **Claude Code as a co-worker** — `cowork-mcp`, one static binary per platform, serves fifteen workflow tools over the API with the person's token and runs Claude Code's hooks: a session starts with its project's state and is reminded at its end; a repository finds its project by its normalised git remote, and an unbound one gets a proposal the person confirms.
- 🪪 **Single sign-on through any OpenID Connect provider** — the code flow with PKCE against a provider discovered at start, a gate of allowed groups and an administrator group, per-tenant group mappings that derive memberships, marked grants beside them, the groups read again every fifteen minutes, and tokens held to the same gate; tested against a minimal Dex.
- 🔐 **A login that needs no identity provider** — a local administrator kept in step with a Secret, local accounts created by tenant administrators, Argon2id, sessions in the database behind an `HttpOnly` `__Host-` cookie, an account lockout and an address throttle that answer every failure alike, and an origin-plus-header CSRF check on every write of a session.
- 🏛️ **Administration that leaves nothing behind a token** — members, grants, group mappings, restricted projects and their access lists in the UI; every act that can give access takes a browser session, every act is recorded with the keyed hash of the client's address, and an administrator's change that would leave a tenant without an administrator is refused.
- 🛡️ **Tenants isolated twice** — every query names its tenant, and forced row-level security under a runtime role that owns nothing backs it; the backend refuses a role that could bypass it.
- 📜 **Contract first** — the OpenAPI 3.1 document in `backend/api/` generates the server, is served at `/api/v1/openapi.json` and validates every request; every error is RFC 9457 problem details with a stable `code`.
- 🧾 **Every act on the record** — an append-only audit record of who did what, with the token and the agent; every act made through a token shows it on the ticket — the agent's mark, or the token's name — so nothing a script or a model does reads as the person's own; `ETag` and `If-Match` keep two writers from overwriting each other.
- 📡 **Live updates** — server-sent events per tenant carry keys and versions, never content, filtered by what the reader may see; a reconnect replays what it missed.
- 🗑️ **Deletion that waits thirty days** — a tenant administrator deletes a ticket, never an agent; from then on it answers like a missing one everywhere but the tenant's bin, which restores it as it was, until the purge — a job thirty days later, or an administrator's second confirmation — removes it with its files, keeping in the audit record only its key, who did what and when.
- 🔖 **Saved filters** — the list filters under a name, the person's own or shared with the tenant with its owner beside it, applied, saved and shared from the backlog's filter bar; a value that no longer holds is a warning, and a shared filter that names what the reader cannot see is shown without its conditions.
- 🔎 **Search with snippets** — PostgreSQL full text over titles, bodies, comments, questions and file names, keys by their beginning and titles by trigram, one ranked hit per ticket with the words found marked; a tenant's from its pages, every tenant's of the person from anywhere, each hit held to what the reader may see.
- 📝 **Markdown rendered on the server** — the body, comments, options and answers rendered with goldmark and held to an allow-list by bluemonday: raw HTML shown as text, links with `rel="noopener noreferrer nofollow"`, images only of the ticket's own raster attachments; Angular's sanitiser runs over it again.
- 🔔 **An inbox and the lists across tenants** — a notification for an assignment, a question asked of you, your question answered, a state change or a comment on a ticket you watch, a blocker closed and an urgent need, written with the act and shown from it; a bell with the unread count, live; and "assigned to me" and "open decisions" across every tenant of the person, each item beside its tenant.
- 🗄️ **Migrations under their own role** — embedded SQL applied by an init container as the owner role, serialised across replicas by an advisory lock; the serving container holds only the runtime credential and refuses a schema with pending migrations.
- 🐘 **PostgreSQL 18 and S3** — `uuidv7()` keys and full-text search in PostgreSQL; attachments in any S3-compatible bucket, served only through the backend.
- ⎈ **One Helm chart** — two hardened Deployments, an Ingress that routes the API to the backend, every credential from an existing Secret, no RBAC because neither container talks to the Kubernetes API, and no NetworkPolicy, because network policies are the cluster's.
- 🧪 **Tested in every layer** — Go unit tests; integration and API tests against PostgreSQL 18, MinIO and Dex — and an issuer in the test's own process for what Dex cannot be made to do — with every response checked against the API document; Angular unit tests, chart lint and render, one container scan per image, release tooling check; all as `make` targets CI runs unchanged.
- 🆕 **Newest toolchains** — Go 1.27 and Angular 22, moved by Renovate as grouped updates.
- 🗂️ **Documentation with five homes** — decisions in ADRs, work lists in tickets that get archived, one security page per perspective.
- 🧭 **Decided, then built** — every founding question was put to the owner one at a time and became an ADR before the code that depends on it.

## 📛 Naming conventions

### Environment variables

Every backend setting is `COWORK_<NAME>`; the full table is under [Configuration](#configuration).
The frontend container takes no variable: its nginx configuration is a file in the image
([frontend container](#frontend-container)). The integration tier reads
`COWORK_TEST_DATABASE_URL`, `COWORK_TEST_S3_ENDPOINT`, `_ACCESS_KEY_ID`, `_SECRET_ACCESS_KEY` and
`COWORK_TEST_OIDC_ISSUER`, every one of them required; `make dev-seed` reads
`COWORK_DEV_SEED_DATABASE_URL`; `make dev` takes `COWORK_DEV_ADMIN` and
`COWORK_DEV_ADMIN_PASSWORD`. The development containers take `CONTAINER_BIND` (`127.0.0.1`
`# default`), `POSTGRES_PORT`, `MINIO_PORT` and `DEX_PORT` from `make`. `cowork-mcp` reads
`COWORK_URL`, `COWORK_TOKEN` and `CLAUDE_PROJECT_DIR` ([CLI (cowork-mcp)](#cli-cowork-mcp)); the
plugin's hooks hand it `CLAUDE_PLUGIN_OPTION_COWORK_URL` and `CLAUDE_PLUGIN_OPTION_COWORK_TOKEN`
under those names. A chat provider's variables are `COWORK_CHAT_<ID>_NAME`, `_KIND`, `_URL`,
`_MODEL` and `_API_KEY`, the id of `COWORK_CHAT_PROVIDERS` upper-cased with its dashes as
underscores — `claude-work` is `COWORK_CHAT_CLAUDE_WORK_URL` ([the chat](#the-chat-in-the-ui));
`make dev` takes `COWORK_DEV_CHAT_MODEL`.

### Kubernetes objects (Helm chart)

| Object | Name | Notes |
|---|---|---|
| Backend Deployment and Service | `<fullname>-backend` | `<fullname>` is `<release>-cowork`, or the release name itself when it contains `cowork`; `fullnameOverride` replaces it |
| Frontend Deployment and Service | `<fullname>-frontend` | the Ingress sends every path but `/api/` and `/auth/` here |
| Ingress | `<fullname>` | only with `ingress.enabled`; per host `/api/` and `/auth/` (`Prefix`) to `<fullname>-backend`, `/` (`Prefix`) to `<fullname>-frontend` |
| ServiceAccount | `<fullname>` | shared by both pods, no token mounted |
| Init container of the backend pod | `migrate` | runs `cowork migrate` as the owner role; only with `backend.config.migrateOnStart` |
| Database Secret rendered by the chart | `<fullname>-database`, key `databaseUrl` | only with `database.url` |
| Owner database Secret rendered by the chart | `<fullname>-database-owner`, key `databaseUrl` | only with `database.owner.url` while `backend.config.migrateOnStart` is true |
| Local administrator Secret rendered by the chart | `<fullname>-local-admin`, keys `username` and `password` | only with the inline `localAdmin.username` and `localAdmin.password` |
| Identity provider's client Secret | not rendered: `auth.oidc.existingSecret` names one of yours, key `auth.oidc.keys.clientSecret` (`clientSecret`) and, when set, `auth.oidc.keys.clientId` | only with `auth.oidc.issuer`; the client secret has no inline value |
| A chat provider's key Secret | not rendered: each entry of `chat.providers` names one of yours in `existingSecret`, key `keys.apiKey` (`apiKey`) | one per provider; required for kind `anthropic`; there is no inline value |
| Pod annotations of the backend | `checksum/database-secret`, `checksum/database-owner-secret`, `checksum/local-admin-secret` | only with the inline values (the owner's while `migrateOnStart` is true); a changed value rolls the pods |
| CA volume of the backend | `s3-ca`, mounted at `/etc/cowork/s3-ca` | only with `storage.endpoint` and `storage.tls.caConfigMap` |
| Labels | `app.kubernetes.io/name=cowork`, `app.kubernetes.io/instance=<release>`, `app.kubernetes.io/component=backend\|frontend`, `app.kubernetes.io/version`, `app.kubernetes.io/managed-by=Helm`, `helm.sh/chart` | selectors use `name`, `instance` and `component` |
| Container ports | `http`, `8080` on both containers | `backend.containerPort`; the frontend's is fixed by the nginx configuration |

### Keys and identifiers

| Thing | Pattern | Example |
|---|---|---|
| Tenant slug | 2–63 characters of `a-z`, `0-9` and `-`, not starting with `-` | `acme` |
| Project key | 2–10 characters: an upper-case letter, then `A-Z` and `0-9`; no hyphen; never reused in its tenant | `COW` |
| Ticket key | `<tenant-slug>/<PROJECT>-<number>`; the number counts per project from 1, is never reused and ends at 2147483647 | `acme/COW-42` |
| Short ticket key | `<PROJECT>-<number>`, where the path fixes the tenant | `COW-42` |
| Question | its number within the ticket, from 1 | `…/tickets/42/questions/1` |
| Every other id | a UUIDv7 | `0199a3c2-1d2e-7f00-8000-000000000001` |
| Personal access token | `cwk_` and 43 base62 characters; cowork stores its SHA-256 only | — |
| Session cookie | `__Host-cowork-session`, 43 base64url characters (256 random bits); cowork stores its SHA-256 only | `HttpOnly; Secure; SameSite=Lax; Path=/`, no `Domain` |
| Login state cookie | `__Host-cowork-oidc`: the state, the nonce and the PKCE verifier of a login through the identity provider, the path to return to and the time, sealed with AES-256-GCM; cleared by the callback | `HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=600`, no `Domain` |
| Username | 1–63 characters of `a-z`, `0-9`, `.`, `_` and `-`, starting with a letter or a digit; unique in the installation; the identity is `local:<username>`, which `POST …/members` takes as well | `ada.lovelace` |
| Person of the identity provider | the issuer and the ID token's `sub`; no username | — |
| Group name | as the provider's groups claim carries it, matched exactly, case and all; in a mapping 1–256 characters with no white space at either end | `cowork-users` `# example` |
| System actor | `system:<name>` in an audit row: `login`, `bootstrap`, `identity-provider`, and the jobs `idempotency-expiry`, `session-expiry`, `login-expiry`, `notification-expiry`, `ticket-purge` | `system:identity-provider` |
| Local account origin | `config` — the one account `COWORK_LOCAL_ADMIN_*` names — or `tenant` — one a tenant administrator created and that tenant manages | — |
| Agent header | `X-Cowork-Agent: <name>/<model>/<session>`, each part 1–64 printable ASCII characters | `claude-code/opus/7f3a` |
| Agent header of the chat | `chat/<model>/<conversation>`: the picked provider's model, its `/` written `:`, and the conversation's id the browser made | `chat/qwen:qwen3-30b-a3b-2507/0199a3c2-1d2e-7f00-8000-000000000042` |
| Chat provider id | 1–32 characters of `a-z`, `0-9` and `-`, a dash neither first nor last; named once in `COWORK_CHAT_PROVIDERS` | `lmstudio`, `claude-work` |
| The chat's browser storage | `localStorage`: `cowork.chat.<person id>` (`open` or `closed`), `cowork.chat.provider.<person id>` (the picked provider's id) | — |
| Agent header of `cowork-mcp` | `<client>/<model>/<session>`: the MCP client's name (`claude-code` in the hooks), the hook's model or `unknown`, the hook's session id or eight hex characters per process; `cowork-mcp/unknown/token-check` and `cowork-mcp/unknown/lookup` for the two subcommands | `claude-code/unknown/1f0c9a2b` |
| Repository identity | `<host>[:<port>]/<path>` of a git remote: scheme, user, a default port, `.git` and trailing slashes removed, the host lower-cased, the path's case kept; unique per tenant with the sub-directory | `github.com/acme/app` for `git@github.com:acme/app.git` |
| Repository sub-directory | relative to the repository root, `/`-separated, no `..`; empty for the whole repository | `services/billing` |
| Proposed project key | the initials of the repository name's parts (`-`, `_`, `.`), else its first three letters, upper-cased; `2`, `3`, … appended while the key is taken | `VO` for `valkey-operator` |
| MCP tool | `session_start`, `get_ticket`, … ([the tools](#the-tools)); in Claude Code `mcp__plugin_cowork_cowork__<tool>` through the plugin, `mcp__cowork__<tool>` for a server added by hand as `cowork` | `mcp__plugin_cowork_cowork__get_ticket` |
| Commit strings for a ticket | the subject ends with `(<PROJECT>-<number>)`, the body ends with the trailer `Cowork-Ticket: <key>`, the branch is `<type>/<PROJECT>-<number>-<slug>` | `feat/COW-42-export-attachments` |
| Request id | `X-Request-Id`, a UUIDv7 the backend makes (an inbound one is ignored); the same value is `request_id` in a problem body and in the request log | — |
| Problem type | `https://cowork.dev/problems/<code, hyphenated>` | `https://cowork.dev/problems/not-found` |
| Attachment object | `<tenant-id>/<attachment-id>` in the configured bucket, derived, never stored | — |
| Event channel | the PostgreSQL `NOTIFY` channel `cowork_events` | — |
| Event names | `ticket.changed` (uploads included), `comment.changed`, `question.changed`, `link.changed`, `interest.changed`, `membership.changed`; on a person-level stream (`?me=true`) also `inbox.changed`; the control events `resync` and `unavailable` | — |

### Development environment

| Thing | Name | Notes |
|---|---|---|
| PostgreSQL container | `cowork-postgres`, `postgres:18` on `localhost:5432` | `make postgres-up`; `POSTGRES_CONTAINER=` and `POSTGRES_PORT=` move it |
| Development database | `cowork`, owned by `cowork_owner`, served as `cowork_app` | created by `make postgres-up`; each password is the role's name |
| MinIO container | `cowork-minio`, `cgr.dev/chainguard/minio` pinned by digest, on `localhost:9000` | `make minio-up`; root keys `cowork` / `cowork-secret`, development values; `MINIO_CONTAINER=` and `MINIO_PORT=` move it |
| Dex container | `cowork-dex`, `ghcr.io/dexidp/dex` pinned by tag and digest (`DEX_IMAGE`), on `localhost:5556`; the issuer `http://localhost:5556/dex`, the client `cowork` with the secret `cowork-dev-dex-secret` | `make dex-up`, configured from [`hack/dex/config.yaml`](hack/dex/config.yaml), copied in; `DEX_CONTAINER=` and `DEX_PORT=` move it; keeps nothing, so `make dex-down` loses nothing |
| Dex users | `ada@example.com` (`cowork-admins`, `cowork-users`), `bob@example.com` (`cowork-users`, `team-red`), `cyd@example.com` (`cowork-users`), `dan@example.com` (`team-red`), each with the password `dev-only-dex` | every credential of Dex is development-only and public in this repository |
| Container binding | `CONTAINER_BIND=127.0.0.1` `# default` | `make postgres-up`, `minio-up` and `dex-up` publish their ports on the loopback address only; a container made before keeps its binding until it is removed |
| All three at once | `make dev-up` | PostgreSQL, MinIO and Dex, what `make dev` and the integration tier need |
| Integration run | roles `cowork_it_owner` and `cowork_it_app`, database `cowork_it_<unix-nanoseconds>`, bucket `cowork-it-<unix-nanoseconds>` | one database and one bucket per run; at the end the database is dropped and the bucket emptied and removed |
| End-to-end stack | the network `cowork-e2e` and the containers `cowork-e2e-postgres`, `-minio`, `-dex`, `-backend`, `-frontend`, `-ingress` (`E2E_NAME=` renames them); the UI on `https://localhost:18443` (`E2E_PORT`), Dex on `http://localhost:5557/dex` (`E2E_DEX_PORT`); the database `cowork_e2e`, the bucket `cowork-e2e` | `make e2e` makes and removes it, `make e2e-up` and `make e2e-down` keep it between runs; the local administrator `e2e-admin` with the password `e2e-only-cowork`, development values |
| End-to-end data | the tenants `e2e` (a project per test, key `E` and seven random characters; the mapping `team-red` → `member`) and `e2e-visual` (the project `VIEW` of the dark-mode screenshot), the token `e2e-seed`, local accounts `e2e-<random>` | made by the suite through the API ([testing.md](docs/developer/testing.md#end-to-end-tests)) |
| Development seed | person `dev`, tenant `dev`, an admin membership, a token named `dev-seed` | `make dev-seed`; every run prints a new token once |
| Development stack | `make dev`: the backend on `localhost:8080`, the UI on `https://localhost:4200` (self-signed), the local administrator `dev` with the password `dev-only-cowork`, Dex as the identity provider (allowed `cowork-users`, administrator group `cowork-admins`, the button *Sign in with Dex*), the group mapping `team-red` → `member` in the tenant `dev`, the bucket `cowork-dev`, a second person `sam`, demo projects `COW`, `OPS`, `WEB` | two ways in: the form as `dev`, or *Sign in with Dex* as one of the four Dex users; state in `.dev/` (untracked): `token` (the demo data's), `session-key`, `backend.log`, the built `cowork`, and the PrimeUI key in `primeui-license`; `make dev-reset` empties the database |

### Files and images

| Thing | Pattern | Example |
|---|---|---|
| Migration | `backend/internal/store/migrations/NNNNNN_<snake_name>.up.sql`, versions `1..n` without a gap, no down files | `000001_tenants.up.sql` |
| Container images | `guidedtraffic/cowork-backend:<semver>`, `guidedtraffic/cowork-frontend:<semver>` | both carry the release version |
| Helm chart | `cowork` in the repository `https://guided-traffic.github.io/cowork/`, source `deploy/helm/cowork/`; chart version = release version | `helm install cowork cowork/cowork --version 0.1.0` |
| Go module | `github.com/guided-traffic/cowork/backend` | — |
| Angular project | `frontend`, output `frontend/dist/frontend/browser/` | — |
| Ticket (interim, in this repository) | `docs/tickets/NNN-<kebab-slug>.md`, `id: T<n>` | rules in [docs/tickets/README.md](docs/tickets/README.md) |
| API document | `backend/api/openapi.yaml` and one file per path family, bundled by `make generate` into `backend/api/openapi.gen.json` | served at `/api/v1/openapi.json` |
| `cowork-mcp` release binary | `cowork-mcp-<version>-<os>-<arch>`, `.exe` on windows, beside it `<file>.sha256`; os `linux`, `darwin`, `windows`, arch `amd64`, `arm64`; attached to the GitHub release, each with a build provenance attestation (`gh attestation verify <file> --repo guided-traffic/cowork`) | `cowork-mcp-0.3.0-darwin-arm64` |
| Binding file | `.cowork.yaml`, the nearest at or above the working directory within the repository; schema at `/api/v1/schemas/cowork-yaml.json` | `tenant: acme`, `project: APP` |
| Session memory of `cowork-mcp` | `<user cache directory>/cowork-mcp/<host and path of COWORK_URL>_<tenant>_<PROJECT>.json`, other characters than letters, digits, `.` and `-` as `_` | `~/.cache/cowork-mcp/cowork.example.com_acme_APP.json` |
| Claude Code plugin | the marketplace `cowork` in `.claude-plugin/marketplace.json`, the plugin `cowork` in `claude/cowork/`; its options `cowork_url` and `cowork_token` | `claude plugin install cowork@cowork` |

### HTTP

The chart's Ingress routes `/api/` and `/auth/` to the backend Service and every other path to the
frontend Service ([ADR 0001](docs/adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3).

| Path | Backend | Frontend (nginx) |
|---|---|---|
| `/healthz` | liveness, through the backend Service | the Ingress sends it here: nginx's own health, `{"status":"ok"}` |
| `/readyz` | readiness: a database ping, through the backend Service | the Ingress sends it here, and nginx answers it with the UI shell (`index.html`, `200`), which says nothing about the backend |
| `/api/v1/…` | the JSON API, routed here by the Ingress; errors are RFC 9457 `application/problem+json` with a stable `code` | not routed here; a request that arrives all the same gets `404` with a problem naming the cause |
| `/auth/options`, `/auth/local`, `/auth/oidc/login`, `/auth/callback`, `/auth/logout` | the browser's login flows, routed here like `/api/`, in the API document; `/auth/callback` is the redirect URI to register at the identity provider | the same `404` problem |
| `/api/v1/tenants/<slug>/events` | the event stream, answered with `X-Accel-Buffering: no`; `?me=true` makes it the person-level stream | — |
| `/api/v1/tenants/<slug>/chat` | a turn of the chat, a `POST` answered as a stream with `X-Accel-Buffering: no` | — |
| hashed bundles | — | served with `Cache-Control: public, max-age=31536000, immutable` |
| everything else | `404` problem details | `index.html` with `Cache-Control: no-store` |

| Media type | Where |
|---|---|
| `application/json` | request bodies and responses |
| `application/problem+json; charset=utf-8` | every error |
| `multipart/form-data` | an upload: the part `file`, optionally the part `comment_id` |
| `text/csv` | on `Accept: text/csv`: the audit record, the tenant's time entries, the time report |
| `text/markdown; charset=utf-8` | a ticket's canonical Markdown and its context |
| `text/event-stream` | the event stream, a turn of the chat |

## 📚 Documentation

| Document | What it is for |
|---|---|
| [docs/developer/](docs/developer/README.md) | Contributor entry point and how the code works: layout, package map, architecture, build/test/lint matrix, testing, CI and release, checklists, conventions |
| [docs/developer/development-credentials.md](docs/developer/development-credentials.md) | Every development-only username, password, key and token of `make dev`, its containers and the test tiers, with the file that sets it |
| [docs/operations/](docs/operations/README.md) | Installing and running: the database roles, the Secrets and the object storage, the Ingress and its controller's settings; runtime behaviour, the limits, what answers what, the event stream behind an Ingress; [Claude Code](docs/operations/claude-code.md) against an installation |
| [docs/security/](docs/security/README.md) | The security architecture, one page per perspective; [SECURITY.md](SECURITY.md) to report a vulnerability |
| [docs/adr/](docs/adr/README.md) | Why cowork is the way it is |
| [OpenID Connect Core 1.0](https://openid.net/specs/openid-connect-core-1_0.html), [Dex](https://dexidp.io/docs/) | The standard the login through an identity provider follows, and the issuer it is developed and tested against |
| [docs/tickets/](docs/tickets/README.md) | The interim work lists and their rules |
| [docs/planning/](docs/planning/) | The project plan — consumed into ADRs and tickets as work proceeds; the question catalog and the VS Code workflow plan are consumed already |
| [CLAUDE.md](CLAUDE.md) | The working rules for an LLM session in this repository |

## 🚀 Fast start

### Prerequisites

Go 1.27, Node.js 26 with npm, Docker (for the local PostgreSQL and MinIO and the images),
Helm 3 or 4, `openssl`. `make help` lists every target.

### Run it locally

The quickest way to see cowork, with demo data and the UI reloading as you edit:

```bash
make dev                # PostgreSQL, MinIO, Dex, the backend, demo data and the UI on https://localhost:4200 — Ctrl-C stops it
```

Sign in as `dev` with the development-only password `dev-only-cowork`, or with *Sign in with Dex*
as `ada@example.com` (a global administrator), `bob@example.com` (a member of the tenant `dev` by
his group), `cyd@example.com` (behind the gate, in no tenant until granted) or `dan@example.com`
(refused at the gate), each with the development-only password `dev-only-dex`. The dev server uses a
self-signed certificate — HTTPS, because Safari stores no `Secure` session cookie from
`http://localhost` — so the browser asks about it once. The parts by hand:

```bash
make postgres-up        # postgres:18 on :5432 — database cowork, roles cowork_owner (migrates) and cowork_app (serves); make dev-up adds MinIO and Dex
make dev-seed           # migrates; then a person, the tenant "dev", an admin membership and a token, printed once
make run                # the backend on :8080: migrates as cowork_owner, serves as cowork_app (text logs)
make frontend-serve     # the Angular dev server on :4200, /api and /auth proxied to :8080 — the stand-in for the Ingress
```

`make run` needs no server key: it makes a throw-away one per start, so list cursors from an
earlier run answer `invalid_cursor`. `make dev-seed` makes a token: an agent's token with scope
`write` and every capability, so its creating `POST`s carry an `Idempotency-Key`.

```bash
TOKEN=cwk_…             # the token make dev-seed printed
AUTH="Authorization: Bearer $TOKEN"
curl -s localhost:8080/readyz                          # {"status":"ready"}
curl -s -H "$AUTH" localhost:8080/api/v1/me            # the person and the tenant dev
curl -s -H "$AUTH" -H "Idempotency-Key: $(uuidgen)" -H 'Content-Type: application/json' \
  -d '{"key":"COW","name":"cowork"}' localhost:8080/api/v1/tenants/dev/projects
curl -s -H "$AUTH" -H "Idempotency-Key: $(uuidgen)" -H 'Content-Type: application/json' \
  -d '{"type":"task","title":"Try cowork","severity":"low","security":"none","effort":"XS"}' \
  localhost:8080/api/v1/tenants/dev/projects/COW/tickets   # the ticket dev/COW-1
curl -s -H "$AUTH" localhost:8080/api/v1/tickets/dev/COW-1                       # by its key
curl -s -H "$AUTH" localhost:8080/api/v1/tenants/dev/projects/COW/tickets/1/markdown
open https://localhost:4200                            # the UI, after make dev; sign in as dev
```

To try the login — a person in a browser session — start the backend with a local
administrator and the origin the browser sees (`make dev` uses `https://localhost:4200`).
Both variables are required together, the password is at least twelve characters, and the first
thing the administrator does is create the first tenant:

```bash
COWORK_BASE_URL=http://localhost:4200 COWORK_LOCAL_ADMIN_USERNAME=admin \
  COWORK_LOCAL_ADMIN_PASSWORD='a long development password' make run
curl -si -H 'Origin: http://localhost:4200' -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"a long development password"}' localhost:8080/auth/local
# 200 {"password_change_required":false} and  Set-Cookie: __Host-cowork-session=…; HttpOnly; Secure; SameSite=Lax; Path=/
COOKIE='__Host-cowork-session=…'   # the value of that cookie
curl -s -H "Cookie: $COOKIE" localhost:8080/api/v1/me                      # the person; global_admin is true
curl -s -H "Cookie: $COOKIE" -H 'Origin: http://localhost:4200' -H 'X-Requested-With: cowork' \
  -H 'Content-Type: application/json' -d '{"slug":"acme","name":"Acme"}' localhost:8080/api/v1/tenants
```

Every write of a session carries the `Origin` of `COWORK_BASE_URL` and `X-Requested-With: cowork`
([CSRF](docs/security/csrf.md)); a token's request carries neither.

The login through Dex by hand is what `make dev` does: `make dex-up`, then `make run` with
`COWORK_OIDC_ISSUER=http://localhost:5556/dex`, `COWORK_OIDC_CLIENT_ID=cowork`,
`COWORK_OIDC_CLIENT_SECRET=cowork-dev-dex-secret`, `COWORK_OIDC_ALLOWED_GROUPS=cowork-users` and
`COWORK_BASE_URL=https://localhost:4200` — the redirect URI Dex knows for development — and the UI
from `make frontend-serve NG_SERVE_FLAGS=--ssl` ([`hack/dev.sh`](hack/dev.sh),
[`hack/dex/config.yaml`](hack/dex/config.yaml)).

Without object storage the backend refuses uploads (`501 uploads_disabled`). To try
attachments, start MinIO, create a bucket with any S3 client — no target creates one — and
hand the backend its keys:

```bash
make minio-up           # MinIO on :9000, root keys cowork / cowork-secret
mc alias set local http://localhost:9000 cowork cowork-secret && mc mb local/cowork
COWORK_S3_ENDPOINT=http://localhost:9000 COWORK_S3_BUCKET=cowork \
  COWORK_S3_ACCESS_KEY_ID=cowork COWORK_S3_SECRET_ACCESS_KEY=cowork-secret make run
```

### Run the tests

```bash
make test                       # backend unit + frontend unit
make dev-up                     # the integration tier needs PostgreSQL, MinIO and Dex
make test-integration           # a database and a bucket of its own per run
make e2e-browsers               # once: Playwright's Chromium and WebKit
make docker-build e2e           # the end-to-end tier: both images behind the Ingress stand-in, a stack of its own
make lint frontend-lint helm-lint
```

### Build the images

```bash
make docker-build       # guidedtraffic/cowork-backend:latest and guidedtraffic/cowork-frontend:latest
```

### Install on Kubernetes

The database comes first: one PostgreSQL 18 database, an owner role that owns it and a
runtime role that owns nothing — both created by you, as
[installation.md](docs/operations/installation.md#the-database-and-its-two-roles) shows.

```bash
kubectl create namespace cowork
kubectl -n cowork create secret generic cowork-database \
  --from-literal=databaseUrl='postgres://cowork_app:CHANGE-ME@postgres:5432/cowork?sslmode=require'
kubectl -n cowork create secret generic cowork-database-owner \
  --from-literal=databaseUrl='postgres://cowork_owner:CHANGE-ME@postgres:5432/cowork?sslmode=require'
kubectl -n cowork create secret generic cowork-session \
  --from-literal=sessionKey="$(openssl rand -base64 32)"
helm repo add cowork https://guided-traffic.github.io/cowork/
# 0.4.0, the class and the host are examples; 0.4.0 is the first release whose Ingress routes the API
helm upgrade --install cowork cowork/cowork --version 0.4.0 -n cowork \
  --set database.existingSecret=cowork-database \
  --set database.owner.existingSecret=cowork-database-owner \
  --set session.existingSecret=cowork-session \
  --set ingress.enabled=true --set ingress.className=my-ingress-class \
  --set 'ingress.hosts[0].host=cowork.example.com'
```

The Ingress sends `/api/` and `/auth/` to the backend and everything else to the frontend; the chart
and the frontend image of a release move together. Its controller must pass a body above the
backend's limit and wait past its request timeout — the chart's notes print both figures —, pass
`text/event-stream` unbuffered, and write the address it saw into `X-Forwarded-For`; the chart sets
none of that ([expose it](docs/operations/installation.md#expose-it)). ingress-nginx is retired
(March 2026, no security fixes since): do not install it for a new installation. There is no UI
without a route like this: a port-forward to the frontend alone serves the shell without its API.

The login needs a local administrator and the public URL: a Secret with its username and
password, and the origin the browser shows. The administrator logs in, creates the first tenant
(or `bootstrap.tenant.slug` and `.name` create it at start) and the accounts of its people:

```bash
kubectl -n cowork create secret generic cowork-local-admin \
  --from-literal=username=admin --from-literal=password="$(openssl rand -base64 24)"
helm upgrade cowork cowork/cowork -n cowork --reuse-values \
  --set localAdmin.existingSecret=cowork-local-admin \
  --set backend.config.baseURL=https://cowork.example.com \
  --set bootstrap.tenant.slug=acme --set bootstrap.tenant.name="Acme Corp"
```

With an identity provider — instead of the local administrator, or beside it — register a
confidential client there with the redirect URI `https://cowork.example.com/auth/callback`, put its
secret into a Secret, and name the issuer, the client and the gate:

```bash
kubectl -n cowork create secret generic cowork-oidc --from-literal=clientSecret='CHANGE-ME'
helm upgrade cowork cowork/cowork -n cowork --reuse-values \
  --set backend.config.baseURL=https://cowork.example.com \
  --set auth.oidc.issuer=https://login.example.com/realms/acme \
  --set auth.oidc.clientId=cowork --set auth.oidc.existingSecret=cowork-oidc \
  --set 'auth.oidc.allowedGroups={cowork-users}' --set auth.oidc.adminGroup=cowork-admins
```

Every value above is an example. The members of the administrator group are global
administrators; with `bootstrap.tenant.slug` and `.name` the start maps the group to the first
tenant's `admin` role, and the installation needs no local administrator. The backend refuses to
start while it cannot fetch the provider's discovery document
([installation.md](docs/operations/installation.md#the-identity-provider)).

Behind an Ingress the login throttle has to be told which networks are the proxies, or it counts
one address for every browser behind a controller pod:
`--set backend.config.trustedProxies=<the Ingress controller's network>`. Every other pod inside
that network that reaches the backend can then choose its address, which only a network policy of
the cluster's prevents — the chart ships none
([the client address](docs/operations/installation.md#the-client-address-and-the-trusted-proxies)).

Attachments need an S3-compatible bucket and three more values; without them uploads are
refused. Without a local administrator or an identity provider nobody can log in. A release publishes the chart and both images (`guidedtraffic/cowork-backend`,
`guidedtraffic/cowork-frontend` on Docker Hub) with one version; the chart's image tags follow
its `appVersion`. Details — the roles, the Secrets,
the object storage, the Ingress annotations, the CloudNativePG note:
[docs/operations/installation.md](docs/operations/installation.md).

<details>
<summary>Upgrade and uninstall</summary>

```bash
helm repo update cowork
helm upgrade cowork cowork/cowork --version <new> -n cowork --reuse-values
helm uninstall cowork -n cowork      # the database and the bucket are left untouched
```

The new backend pods migrate the schema in their init container before the server starts; see
[docs/operations/runtime.md](docs/operations/runtime.md#the-migration-run).

</details>

### Connect Claude Code

`cowork-mcp` of the installation's release on the `PATH`, a token from the UI's token page —
an agent token with `write` scope — and the plugin of this repository:

```bash
cowork-mcp version                                   # from the release assets, or make build-mcp
COWORK_URL=https://cowork.example.com COWORK_TOKEN=cwk_… cowork-mcp token check   # example values
claude plugin marketplace add guided-traffic/cowork
claude plugin install cowork@cowork                  # then /plugin configure cowork@cowork: the URL and the token
cd ~/src/app && claude                               # the session starts with the repository's project, or a proposal
```

The setup by hand, the token choices, the `CLAUDE.md` block and `.cowork.yaml`:
[docs/operations/claude-code.md](docs/operations/claude-code.md).

## 📖 Reference

### Configuration

Every variable is read once at backend start. Invalid values and missing required ones end the
process with exit code 1: every problem the configuration has is listed together, and what only
`serve` or `migrate` needs is checked once the rest passes. An error names the variable and
quotes a rejected setting such as a size or a duration, never the value of a URL, a key or a
secret. Source: [`backend/internal/config/config.go`](backend/internal/config/config.go). A
size is a number of bytes or a number with `KiB`, `MiB` or `GiB`; a duration is a Go duration
(`30s`, `5m`). Where a limit takes `0`, `0` switches it off —
[runtime.md, limits](docs/operations/runtime.md#limits) says what that costs.

**Server and database**

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_DATABASE_URL` | — (required) | `postgres://cowork_app:…@postgres:5432/cowork?sslmode=require` `# example` | The runtime role's connection URL; every request runs as this role. It must not be a superuser, have `BYPASSRLS`, own a relation of the schema or be a member of the owner role — `serve` and `migrate` refuse it otherwise. `pool_max_conns=<n>` in the URL sizes the connection pool. **Security:** a credential: from a Secret. cowork never logs it; a URL pgx cannot parse appears in the startup error with its password masked, which pgx does on a best-effort basis |
| `COWORK_DATABASE_OWNER_URL` | empty `# default` | `postgres://cowork_owner:…@postgres:5432/cowork?sslmode=require` `# example` | The owner role's URL, which the migrations run under; it must name another role than `COWORK_DATABASE_URL`. Required by `cowork migrate`, and by `cowork serve` while `COWORK_MIGRATE_ON_START` is `true`. **Security:** the owner can switch row-level security off, so a serving process that holds this URL loses the second line of tenant isolation against its own compromise. The chart hands it to the `migrate` init container only |
| `COWORK_MIGRATE_ON_START` | `true` `# default` | `true`, `false` | `serve` applies pending migrations before it listens; with `false` it refuses to start while migrations are pending. The chart sets `false` and migrates in an init container |
| `COWORK_SESSION_KEY` | — (required by `serve`) | standard base64 of at least 32 bytes, `openssl rand -base64 32` | The server key: keys derived from it sign the list cursors, key the fingerprint of an idempotent request and the hashes of a client's address — the login throttle's and the one every audit row of a request carries — and seal the identity provider's login state and refresh tokens. Every replica needs the same key. A new key invalidates the cursors clients hold (`400 invalid_cursor`), fails the logins through the provider under way, gives every address another hash — the login throttle counts it anew, and audit rows before and after cannot be compared —, refuses the retry of an idempotent request keyed before it (`422 idempotency_mismatch`), and ends each session of the provider that holds a refresh token at its next refresh — no previous key is kept to open what the old one sealed ([ADR 0031](docs/adr/0031-server-side-sessions-in-an-httponly-cookie.md) D1; [rotating it](docs/operations/installation.md#the-secrets)); it signs no session, and a session of the local login survives it. **Security:** a secret: from a Secret, never echoed; the chart has no inline value for it. With the database, it opens the stored refresh tokens ([H-27](docs/security/identity-provider.md#h-27)) and reverses the audit rows' IPv4 hashes ([H-30](docs/security/tokens.md#h-30)). `make run` makes a throw-away one |
| `COWORK_LISTEN_ADDR` | `:8080` `# default` | `host:port` | The backend listener for API and health |
| `COWORK_LOG_LEVEL` | `info` `# default` | `debug`, `info`, `warn`, `error` | Minimum level |
| `COWORK_LOG_FORMAT` | `json` `# default` | `json`, `text` | `text` for a terminal |
| `COWORK_SHUTDOWN_TIMEOUT` | `15s` `# default` | a positive duration | Drain bound after `SIGTERM`; the event streams end as the drain begins. A drain that outlasts it ends the process with exit 1. Keep it below the pod's grace period |
| `COWORK_BASE_URL` | empty `# default` | `https://cowork.example.com` `# example` | The public URL, as the browser shows it: an origin — `http` or `https`, a host, an optional port, no path, no query. **Required while `COWORK_LOCAL_ADMIN_USERNAME` or `COWORK_OIDC_ISSUER` is set.** The CSRF check compares the `Origin` (or `Referer`) of every write of a session, and of the local login, with it exactly ([CSRF](docs/security/csrf.md)); a URL the browser does not show makes every such write `403 csrf`, and without one no write of a cookie passes. The identity provider's redirect URI is this URL and `/auth/callback`, and its logout returns to `/login`. **Security:** nothing secret; a wrong value locks the browser out, never lets another origin in |

**Login, sessions and accounts** ([ADR 0031](docs/adr/0031-server-side-sessions-in-an-httponly-cookie.md),
[0032](docs/adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md),
[0033](docs/adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md),
[0035](docs/adr/0035-personal-access-tokens.md) D4, [0037](docs/adr/0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md)).
Chart values are in [Helm chart values](#helm-chart-values); the pages are
[sessions](docs/security/sessions.md), [local accounts](docs/security/local-accounts.md) and
[CSRF](docs/security/csrf.md).

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_LOCAL_ADMIN_USERNAME` | empty `# default` | `admin` `# example` | With `COWORK_LOCAL_ADMIN_PASSWORD`: the one account the configuration keeps, a global administrator — it creates tenants and holds no role in any until it grants itself one — and a full account (`local:<username>`). Synchronised at every start, after the migrations, under an advisory lock: created; re-hashed with all its sessions ended when the password changed; deactivated, its tokens revoked and its sessions ended, when both variables are empty; never deleted. Both set or both empty — one alone refuses the start, naming the missing variable. Needs `COWORK_BASE_URL`. 1–63 characters of `a-z`, `0-9`, `.`, `_`, `-`, starting with a letter or a digit |
| `COWORK_LOCAL_ADMIN_PASSWORD` | empty `# default` | at least `COWORK_PASSWORD_MIN_LENGTH` characters | The account's password, read as it is — spaces included. **Security:** a secret: from a Secret, never echoed or logged; anyone who can read the pod spec or the Secret can read it. A leaked one stays valid until the Secret is rotated **and** the backend restarted; that rotation also unlocks a locked administrator and ends its sessions. The account's password cannot be changed in the UI: this is its source |
| `COWORK_BOOTSTRAP_TENANT_SLUG` | empty `# default` | `acme` `# example` | With `COWORK_BOOTSTRAP_TENANT_NAME`: while no tenant exists, a start creates this tenant, gives the local administrator, when one is configured, a marked grant as its `admin`, and maps `COWORK_ADMIN_GROUP`, when one is set, to its `admin` role; once a tenant exists these variables do nothing, whatever they say. Both or neither, and only with the local administrator or an administrator group, which becomes the tenant's first administrator |
| `COWORK_BOOTSTRAP_TENANT_NAME` | empty `# default` | `Acme Corp` `# example` | The tenant's name, 1–200 characters |
| `COWORK_PASSWORD_MIN_LENGTH` | `12` `# default` | `8` to `1024` | The shortest password of a local account, counted in characters; the policy is length only — no character classes, no history — and it holds for the local administrator too. Below `8` the start is refused |
| `COWORK_LOGIN_LOCKOUT` | `window` `# default` | `window`, `admin` | `window`: a username locked by failures is free again when the 15-minute window passes. `admin`: it stays locked until a tenant administrator unlocks it (`DELETE …/accounts/{username}/lockout`) — or, for the local administrator, until the Secret is rotated and the backend restarted. **Security:** `admin` lets anyone who knows a username keep its account locked |
| `COWORK_LOGIN_MAX_FAILURES` | `5` `# default` | a count; `0` never locks | Failed attempts of one username within fifteen minutes that lock it — a username nobody has too, so neither the answer nor the lock says whether an account exists |
| `COWORK_LOGIN_ADDRESS_LIMIT` | `20` `# default` | a count; `0` disables | Login attempts of one client address — an IPv6 client by its /64 — within a minute before `429 too_many_attempts`. The client address is the TCP peer's unless the peer is inside `COWORK_TRUSTED_PROXIES` ([H-17](docs/security/local-accounts.md#h-17)); with that list empty, behind an Ingress the peer is a controller pod and the limit holds for every browser behind it |
| `COWORK_TRUSTED_PROXIES` | empty `# default` | comma-separated CIDRs, IPv4 and IPv6; `10.244.0.0/16,fd00:10:244::/48` `# example` | The networks of the proxies in front of the backend. The client address — which the login throttle counts and whose keyed hash every audit row of a request carries — is found by walking `X-Forwarded-For` from the right: from the TCP peer, while the current address is inside these networks the entry to its left becomes the current one; the first address outside them is the client, and nothing to its left is read. Empty: the peer is the client and the header is never read. A single host is `/32` or `/128`; an entry that is no CIDR refuses the start, naming the variable and that entry. The proxy in front of the backend is the Ingress controller. **Security:** name the proxies and no more — a client inside a trusted network chooses its own address, which defeats the throttle and lets it fill another client's bucket; a network that holds other pods lets each of them that reaches the backend do the same, which only a network policy of the cluster's prevents — the chart ships none; an empty list leaves one address for every browser behind a controller pod ([installation.md](docs/operations/installation.md#the-client-address-and-the-trusted-proxies)) |
| `COWORK_SESSION_LIFETIME` | `12h` `# default` | a positive duration | The absolute lifetime of a session; it is also the cookie's `Max-Age` |
| `COWORK_SESSION_IDLE` | `2h` `# default` | a positive duration | How long a session may lie unused; a request within it extends the session up to the lifetime. The idle clock moves at most once a minute |
| `COWORK_TOKEN_DEFAULT_LIFETIME` | `2160h` (90 days) `# default` | a positive duration, not above the maximum | The lifetime of a token whose creator named none |
| `COWORK_TOKEN_MAX_LIFETIME` | `8760h` (one year) `# default` | a positive duration | The longest lifetime a token may have; a longer request is shortened to it and the answer says what the token got |

**Identity provider** ([ADR 0029](docs/adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md),
[0030](docs/adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md),
[0031](docs/adr/0031-server-side-sessions-in-an-httponly-cookie.md) D1, D3,
[0035](docs/adr/0035-personal-access-tokens.md) D8). `COWORK_OIDC_ISSUER` turns the login through
an OpenID Connect provider on; every other variable here is read only with it, and set without it
refuses the start, naming itself. Chart values are `auth.oidc.*` in
[Helm chart values](#helm-chart-values); the page is
[identity provider](docs/security/identity-provider.md).

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_OIDC_ISSUER` | empty `# default` | `https://login.example.com/realms/acme` `# example` | The issuer. `https://`, or `http://` on a loopback host (`localhost`, `127.0.0.0/8`, `::1`) for development; no user, query or fragment. Kept as written: it must equal the `issuer` the discovery document names, trailing slash and all. The backend fetches `<issuer>/.well-known/openid-configuration` at every start — no redirect followed, at most 1 MiB — and **refuses to start when it cannot**, when the document's authorization, token, keys or UserInfo endpoint is neither `https` nor `http` on a loopback host, or when it names no signature algorithm cowork verifies (the asymmetric ones; `RS256` when it names none); an `end_session_endpoint` that breaks the rule is dropped with a warning. Requires `COWORK_BASE_URL` — the redirect URI to register at the provider is `COWORK_BASE_URL` + `/auth/callback` — and the client's id and secret |
| `COWORK_OIDC_CLIENT_ID` | — (required with the issuer) | `cowork` `# example` | cowork's client at the provider |
| `COWORK_OIDC_CLIENT_SECRET` | — (required with the issuer) | — | The client's secret, read as it is; a public client without a secret is not supported. A secret the provider no longer accepts (`invalid_client`) logs nobody out: sessions are served on their groups, and the log says so at error level. **Security:** a secret: from a Secret, never echoed; the chart reads it from `auth.oidc.existingSecret` only. With it, the server key and the database, the stored refresh tokens can be redeemed at the provider ([H-27](docs/security/identity-provider.md#h-27)) |
| `COWORK_OIDC_SCOPES` | `openid profile email groups offline_access` `# default` | separated by spaces or commas; `openid` among them | The scopes a login asks for. `offline_access` brings the refresh token the groups refresh needs — most providers issue one only for it; without one a session never reads the groups anew and is judged on those of the person's last login until it ends ([H-25](docs/security/identity-provider.md#h-25)) |
| `COWORK_OIDC_GROUPS_CLAIM` | `groups` `# default` | a claim's name; `roles` `# example` | The claim that carries the groups, read from the ID token and, when it lacks it, from UserInfo; a string is one group, a list of strings the groups, anything else fails the login |
| `COWORK_OIDC_ALLOWED_GROUPS` | empty `# default` | comma-separated group names, each at most 256 bytes; `cowork-users,Domain Users` `# example` | **The gate:** a person logs in through the provider only when one of their groups is here or is `COWORK_ADMIN_GROUP`, matched exactly, case and all; trimmed, a repetition dropped; a name cannot hold a comma. With this and `COWORK_ADMIN_GROUP` both empty the gate admits nobody: the login page offers no button and the start warns. A personal access token of a person of the provider meets the gate as well, on the groups of their last login or refresh (`401 not_allowed`). A person of another issuer than the configured one — or any person of a provider once none is configured — is outside the gate whatever their groups. **Security:** whoever can put a person into an allowed group at the provider lets them in |
| `COWORK_ADMIN_GROUP` | empty `# default` | a group name of at most 256 bytes; `cowork-admins` `# example` | Its members are global administrators and pass the gate: they create tenants — each of which they then administer — and hold no role in a tenant they were not given until they grant themselves one, which the tenant's audit records; while no tenant exists, they and the local administrator alone log in. With `COWORK_BOOTSTRAP_TENANT_SLUG` the start maps it to the bootstrap tenant's `admin` role, and no local administrator is needed. **Security:** whoever can change this group at the provider administers the installation |
| `COWORK_OIDC_GROUPS_REFRESH` | `15m` `# default` | a duration of at least `1m` | How often a session of the provider reads the person's groups again — on its next request, and at an open event stream's heartbeat — and how often a token of a person of the provider meets the gate. Shorter: a group left at the provider reaches cowork sooner, for a call to the provider per session and interval |
| `COWORK_OIDC_GROUPS_MAX_AGE` | `168h` `# default` | a duration longer than `COWORK_OIDC_GROUPS_REFRESH`, which the start refuses otherwise | How old a person's groups — read at their last sign-in, or by the last session refresh that read them — may be for their personal access tokens to work: older ones refuse every token of the person with `401 not_allowed` until they sign in to the browser once, and an open event stream of such a token ends at its heartbeat. A local account's tokens are not affected. **Security:** bounds how long a person removed from a group at the provider keeps working tokens without signing in to the browser ([H-23](docs/security/identity-provider.md#h-23)); shorter asks people to sign in more often |
| `COWORK_OIDC_EMAIL_TRUSTED` | `false` `# default` | `true`, `false` | Whether an address about which the provider says nothing — no `email_verified` claim, as Entra sends none — counts as verified when an administrator grants a role by e-mail address (`POST …/members`). With `false` only an address the provider marked verified finds a person, and a person of a provider that sends no claim is admitted by a group mapping instead; an address marked unverified never matches. **Security:** set `true` only for a provider whose addresses administrators issue: where people choose their own address unverified, one can claim a colleague's and be granted the colleague's role ([H-26](docs/security/identity-provider.md#h-26)) |
| `COWORK_OIDC_DISPLAY_NAME` | `single sign-on` `# default` | at most 64 characters; `Acme SSO` `# example` | The login page's button: "Sign in with `<name>`" |

**Limits** ([ADR 0039](docs/adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md))

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_MAX_JSON_BODY` | `1MiB` `# default` | a size; `0` disables | A larger JSON body is `413 payload_too_large` |
| `COWORK_REQUEST_TIMEOUT` | `30s` `# default` | a duration, not negative; `0` disables | A request still running is cancelled and answered `504 timeout`; the event stream is exempt |
| `COWORK_MAX_PAGE_SIZE` | `200` `# default` | a count; `0` disables | The largest page a list returns; a larger `limit` is clamped, not refused |
| `COWORK_MAX_QUERY_LENGTH` | `256` `# default` | a count of characters; `0` disables | A longer full-text query `q` — the ticket lists' filter and the search — is `400 validation_failed` |
| `COWORK_ATTACHMENT_MAX_BYTES` | `10MiB` `# default` | a size; `0` disables | The largest upload; above it `413`, before anything is stored. Uploads are buffered in memory: with `0` one upload at a time is read whole, whatever its size ([docs/security/attachments.md](docs/security/attachments.md#h-12)) |
| `COWORK_ATTACHMENT_MAX_PER_TICKET` | `100` `# default` | a count; `0` disables | The attachments one ticket takes; one more is `409 attachment_limit` |

**Object storage** ([ADR 0016](docs/adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)) —
the endpoint, the bucket and both keys together, or none of them. Without them uploads answer
`501 uploads_disabled` and the log warns at start; the other three are read only with them.

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_S3_ENDPOINT` | empty `# default` | `https://s3.example.com` `# example` | `http://` or `https://`, a host and an optional port. Not contacted at start; the first upload is the test |
| `COWORK_S3_BUCKET` | empty `# default` | `cowork` `# example` | The bucket; it must exist — the backend never creates one |
| `COWORK_S3_ACCESS_KEY_ID` | empty `# default` | `cowork-app` `# example` | The access key's id |
| `COWORK_S3_SECRET_ACCESS_KEY` | empty `# default` | — | **Security:** a secret, never echoed; the key of a policy that reaches this bucket only, never root credentials |
| `COWORK_S3_REGION` | empty `# default` | `eu-central-1` `# example` | Empty lets the client ask the server |
| `COWORK_S3_USE_PATH_STYLE` | `true` `# default` | `true`, `false` | Path-style addressing, as MinIO expects; `false` for virtual-host style |
| `COWORK_S3_CA` | empty `# default` | `/etc/cowork/s3-ca/ca.crt` `# example` | A PEM file of a private authority, trusted in addition to the system's |

**Event stream** ([ADR 0054](docs/adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md))

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_SSE_REPLAY_WINDOW` | `5m` `# default` | a duration, not negative | How long a replica keeps events for a reconnect's `Last-Event-ID`; beyond it the stream starts with `resync`. `0` keeps no replay |
| `COWORK_SSE_MAX_STREAMS_PER_PERSON` | `10` `# default` | a count; `0` disables | The streams one person holds on one replica; one more closes the oldest with `event: unavailable` |

#### The chat in the UI

([ADR 0076](docs/adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md),
[docs/operations/chat.md](docs/operations/chat.md)) — without `COWORK_CHAT_PROVIDERS` there is no
chat, and a limit below set without it ends the start. Each id of the list has the variables of the
second table. Source: [`backend/internal/config/chat.go`](backend/internal/config/chat.go).

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_CHAT_PROVIDERS` | empty `# default` | `lmstudio,claude-work` `# example` | The providers the person picks from, by id, comma-separated; the first is a turn's default. **Security:** every provider listed receives what the chat reads for its person in every tenant, confidential tickets included — the owner's accepted risk ([H-37](docs/security/chat.md#h-37)); list a hosted provider only where every tenant's data may go to it |
| `COWORK_CHAT_TURN_TIMEOUT` | `5m` `# default` | a duration, not negative; `0` disables | How long one turn — the model's calls and the tools' — may take; past it the turn ends with the `error` event `timeout` |
| `COWORK_CHAT_MAX_STEPS` | `8` `# default` | a count; `0` disables | The calls of the model in one turn |
| `COWORK_CHAT_TURNS_PER_PERSON` | `2` `# default` | a count; `0` disables | The turns one person runs at once on one replica; one more is `429 chat_busy` |

| Variable of a provider | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_CHAT_<ID>_NAME` | the id `# default` | `LM Studio` `# example`; 1–64 characters without control characters | What the panel shows |
| `COWORK_CHAT_<ID>_KIND` | — (required) | `openai`, `anthropic` | The wire format: OpenAI Chat Completions (LM Studio, Ollama, vLLM, OpenAI) or the Anthropic Messages API |
| `COWORK_CHAT_<ID>_URL` | — (required) | `http://localhost:1234/v1` `# example`, `https://api.anthropic.com` `# example` | The base URL; `/chat/completions` is appended for `openai`, `/v1/messages` for `anthropic`; no user, query or fragment. **Security:** `https://`, or `http://` only on a host of the operator's network by its name or address — the key and every turn's text travel in it ([H-41](docs/security/chat.md#h-41)); never quoted in an error |
| `COWORK_CHAT_<ID>_MODEL` | — (required) | `qwen/qwen3-30b-a3b-2507` `# example`; 1–200 characters without spaces or control characters | The model by the name its provider knows it; it is the mark's middle part |
| `COWORK_CHAT_<ID>_API_KEY` | empty `# default`; required for `anthropic` | — | Sent as `Authorization: Bearer` (`openai`, where set) or `x-api-key` (`anthropic`), to that provider's host only. **Security:** a secret: from a Secret of its own in the chart, never echoed; the log's clip of a provider's error has it replaced ([H-42](docs/security/chat.md#h-42)) |

#### Frontend container

The frontend image takes no variable of cowork's: its nginx configuration,
[`frontend/nginx/default.conf`](frontend/nginx/default.conf), is a file in the image, nothing in it
is substituted at start, and it serves the UI and nothing else — the Ingress routes `/api/` and
`/auth/` to the backend. `frontend.extraEnv` reaches the image's entrypoint, whose own switches
(`NGINX_ENTRYPOINT_QUIET_LOGS`, for instance) are all it reads.

### CLI (backend)

| Command | Does |
|---|---|
| `cowork serve` | Load the configuration (`COWORK_SESSION_KEY` required, `COWORK_DATABASE_OWNER_URL` too while migrating on start); migrate unless `COWORK_MIGRATE_ON_START=false`; connect as the runtime role; refuse a runtime role that could bypass row-level security, a dirty schema and pending migrations; discover the identity provider when `COWORK_OIDC_ISSUER` is set, and refuse to start when it cannot; synchronise the local administrator and the bootstrap tenant under an advisory lock; listen until `SIGINT`/`SIGTERM` |
| `cowork migrate` | Load the configuration (`COWORK_DATABASE_URL` names the runtime role the migrations grant to, `COWORK_DATABASE_OWNER_URL` is the role they run as); apply pending migrations; exit 0. Exit 1 on a dirty or failing schema or a runtime role that could bypass row-level security |
| `cowork version` | Print `cowork <version> (commit <sha>, built <epoch>)` |
| `cowork help` | Print the usage (also `-h`, `--help`) |

Exit codes: `0` success, `1` configuration or runtime error, `2` unknown command or no command.

### CLI (cowork-mcp)

The MCP server for Claude Code and the commands its hooks run
([ADR 0041](docs/adr/0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md),
[ADR 0067](docs/adr/0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md),
[ADR 0070](docs/adr/0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md)). It is a
client of `/api/v1` with the person's token and nothing else: every request carries
`Authorization: Bearer`, `X-Cowork-Agent` ([the agent header of `cowork-mcp`](#keys-and-identifiers))
and the `User-Agent` `cowork-mcp/<version>`, every `POST` an `Idempotency-Key` of its own, and a
failed connection is retried twice where a repetition cannot act twice. It follows no redirect.
Setup: [docs/operations/claude-code.md](docs/operations/claude-code.md); what it holds and leaves
open: [docs/security/agent-client.md](docs/security/agent-client.md).

| Variable | Default | Values | Meaning |
|---|---|---|---|
| `COWORK_URL` | — (required) | `https://cowork.example.com` `# example` | The installation, as the browser shows it; `http://` only for `localhost` or a loopback address; no user, query or fragment. **Security:** the token goes to this URL and nowhere else |
| `COWORK_TOKEN` | — (required) | `cwk_…` | A personal access token, best an agent token with `write` scope. An error names the variable, never the value. **Security:** a credential: from the plugin's sensitive option (the system's credential store) rather than a shell profile or a settings file |
| `CLAUDE_PROJECT_DIR` | the working directory | `/home/ada/src/app` `# example` | The directory whose repository a session works in; set by Claude Code. A hook's own `cwd` comes first |

| Command | Does |
|---|---|
| `cowork-mcp serve` | The MCP server on stdio. At start it compares its major version with the installation's, checks that the installation serves every operation its tools use and reads the token; a refusal answers every tool call with the reason, an installation it cannot reach is asked again at the next call. Exit 1 at once on a missing or malformed variable |
| `cowork-mcp session-context` | The `SessionStart` hook: reads the hook's JSON on standard input (`session_id`, `cwd`, `source`, `model`), prints the session block — the binding or the proposal, the active ticket with its context or the top of the backlog, what happened since the last session — as plain text of at most about 9 000 bytes, and records the start (not after a compaction). Nothing in a directory with no remote and no binding file, or without the two variables; one line naming the cause on a malformed variable or a failure, and the token page where a new token may help. Exit 0 always |
| `cowork-mcp session-end` | The `Stop` hook: prints `{"systemMessage": "cowork: …"}` when a ticket of the person is `in-progress` in the bound project, the repository shows work since the session started and nothing was recorded on the ticket since; nothing otherwise, also on every error and while `stop_hook_active`. Exit 0 always |
| `cowork-mcp token check` | Whether the token works against the installation: its person, name, scope, restriction, capabilities and expiry; `--json` for the same as an object. Exit 1 when it does not work, with the installation's answer and the token page |
| `cowork-mcp lookup` | The working directory's remotes, binding file and binding — or the proposal, or why there is none; `--json` for the same as an object |
| `cowork-mcp version` | `cowork-mcp <version> (commit <sha>, built <epoch>, API /api/v1: <n> operations)` |
| `cowork-mcp help` | The usage (also `-h`, `--help`) |

Exit codes: `0` success, `1` configuration or runtime error, `2` unknown command, no command or
`--json` where a command takes none.

#### The tools

Every tool answers Markdown that names the canonical key of what it touched; `api` answers the
API's JSON. A refusal of the API is the tool's error with the API's status and `code`. Each
description names the limits the token can run into, and once the token is read, which
capabilities it holds and lacks
([ADR 0042](docs/adr/0042-twelve-workflow-tools-and-one-escape-hatch.md),
[ADR 0043](docs/adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)).
A key is `tenant/PROJECT-n`, or `PROJECT-n` in a bound session.

| Tool | Arguments | Does | Capability |
|---|---|---|---|
| `session_start` | — | the session block of `session-context`, again; the binding it finds is the session's | — |
| `get_ticket` | `key`, `comments` (10), `activity` (10) | the ticket's context document and the commit strings for it; read only | — |
| `search` | `query` (optional in one project: without it, the project's tickets in rank order), `scope` (`project`, `tenant`, `all`), `project`, `state[]`, `type[]`, `assigned_to_me`, `include_terminal` | full text over titles and bodies through the ticket lists' `q` filter, combined with the other filters, at most 20 hits in the list's order — a project's rank, a tenant's newest first — not the ranked search of `GET …/search`; read only | — |
| `file_ticket` | `type`, `title`, `severity`, `security`, `effort`, `body`, `threat`, `parent`, `project`, `links[]`, `horizon` (`later`), `after`, `before` | files a ticket in the bound or the named project into its horizon, directly after or before a ticket of that horizon or at its end, then its links | `override-urgency` for a horizon other than `later`, `rank` for a place |
| `record_state` | `key`, `body`, `comment` | replaces the body as a whole with `If-Match` of the version it read | — |
| `comment` | `key`, `text` | comments, in the person's name with the agent's mark | — |
| `link` | `key`, `type`, `other_key` | links two tickets of a tenant; an existing link is success | — |
| `watch` | `key` | sets the person's `watch` interest | — |
| `place_ticket` | `key`, `horizon`, `after`, `before`, `reason` | moves a ticket to another horizon with a reason, to a place directly after or before a ticket of its horizon, or both; a horizon is a planning category, not a state | `override-urgency` for a horizon, `rank` for a place |
| `open_question` | `key`, `question`, `options`, `recommendation`, `asked_of` | opens one question for a person (`me`, a username, a display name or an id; left out, the tenant) | — |
| `record_answer` | `key`, `question`, `answer` | writes down the answer the person gave in chat, marked as recorded by the agent | `record-answer` |
| `transition` | `key`, `to`, `reason_or_note`, `block_kind`, `blocked_by`, `comment` | moves the ticket from the state it read | `decide` to `decided`, `close` to `done`, `drop` to `dropped` |
| `set_progress` | `key`, `percent`, `stage` (`implementation`), `note`, `reason`, `comment` | sets one of the three stages | `close` when it closes the ticket |
| `finish_work` | `key`, `verification_note` | the note as a comment, the implementation stage at 100 — unless that would close the ticket without `close` —, then `done` with `close` from `in-progress` or `review`, else `review` from `in-progress`; says what remains for a person and for the repository | `close` for `done` |
| `create_project` | `tenant`, `key`, `name`, `remote`, `path` | creates a project and binds the repository in one act, after the person's yes to the proposal; idempotent over the remote | `create-project` |
| `api` | `method`, `path`, `body`, `if_match` | any route under `/api/v1/` of the installation with the same token and limits; a `POST` gets an `Idempotency-Key` | as the route needs |

`session_start` is for a terminal session; a host that runs the tools in the backend leaves it
out (`tools.Catalogue(tools.Anywhere)`, [mcp.md](docs/developer/mcp.md)).

### API (backend)

The contract is the OpenAPI 3.1 document in [`backend/api/`](backend/api/openapi.yaml)
([ADR 0046](docs/adr/0046-spec-first-the-openapi-document-is-the-contract.md)); the backend
serves it at `/api/v1/openapi.json` with `info.version` set to its own version, and validates
every request against it. What this section says in one line per route, the document says in
full.

- **Authentication.** Every route under `/api/v1/` except `version`, `openapi.json` and
  `schemas/cowork-yaml.json` takes
  one of two credentials, and the document says which per operation (`bearerToken`,
  `sessionCookie`). A personal access token, `Authorization: Bearer cwk_…`, is for scripts and
  agents; the session cookie `__Host-cowork-session` of a browser login — `POST /auth/local`, or
  the identity provider's `GET /auth/callback` — is for the browser. A request with an
  `Authorization` header is a token's, whatever cookie it carries. Without a valid credential the
  answer is `401` (`unauthenticated`, `token_expired`, `token_revoked`, and `not_allowed` for a
  token whose person the identity provider's gate no longer admits) with
  `WWW-Authenticate: Bearer realm="cowork"`. Sixteen routes take a **session only** and answer a
  token `403 session_required`: creating a token, a tenant or a local account, resetting or
  changing a password, logging out, a turn of the chat and stopping one, choosing the chat's
  capabilities, a global administrator's list of every tenant, and the administration acts that can give access — adding a
  member, setting a grant, making or changing a group mapping, restricting or opening a project,
  putting a person on its access list ([ADR 0035](docs/adr/0035-personal-access-tokens.md) D5,
  [tokens](docs/security/tokens.md#what-only-a-session-does)). A **write of a session** must come
  from `COWORK_BASE_URL` — its `Origin`, or without one its `Referer` — and carry
  `X-Requested-With: cowork`, else `403 csrf`; a token's writes need neither
  ([CSRF](docs/security/csrf.md)). A session whose account has a temporary password can only read
  `GET /api/v1/me`, change the password and log out; everything else is
  `403 password_change_required`. `X-Cowork-Agent: <name>/<model>/<session>` marks a request as an
  agent's; a token with the agent flag makes it one with or without the header.
- **Who made an act.** The person is the actor of every act, through a token too. An act made
  through a token carries the token — `token`, `{"id","name"}`, the name as the token has it, kept
  after a revocation, never its secret — beside the agent mark: an act of the activity, a comment
  and a revision of it, a file, a time entry and a revision of it, a stake (`agent`, `token`), a
  question (`asked_by_token`, `answered_by_token`), the ticket's filing (`reporter_agent`,
  `reporter_token`); `null` for a browser session. Whoever reads the act reads the token's name;
  the activity of an act recorded before the name was kept has `"name": null`. The context
  document says `through the token <name>` where it says `via <agent>` of an agent's act, and the
  tenant's audit view carries `token_name` beside `token_id` in JSON and as the last column of its CSV
  ([ADR 0036](docs/adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
  D6, [tokens](docs/security/tokens.md#what-is-recorded)).
- **Tenants.** A route under `/api/v1/tenants/{tenant}` answers `404 not_found` alike for an
  unknown slug, a tenant the person does not belong to and a token restricted to another. A global
  administrator who holds no role in the tenant reaches, in a browser session no `X-Cowork-Agent`
  marks, its administration — `GET …`, `GET …/members` (without addresses), `GET …/group-mappings`
  — and the grant of a role to themselves, `PUT …/members/{their id}/grant`; every other route
  answers them that `404` too ([tenancy](docs/security/tenancy.md#a-global-administrator-without-a-role)).
- **Writes.** An entity's `ETag` is its version; an overwriting write needs it in `If-Match`
  (`428` without, `412` with a stale one). An `Idempotency-Key` (a UUID) makes a creating
  `POST` safe to retry for 24 hours — the same request replays the stored answer, another one
  is `422`; an agent's creating `POST` must carry one.
- **Lists.** `limit` (default 50, clamped to `COWORK_MAX_PAGE_SIZE`) and `cursor`, from the
  previous page's `next_cursor`. The ticket lists, the tenant's time entries, the audit record,
  the members, the person's tokens and the projects also take numbered pages, `page` and
  `per_page` (`25`, `50`, `100`; `50` without it, clamped like `limit`), answered with `total`,
  `page` and `per_page`, up to row 10 000 — not together with `cursor` or `limit`;
  the ticket lists, the projects, the members, the group mappings, a project's access list, the
  lists of a ticket — comments, activity, questions, links, interest, attachments, time entries,
  the prerequisite tree —, the person's inbox, assigned tickets and decisions, the bin of deleted
  tickets and the saved filters answer a weak `ETag`, the caller's page, and `304` without a body
  to it in `If-None-Match`. A query
  parameter the route does not declare is `400`; a path parameter that cannot name anything is
  `404`. The person-level lists under `/api/v1/me/` — the inbox, the tickets assigned to the
  person, the open decisions, the search — span every tenant of the person, name the tenant on every item, take
  `tenant=<slug>` to narrow to one (`404 not_found` for a slug that names none of theirs, as for an
  unknown one) and carry cursors only; a token restricted to a tenant or a project reads that
  tenant or that project alone.
- **Every response** carries `Cache-Control: no-store` (the event stream `no-cache`) and
  `X-Request-Id`. An error is `application/problem+json`
  ([RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)): `type`, `title`, `status`, `detail`,
  `instance`, the stable `code` (snake_case, [the table below](#problem-codes)), the
  `request_id` and, for invalid fields, `errors[]` with a `pointer` each — for example
  `{"type":"https://cowork.dev/problems/not-found","title":"Not found","status":404,"detail":"no such tenant","instance":"/api/v1/tenants/acme/projects","code":"not_found","request_id":"0199a3c2-1d2e-7f00-8000-0000000000ff"}`.

| Method and path | Answers |
|---|---|
| `GET /healthz` | `200 {"status":"ok"}` — the process serves; no authentication |
| `GET /readyz` | `200 {"status":"ready"}`, or `503 not_ready` when the database does not answer (the error goes to the log); no authentication |
| `GET /api/v1/version` | `200 {"version":"…","commit":"…","build_time":"…"}`; no authentication |
| `GET /api/v1/openapi.json` | the API document; no authentication |
| `GET /api/v1/schemas/cowork-yaml.json` | the JSON Schema (draft 2020-12) of a repository's `.cowork.yaml`: `tenant` and `project` required, `path` and `url` optional, nothing else; no authentication |
| a known path with another method | `405 method_not_allowed`, `Allow` names the methods the API document declares there; the document declares no `HEAD`, so `HEAD` on the API is `405` (the health endpoints answer it) |
| any other path | `404 not_found`, `detail: no route <METHOD> <path>` |
| `GET /auth/options` | `200 {"local": bool, "oidc": bool, "oidc_name": string or null, "password_min_length": int, "token_max_lifetime_days": int}` — what the login page offers: the local form while an active local account exists; the identity provider's button while one is configured and its gate names a group, with its name (`COWORK_OIDC_DISPLAY_NAME`; `null` without a provider); the minimum password length every password form follows (`COWORK_PASSWORD_MIN_LENGTH`); and the longest lifetime of a new token in whole days, rounded down (`COWORK_TOKEN_MAX_LIFETIME`; `0` under a day), the bound of the token form; no authentication |
| `GET /auth/oidc/login?return_to=<path>` | a browser navigation: `302` to the identity provider's authorization endpoint for the code flow with PKCE (`S256`), a `state` and a `nonce`, setting the state cookie `__Host-cowork-oidc` for ten minutes; `303` to `/login?error=oidc_unavailable` without a provider or with a gate that admits nobody. `return_to` is a path of this installation — one leading `/`, no `//`, `/\`, backslash or control character, at most 2048 bytes — or `/`; no authentication |
| `GET /auth/callback` | the provider's return, the redirect URI: the state cookie (at most ten minutes old) and its `state`, the code redeemed with the PKCE verifier, the ID token verified, the groups read and judged by the gate, the person found by issuer and subject or made, their memberships derived from the tenants' group mappings, the session made; `303` to `return_to` with the session cookie, or `303` to `/login?error=<code>&return=<path>` with `oidc_failed`, `not_allowed`, `not_initialised` or `oidc_unavailable` — the state cookie cleared either way. Parameters the provider adds (`iss`, `session_state`) are taken and not read; no authentication |
| `POST /auth/local` | `{"username","password"}` → `200 {"password_change_required": bool}` and the session cookie; every failure is `401 invalid_credentials`, the same answer in the same time for an unknown username, a wrong password, a locked or a deactivated account; `429 too_many_attempts` from the address throttle; `403 not_initialised` for a person who is not a global administrator while no tenant exists; `403 csrf` unless the `Origin` is `COWORK_BASE_URL`; no authentication |
| `POST /auth/logout` | a session, CSRF-checked: ends it, clears the cookie, `204` — or, for a session of the identity provider whose discovery names an `end_session_endpoint`, `200 {"end_session_url"}`: that endpoint with `client_id` and `post_logout_redirect_uri` = `COWORK_BASE_URL` + `/login`, for the browser to go to; cowork does not call it |
| `GET /api/v1/me` | the calling person and their memberships — each with the effective role and its `origins`, `mapping` and `grant` with their own roles — whether they are a global administrator (`global_admin`), have a local account (`local`) and must change a temporary password (`password_change_required`) |
| `PUT /api/v1/me/password` | a session only: `{"current_password","new_password"}`; the current password counts like a login attempt towards the lockout; the new one meets `COWORK_PASSWORD_MIN_LENGTH` and differs; every other session of the account ends; `204`. Not for the local administrator, whose password is the configuration's, nor for a person of the identity provider, who has none (`403 forbidden`) |
| `GET /api/v1/me/tokens` | the person's tokens, newest first, revoked and expired ones included, numbered pages with a total — metadata only: a restriction names its tenant by slug (`restricted_tenant`) and its project by key (`restricted_project`, `null` where the person no longer sees the project or belongs to its tenant — the token reaches nothing then); `restricted_project_id`, the project's id, is deprecated and kept in `/api/v1` |
| `POST /api/v1/me/tokens` | a session only: `{"name","scope"}` and optionally `agent`, `capabilities`, `tenant`, `project`, `lifetime_days`; `201` with the token **and its plaintext, once** — a replay for an `Idempotency-Key` answers without it. The lifetime defaults to `COWORK_TOKEN_DEFAULT_LIFETIME` and is shortened to `COWORK_TOKEN_MAX_LIFETIME`; an agent token has at most `write` scope and every capability when `capabilities` is left out — an empty list is none, the baseline only. The `name` shows on every act made through the token, to whoever reads the act |
| `DELETE /api/v1/me/tokens/{token_id}` | revoke one; a token may always revoke itself, another needs `write`, an agent revokes only its own |
| `GET /api/v1/me/token` | the token the request presents: its metadata as the list shows it, and `request` — whether the request is an agent's, the agent its acts record and the capabilities it holds; a browser session presents none, `404 not_found` |
| `GET /api/v1/me/chat` | the capabilities the person gives the chat in the UI: `{"capabilities": [...], "chosen": bool}` — `chosen` false is the default, every capability but `decide`, `close`, `drop` and `record-answer` |
| `PUT /api/v1/me/chat` | a session only, never an agent-marked one: `{"capabilities": [...]}`, the whole set, unique — empty leaves the chat the baseline; `200` with the set in the catalogue's order; the chat's next request holds it; no `If-Match`; a change is the person's recorded act |
| `GET /api/v1/me/repositories/lookup` | `remote` (1–10, repeatable, in order of preference) and `path` → `status` `bound`, `ambiguous` or `unbound`; the remotes with their identities (`null` for one that names no host); the bindings of the first remote that has one covering `path`, in the projects the caller sees across the person's tenants — a restricted token's only; for `unbound` a `proposal` (identity, name, the tenant and the reason `only-tenant`, `remote-owner` or `choose`, a free key per tenant) or `proposal_unavailable` saying why not. A remote's credentials are dropped, and a proxy's log may still carry the query |
| `GET /api/v1/me/inbox` | the person's notifications across their tenants, newest first: `{"items": [...], "next_cursor", "unread"}`, each item `id`, `tenant` `{slug, name}`, `ticket` `{key, title, state}` as it is now, `reason` — `assigned`, `asked`, `answered`, `state_changed`, `blocker_closed`, `commented`, `urgent` —, `act` (the act it renders from, as the ticket's activity shows it, without its payload where it names a ticket the person cannot see), `blocker` (for `blocker_closed`, the ticket that blocked it, as it is now), `withdrawn` (the comment or question has been withdrawn since), `read`, `created_at`; `unread` counts the unread ones. A notification of a ticket the person no longer sees, or of a tenant they left, is absent and counts nowhere. `tenant`, `limit`, `cursor` |
| `PUT /api/v1/me/inbox/read` | `{"through": "<notification id>"}`: every unread notification of the person up to and including that one, read — one that arrived after it stays unread; `tenant` narrows; `write` scope; `200 {"unread"}`; one act `read` per tenant where something changed |
| `PUT /api/v1/me/inbox/{notification}/read` | one notification read; `write` scope; `200 {"unread"}`; one already read records nothing; another person's, or one of a ticket the person no longer sees, `404 not_found` |
| `GET /api/v1/me/assigned` | the open tickets — neither `done` nor `dropped` — assigned to the person across their tenants: `{"items": [{"tenant": {slug, name}, "ticket": {...}}], "next_cursor"}`, in the order of the tenant's slug, the project's key and the project's rank, until the score of ADR 0014 exists; `tenant`, `limit`, `cursor` |
| `GET /api/v1/me/decisions` | the open questions asked of the person and those open in their tenants — asked of nobody —, on tickets they see: `{"items": [{"tenant", "ticket": {key, title, state}, "question": {...}}], "next_cursor"}`, in the order of the tenant's slug, the project's key, the ticket's place in the rank — `done` and `dropped` tickets after the ranked ones — and the question's number; `tenant`, `limit`, `cursor` |
| `GET /api/v1/me/search` | the search of `GET …/search` below over every tenant of the person — one read per tenant, merged by rank, each hit naming its tenant: `q` (required, white space alone is `400 validation_failed`), `tenant`, `limit`, `cursor`; a restricted token searches its tenant or its project alone |
| `GET /api/v1/tenants` | a global administrator, session only: every tenant of the installation by slug, `{"slug","name","role"}` — `role` the caller's, the higher of mapping and grant, `null` where they hold none; `limit` and `cursor`. Anybody else `403 forbidden`; a token `403 session_required` |
| `POST /api/v1/tenants` | a global administrator, session only: `{"slug","name"}` → `201`; the creator becomes the tenant's first administrator by a marked grant, in the same transaction; `409 tenant_slug_taken` |
| `GET /api/v1/tickets/{tenant}/{key}` | a ticket by its short key, `<PROJECT>-<number>` — the body and `ETag` of its own route |

The routes of a tenant start with `/api/v1/tenants/{tenant}`, written `…` below; `…/{number}`
stands for `/api/v1/tenants/{tenant}/projects/{project}/tickets/{number}`.

<details>
<summary>Local accounts — 6 routes</summary>

For the tenant's administrators with `admin` scope, never an agent. **Creating an account and
resetting a password take a browser session only** — a token, an administrator's included, is
`403 session_required` — because an account or a password made with a leaked token would outlive
its revocation; the other four routes work with an `admin`-scope token. A tenant manages the local
accounts **its own administrators created** and no others: another tenant's account, a global
administrator and the local administrator answer `404 not_found`, like a username nobody has.
An administrator's own account is off limits for a password reset, an unlock and a deactivation
(`403 forbidden`).

| Method and path | Does |
|---|---|
| `GET …/accounts` | the accounts the tenant manages, by person id: username, display name, role, `password_change_required`, `locked`, `deactivated_at` |
| `POST …/accounts` | a session only: create one: `{"username","display_name","temporary_password","role"}`; the person changes the password at the first login; a marked grant with that role; `409 username_taken` (the installation has one namespace) |
| `PUT …/accounts/{username}/password` | a session only: set a new temporary password; every session of the account ends |
| `DELETE …/accounts/{username}/lockout` | forget the failures and the lock of the username |
| `PUT …/accounts/{username}/deactivation` | deactivate: no login, tokens revoked, sessions ended, the person and their acts stay; `409 last_admin`, changing nothing, when the tenant would be left without an administrator who can log in — the deactivation takes the tenant's lock, so two administrators who deactivate each other at once are decided one after the other; the other tenants the person administers are not asked |
| `DELETE …/accounts/{username}/sessions` | end every session of the account, at once; tokens are not affected |

</details>

<details>
<summary>Members, group mappings and project access — 12 routes</summary>

For the tenant's administrators, never an agent, unless a row says otherwise — a global
administrator who holds no role in the tenant reads the members and the mappings and grants a role
to themselves, and one who holds a role below `admin` raises their own grant. Every act that can give access takes a **browser session only** — a token, an administrator's included, is
`403 session_required` — because what it gives would outlive a leaked token's revocation; removing a
grant, a mapping or an access entry works with an `admin`-scope token as well. A change of a grant or
of a mapping that would leave the tenant without an administrator who can log in — mapped or
granted, active, and a local account or a person of the configured issuer whom the gate admitted at
their last login, refresh or check — is `409 last_admin` and changes nothing; these changes take the
tenant's lock, so two at once are decided one after the other. Every change is recorded and
announced on the event stream as `membership.changed`.

| Method and path | Does |
|---|---|
| `GET …/members` | every member reads it, and a global administrator without a role in the tenant, in a session: the members by person id, each with the effective role — the higher of the mapped and the granted one — every origin with its own role (`mapping`, `grant`), `local` for a person with a local account, and `email`: the person's address for the tenant's administrators, `null` for everyone else and for a person without one; numbered pages with a total |
| `POST …/members` | a session only: `{"person","role"}` grants a role to a person who exists — an e-mail address, compared without regard to case with the address the identity provider asserted at the person's last login — only one it marked verified, or, with `COWORK_OIDC_EMAIL_TRUSTED=true`, one it said nothing about; never one it marked unverified — among the persons of the configured issuer, or a local account's username, with or without `local:`; `201` with the member; `404 person_not_found`, `409 person_ambiguous` (an address several persons share), `409 grant_exists`; takes an `Idempotency-Key` |
| `PUT …/members/{person_id}/grant` | a session only: `{"role"}` creates the member's grant or changes its role; the mapped membership is never touched; `404 person_not_found` for a person who is no member. A global administrator who does not hold `admin` in the tenant — no role there, or a lower one — sets their own grant here, their own id and any role: a marked grant made or its role changed, recorded in the tenant with them as its actor, never `409 last_admin`; a grant to anybody else is `403 forbidden` |
| `DELETE …/members/{person_id}/grant` | removes the grant; a mapped membership stays; `204`, also when there was none |
| `GET …/group-mappings` | `read` scope, and a global administrator without a role in the tenant, in a session: the mappings by group, each with `includes_caller` — whether the caller's own groups, as of their last login or refresh, hold it |
| `POST …/group-mappings` | a session only, of a global administrator who administers the tenant — any other administrator is `403 forbidden`, and nothing is written, because every tenant shares the provider's groups: `{"group","role"}`, the group as the provider's claim carries it, case and all; the memberships of every active person of the configured issuer behind the gate whose groups hold it are derived at once — whoever holds the group joins the tenant; `201` with `ETag` and `Location`; `409 mapping_exists`; takes an `Idempotency-Key` |
| `PATCH …/group-mappings/{mapping_id}` | a session only, of a global administrator who administers the tenant (`403 forbidden` otherwise): `{"role"}` with `If-Match`; the memberships follow at once |
| `DELETE …/group-mappings/{mapping_id}` | any administrator of the tenant: removes it; the memberships it derived go, or fall to the person's other mapped groups; grants stay; `204`, also when there was none |
| `PUT …/projects/{project}/restriction` | a session only: `{"restricted": bool}` with the project's `If-Match`; a restricted project is visible to the tenant's administrators and the people on its access list, and to nobody else |
| `GET …/projects/{project}/access` | `read` scope: the project's access list by person id, each entry `member` or `viewer`, with the person's `email` |
| `PUT …/projects/{project}/access/{person_id}` | a session only: `{"role"}`, `member` or `viewer`, puts a member of the tenant on the list or changes their entry — their role in the project is the lower of their tenant role and the entry; `404 person_not_found` for a person who is no member. The list may be written before the project is restricted |
| `DELETE …/projects/{project}/access/{person_id}` | takes a person off the list; `204`, also when they were not on it |

</details>

<details>
<summary>The chat in the UI — 3 routes</summary>

Every member of the tenant ([ADR 0076](docs/adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md),
[docs/developer/chat.md](docs/developer/chat.md)).

| Method and path | Does |
|---|---|
| `GET …/chat` | whether the tenant's members may chat: `{"available", "providers": [{"id","name","kind","model"}], "reason"}` — the providers in the configured order, never a URL or a key; `reason` is `not_configured` without a provider, `null` otherwise; a token as well |
| `POST …/chat` | a session only, CSRF-checked: one turn, `{"conversation","messages"}` and optionally `provider` (an id of the list; the first when left out; another is `400 validation_failed`) and `context` (the page); `200 text/event-stream` with the events `text`, `tool_call`, `ui`, `tool_result`, `error` and `done` last, which carries the messages to append and the reason `answered`, `step_limit`, `stopped` or `error`. Every tool call runs at once as the person's agent, `chat/<model>/<conversation>`, with the person's chat capabilities; `409 chat_unavailable` without a provider, `429 chat_busy` past `COWORK_CHAT_TURNS_PER_PERSON` |
| `DELETE …/chat/turns` | a session only, never an agent-marked one, CSRF-checked: ends every running turn of the session's person in the tenant on the replica that answers, at once; `204` once they have ended, or after five seconds; nothing running is no error. Another replica's turns are not reached ([H-48](docs/security/chat.md#h-48)) |

</details>

<details>
<summary>Tenant and projects — 12 routes</summary>

| Method and path | Does |
|---|---|
| `GET …` | the tenant and its settings; also to a global administrator without a role in the tenant, in a session |
| `PATCH …` | change the name or the settings — an administrator with `admin` scope, never an agent; `If-Match` |
| `GET …/audit` | the audit record, newest first, for administrators, each act with `token_id` and the token's name, `token_name`; filters `actor`, `token`, `action`, `entity_type`, `from`, `to`; numbered pages with a total; CSV on `Accept: text/csv`, `token_name` its last column, after the columns released before — a CSV page carries no cursor, so a client reads several as numbered pages with `to` held at the moment it began |
| `GET …/events` | the event stream of the changes the caller may see ([runtime.md](docs/operations/runtime.md#the-event-stream)); with `me=true` the person-level stream: every event of every tenant the person belongs to that the tenant's own stream would carry to them, each with its `id:` and replayed across the tenants after a reconnect, and `inbox.changed {"unread": n}` — when it opens and whenever the person's inbox changes, without an `id:`; a token restricted to a tenant hears its tenant alone. `membership.changed` names its tenant: `{"tenant", "person_id", "project_id", "mapping_id"}`, each key but `tenant` where it applies |
| `GET …/projects` | the projects the caller can see, by key; `include_archived`; numbered pages with a total |
| `POST …/projects` | create one — `write`; a member while the tenant allows it, an administrator always, an agent with `create-project`. With `repository` (`remote`, optionally `path`) the repository is bound in the same act, and when a project of the tenant binds it already the answer is `200` with that project and nothing is created — `409 repository_bound` when the caller cannot see it |
| `GET …/projects/{project}` | one project |
| `PATCH …/projects/{project}` | change its name, its description or its advisory WIP limits per state — `analysed`, `decided`, `in-progress`, `review`, `blocked` — a member with `write`, an agent too; `If-Match` |
| `PUT …/projects/{project}/archive` | archive it — an administrator, never an agent; it keeps its tickets and refuses new ones |
| `GET …/projects/{project}/repositories` | the repositories it owns, in the order they were bound: `identity`, `path`, `remote` as last given without credentials |
| `POST …/projects/{project}/repositories` | bind one, `{"remote"}` and optionally `path` — those who may create a project, judged by the role in it; `201` new, `200` when the project binds it already, its remote updated to the form given; `409 repository_bound` when another project of the tenant binds it, naming that project only to a caller who sees it |
| `DELETE …/projects/{project}/repositories/{repository}` | unbind it — the same people; `204` also when the binding is gone; the project and its tickets stay |

</details>

<details>
<summary>Tickets — 24 routes</summary>

| Method and path | Does |
|---|---|
| `GET …/tickets` | the tenant's tickets across its projects, newest first; filters `project`, `state`, `type`, `severity`, `security`, `urgency`, `effort`, `assignee`, `reporter`, `parent`, `progress_min`, `progress_max` (the implementation stage), `opened_after`, `opened_before`, `updated_after`, `updated_before`, `done_after` (done after it — like the four timestamps before it, the bound itself excluded), `q` (every word in the title and body, a filter in the list's order, no rank and no snippet), `include_terminal`, `blocked`, `has_open_questions`, `interest` ([ADR 0049](docs/adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md)) |
| `GET …/search` | search the tenant's tickets ([ADR 0025](docs/adr/0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md)): `q` (required) by full text — the `simple` dictionary after `unaccent`, every word in one text — over the title and body, the comments but a withdrawn one, the questions and the file names; a key by its beginning (`COW-1`, `acme/COW-12`), a title by trigram. `{"items": [...], "next_cursor"}`, one hit per ticket at its best match, the best first — a key, the title, the body, then comments, questions and file names, a title by trigram last: `tenant`, `key`, `title`, `type`, `state`, `found_in` (`key`, `ticket`, `comment`, `question`, `attachment`), `comment` (its id) or `question` (its number) where it matched, and `snippet`, `[{"text","match"}]`, the found words marked — text, never HTML. What the caller cannot see is not searched, snippets included; `limit`, `cursor` — a cursor belongs to its query; a project-restricted token searches its project |
| `GET …/projects/{project}/tickets` | the project's tickets in its rank: the ranked by their key, then the unranked — done and dropped, and open ones a release before the rank filed — by number ([ADR 0014](docs/adr/0014-rank-is-the-decision-score-is-the-warning.md)); the same filters but `project`; a cursor from before the rank is `400 invalid_cursor` |
| `POST …/projects/{project}/tickets` | file a ticket (`type`, `title`, `severity`, `security`, `effort`); its number is the project's next. `urgency` is its horizon, `later` when left out; `after` or `before` names an open ticket of that horizon it is placed directly next to, the bottom of the rank — the end of its horizon — when left out ([ADR 0010](docs/adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3, [ADR 0014](docs/adr/0014-rank-is-the-decision-score-is-the-warning.md) D2); an agent needs `override-urgency` for a horizon other than `later` and `rank` for a place |
| `GET …/{number}` | one ticket: its state — `filed`, `analysed`, `decided`, `in-progress`, `review`, `blocked`, `done`, `dropped` — its three progress stages `progress_refinement`, `progress` (implementation) and `progress_review`, derived from its children while it has any, `done_from` and `done_by_hand` while it is done, and `open_prerequisites`, the open tickets that block it which the caller can see |
| `PATCH …/{number}` | change its fields; `If-Match`. A stage takes 0–100 in steps of five in every state but `dropped`, never on a ticket with children. The change that brings the last of the three stages of a ticket without children to 100 is the done act: it needs `note`, the verification, and over open prerequisites it is `409 open_prerequisites` unless a person sends `override_prerequisites` with a `reason`; an agent needs `close` and a ticket in `in-progress` or `review`. The change that lowers a stage of a ticket done by its stages reopens it to `done_from` with a `reason`, ranked at the bottom; a ticket done by hand stays done while its stages change; an open ticket whose three stages are full already — a parent whose last child left — is closed by hand |
| `GET …/{number}/body` | its body, `{"body","body_html","version"}`: the Markdown as written and the HTML rendered and sanitised on the server — raw HTML shown as text; a link only to an `http`, `https`, `mailto` or relative address, with `rel="noopener noreferrer nofollow"` and `target="_blank"`; an image only of one of the ticket's own raster attachments, from that attachment's path, any other image a link to its address. The `ETag` is the ticket's ([rendered Markdown](docs/security/rendered-markdown.md)) |
| `PUT …/{number}/body` | replace its body as a whole; `If-Match` |
| `PUT …/{number}/urgency-override` | set the ticket's horizon — `now`, `release`, `next`, `later` or `icebox`, a planning category independent of the state, which nothing derives: the derived value is `later` for every ticket — until it is withdrawn or replaced; the reason is optional for a person and required of an agent, which needs `override-urgency`; `If-Match` |
| `DELETE …/{number}/urgency-override` | return the ticket to `later`, the derived horizon; `If-Match` |
| `PUT …/{number}/confidential` | set or lift the confidential flag — an administrator with `admin` scope, never an agent; lifting needs a reason; `If-Match` |
| `DELETE …/{number}` | delete it into the tenant's bin — an administrator with `admin` scope, never an agent (`403 agent_forbidden`, `hard-off: deleting, restoring or purging`); `204`. From then on it answers like a missing ticket everywhere but the bin: every route of it and under it `404`, absent from every list, tree, person-level list, inbox and context, its links hidden, its parent's derived stages without it; its key stays taken. Not refused when other tickets depend on it; a ticket already deleted is `404` |
| `POST …/{number}/transitions` | move it to another state: forward one step to `review`, back with a reason, into `blocked` and out to where it came from, `dropped` with a reason and back to `filed`; `from` must be the current state, else `409 state_conflict`. To `done` is done by hand — from any open state for a person, from `in-progress` or `review` for an agent with `close` — with a verification note, and over open prerequisites it is `409 open_prerequisites` unless a person overrides with a reason; done → `done_from` with a reason withdraws it, unless it has no children and its three stages are full, when the ticket stays done by them; a ticket done by its stages leaves done only by a lower stage. An agent needs `decide`, `close` or `drop` for those moves; done and dropped take the rank away, leaving them ranks the ticket at the bottom |
| `PUT …/{number}/rank` | place it directly after or before another open ticket of the project, `{"after": n}` or `{"before": n}`: one key written between the neighbour's and the next one's on that side, those the caller cannot see counted, recorded as `ranked` with the neighbour, the version raised — the key itself is never shown, the list's order is the rank; a ticket already there among those the caller can see is `200` unchanged; no `If-Match` — the last move wins; an agent needs `rank`; a done or dropped ticket or neighbour is `409 state_conflict`, a neighbour the caller cannot see the `400` of one that does not exist |
| `GET …/{number}/links` | its links in both directions |
| `PUT …/{number}/links/{type}/{other}` | link it, as the source, to `other` (a short key): `blocks`, `relates-to`, `duplicates`, `found-in`; `201` new, `200` existing; a `blocks` cycle is `409 link_cycle` |
| `DELETE …/{number}/links/{type}/{other}` | remove the link; `204` also when there was none |
| `GET …/{number}/prerequisites` | its prerequisite tree ([ADR 0012](docs/adr/0012-four-typed-directed-links-within-a-tenant.md) D6): the tickets that block it, what blocks those, and so on, eight levels deep, depth first; `direction=up` reads it upward, its dependents. Each node with its key, title, state, `blocked_from`, assignee, the three progress stages, `depth`, `settled` (done or dropped) and `repeated` — a ticket the tree holds under two others stands in full under the first and as `repeated` under each other; `open` counts the open ones of the whole tree, each once, on every page. A ticket the caller cannot see is absent, and so is what lies only behind it |
| `GET …/{number}/interest` | who holds a stake in it |
| `PUT …/{number}/interest` | set the caller's own stake; `201` new, `200` otherwise; the stake carries the agent mark and the token of the write that set it |
| `DELETE …/{number}/interest` | remove the caller's own stake |
| `GET …/{number}/markdown` | its canonical Markdown, `text/markdown`; the `ETag` is its version; every call is recorded |
| `GET …/{number}/context` | the ticket for reading, `text/markdown`: one first line naming the ticket, the time, the person and the agent — or the token, `(through the token <name>)` —, the canonical Markdown, then `## Links`, `## Prerequisites` (the tree of `…/prerequisites`, each prerequisite once), `## Recent comments` (the last `comments`, default 10, up to 100; `0` leaves the section out), `## Attachments` and `## Recent activity` (the last `activity`, the same bounds); what the caller cannot see is absent; no `ETag`; every call is recorded. No import format ([grammar](docs/developer/markdown-grammar.md#the-context)) |
| `GET …/{number}/activity` | every recorded act on it, from the audit record |

</details>

<details>
<summary>The bin of deleted tickets — 3 routes</summary>

For the tenant's administrators, never an agent
([ADR 0024](docs/adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D1, D2, D7). A token restricted to a project is refused like an unknown tenant. `{key}` is a short
key, `<PROJECT>-<number>`, of a ticket in the bin; any other key — a ticket that is not deleted
included — is `404 not_found`.

| Method and path | Does |
|---|---|
| `GET …/deleted-tickets` | `read` scope: the deleted tickets the caller can see, the last deleted first — `key`, `project`, `number`, `type`, `title`, `state`, `confidential`, `deleted_at`, `deleted_by`, `purge_at` (thirty days after the deletion); `limit` and `cursor` |
| `PUT …/deleted-tickets/{key}/restore` | `admin` scope: bring it back as it was — its links, comments, stakes and place in the rank with it; `200` with the ticket and its `ETag`, the version raised; recorded as `restored` |
| `DELETE …/deleted-tickets/{key}` | `admin` scope: purge it now — the ticket, its comments and their revisions, questions, links, stakes, time entries and their revisions, notifications and attachments, then the attachments' objects; its children become roots, and a block that waited on it waits on its key as an external reference; its audit rows stay with their content emptied; `204`, recorded as `purged`. The job `ticket-purge` does the same thirty days after the deletion |

</details>

<details>
<summary>Saved filters — 5 routes</summary>

A saved filter is a named set of the ticket lists' filter parameters
([ADR 0018](docs/adr/0018-the-views-of-the-first-release.md) D5,
[ADR 0049](docs/adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md)
D6, D7), the owner's, and shared with every member of the tenant when `shared`. A token restricted
to a project is refused like an unknown tenant.

| Method and path | Does |
|---|---|
| `GET …/filters` | `read` scope: the caller's filters and those shared with the tenant, oldest first — `id`, `name`, `owner`, `shared`, `parameters`, `warnings` (a value that no longer holds, checked as it is read), `redacted` (another member's filter that names a project or a ticket the caller cannot see, or one that is gone: shown without `parameters` and `warnings`), `version`; `limit` and `cursor` |
| `POST …/filters` | any role, `write` scope: `{"name","parameters"}` and optionally `shared`; `parameters` takes the lists' filter parameters as a JSON object — `state`, `type`, `severity`, `security`, `urgency`, `effort`, `assignee`, `reporter`, `parent`, `interest`, `project` as arrays, `progress_min`, `progress_max`, the time bounds, `q`, `include_terminal`, `blocked`, `has_open_questions` — and refuses what the lists refuse, `400` at `/parameters/<name>`; `me` stays `me`, whoever applies it; `201` with `ETag` and `Location`; `Idempotency-Key` |
| `GET …/filters/{filter}` | one of the caller's or a shared one; `ETag` |
| `PATCH …/filters/{filter}` | the owner, `write` scope: `name`, `parameters` (the whole set) and `shared`; `If-Match`; another member's shared filter is `403 forbidden`, a filter the caller cannot see `404` |
| `DELETE …/filters/{filter}` | the owner, `write` scope; `204`; another member's shared filter is `403 forbidden` |

</details>

<details>
<summary>Questions and comments — 12 routes</summary>

| Method and path | Does |
|---|---|
| `GET …/{number}/questions` | its questions, by number; every question, wherever the API answers one, carries `options_html` and `answer_html` (`null` without an answer) beside the Markdown, rendered as the body is |
| `POST …/{number}/questions` | ask one; the person asked must be able to see the ticket — without one the question is open to the tenant |
| `GET …/{number}/questions/{question}` | one question |
| `PATCH …/{number}/questions/{question}` | edit it while open — the asker; `If-Match` |
| `PUT …/{number}/questions/{question}/answer` | answer it, or change one's answer — a person decides; an agent with `record-answer` writes down its person's answer; `If-Match` once answered |
| `PUT …/{number}/questions/{question}/withdrawal` | withdraw an open question — the asker |
| `GET …/{number}/comments` | the comment thread, oldest first (`order=desc` for newest); every comment carries `body_html` beside the Markdown, rendered as the body is, `null` once withdrawn |
| `POST …/{number}/comments` | comment |
| `GET …/{number}/comments/{comment}` | one comment |
| `PATCH …/{number}/comments/{comment}` | edit it — its author, or the person whose agent wrote it; the old text is kept; `If-Match` |
| `GET …/{number}/comments/{comment}/revisions` | a comment's earlier texts, oldest first; empty once withdrawn |
| `PUT …/{number}/comments/{comment}/withdrawal` | withdraw it: the text is hidden, the entry stays — its author, their person, or an administrator |

</details>

<details>
<summary>Time — 8 routes</summary>

| Method and path | Does |
|---|---|
| `GET …/{number}/time-entries` | the ticket's time entries the caller may see, and their sum |
| `POST …/{number}/time-entries` | book the caller's own time — a member, never an agent; a locked day is `409 period_locked` |
| `GET …/{number}/time-entries/{entry}` | one entry |
| `PATCH …/{number}/time-entries/{entry}` | correct it — its author; the previous values are kept; `If-Match` |
| `PUT …/{number}/time-entries/{entry}/void` | void it — its author; kept, and left out of every sum |
| `GET …/{number}/time-entries/{entry}/revisions` | its previous values |
| `GET …/time-entries` | the tenant's entries the caller may see, newest first; filters `from`, `to`, `project`, `ticket`, `person`, `include_voided`; CSV on `Accept: text/csv` |
| `GET …/time-report` | minutes summed per `ticket`, `project`, `person` or `tenant` (`group_by`) over a period; CSV on `Accept: text/csv` |

</details>

<details>
<summary>Attachments — 4 routes</summary>

| Method and path | Does |
|---|---|
| `GET …/{number}/attachments` | the ticket's attachments |
| `POST …/{number}/attachments` | upload a file to the ticket, or to one of its comments: `multipart/form-data` with `file` and optionally `comment_id`; the type is detected from the bytes — PNG, JPEG, GIF, WebP, PDF, UTF-8 text, SVG — anything else is `415`; `501 uploads_disabled` without object storage |
| `GET …/{number}/attachments/{attachment}` | its metadata |
| `GET …/{number}/attachments/{attachment}/content` | its bytes, with `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox`; raster images inline, everything else as a download; the `ETag` is the SHA-256 of the bytes; every `200` is recorded |

</details>

#### Problem codes

Generated by `make generate` from [`backend/internal/problem`](backend/internal/problem/problem.go);
every error body carries one of these as `code`.

<!-- problem-codes:start -->
| `code` | Status | Meaning |
|---|---|---|
| `validation_failed` | 400 | The request does not match the API document, or a field breaks a rule; `errors[]` names each field |
| `idempotency_key_required` | 400 | An agent's `POST` that creates something came without an `Idempotency-Key`; a transition needs none (docs/adr/0045 D2, D3) |
| `invalid_cursor` | 400 | The cursor was altered, belongs to another list, or comes from another installation |
| `page_too_deep` | 400 | A numbered page beyond the depth cap; follow the cursor instead |
| `unauthenticated` | 401 | No token or session, a malformed one, or one cowork does not know; a session that expired or was ended answers the same |
| `token_expired` | 401 | The token is past its expiry (docs/adr/0035 D4) |
| `token_revoked` | 401 | The token was revoked, or its person deactivated (docs/adr/0035 D6) |
| `not_allowed` | 401 | The token's person is outside the identity provider's gate: none of their groups, as of their last login or groups refresh, is in COWORK_OIDC_ALLOWED_GROUPS or is COWORK_ADMIN_GROUP, or the person belongs to another issuer than the configured one, or their groups were read longer ago than COWORK_OIDC_GROUPS_MAX_AGE, until a sign-in in the browser reads them again. The token is refused, not revoked, and works again once the person is back inside (docs/adr/0035 D8) |
| `invalid_credentials` | 401 | The local login failed: the same answer, in the same time, for an unknown username, a wrong password, a locked or a deactivated account (docs/adr/0033 D6) |
| `forbidden` | 403 | The person's role does not allow the act (docs/adr/0034) |
| `insufficient_scope` | 403 | The token's scope does not reach the act (docs/adr/0035 D3) |
| `agent_forbidden` | 403 | The act is on the agent hard-off list, needs a capability the token lacks, or lies outside what the capability grants — `close` closes from in-progress and review only; `detail` names which (docs/adr/0043 D4, D5) |
| `session_required` | 403 | The route is for a person in a browser session; a personal access token cannot call it (docs/adr/0035 D5) |
| `password_change_required` | 403 | The session's account has a temporary password, which has to be changed before anything else (docs/adr/0033 D4) |
| `not_initialised` | 403 | The installation has no tenant yet and the person is not a global administrator (docs/adr/0032 D5) |
| `csrf` | 403 | A cookie-authenticated write, or the login, did not come from COWORK_BASE_URL or lacks `X-Requested-With: cowork` (docs/adr/0037 D1) |
| `not_found` | 404 | No such route, or a tenant, project or ticket the caller cannot see — the answer does not say which (docs/adr/0047 D5) |
| `person_not_found` | 404 | No active person has this e-mail address or username — nobody logged in with it through the identity provider and no local account has it — or the person named is not a member of the tenant (docs/adr/0030 D3) |
| `method_not_allowed` | 405 | The path exists with other methods; `Allow` names them |
| `username_taken` | 409 | The installation has a person with this username; usernames are unique (docs/adr/0033 D2) |
| `tenant_slug_taken` | 409 | The installation has a tenant with this slug; slugs are never reused (docs/adr/0005 D4) |
| `project_key_taken` | 409 | The tenant has a project with this key; keys are never reused (docs/adr/0007 D4) |
| `repository_bound` | 409 | Another project of the tenant binds this repository and path; a repository is in at most one project (docs/adr/0066 D6) |
| `person_ambiguous` | 409 | Several persons have this e-mail address, which is a display attribute and not an identity (docs/adr/0029 D5); nobody was granted |
| `grant_exists` | 409 | The person holds a grant in this tenant already; change its role with `PUT …/members/{person_id}/grant` (docs/adr/0030 D3) |
| `mapping_exists` | 409 | The tenant maps this group already; change that mapping's role instead (docs/adr/0030 D2) |
| `last_admin` | 409 | The change would leave the tenant without an administrator who can sign in: nobody active and admitted by the gate would hold the admin role, mapped or granted (docs/adr/0034 D1) |
| `project_archived` | 409 | An archived project refuses new tickets (docs/adr/0006 D4) |
| `state_conflict` | 409 | The ticket is not in the state the request assumed, or its state does not allow the change; `errors[]` names the current state (docs/adr/0045 D2) |
| `parent_cycle` | 409 | The new parent is the ticket itself or one of its descendants (docs/adr/0008 D2) |
| `link_cycle` | 409 | The blocks link would close a cycle of prerequisites (docs/adr/0012 D4) |
| `open_prerequisites` | 409 | Tickets that block this one are not done or dropped; `errors[]` lists them, and a person may override with a reason (docs/adr/0012 D7) |
| `period_locked` | 409 | The day lies on or before the tenant's time_locked_until: the period is closed to new, changed and voided entries (docs/adr/0017 D8) |
| `attachment_limit` | 409 | The ticket holds as many attachments as COWORK_ATTACHMENT_MAX_PER_TICKET allows (docs/adr/0016 D6) |
| `uploads_disabled` | 501 | The installation has no object storage configured; attachments cannot be uploaded (docs/adr/0016 D1) |
| `chat_unavailable` | 409 | The tenant has no chat: the installation configures no provider; `GET …/chat` says so (docs/adr/0076) |
| `precondition_failed` | 412 | The `If-Match` version is stale; the response carries the current `ETag` and `errors[]` the current values (docs/adr/0050 D5) |
| `payload_too_large` | 413 | The body is larger than the configured limit (docs/adr/0039 D2) |
| `unsupported_media_type` | 415 | The body's type is not one the route accepts |
| `idempotency_mismatch` | 422 | The `Idempotency-Key` was used before with a different request (docs/adr/0045 D4) |
| `precondition_required` | 428 | An overwriting write came without `If-Match` (docs/adr/0050 D3) |
| `too_many_attempts` | 429 | More login attempts from this address within a minute than COWORK_LOGIN_ADDRESS_LIMIT allows; `Retry-After` says how long to wait (docs/adr/0033 D6) |
| `chat_busy` | 429 | The person has as many turns of the chat running as COWORK_CHAT_TURNS_PER_PERSON allows on this replica — in another tab, say; one ends, or is stopped with `DELETE …/chat/turns`, first (docs/adr/0076) |
| `internal` | 500 | Something failed inside cowork; the `request_id` finds it in the log |
| `chat_provider_failed` | 502 | The chat's provider could not be reached, refused the request, or answered what cowork cannot read; `detail` says which, never with the provider's answer. It comes as the `error` event of a chat turn, whose answer has begun (docs/adr/0076) |
| `not_ready` | 503 | The backend cannot do the work now: it cannot reach its database, it streams no events, or it is shutting down and ends a turn of the chat |
| `timeout` | 504 | The request took longer than the configured limit (docs/adr/0039 D2) |
<!-- problem-codes:end -->

### Helm chart values

Source: [`deploy/helm/cowork/values.yaml`](deploy/helm/cowork/values.yaml). Every value below is the default.

```yaml
nameOverride: ""
fullnameOverride: ""
imagePullSecrets: []
serviceAccount:
  create: true
  annotations: {}
  name: ""
  automountServiceAccountToken: false # neither pod talks to the Kubernetes API
database:                             # the runtime role: owns nothing, held to row-level security
  existingSecret: ""                  # preferred: the URL never enters the release
  existingSecretKey: databaseUrl
  url: ""                             # renders <fullname>-database; plain text in the release Secret and in `helm get values`
  owner:                              # the role the migrations run as; only the migrate init container reads it
    existingSecret: ""                # preferred; this or url is required while backend.config.migrateOnStart is true
    existingSecretKey: databaseUrl
    url: ""                           # renders <fullname>-database-owner while migrateOnStart is true; plain text in the release either way
session:                              # COWORK_SESSION_KEY, from a Secret only
  existingSecret: ""                  # required: rendering fails without it
  keys:
    key: sessionKey
  lifetime: 12h                       # COWORK_SESSION_LIFETIME, the absolute lifetime of a browser session
  idle: 2h                            # COWORK_SESSION_IDLE, how long a session may lie unused
localAdmin:                           # the local administrator of docs/adr/0032: a global administrator kept in step with this Secret at every start
  existingSecret: ""                  # preferred; with the inline values empty there is no local administrator and nobody can log in
  keys:
    username: username                # COWORK_LOCAL_ADMIN_USERNAME
    password: password                # COWORK_LOCAL_ADMIN_PASSWORD
  username: ""                        # renders <fullname>-local-admin; plain text in the release and in `helm get values`
  password: ""                        # both inline values or neither; the existing Secret wins; needs backend.config.baseURL
bootstrap:
  tenant:                             # created at start while no tenant exists; once one does, these do nothing; needs localAdmin or auth.oidc.adminGroup
    slug: ""                          # COWORK_BOOTSTRAP_TENANT_SLUG; the administrator group, when set, is mapped to its admin role
    name: ""                          # COWORK_BOOTSTRAP_TENANT_NAME; both or neither
auth:
  local:
    passwordMinLength: 12             # COWORK_PASSWORD_MIN_LENGTH, characters; below 8 the backend refuses to start
    lockout: window                   # COWORK_LOGIN_LOCKOUT: window | admin
    maxFailures: 5                    # COWORK_LOGIN_MAX_FAILURES within fifteen minutes; 0 never locks
    addressLimit: 20                  # COWORK_LOGIN_ADDRESS_LIMIT attempts per client address (IPv6: per /64) and minute; 0 disables
  oidc:                               # the login through an OpenID Connect provider; every other value is rendered only with the issuer
    issuer: ""                        # COWORK_OIDC_ISSUER, e.g. https://login.example.com; its discovery is fetched at every start, which fails when it cannot be; needs backend.config.baseURL
    clientId: ""                      # COWORK_OIDC_CLIENT_ID; or from existingSecret, with keys.clientId
    existingSecret: ""                # required with the issuer: the client secret comes from a Secret only, there is no inline value
    keys:
      clientSecret: clientSecret      # COWORK_OIDC_CLIENT_SECRET
      clientId: ""                    # empty: the client id is clientId above
    scopes: openid profile email groups offline_access # COWORK_OIDC_SCOPES; offline_access brings the refresh token the groups refresh needs
    groupsClaim: groups               # COWORK_OIDC_GROUPS_CLAIM
    allowedGroups: []                 # COWORK_OIDC_ALLOWED_GROUPS, rendered comma-separated: the gate; a name with a comma fails rendering
    adminGroup: ""                    # COWORK_ADMIN_GROUP: global administrators; with allowedGroups empty as well, nobody logs in through the provider
    groupsRefresh: 15m                # COWORK_OIDC_GROUPS_REFRESH, at least 1m
    groupsMaxAge: 168h                # COWORK_OIDC_GROUPS_MAX_AGE, longer than groupsRefresh: older groups refuse the person's tokens until they sign in
    emailTrusted: false               # COWORK_OIDC_EMAIL_TRUSTED: true lets a grant by address match a person without the email_verified claim; only for a provider whose addresses administrators issue
    displayName: single sign-on       # COWORK_OIDC_DISPLAY_NAME: "Sign in with <name>"
storage:                              # S3-compatible object storage; without an endpoint uploads are refused
  existingSecret: ""                  # required with an endpoint: the key of a bucket-scoped policy, never root
  keys:
    accessKeyId: accessKeyId          # COWORK_S3_ACCESS_KEY_ID
    secretAccessKey: secretAccessKey  # COWORK_S3_SECRET_ACCESS_KEY
  endpoint: ""                        # COWORK_S3_ENDPOINT, e.g. https://s3.example.com; setting it turns the storage on
  bucket: ""                          # COWORK_S3_BUCKET; required with an endpoint
  region: ""                          # COWORK_S3_REGION, set only when non-empty; empty asks the server
  pathStyle: true                     # COWORK_S3_USE_PATH_STYLE
  tls:
    caConfigMap: ""                   # ConfigMap with a private authority's PEM, mounted at /etc/cowork/s3-ca
    keys:
      ca: ca.crt                      # the ConfigMap's key; COWORK_S3_CA=/etc/cowork/s3-ca/<key>
chat:                                 # the chat in the UI; every value is rendered only with a provider
  providers: []                       # COWORK_CHAT_PROVIDERS and COWORK_CHAT_<ID>_*: the person picks one, the first is the default.
                                      # Each: {id, name, kind: openai|anthropic, url, model, existingSecret, keys: {apiKey: apiKey}};
                                      # a key from that provider's own Secret only, an inline apiKey fails rendering.
                                      # Every provider receives what the chat reads in every tenant, confidential tickets included
  turnTimeout: 5m                     # COWORK_CHAT_TURN_TIMEOUT; 0 disables
  maxSteps: 8                         # COWORK_CHAT_MAX_STEPS; 0 disables
ingress:                              # per host: /api/ and /auth/ (Prefix) to the backend Service, / (Prefix) to the frontend Service
  enabled: false
  className: ""
  annotations: {}                     # the controller's settings: body limit and read timeout above the backend's, text/event-stream unbuffered — docs/operations/installation.md#expose-it
  hosts:
    - host: cowork.example.com        # example; the paths are the chart's, a host has none of its own
  tls: []
backend:
  replicaCount: 1                     # every pod migrates in its init container; they serialise on an advisory lock
  image:                              # the migrate init container runs the same image
    repository: guidedtraffic/cowork-backend
    pullPolicy: IfNotPresent
    tag: ""                           # default: the chart appVersion
  containerPort: 8080                 # COWORK_LISTEN_ADDR=":8080"
  service:
    type: ClusterIP
    port: 8080
  config:
    migrateOnStart: true              # the migrate init container; false: nothing migrates — run `cowork migrate` yourself
    logLevel: info                    # COWORK_LOG_LEVEL, also for the init container
    logFormat: json                   # COWORK_LOG_FORMAT, also for the init container
    shutdownTimeout: 15s              # COWORK_SHUTDOWN_TIMEOUT; keep below terminationGracePeriodSeconds
    baseURL: ""                       # COWORK_BASE_URL, set only when non-empty: the origin the browser shows; required with localAdmin or auth.oidc.issuer
    trustedProxies: ""                # COWORK_TRUSTED_PROXIES, set only when non-empty: the Ingress controller's networks, comma-separated CIDRs
    maxJsonBody: 1048576              # COWORK_MAX_JSON_BODY, bytes; 0 disables
    attachmentMaxBytes: 10485760      # COWORK_ATTACHMENT_MAX_BYTES, bytes; 0 disables (the notes then ask the controller for no body limit either)
    attachmentMaxPerTicket: 100       # COWORK_ATTACHMENT_MAX_PER_TICKET; 0 disables
    requestTimeout: 30                # COWORK_REQUEST_TIMEOUT, seconds; 0 disables; the event stream is exempt
    maxPageSize: 200                  # COWORK_MAX_PAGE_SIZE; 0 disables
    maxQueryLength: 256               # COWORK_MAX_QUERY_LENGTH, characters; 0 disables
    sseReplayWindow: 5m               # COWORK_SSE_REPLAY_WINDOW
    sseMaxStreamsPerPerson: 10        # COWORK_SSE_MAX_STREAMS_PER_PERSON, per replica; 0 disables
  extraEnv: []                        # appended verbatim to the backend container's env
  podAnnotations: {}
  podLabels: {}
  podSecurityContext:                 # distroless nonroot user
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    fsGroup: 65532
    seccompProfile:
      type: RuntimeDefault
  securityContext:                    # the binary writes nothing to disk; the init container has the same
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    capabilities:
      drop: [ALL]
  resources:                          # the init container has the same
    limits:
      memory: 256Mi
    requests:
      cpu: 50m
      memory: 128Mi
  probes:
    startup:                          # /healthz; the migration ran before, in the init container
      periodSeconds: 5
      failureThreshold: 36
    liveness:                         # /healthz
      periodSeconds: 10
    readiness:                        # /readyz, a database ping
      periodSeconds: 10
  terminationGracePeriodSeconds: 30
  nodeSelector: {}
  tolerations: []
  affinity: {}
frontend:
  replicaCount: 1
  image:
    repository: guidedtraffic/cowork-frontend
    pullPolicy: IfNotPresent
    tag: ""                           # default: the chart appVersion
  containerPort: 8080                 # fixed by the nginx configuration in the image
  service:
    type: ClusterIP
    port: 80
  extraEnv: []                        # the chart sets none; the image's entrypoint switches, e.g. NGINX_ENTRYPOINT_QUIET_LOGS
  podAnnotations: {}
  podLabels: {}
  podSecurityContext:                 # nginx-unprivileged user
    runAsNonRoot: true
    runAsUser: 101
    runAsGroup: 101
    fsGroup: 101
    seccompProfile:
      type: RuntimeDefault
  securityContext:                    # the emptyDir at /tmp is all nginx writes; the configuration is in the image
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    capabilities:
      drop: [ALL]
  resources:
    limits:
      memory: 64Mi
    requests:
      cpu: 10m
      memory: 32Mi
  probes:
    liveness:                         # /healthz, answered by nginx
      periodSeconds: 10
    readiness:
      periodSeconds: 10
  terminationGracePeriodSeconds: 30
  nodeSelector: {}
  tolerations: []
  affinity: {}
```

Rendering fails, naming the value, without `database.existingSecret` or `database.url`;
without `database.owner.existingSecret` or `database.owner.url` while
`backend.config.migrateOnStart` is `true`; without `session.existingSecret`; with a
`storage.endpoint` but no `storage.bucket` or no `storage.existingSecret`; with one of
`localAdmin.username` and `localAdmin.password` alone, or a local administrator without
`backend.config.baseURL`; with an `auth.oidc.issuer` but no `backend.config.baseURL`, no client
id (`auth.oidc.clientId`, or `auth.oidc.keys.clientId` with the Secret) or no
`auth.oidc.existingSecret`, or with a group in `auth.oidc.allowedGroups` that holds a comma; and
with `bootstrap.tenant.slug` and `.name` apart, or without a local administrator or an
`auth.oidc.adminGroup` of a configured issuer. `helm lint` reports
these as info lines, `helm template` and `helm install` fail. Where a Secret reference and its
inline URL are both set, the reference wins and the URL is ignored.

The modes that change what is exposed:

- **Secret references or inline URLs.** With `database.existingSecret` and
  `database.owner.existingSecret` no credential enters the release. `database.url` and
  `database.owner.url` are for throw-away installations: each is stored in plain text in the
  Helm release Secret and returned by `helm get values`, and the chart's notes warn for each.
  The owner URL is the worse one to inline — its role can switch row-level security off.
- **The owner credential reaches the `migrate` init container only.** The serving container
  gets the runtime URL and `COWORK_MIGRATE_ON_START=false`; never add the owner URL to
  `backend.extraEnv`. With `migrateOnStart: false` the chart needs no owner credential and
  renders no owner Secret — an inline `database.owner.url` still stays in the release values,
  readable with `helm get values`, so leave it empty — and nothing migrates: the backend
  refuses to start until `cowork migrate` has run.
- **The server key has no inline path.** One Secret gives every replica the same key;
  rotating it invalidates the cursors clients hold and ends each session of the identity provider
  that holds a refresh token at its next refresh.
- **The identity provider's client secret comes from a Secret only.** `auth.oidc.existingSecret`
  names it — there is no inline value — and the client id is a value or the same Secret's
  `auth.oidc.keys.clientId`. Every other `auth.oidc` value is rendered only while
  `auth.oidc.issuer` is set, so emptying the issuer alone switches the provider off; the notes name
  the redirect URI to register, `<backend.config.baseURL>/auth/callback`, and warn while the gate
  names no group. The members of `auth.oidc.adminGroup` are global administrators: whoever may change
  that group at the provider administers the installation
  ([operations](docs/operations/installation.md#the-identity-provider)).
- **The local administrator: a Secret or inline values.** `localAdmin.existingSecret` names a
  Secret whose keys `localAdmin.keys.username` and `.password` hold the account; the chart never
  sees the password. `localAdmin.username` and `.password` render `<fullname>-local-admin` for
  a throw-away installation: plain text in the release Secret and in `helm get values`, like
  `database.url`, with a warning in the notes. The account follows the Secret at every start —
  a changed value reaches the pods when they restart; rotate the Secret **and** restart to end a
  leaked password ([operations](docs/operations/installation.md#the-local-administrator)).
- **`backend.config.trustedProxies` and network policies.** Empty, the login throttle counts the
  Ingress controller pod as the one client of every browser behind it; set to the controller's
  networks it counts the browser. Too wide a list lets a client choose its address, and a network
  that holds other pods lets every one of them that reaches the backend choose its own. The chart
  ships no NetworkPolicy — `networkPolicy.*` is gone, and network policies are the cluster
  administrator's —, so keeping other pods away from the backend is a policy of the cluster's. The
  chart's notes warn about the empty list and about the list set
  ([operations](docs/operations/installation.md#the-client-address-and-the-trusted-proxies)).
- **`ingress.annotations` are the controller's limits.** The Ingress routes `/api/` and `/auth/`
  to the backend, so the controller's body limit and read timeout must sit above the backend's or
  it answers the `413` and `504` itself, as its own page; the chart does not know the controller
  and prints the figures in its notes ([expose it](docs/operations/installation.md#expose-it)).
- **The storage key comes from a Secret only**; endpoint, bucket and region are plain values.
  Without `storage.endpoint` the backend runs without object storage and refuses uploads.
- **`0` in `backend.config`** switches a backend limit off, and the chart's notes then ask the
  Ingress controller for no limit either — `maxJsonBody: 0` for no body limit, `requestTimeout: 0`
  for an hour's read timeout. No production values file should carry one
  ([runtime.md, limits](docs/operations/runtime.md#limits)). `attachmentMaxBytes: 0` removes the
  upload maximum; one upload at a time is then read whole
  ([attachments.md H-12](docs/security/attachments.md#h-12)).

## 🛠 Development

```bash
make help                 # every target, grouped
make dev                  # the whole stack with demo data; the UI on :4200 with live reload
make generate             # after a change to backend/api/, the SQL queries or the problem catalogue; CI fails on drift
make frontend-generate    # after make generate changed the API document: the Angular client; CI fails on drift
make lint cyclo gosec vuln
make dev-up               # what the integration tier needs: PostgreSQL, MinIO and Dex (dex-up and dex-down alone)
make test test-integration
make frontend-lint frontend-test-coverage frontend-build
make docker-build e2e     # the end-to-end suite in Chromium and WebKit against both images (make e2e-browsers once)
make build                # bin/cowork and frontend/dist/frontend/browser
make build-mcp            # bin/cowork-mcp; GOOS= GOARCH= cross-compile
make docker-build         # both images, from backend/Containerfile and frontend/Containerfile
```

[docs/developer/](docs/developer/README.md) has the layout, the package map, the architecture,
the build and test matrix, the checklists, the toolchain versions and the CI and release
process.

## 📄 License

[Apache License 2.0](LICENSE).

The UI uses PrimeNG, PrimeIcons and `@primeuix/themes`, which since version 22 are under the
PrimeUI License: free under its Community License with a key, which the released images carry.
A build without a key works and shows PrimeNG's license notice; a fork needs a key of its own
([ADR 0052](docs/adr/0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md)
D9).
