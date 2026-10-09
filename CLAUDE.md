# cowork

Repo: https://github.com/guided-traffic/cowork — a multi-tenant backlog and kanban board for
one person working across many projects with an LLM as co-worker. Two containers: a Go
backend (`backend/`, the API, PostgreSQL 18 migrated on start) and an nginx frontend
(`frontend/`, the Angular bundle and nothing else); one Helm chart, whose Ingress routes `/api/`
and `/auth/` to the backend and the rest to the frontend; and a third binary, `cowork-mcp`, on a
person's machine for Claude Code.
**Status: phases 6 (import and cut-over) and 7 (hardening and 1.0), the plan's last, started on
2026-10-06 and are worked as tickets; phase 3 (UI v1) awaits the owner's reviews; the sign-in
without a click is released as `0.8.0`, metrics, the chart's references with the migration Job and
the GitHub webhook as `0.9.0`, the consistency check as `0.10.0`, the import and the export as
`0.11.0`, the hardening that the review of the security pages found as `0.12.0`; 1.0 waits until every open question is answered, by the owner's rule of 2026-10-06
(ADR 0003 D9)** — the work lists, each open phase a family ticket with its children, are in
[docs/tickets/](docs/tickets/README.md). Every founding decision is an ADR, and the project plan
was consumed into the phase tickets (ADR 0074).

## Language policy

All code, comments, commit messages, documentation and configuration in this repository
**must be written in English**. Conversation with the owner may be German.

## Where things are, and where a statement goes

A statement has exactly one home
([ADR 0002](docs/adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)):

| Kind | Home |
|---|---|
| A decision — what cowork does and why, what was rejected | an [ADR](docs/adr/README.md) |
| How the code works and how to contribute | [docs/developer/](docs/developer/README.md) — there is no `DEVELOPER.md` (ADR 0075) |
| What somebody running cowork needs | [docs/operations/](docs/operations/README.md) |
| The threat model and the gap each mechanism leaves | [docs/security/](docs/security/README.md), one page per perspective, each ending with `## What this does not cover`; reporting is [SECURITY.md](SECURITY.md) |
| Work still outstanding | a [ticket](docs/tickets/README.md), archived when the work lands |
| The reference tables (configuration, CLI, API, Helm values) | [README.md](README.md) and nowhere else |
| An open decision | the `## Open questions` section of a [ticket](docs/tickets/README.md) |
| The plan | a phase's family ticket and its children in [docs/tickets/](docs/tickets/README.md) — the project plan was consumed into them (ADR 0074) |

**Read the page for a subsystem before you change it, and update it in the same change.**

## Decisions live in ADRs; tickets are work lists that get archived

- **Every durable decision is an ADR** in the format of [docs/adr/README.md](docs/adr/README.md),
  written in the session the decision is taken. Changing behaviour an ADR describes means
  updating that ADR in the same change; a superseded rule is marked in place, never silently
  removed. Every claim is verified against the code; anything unverified says so.
- **A ticket is a work list and nothing else:** `docs/tickets/NNN-<slug>.md`, frontmatter per
  the rules page, current state only (no History, no strike-throughs, no dated annotations),
  closed by extraction then moved to `docs/tickets/archive/`. A finding goes into an existing
  ticket first. A number is never reused.
- **An open security finding is embargoed:** `security: live|boundary` unfixed → the file is
  `local_NNN-<slug>.md` (gitignored), and no tracked file, commit or PR carries its details or
  its file name.
- **Nothing outside `docs/tickets/` cites a ticket** — not by number, label, path or file name.
  Cite the ADR.

## Open decisions are worked one question at a time

The founding question catalog is consumed: every founding question became an ADR (ADR 0074).
A new open decision lives in a ticket's `## Open questions` section.
Present **one** question per turn to the owner, with the options researched against this tree
and the recommended one justified. An answered question becomes an ADR (or an amendment) in
the same session. A question that needs code to answer becomes a ticket. Do not build on an
unanswered question.

## Stack facts

- Backend: module `github.com/guided-traffic/cowork/backend`, Go 1.27 (`backend/go.mod`).
  `/api/` is routed and validated against the OpenAPI document by kin-openapi and served by
  the oapi-codegen strict server on `net/http`; sqlc over `pgx/v5`, `golang-migrate` over
  embedded SQL, `minio-go`, `log/slog`, `testify`. It serves no UI: unknown path → JSON 404,
  wrong method → 405.
