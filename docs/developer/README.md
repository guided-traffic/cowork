# Developer documentation

The contributor entry point and the overviews for people changing this code. The detail is
in the code; what lives here is the shape of things — how the pieces fit, which invariants
hold, why a design that looks odd is the way it is, and the workflow around it.

**What belongs here:** anything a future developer needs before touching a subsystem, that the
code cannot state on its own, and everything about contributing: the layout, the build and
test matrix, continuous integration and the release, the checklists, the conventions.

**What does not:** decisions (those are [ADRs](../adr/README.md)), work lists (those are
[tickets](../tickets/README.md), archived when the work lands), what somebody running cowork
needs (that is [docs/operations/](../operations/README.md), with the reference tables in
[README.md](../../README.md)), and the security design (that is [docs/security/](../security/README.md)).
[ADR 0002](../adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
is the rule that separates those homes; [ADR 0075](../adr/0075-developer-documentation-lives-in-docs-developer-and-the-general-standard-says-so.md)
is why there is no `DEVELOPER.md` at the root.

A page here **may and should** point at files and functions. That is the point of it. It also
means it goes stale when the tree moves, so whoever moves the tree updates the page in the same
change.

## What has to be in your head first

- **Two containers, one origin.** The Go backend in `backend/` serves the API and migrates
  the schema; the nginx frontend in `frontend/` serves the Angular bundle and nothing else; the
  Ingress routes `/api/` and `/auth/` to the backend and everything else to the frontend
  ([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
  D3). Locally the dev server's proxy stands in for the Ingress, and a small nginx does for runs of
  the two images.
- **Two database roles.** The owner role owns the schema and runs the migrations; the runtime
  role the server connects as owns nothing and is held by row-level security on every table.
  `cowork serve` refuses a runtime role that could see past it
  ([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2,
  [data-access.md](data-access.md#two-database-roles)).
- **The API document is the contract.** A change to the API starts in
  [`backend/api/`](../../backend/api/); `make generate` turns it into the Go server interface,
  the client and the bundle the server validates every request against
  ([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md), [api.md](api.md)).
- **One resolver, two credentials.** A request is a token's when it carries an `Authorization`
  header and a browser session's when it carries only the cookie; everything after the resolver —
  the tenant boundary, the role, the predicates — is the same code. A session's writes are
  CSRF-checked, sixteen routes take a session only — what can give access, or outlive a leaked
  token, the chat's turn, its stop and its capabilities, and a global administrator's list of every
  tenant — and a temporary
  password gates everything but its own change;
  `X-Cowork-Agent` makes a token's or a session's request an agent's and only narrows it, and
  every act made through a token records and shows the token's id and name beside the agent mark
  ([api.md](api.md#authentication), [domain.md](domain.md#who-made-an-act),
  [sessions](../security/sessions.md)).
- **One tool catalogue, two hosts.** `internal/tools` is the catalogue of workflow tools: `cowork-mcp`
  serves it to Claude Code over stdio with a token, and the chat in the UI runs it inside the backend
  with the person's session marked as its agent, holding the capabilities the person chose — every
  tool call a request through the whole pipeline, never a shortcut to the store ([mcp.md](mcp.md), [chat.md](chat.md)).
- **Two logins, one kind of session.** The local login checks a password; the identity provider's
  login is the OpenID Connect code flow with PKCE against an issuer discovered at start. Its groups
  decide who gets in and, through each tenant's mappings, who belongs where; its session reads
  them again every fifteen minutes inside the resolver — one request claims the refresh and asks the
  issuer holding no connection, the others are served — and its person's tokens meet the same gate
  ([architecture.md](architecture.md#the-two-logins),
  [identity provider](../security/identity-provider.md)).
- **The tenant boundary, then the visibility predicate.** A request under
  `/api/v1/tenants/{tenant}` is admitted to the tenant before any handler runs — a member, or a
  global administrator without a role to the tenant's administration only; inside, every
  query runs in a transaction bound to that tenant, and the predicates in SQL hide restricted
  projects and confidential tickets. What the caller may not see answers exactly like what does
  not exist ([api.md](api.md#the-tenant-boundary), [data-access.md](data-access.md#visibility-in-sql)).
- **Every write is `Mutate`, and every act is an audit row.** A request's write commits through
  `store.Mutate` together with one audit row per act it records — no act, no commit — and a
  ticket's act and a membership's are published to the event streams at commit; a job commits
  through `store.RunJob`; the token's last-used day, a session's idle clock, the login's attempt
  count, a groups refresh's lease and the identity provider's decisions — a login, a refresh's
  answer, a token's gate check — are the writes outside both, each in a transaction of its own — the
  login's attempts and the identity provider's decisions still write their acts ([data-access.md](data-access.md#the-identity-providers-transactions),
  [ADR 0027](../adr/0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md),
  [ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md)).
- **`make` is the entry point**, from the repository root. CI runs Makefile targets; so do
  you ([ADR 0003](../adr/0003-test-and-ci-policy.md) D1). Go targets `cd backend`, npm
  targets `cd frontend`. `make help` lists them.
- **Nothing is skipped.** No `-short`, no `testing.Short()`, no "skip when the database is
  missing". The integration tier fails and tells you how to start PostgreSQL, MinIO and Dex
  (`make dev-up`).
- **Newest toolchains.** Go 1.27 and Angular 22 today, moved by Renovate; a lagging version
  is a defect (ADR 0001 D9).
- **A statement has one home.** Decision → ADR; work → ticket; how → `docs/developer/`; run →
  `docs/operations/`; threat → `docs/security/`; reference tables → `README.md`
  ([ADR 0002](../adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)).
  Whoever changes behaviour updates the page that describes it in the same change.
- **English only**, in code, comments, commits and documentation.

| Page | Read it when |
|---|---|
| [repository-layout.md](repository-layout.md) | You are new and want the tree |
| [package-map.md](package-map.md) | You are looking for where something lives and what it is responsible for |
| [architecture.md](architecture.md) | You want the picture: what runs where, what a request goes through, what happens at start |
| [api.md](api.md) | You touch the API: the document, generation, the pipeline, authentication, the tenant's dashboard, the tenant boundary, authorization, errors, idempotency, versions, paging, filters |
| [data-access.md](data-access.md) | You write SQL or a mutation: the two roles, the wrappers, the settings the policies read, the visibility lint, the list builder, locks, jobs, publication |
| [domain.md](domain.md) | You change a rule of tickets, links, transitions, questions, comments, interest, progress or time |
| [storage.md](storage.md) | You touch attachments or the object storage |
| [events.md](events.md) | You touch the event stream, from `NOTIFY` to the Ingress |
| [frontend.md](frontend.md) | You touch the UI: the folders, the theme and the logo, the services, how an event reaches the screen, the generated client, `make dev` |
| [markdown-grammar.md](markdown-grammar.md) | You touch the Markdown export or the context document, or need their exact form |
| [mcp.md](mcp.md) | You touch `cowork-mcp`: the tool catalogue, the MCP layer, the hooks and subcommands, the Claude Code plugin; or you add a tool |
| [chat.md](chat.md) | You touch the chat in the UI: the turn and its stop, the loop, the loopback, the person's capabilities, the providers and the gateway to the model, the stream and the panel, the shell's content-security policy; or you add a tool to the chat |
| [build-test-lint.md](build-test-lint.md) | You want to build, generate, run or lint anything, locally or the images together |
| [development-credentials.md](development-credentials.md) | You need a username, password, key or token of `make dev`, its containers or the test tiers |
| [testing.md](testing.md) | You are adding a test, choosing a tier, or a suite is failing and you need to know what it is for and what it needs |
| [ci-and-release.md](ci-and-release.md) | You touch a workflow, Renovate or the release |
| [adding-things.md](adding-things.md) | You add an API operation, a table, a migration, a problem code, a configuration variable, a frontend feature, an nginx path, a chart value or a CI job |
| [conventions.md](conventions.md) | You write a commit, Go, SQL, an act, Angular, documentation or anything security-relevant |

## Core flows, one fact each

| Flow | The fact | Where |
|---|---|---|
| Backend start | Configuration is validated completely before anything else runs; the migration runs as the owner role before the pool opens; `serve` refuses a role that could bypass row-level security and a dirty or pending schema; a configured identity provider must be discoverable; then the local administrator and the bootstrap tenant are synchronised under an advisory lock | [architecture.md](architecture.md#backend-startup-sequence-cowork-serve) |
| Backend request | Request id, log and recovery wrap a mux; `/api/` and `/auth/` run the pipeline: route in the document, authenticate (token or cookie — a provider's session refreshed when due, a provider person's token held to the gate), the session rules (CSRF, an agent-marked session refused what only a session does, temporary password), tenant boundary, limits, validation, then the generated handler — or, for the event stream and a turn of the chat, a handler of their own that streams | [architecture.md](architecture.md#backend-request-path), [api.md](api.md#the-pipeline) |
| A local login | The password is verified — against the account's hash or a dummy, one computation either way — before the attempt is recorded under the username's advisory lock; every refusal is the same `401`; a success commits a session whose cookie value is never stored | [api.md](api.md#the-login-flows), [local-accounts](../security/local-accounts.md) |
| A login through the identity provider | The start seals the state, the nonce and the PKCE verifier into a cookie and redirects; the callback checks them, redeems the code, verifies the ID token, and one transaction under the person's advisory lock applies the gate, keeps the person, derives the memberships and makes the session | [architecture.md](architecture.md#the-two-logins), [identity provider](../security/identity-provider.md) |
| A read | A read-only transaction bound to the tenant and the caller; the predicates in SQL decide what exists for the caller | [data-access.md](data-access.md#the-wrappers) |
| A write | `Mutate` commits the change with one audit row per act, stores a keyed response, and publishes a ticket's acts and the membership acts with `NOTIFY` (not downloads, exports or time entries) — or commits nothing | [data-access.md](data-access.md#mutate-acts-idempotency-publication) |
| An event | `NOTIFY` at commit, one listener per replica, a hub that filters per stream; a key and a version — for `membership.changed` the ids of what changed — never content | [events.md](events.md) |
| A notification | Written by the act's own transaction for each person the act tells — an active member who sees the ticket, never the actor —, referencing the audit row it renders from; read per tenant, counted on the person-level stream as `inbox.changed` | [data-access.md](data-access.md#notifications), [events.md](events.md#the-person-level-stream) |
| Frontend request | the Ingress sends `/api/` and `/auth/` to the backend and the rest to nginx: `/healthz` itself, hashed bundles immutable, everything else `index.html` with `no-store`, the shell's content-security policy on all of the UI, and a `404` problem for an `/api/` or `/auth/` path that reaches it by mistake | [architecture.md](architecture.md#frontend-container) |
| A change on screen | An event names a key and a version; the tickets service refetches what it holds and reloads the open lists once per burst; every view reads the one cache | [frontend.md](frontend.md#how-a-change-reaches-the-screen) |
| Migration | golang-migrate over embedded files as the owner role, granting the runtime role named in `cowork.runtime_role`; advisory lock across replicas; a dirty version refuses to start | [data-access.md](data-access.md#two-database-roles), [runtime.md](../operations/runtime.md#the-migration-run) |
| A Claude Code session | The SessionStart hook runs `cowork-mcp session-context`, which finds the binding by the git remotes and prints the active ticket's context; the tools of `internal/tools` call the API through the generated client with the token and the agent header; the Stop hook reminds of a ticket left standing | [mcp.md](mcp.md) |
| A turn of the chat | The browser posts the whole conversation and the provider the person picked; the backend streams the turn: it calls that provider's model through `internal/llm`, runs every tool the model calls at once through the server's own handler as the person's agent — the person's chosen capabilities — in the turn's tenant, and ends with `done`, the messages to append — it keeps nothing; Stop aborts the request and `DELETE …/chat/turns` ends the person's turns on the replica | [chat.md](chat.md) |

## What has no page here

The tenant board, the score beside the rank, "next for me", deletion, import and metrics are not built ([architecture.md](architecture.md#what-is-not-built)); the tenant's
dashboard is a section of [api.md](api.md#the-dashboard), [data-access.md](data-access.md#the-dashboards-queries)
and [frontend.md](frontend.md#the-dashboard); the inbox
and the person-level lists are sections of [data-access.md](data-access.md#notifications),
[api.md](api.md#the-person-level-routes), [events.md](events.md#the-person-level-stream) and
[frontend.md](frontend.md#the-person-level-pages); the rank itself is a section of
[domain.md](domain.md#rank), the repository bindings one of [domain.md](domain.md#repositories),
the identity provider's login a section of [architecture.md](architecture.md#the-two-logins) and
its own security page, [identity-provider.md](../security/identity-provider.md), and the
end-to-end tier a section of [testing.md](testing.md#end-to-end-tests).
The order in which they come is [docs/planning/project-plan.md](../planning/project-plan.md);
each gets its page here when it exists.
