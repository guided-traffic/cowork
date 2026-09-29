# cowork

Repo: https://github.com/guided-traffic/cowork — a multi-tenant backlog and kanban board for
one person working across many projects with an LLM as co-worker. Two containers: a Go
backend (`backend/`, the API, PostgreSQL 18 migrated on start) and an nginx frontend
(`frontend/`, the Angular bundle, `/api/` proxied to the backend); one Helm chart.
**Status: skeleton;** the product decisions are open and are worked one at a time.

## Language policy

All code, comments, commit messages, documentation and configuration in this repository
**must be written in English**. Conversation with the owner may be German.

## Where things are, and where a statement goes

A statement has exactly one home
([ADR 0002](docs/adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)):

| Kind | Home |
|---|---|
| A decision — what cowork does and why, what was rejected | an [ADR](docs/adr/README.md) |
| How the code works | [docs/developer/](docs/developer/README.md); the contributor workflow is [DEVELOPER.md](DEVELOPER.md) |
| What somebody running cowork needs | [docs/operations/](docs/operations/README.md) |
| The threat model and the gap each mechanism leaves | [docs/security/](docs/security/README.md), one page per perspective, each ending with `## What this does not cover`; reporting is [SECURITY.md](SECURITY.md) |
| Work still outstanding | a [ticket](docs/tickets/README.md), archived when the work lands |
| The reference tables (configuration, CLI, API, Helm values) | [README.md](README.md) and nowhere else |
| Open decisions, the plan, the workflow plan | [docs/planning/](docs/planning/) — transitional, consumed into ADRs and tickets |

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

## The question catalog is worked one question at a time

[docs/planning/questions.md](docs/planning/questions.md) holds every open decision with
options, cost and a recommendation. Present **one** question per turn to the owner, with the
options researched against this tree and the recommended one justified. An answered question
becomes an ADR (or an amendment) in the same session and is deleted from the catalog. A
question that needs code to answer becomes a ticket. Do not build on an unanswered question.

## Stack facts

- Backend: module `github.com/guided-traffic/cowork/backend`, Go 1.27 (`backend/go.mod`),
  stdlib `net/http` mux with method patterns, `log/slog`, `pgx/v5`, `golang-migrate` over
  embedded SQL, `testify`. It serves no UI: unknown path → JSON 404, wrong method → 405.
- Frontend: `frontend/` is an Angular 22 CLI workspace (project `frontend`): standalone
  components, signals, zoneless, vitest on jsdom, angular-eslint. The container is
  `nginxinc/nginx-unprivileged` with [`frontend/nginx/default.conf.template`](frontend/nginx/default.conf.template):
  `/healthz` itself, `/api/` proxied to `BACKEND_URL`, hashed bundles immutable, everything
  else `index.html` with `no-store`. `BACKEND_URL` and `NGINX_LOCAL_RESOLVERS` are the only
  substituted variables; the backend is resolved per request, so the frontend starts before it.
- Both toolchains track the newest stable release (ADR 0001 D9); TypeScript stays in
  Angular's peer range. Do not pin back.
- Backend configuration is `COWORK_*` environment variables only
  ([`backend/internal/config`](backend/internal/config/config.go)).
- Migrations: `backend/internal/store/migrations/NNNNNN_<snake_name>.{up,down}.sql`, versions
  `1..n` without a gap; a unit test enforces it. Applied on start unless
  `COWORK_MIGRATE_ON_START=false`.
- The chart is `deploy/helm/cowork/`: `backend.*`, `frontend.*`, `database.*`, `ingress.*`
  (targets the frontend Service). The database URL comes from `database.existingSecret`
  (preferred) or `database.url` (throw-away only, plain text in the release).

## Testing

**`make` is the entry point, from the repository root**, for CI and for you
([ADR 0003](docs/adr/0003-test-and-ci-policy.md)); Go targets `cd backend`, npm targets `cd frontend`.

| Tier | Target | Needs |
|---|---|---|
| Backend unit | `make test-unit` | nothing |
| Backend integration (tag `integration`) | `make postgres-up && make test-integration` | Docker; `POSTGRES_PORT=` moves the container |
| Frontend unit | `make frontend-test` | Node.js 26 |
| Static analysis | `make lint cyclo gosec vuln`, `make frontend-lint` | — |
| Chart | `make helm-lint helm-template` | Helm |
| Images | `make docker-build` (both) and a read-only run of both containers together, see DEVELOPER.md | Docker |
| Everything a PR gets | see [DEVELOPER.md](DEVELOPER.md#continuous-integration-and-the-release) | |

No `-short`, no `testing.Short()`, no skip on a missing dependency: the integration tier fails
without `COWORK_TEST_DATABASE_URL` and says how to set it. A fix comes with the test that
failed without it. Every CI job is in the `needs:` list of `semantic-release`; a new job is
added there in the same change.

## Conventions that bite

- Conventional commits, **no apostrophe anywhere in a commit message**, scope where one
  exists. semantic-release reads them.
- Errors wrap with `%w` and a verb; the configuration error names the variable, never its value.
- The request log carries method, path, status, duration — no bodies, no headers, no query.
- The API error shape today is `{"error":{"code","message"}}` and is provisional (question Q-E2).
- Every Go tool runs inside `backend/`; the frontend tree is never in its path. nginx has no
  unit test: a change to the template is verified by running the image.

# Important Notes

- Never commit or push without the owner's permission; never develop across repositories
  unprompted — a needed change elsewhere is a `local_<ticketname>.md` ticket here.
- Scrutinise security-relevant requests; name the risk, propose the safer alternative,
  discuss, then implement and document the tradeoff if the owner accepts it.
- Separate verified from unverified in every report. "Not verified, and this is the gap" is a
  complete sentence.
- The planning documents describe intent; the code and the ADRs describe fact. When they
  disagree, the code wins and the disagreement is named.

## graphify

When a `graphify-out/` directory exists, read `graphify-out/GRAPH_REPORT.md` for the
community structure before searching raw files; `.graphifyignore` excludes generated output
and the ticket archive.