- Frontend: `frontend/` is an Angular 22 CLI workspace (project `frontend`): standalone
  components, signals, zoneless, vitest on jsdom, angular-eslint; PrimeNG 22 with the cowork
  preset over Aura (colours only as its tokens, dark first, both schemes must work), the API
  client generated by ng-openapi-gen into `src/app/api/` (`make frontend-generate`, never
  edited). PrimeNG is under the PrimeUI Community License: its key lives in `.dev/primeui-license`
  or `PRIMEUI_LICENSE` and **never in the repository** (ADR 0052 D9). How the UI is built:
  [docs/developer/frontend.md](docs/developer/frontend.md); `make dev` runs the whole stack with
  demo data and the UI on `https://localhost:4200` (self-signed: Safari stores no `Secure` cookie
  from plain-HTTP localhost) for the owner to watch; the browser logs in as `dev` /
  `dev-only-cowork`, and the dev proxy — the developer's stand-in for the Ingress — holds no
  credential. The container is `nginxinc/nginx-unprivileged` with
  [`frontend/nginx/default.conf`](frontend/nginx/default.conf), a plain file, nothing substituted
  at start: `/healthz` itself, hashed bundles immutable, everything else `index.html` with
  `no-store`, the shell's `Content-Security-Policy` on all it serves of the UI (every source
  `'self'`; `'unsafe-inline'` for styles only), and a `404` problem for `/api/` and `/auth/`,
  which the Ingress routes to the backend — the frontend never reaches it (ADR 0001 D3). Runs of
  the two images put [`hack/ingress/default.conf`](hack/ingress/default.conf) in front as the
  Ingress stand-in.
- Both toolchains track the newest stable release (ADR 0001 D9); TypeScript stays in
  Angular's peer range. Do not pin back.
- Backend configuration is `COWORK_*` environment variables only
  ([`backend/internal/config`](backend/internal/config/config.go)); the chat's providers are
  `COWORK_CHAT_PROVIDERS` and `COWORK_CHAT_<ID>_*`, never a request's — a request picks one of
  them by its id.
- Migrations: `backend/internal/store/migrations/NNNNNN_<snake_name>.up.sql`, versions `1..n`
  without a gap, **no down files**; a unit test enforces it. A migration never drops, renames
  or narrows what the previous release reads (expand before contract, ADR 0028). They run as
  the owner role (`COWORK_DATABASE_OWNER_URL`) — the chart's `migrate` init container, locally
  `make run` and `make migrate`; `serve` connects as the runtime role and refuses a dirty
  schema or pending migrations.
- The chart is `deploy/helm/cowork/`: `backend.*`, `frontend.*`, `database.*` (with
  `database.owner.*`; a URL or its components, ADR 0058), `migrations.*` (`onStart`, or `job` — a
  pre-install/pre-upgrade hook Job, ADR 0057), `session.*`, `storage.*`, `localAdmin.*`,
  `bootstrap.*`, `auth.*`, `chat.*`, `metrics.*` (the metrics port on the pods, never on a Service;
  the monitors off by default, ADR 0060), `ingress.*` (per host `/api/` and `/auth/` to the backend Service, `/` to the frontend
  Service; the controller's limits are annotations the installation sets). No NetworkPolicy:
  network policies are the cluster administrator's. The runtime and the owner URL each come from an
  `existingSecret` (preferred) or a `url` (throw-away only, plain text in the release); the owner
  URL reaches only the migration run — the `migrate` init container, or the migration Job in job mode,
which takes `existingSecret` references only; the session key, the storage key, the identity
  provider's client secret and the chat's API key come from Secrets only, the local
  administrator's password from an `existingSecret` (preferred) or inline values with the same
  warning as `database.url`.
- Login (phase 3): server-side sessions in the `__Host-cowork-session` cookie, the local
  administrator from configuration, local accounts made by administrators, CSRF by origin and
  `X-Requested-With: cowork`; token creation, password changes, tenant creation, creating or
  resetting a local account, the acts that give access, a turn of the chat, the purge of a ticket
  and the removal of orphaned objects are session-only — eighteen operations, held by a unit test;
  a token gets `403` (ADR 0031–0033, 0035, 0037). A session's idle clock moves only on a write or
  the browser's keep-alive read, and the login page signs a person of the identity provider in
  again with `prompt=none` without a click (ADR 0029 D6, 0031 D3).
- `cowork-mcp` (`backend/cmd/cowork-mcp` over `internal/mcpcli`, `internal/mcpserver`,
  `internal/tools`): the MCP server and hooks for Claude Code, a client of `/api/v1` through the
  generated client and nothing else — it imports no store and no API handler, and a unit test
  holds that (ADR 0040). `make build-mcp`; the Claude Code plugin is `claude/cowork/`. How it is
  built: [docs/developer/mcp.md](docs/developer/mcp.md).
- The chat in the UI (ADR 0076): `POST /api/v1/tenants/{tenant}/chat`, a session only, streams a
  turn through the provider the person picks from the chart's list; the loop (`internal/chat`)
  calls the model through `internal/llm` (OpenAI Chat Completions or Anthropic Messages) and runs
  the same `internal/tools` catalogue in-process through the server's own handler as the person's
  agent, `chat/<model>/<conversation>`, with the capabilities the person chose (`/me/chat`) and no
  confirmations; `DELETE …/chat/turns` stops the person's running turns. A configured provider
  sees everything the person can read — the owner's accepted risk. The integration tier talks to
  `test/stubllm`, never to a real model. How it is built:
  [docs/developer/chat.md](docs/developer/chat.md).

## Testing

**`make` is the entry point, from the repository root**, for CI and for you
([ADR 0003](docs/adr/0003-test-and-ci-policy.md)); Go targets `cd backend`, npm targets `cd frontend`.

| Tier | Target | Needs |
|---|---|---|
| Backend unit | `make test-unit` | nothing |
| Backend integration (tag `integration`) | `make postgres-up postgres-tls-up minio-up dex-up && make test-integration` | Docker; `POSTGRES_PORT=`, `POSTGRES_TLS_PORT=`, `MINIO_PORT=` and `DEX_PORT=` move the containers |
| Frontend unit | `make frontend-test` | Node.js 26 |
| Static analysis | `make lint cyclo gosec vuln`, `make frontend-lint` | — |
| Chart | `make helm-lint helm-template` | Helm |
| Images | `make docker-build` (both) and a read-only run of both containers together behind the Ingress stand-in, see [docs/developer/build-test-lint.md](docs/developer/build-test-lint.md) | Docker |
| Everything a PR gets | see [docs/developer/ci-and-release.md](docs/developer/ci-and-release.md) | |

No `-short`, no `testing.Short()`, no skip on a missing dependency: the integration tier fails
without `COWORK_TEST_DATABASE_URL`, the `COWORK_TEST_DATABASE_TLS_*` or the `COWORK_TEST_S3_*`
variables and says how to set them. A fix comes with the test that failed without it. Every CI job is in the `needs:` list
of `semantic-release`; a new job is added there in the same change.

## Conventions that bite

- Conventional commits, **no apostrophe anywhere in a commit message**, scope where one
  exists. semantic-release reads them.
- Errors wrap with `%w` and a verb; the configuration error names the variable and never
  echoes the value of a URL, a key or a secret.
- The request log carries method, path, status, duration and the request id — no bodies, no
  headers, no query.
- Every API error is an RFC 9457 problem details body (`application/problem+json`: `type`,
  `title`, `status`, `detail`, `instance`, `code`, `request_id`, and `errors[]` for invalid
  fields), written through `problem.Write` with a code from the catalogue in
  `backend/internal/problem` — never an ad-hoc JSON error (ADR 0047).
- Every Go tool runs inside `backend/`; the frontend tree is never in its path. nginx has no
  unit test: a change to its configuration is verified by running the image behind the Ingress
  stand-in.

# Important Notes

- Never commit or push without the owner's permission; never develop across repositories
  unprompted — a needed change elsewhere is a `local_<ticketname>.md` ticket here.
- Scrutinise security-relevant requests; name the risk, propose the safer alternative,
  discuss, then implement and document the tradeoff if the owner accepts it.
- Separate verified from unverified in every report. "Not verified, and this is the gap" is a
  complete sentence.
- A ticket describes the work as intended; the code and the ADRs describe fact. When they
  disagree, the code wins and the disagreement is named.

## graphify

When a `graphify-out/` directory exists, read `graphify-out/GRAPH_REPORT.md` for the
community structure before searching raw files; `.graphifyignore` excludes generated output
and the ticket archive.
