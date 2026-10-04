# Testing

The test tiers, what each needs, the fixtures a test builds on, and the environment variables
that steer the suites. **The rules — what belongs in which tier, what may be skipped (nothing),
what a fix has to prove — are [ADR 0003](../adr/0003-test-and-ci-policy.md) and are not restated
here.** The Make targets themselves are listed in [build-test-lint.md](build-test-lint.md).

Read against the tree on 2026-10-04.

## The tiers

| Tier | Command | Build tag | Needs | What it is for |
|---|---|---|---|---|
| Backend unit | `make test-unit` | none | nothing running | Configuration, the domain rules, tokens and authorization, passwords and sessions, the sealing, the CSRF rule, the client address, the relying party and the identity provider's routes against an issuer in the test's process, the event hub, the Markdown grammar and the context document, the remote identity, the outer handler and the server lifecycle, the command dispatch, the tool catalogue against a fake API, the MCP server over an in-memory transport, `cowork-mcp`'s command line and hooks, the chat's loop against a scripted model and its gateway against the stub provider, the API document's completeness (`TestEveryOperationIsDeclaredCompletely`, which holds the session-only operations to a set of fourteen), and the lints over the migration set and the query files |
| Backend integration | `make test-integration` | `integration` | PostgreSQL 18 at `COWORK_TEST_DATABASE_URL`, an S3 server at `COWORK_TEST_S3_*` and an OpenID Connect issuer at `COWORK_TEST_OIDC_ISSUER` (`make dev-up` provides all three); `git` on the `PATH` | What only the database and the whole handler decide: migrations, roles, isolation, the wrappers, every API route, the local login with its lockout and sessions, the login through the identity provider with its gate, refresh and derivation, the administration of members and mappings, the start-up synchronisation, `cowork-mcp` against the real API, the chat's turns through the whole server against the stub provider — no real model |
| Frontend unit | `make frontend-test` | — | Node.js and `frontend/node_modules` (`make frontend-install`) | Components and services, vitest on jsdom, no browser |
| Chart | `make helm-lint`, `make helm-template` | — | Helm | Strict lint and a render per `deploy/helm/cowork/ci/*-values.yaml` |
| Release tooling | `make test-release-tooling` | — | Node.js and `npm ci` at the root | The semantic-release plugins still render notes |
| End-to-end | `make e2e` (planned) | — | the built images, PostgreSQL, MinIO, Dex, a browser | **Not built.** Decided in [ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md): Playwright in `frontend/e2e/`, two identities, both colour schemes |

Per-tier timeouts: the integration target passes `-timeout=10m`; the others use the Go default.

## Backend unit tests

They sit next to the code under `backend/`. Two `httpserver` tests listen on `127.0.0.1:0`
(`TestServeShutsDownOnContextCancel`, `TestListenAndServeReportsBindError`), the identity
provider's tests start the fake issuer and the gateway's tests the stub provider on `httptest`'s
loopback listener (below), and `TestTheBinaryContainsNoTestPackage` and `TestTheBinaryIsAClientOnly`
run `go list -deps`; nothing else opens a socket or needs a tool — the tools and the chat's loop reach
their fake API in process and read no git.

| Fixture | Where | What it gives you |
|---|---|---|
| `envOf(map[string]string)` | [`config_test.go`](../../backend/internal/config/config_test.go), [`main_test.go`](../../backend/cmd/cowork/main_test.go) | A lookup with the contract of `os.LookupEnv` over a map; the way every test sets configuration without touching the process environment |
| `do(t, handler, method, target)`, `decode(t, res)`, `problemOf(t, res)` | [`server_test.go`](../../backend/internal/httpserver/server_test.go) | One request through the handler, one JSON body as a map, a problem body with its envelope checked: media type, `type` from `code`, `status`, `request_id` equal to `X-Request-Id` |
| `newRecordingLogger(&lines)` | [`logger_test.go`](../../backend/internal/httpserver/logger_test.go) | A `slog.Logger` that collects records as maps, for asserting on the request log |
| `Options.Ready`, `Options.API` | [`server.go`](../../backend/internal/httpserver/server.go) | Inject a readiness check, or a stand-in for the API handler |
| `newFixtures()`, `note`, `filter`, `Hub.now` | [`hub_test.go`](../../backend/internal/events/hub_test.go) | Notifications and filters without a database; a fixed clock for the replay window |
| `Options.Now` | [`api.go`](../../backend/internal/api/api.go) | The clock every session and login window reads; a test that moves it ages a session or a lock without waiting ([`session_test.go`](../../backend/internal/api/session_test.go) `sessionLive`, the integration tier's `clock`) |
| `auth.Computations()` | [`password.go`](../../backend/internal/auth/password.go) | A counter of the Argon2id computations this process made, to assert that a refusal costs what a success costs and that a throttled attempt computes nothing — exported for the integration tier and read by nothing else |
| the golden files, `-update` | [`markdown_test.go`](../../backend/internal/markdown/markdown_test.go), [`context_test.go`](../../backend/internal/markdown/context_test.go) | `TestRender` compares `Render` with `testdata/*.md`, `TestRenderContext` `RenderContext` with `testdata/context-*.md`; `cd backend && go test ./internal/markdown -update` rewrites them after a deliberate change ([markdown-grammar.md](markdown-grammar.md#changing-the-grammar)) |
| `fakeAPI` — `newFake(t)`, `on`, `text`, `refuse`, `calls`, `writes`, `session(bound)`, `call(t, s, name, args)` | [`tools/fake_test.go`](../../backend/internal/tools/fake_test.go) | The API as the tools see it, reached through `HandlerDoer` in process: a canned answer per route, a `not_found` problem for any other, every request recorded with its headers and body; a session bound to `acme/COW` or unbound; one tool call with its arguments as JSON, held to the tool's schema as the MCP server holds it |
| `fakeWorkspace`, `startSession` | [`tools/start_test.go`](../../backend/internal/tools/start_test.go) | A working directory without git: its remotes, its path in the repository, a `.cowork.yaml`, whether files changed |
| `fakeAPI(t, routes)`, `noRepo`, `run(t, e, args…)`, `envOf` | [`mcpcli/cli_test.go`](../../backend/internal/mcpcli/cli_test.go) | `cowork-mcp` by its command line through `mcpcli.Env`: the exit code, standard output and standard error, against a mux, outside any repository |
| `connect(t, o)`, `fakeSession` | [`mcpserver/server_test.go`](../../backend/internal/mcpserver/server_test.go) | An MCP client connected to the server over `mcp.NewInMemoryTransports` |
| `model`, `api`, `turn(t, a, msgs)`, `turnHolding(t, a, capabilities, msgs)`, `events` | [`chat/chat_test.go`](../../backend/internal/chat/chat_test.go) | A turn of the chat without a network: a model that answers from a script and keeps what it was asked, a fake API behind the real loopback that records every request with its body, a turn of the person in `acme` on the board of `COW` with the default or a chosen set of capabilities, and the events the person would see |
| `stubllm.New(t)`, `Reply`, `ReplyWith`, `Requests`, `Cancelled` | [`test/stubllm`](../../backend/test/stubllm/stubllm.go) | The gateway's provider: both wire formats on `httptest`, answered from a script ([the chat's model](#the-chats-model-in-the-tests)) |

The lints run in this tier, without a database:

| Test | Holds |
|---|---|
| `TestMigrationFilesAreWellFormed` ([`migrate_test.go`](../../backend/internal/store/migrate_test.go)) | every file matches `NNNNNN_<snake_name>.up.sql`, none is empty, the versions are 1..n without a gap |
| `TestEveryTableHasItsPolicyAndGrant` ([`policy_test.go`](../../backend/internal/store/policy_test.go)) | every table has row-level security enabled and forced, a policy and a grant to the runtime role; outside the named list, `tenant_id` and the canonical `tenant_isolation` policy |
| `TestPoliciesReadSettingsGuarded` | every `current_setting('app.…')` in a migration is guarded by `NULLIF` (or is the `app.job` comparison) |
| `TestNothingCascadesIntoTheAuditRecord` | no `ON DELETE` in `audit_events`, and its grant is `SELECT, INSERT` |
| `TestLiftedForceIsRestoredInTheSameMigration` | a migration that lifts the force of row-level security on a table restores it later in the same file |
| `TestEveryReadOfProjectsAndTicketsCarriesTheVisibilityPredicate` ([`queries_test.go`](../../backend/internal/store/queries_test.go)) | the visibility lint ([data-access.md](data-access.md#visibility-in-sql)) |
| `TestTicketListSelectsWhatTheQueriesSelect` | the list builder's columns and joins equal `GetTicketByNumber`'s |
| `TestTheBinaryContainsNoTestPackage` ([`main_test.go`](../../backend/cmd/cowork/main_test.go)) | `cmd/cowork` depends on nothing under `backend/test/` |
| `TestTheBinaryIsAClientOnly` ([`cmd/cowork-mcp/main_test.go`](../../backend/cmd/cowork-mcp/main_test.go)) | `cmd/cowork-mcp` depends on nothing under `backend/test/`, not on `internal/store`, `internal/api`, the PostgreSQL driver or the S3 client ([ADR 0040](../adr/0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md) D1) |
| `TestEveryOperationOfAToolIsInTheDocument` ([`tools_test.go`](../../backend/internal/tools/tools_test.go)) | every operation a tool declares is in the API document — what `cowork-mcp` compares with the server's at start |
| `TestDescriptionsNameTheLimits` | `transition`'s description names the capability each move needs and, once the token is read, what an agent's token holds and lacks; a person's token is not bounded by capabilities ([ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D3) |
| `TestEveryOperationIsDeclaredCompletely` ([`document_test.go`](../../backend/api/document_test.go)) | every operation has an id, a tag, the problem response and a security requirement of one of three shapes: public (empty: `getVersion`, `getOpenAPI`, `getCoworkYamlSchema`, `getAuthOptions`, `loginLocal`, `loginOidc`, `oidcCallback`), the session cookie alone (exactly `logout`, `changeMyPassword`, `createMyToken`, `createTenant`, `createAccount`, `resetAccountPassword`, `addMember`, `setMemberGrant`, `createGroupMapping`, `updateGroupMapping`, `setProjectRestriction`, `setProjectAccess`, `runChatTurn`), or the bearer token and the session cookie; a public write carries `x-cowork-origin-check`, and only `oidcCallback` takes query parameters it does not declare (`x-cowork-open-query`) |
| `TestEveryToolIsNamed` ([`chat/chat_test.go`](../../backend/internal/chat/chat_test.go)) | every tool of the shared catalogue is named in the chat's `offered`, offered or left out — a tool the chat does not name is not offered, and a new one fails this until it is ([chat.md](chat.md#the-capabilities)) |

The `run(ctx, args, lookup, stdout, stderr)` tests in `backend/cmd/cowork` cover the command
dispatch and the configuration errors without a database: `migrate` without
`COWORK_DATABASE_URL` or without `COWORK_DATABASE_OWNER_URL`, `serve` without the database URL or
— while it migrates on start — without the owner URL, and with a local administrator's
username but no password or the other way round (`TestServeRefusesAHalfConfiguredLocalAdministrator`),
each exit 1 naming the variable; an unparsable URL exits 1 with "migration failed".

The unit tests of the login's pieces: [`password_test.go`](../../backend/internal/auth/password_test.go)
(the recorded parameters, the salts, verification by the parameters a hash records, the refusal of a
malformed or oversized hash, the context, the dummy hash, the policy),
[`session_test.go`](../../backend/internal/auth/session_test.go) (the cookie value and its shape, the
username rule), [`api/session_test.go`](../../backend/internal/api/session_test.go) (the cookie's
attributes, the CSRF table of headers, the fail-closed rule, both limits of a session, the keyed hash
of an address, the option defaults, the credentials read from the document),
[`api/clientaddr_test.go`](../../backend/internal/api/clientaddr_test.go) (the walk of
`X-Forwarded-For` from the right: no header, one hop, chains of trusted hops, spoofed entries to the
left, malformed entries, IPv6, IPv4-mapped IPv6, several header lines, the empty list, a long
chain; `FuzzClientAddress` holds that an untrusted peer is always the client — its seeds run with
`go test`, `go test ./internal/api -run '^$' -fuzz FuzzClientAddress` explores) and the login cases of
[`config_test.go`](../../backend/internal/config/config_test.go) (all-or-neither, the password floor,
the lockout mode, the lifetimes, the base URL as an origin, the bootstrap tenant, the trusted
proxies as CIDRs and the error that quotes the offending entry only).

The unit tests of the identity provider's pieces run against the fake issuer of
[`test/fakeissuer`](../../backend/test/fakeissuer/fakeissuer.go), started per test on `httptest`,
and need nothing else: [`oidc/oidc_test.go`](../../backend/internal/oidc/oidc_test.go) (the code flow
with PKCE, an ID token that does not verify — another key, another audience, expired, another
nonce —, the authorized party (`TestAuthorizedParty`), a code without its verifier, the groups from
UserInfo and as a string, the claim's shapes, the display name, the refresh that reads the groups
again and rotates the token, every class of the token endpoint's answer — the person's refusal,
cowork's client refused, the issuer's trouble — with no answer's body in an error
(`TestRefreshRefusalAndUnreachability`), a refreshed ID token of a rotated, an unpublished or an
unfetchable key (`TestRefreshedIDTokens`), the end-session URL, a discovery that fails without
echoing the secret, one that redirects, exceeds 1 MiB, names another issuer, an endpoint over plain
`http` or no algorithm cowork verifies (`TestDiscoveryHoldsTheIssuerToItsRules`), and the dropped
end-session endpoint in the log), [`api/oidc_test.go`](../../backend/internal/api/oidc_test.go)
(both routes without a provider through the whole pipeline, the start and its sealed cookie, the
callback's refusals before the issuer is asked, the path a failed login keeps, `safeReturnTo`, the
gate and its issuer check),
[`auth/seal_test.go`](../../backend/internal/auth/seal_test.go) (a sealed value opens only with its
key, label and binding), [`config/oidc_test.go`](../../backend/internal/config/oidc_test.go) (the
defaults, the overrides, every variable refused without the issuer, the issuer rule, a groups maximum
age that is no longer than the refresh, the bootstrap tenant of the administrator group) and [`store/identity_test.go`](../../backend/internal/store/identity_test.go)
(`RefreshDue`, `GateDue`).

The MCP layers without a server ([mcp.md](mcp.md)): every tool against the fake API in
[`tools/`](../../backend/internal/tools/) — the requests it sends (method, path, body, an
`Idempotency-Key` on every `POST`, `If-Match` where it overwrites, the agent header) and the
Markdown it answers, refusals included —, the session-start block bound, unbound, with a
`.cowork.yaml` and within its size, the reminder, the escape hatch's paths, the retries, the
memory file, the parsing of `git` output and of `.cowork.yaml`, the compatibility check;
[`mcpserver/server_test.go`](../../backend/internal/mcpserver/server_test.go) the catalogue as an
MCP client lists and calls it, and a server whose start was refused answering every tool with the
reason; [`mcpcli/cli_test.go`](../../backend/internal/mcpcli/cli_test.go) the configuration
(`https` except on loopback, the token's shape, no value echoed), the subcommands, the hooks
silent outside a repository, `token check`, and `serve` refusing an API it does not know.
[`domain/repository_test.go`](../../backend/internal/domain/repository_test.go) is the table of
remote identities, sub-directories and proposed keys.

The rules of the states and the progress stages without a database:
[`domain_test.go`](../../backend/internal/domain/domain_test.go) (the move matrix, what a write of
the stages does, which done is by the stages) and
[`api/transitions_test.go`](../../backend/internal/api/transitions_test.go) (the transition and
`PATCH` checks for persons and agents over `store.TicketRow` values built by `in`, the stages and
the done by hand a ticket shows — rows the release before the stages left included).

## Backend integration tests

[`backend/test/integration/`](../../backend/test/integration/), build tag `integration`.
`TestMain` ([`main_test.go`](../../backend/test/integration/main_test.go)) prepares one run:

1. `COWORK_TEST_DATABASE_URL`, the three `COWORK_TEST_S3_*` variables and
   `COWORK_TEST_OIDC_ISSUER` are required; a missing one ends the run with exit 1 and the command
   that sets it, and so does an issuer that does not answer its discovery (`prepareIssuer`). There
   is no skip.
2. A bucket of its own on the S3 server, `cowork-it-<nanoseconds>`; at the end its objects and the
   bucket are removed (`removeBucket`), so a run leaves no bucket behind, as it leaves no database.
3. Over the administrative URL: the roles `cowork_it_owner` and `cowork_it_app` (the latter
   `NOSUPERUSER NOBYPASSRLS`) when they are missing, and a database of its own,
   `cowork_it_<nanoseconds>`, owned by the owner role; it is dropped `WITH (FORCE)` at the end.
4. `store.Migrate` as the owner role with `cowork_it_app` as the runtime role — the result is
   `env.Migrated`.

`mcp_test.go` runs `git` to make the repository a session starts in, and `go build` for the
binary; without either on the `PATH` those tests fail and say so.

Every store and API test then connects as the runtime role, as `cowork serve` does; the
administrative connection only creates and seeds. The tests share the run's database and keep
apart by unique slugs, so nothing resets between them.

```
make dev-up                           # postgres:18 on :5432, MinIO on :9000, Dex on :5556
make test-integration                 # exports COWORK_TEST_DATABASE_URL, COWORK_TEST_S3_* and COWORK_TEST_OIDC_ISSUER for them
COWORK_TEST_DATABASE_URL=… make test-integration   # any other PostgreSQL 18, as a role that may create roles and databases
make postgres-down minio-down dex-down
```

`POSTGRES_PORT=55432 make postgres-up test-integration` moves the container off a port that is
taken; `MINIO_PORT=` does the same for MinIO, and `DEX_PORT=` for Dex and its issuer, which
`make test-integration` follows when it is given the same `DEX_PORT=`.

### Fixtures of the integration tier

| Fixture | Where | What it gives you |
|---|---|---|
| `fixture.DB` — `fixtures(t)` | [`test/fixture/fixture.go`](../../backend/test/fixture/fixture.go), [`helpers_test.go`](../../backend/test/integration/helpers_test.go) | Rows written over the administrative connection, past row-level security and without audit rows: `Person`, `Tenant`, `Member` (a marked grant), `Account` (a local account with an Argon2id hash of the password, managed by a tenant, optionally with a temporary password), `GlobalAdmin`, `Project` (with its counter), `Ticket` (a plain task with the next number and no rank, as a release before the rank files one), `Token(TokenSpec)` returning the plaintext once; `Exec`, `Query`, `QueryRow`, `QueryCount` for a state no route reaches. A zero `TokenSpec` is a write token of ninety days; `Agent` without capabilities holds every one, `fixture.AssistedCapabilities` is the "assisted" set |
| `world`, `newWorld(t)` | `helpers_test.go` | Two tenants with unique slugs; an admin, a member and a viewer of A, a member of B, a person in both; the projects `ALPHA` in A and `BETA` in B |
| `seedEveryTenantTable(t, w)`, `tenantBoundTables(t)` | `helpers_test.go` | A row of each tenant in every table with `tenant_id`, and the list of those tables from the catalog. `TestUnfilteredQueryUnderTenantSeesNothingOfAnother` walks the catalog and fails for a table the seed leaves empty |
| `openRuntime(t)`, `openStore(t, url)`, `as(person)` | `helpers_test.go` | The store as the runtime role (or another URL); a context whose store calls act for a person |
| `issueTokens(t, w)` | [`api_helpers_test.go`](../../backend/test/integration/api_helpers_test.go) | A token per person of the world, the administrator's with admin and with write scope, an agent token with every capability and an assisted one |
| `newAPI(t, opts…)` → `apiServer` | `api_helpers_test.go` | The whole handler on `httptest`: health, request id, log and pipeline, the runtime role, a real event listener, the run's bucket (1 MiB files, five per ticket) and response validation on; `opts` change `api.Options` |
| `apiServer.client(t, caller)`, `apiServer.do(…)`, `assertProblem(t, res, status, code)` | `api_helpers_test.go` | The generated Go client acting as a token and agent header; a raw request for what the client cannot express; a problem's status, code and envelope |
| `withLogin`, `testOrigin`, `testPassword` | [`login_helpers_test.go`](../../backend/test/integration/login_helpers_test.go) | What a cookie login needs on `api.Options`: the base origin, the lockout, the password minimum — and the per-address throttle **off**, because every test of the run reaches the server from `127.0.0.1` and one shared limit would make the rest depend on speed; the tests of the throttle switch it on with a server key of their own, and with `Options.TrustedProxies` naming `127.0.0.0/8` the test server plays the proxy and `X-Forwarded-For` names the clients |
| `browser`, `apiServer.browser(t)`, `login`, `mustLogin`, `get`, `request` | `login_helpers_test.go` | A client that holds one session cookie and sends `Origin` and `X-Requested-With` on unsafe methods, as the frontend does; `withHeader`, `without` and `withBearer` take one away or add one to see a refusal |
| `clock`, `newClock()`, `withClock` | `login_helpers_test.go` | A clock a test moves, installed as `api.Options.Now`: hours of a session and minutes of a lock pass without waiting |
| `withAccounts(t, w)` | `login_helpers_test.go` | Local accounts with `testPassword` for the persons of a world, managed by tenant A (B's member by B), and their usernames |
| `isolated`, `newIsolated(t)` | `login_helpers_test.go` | A database of its own, migrated and empty, with the runtime role's store open on it, for what needs an installation that has no tenant and no person: the init state, the start-up synchronisation |
| `recordingLogger` | `login_helpers_test.go` | A logger that collects every record at every level as text, for searching a run for a secret |
| `simultaneously(sends…)`, `times(n, send)` | `api_helpers_test.go` | Starts requests at the same instant and collects their status codes, for the races a conditional write can lose: the request that comes second must answer as a later one would, never `500`. A send runs in its own goroutine, so it builds nothing with `require` |
| `ticketEnv`, `newTicketEnv(t)`, `task(…)`, `file(…)` | [`api_tickets_test.go`](../../backend/test/integration/api_tickets_test.go) | A world with its tokens and a running API; a plain ticket body; filing as a caller, with a key for an agent |
| `openStream`, `next` | [`api_events_test.go`](../../backend/test/integration/api_events_test.go) | An event stream read message by message |
| `adminWorld`, `newAdminWorld(t)`, `members`, `nextMembership` | [`api_members_test.go`](../../backend/test/integration/api_members_test.go) | A world whose persons have local accounts, tenant A's administrator in a session and an administrator's token beside it; the member list as a map; the next `membership.changed` of a stream |
| `repoEnv`, `newRepoEnv(t)`, `bind`, `lookup` | [`api_repositories_test.go`](../../backend/test/integration/api_repositories_test.go) | A world with its tokens and a running API; a binding with a fresh key; a lookup's answer |
| `contextOf` | [`api_context_test.go`](../../backend/test/integration/api_context_test.go) | A ticket's context document and its response |
| `mcpEnv`, `newMCPEnv(t)`, `run`, `serve`, `callTool`, `mustCall` | [`mcp_test.go`](../../backend/test/integration/mcp_test.go) | A git repository on disk whose `origin` lies under tenant A's slug, the API, and `cowork-mcp`'s environment against both: a subcommand by its command line with its exit code and output, the MCP server on an in-memory transport with a client that names itself `claude-code`, a tool call's text |

### The identity provider in the tests

Two issuers, for two purposes ([ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D3):

- **Dex**, the reference: [`hack/dex/config.yaml`](../../hack/dex/config.yaml) in the container
  `cowork-dex` of `make dex-up`, at `COWORK_TEST_OIDC_ISSUER`. One static client, `cowork`
  ([its secret](development-credentials.md#the-containers)), whose redirect URIs are `make dev`'s and the tests'
  `http://cowork.test/auth/callback` — a name that never has to resolve, because the test intercepts
  the redirect and replays it against its own server. Four static users
  ([their password](development-credentials.md#signing-in-to-the-ui-under-make-dev)); under the gate of `make dev` and the tests (`cowork-users` allowed, `cowork-admins`
  the administrator group, the mapping `team-red` → `member` in the tenant `dev`):

  | User | Groups | Is to cowork |
  |---|---|---|
  | `ada@example.com` | `cowork-admins`, `cowork-users` | a global administrator, by the administrator group |
  | `bob@example.com` | `cowork-users`, `team-red` | behind the gate, a member of `dev` by the mapping |
  | `cyd@example.com` | `cowork-users` | behind the gate, in no mapped group: needs a grant |
  | `dan@example.com` | `team-red` | outside the gate: refused, whatever the mapping says |

  Dex keeps nothing: a person a test logged in is a row of the run's database, not of Dex. A
  person is one per issuer and subject, so a test that logs Dex's users in takes an isolated
  database of its own (`dexWorld`).
- **The fake issuer** of [`test/fakeissuer`](../../backend/test/fakeissuer/fakeissuer.go), in the
  test's process, for what Dex cannot be made to do: sign with a key it does not publish or one it
  rotated to, take its keys down or pad them beyond 1 MiB, name another or an extra audience and an
  authorized party, issue an expired token or another nonce, keep the groups in UserInfo only, send a
  string as the claim, change the groups between a login and a refresh and the address between two
  logins, refuse a
  refresh with an OAuth error, answer it with a bare status, drop the connection, send no refresh
  token, send a refreshed ID token, name an end-session endpoint, redirect or pad its discovery or
  name other endpoints in it, and hang until the test releases it (`Hang`, `Release`). Its knobs are
  exported fields a test turns between requests under `Lock`; `Issued` lists every code and token it
  handed out, for the test that searches the log and the audit record for them
  (`TestNoIssuerSecretIsLoggedOrRecorded`).

| Fixture | Where | What it gives you |
|---|---|---|
| `dexProvider(t)`, `fakeProvider(t, is)` | [`oidc_helpers_test.go`](../../backend/test/integration/oidc_helpers_test.go) | The provider discovered from Dex or from a fake issuer, with the default scopes and claim |
| `withIdentity(p, allowed, admin)`, `devGate(p)` | `oidc_helpers_test.go` | A server's identity provider and gate on `api.Options`; `devGate` is `make dev`'s: `cowork-users` allowed, `cowork-admins` the administrator group |
| `browser.oidcLogin(login, returnTo)`, `walkIssuer` | `oidc_helpers_test.go` | A login as a browser makes it: the start sets the state cookie, the issuer's pages are walked — Dex's form filled for `login` — up to the redirect to `testRedirect`, which is replayed against the test server with the browser's cookies; the browser keeps the session cookie |
| `withState(value)`, `cookieValue(res, name)`, `stateCookieName` | `oidc_helpers_test.go` | The state cookie beside the session's, and what an answer sets |
| `providerPerson(t, f, issuer, email, verified, groups)` | `oidc_helpers_test.go` | A person of an issuer written over the administrative connection, as their first login would make them |
| `dexWorld`, `newDexWorld(t)` | [`api_oidc_test.go`](../../backend/test/integration/api_oidc_test.go) | An isolated installation with `make dev`'s shape: the tenant `dev` with the mapping `team-red` → `member` and a local account `root` that administers it |
| `fakeWorld`, `newFakeWorld(t)` | [`api_oidc_fake_test.go`](../../backend/test/integration/api_oidc_fake_test.go) | A tenant whose mapping makes a group of its own members, a fake issuer whose one person is in that group and behind the gate, and a server with a clock the test moves |

### The chat's model in the tests

No tier talks to a real model ([chat.md](chat.md#tests)). [`test/stubllm`](../../backend/test/stubllm/stubllm.go)
is one `httptest` server that speaks the OpenAI format under `/v1/chat/completions` and the
Anthropic format under `/v1/messages`, answers each request with the next reply of a script — text
streamed three characters at a time, tool calls with their arguments in fragments, an error status
with a body, a raw stream that breaks or overflows, one JSON body instead of a stream, a delay or a
stall, both cut short when the client ends the request, which `Cancelled` counts — and records every request in a neutral form (the format, the headers, the model, the
instructions, the messages, the tools offered with their schemas, `max_tokens`), so a test asserts
what the chat sent whichever format it spoke. `RequireKey` refuses a request without `stubllm.Key`
and echoes the key it got in its error, so a test sees the key taken out of the log. The gateway's
unit tests and the integration tier use it; the loop's unit tests use a scripted `llm.Provider`.

| Fixture | Where | What it gives you |
|---|---|---|
| `withChat(stub, format, edit…)`, `stubProvider(stub, id, format, model)` | [`api_chat_test.go`](../../backend/test/integration/api_chat_test.go) | The chat on `api.Options` with one provider, `stub`, in one of the stub's formats, with a minute's turn and eight steps unless `edit` changes `ChatOptions` — another provider among them |
| `chatEnv`, `newChatEnv(t, format, edit…)`, `turn`, `postTurn`, `postTurnAs` | `api_chat_test.go` | A world with its accounts and tokens, the stub, and the browser of tenant A's member; a turn posted on the board of `ALPHA`; a turn posted from another goroutine by a browser in a tenant, as a second tab |
| `readEvents`, `names`, `eventData[T]`, `lastEvent`, `said` | `api_chat_test.go` | A turn's stream read to its end — a comment is the event `:` —, the events' names without the text, an event's data as a type, the person's message |

### Response validation

`newAPI` sets `api.Options.ValidateResponses`: `serveValidated` runs the handler into a
recorder, validates status, headers and body against the API document, and answers `500` naming
the mismatch instead of the response. The server's warnings and errors go to the test's log
(`testLog`). A handler that drifts from the document fails the test that exercises it
([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md) D4); the event stream
and a turn of the chat are not generated responses and are not validated — a turn's events are the
generated types the server encodes, and `TestATurnIsHeldToTheDocument` in
[`api/chat_test.go`](../../backend/internal/api/chat_test.go) holds a turn's body to the document.

### What runs

| File | What it proves |
|---|---|
| [`migrate_test.go`](../../backend/test/integration/migrate_test.go) | A fresh database reaches the embedded version, a second run applies nothing, the runtime role reads the version; PostgreSQL 18 or newer; a schema ahead of the binary is served; tenant ids are UUIDv7; migration 17 ranks every project's open tickets in number order and restores the force it lifts (`TestRankMigrationKeepsNumberOrder`, on a database of its own that `migrateTo` brings to version 16 first); migrations 18 and 19 backfill the three progress stages, `done_from` and `done_by_hand` on the tickets a release before them left, derive the parents' new stages a level at a time, grant the new columns and restore the force (`TestStagesMigrationBackfill`, from version 17) |
| [`store_test.go`](../../backend/test/integration/store_test.go) | The runtime role check; an unfiltered query under tenant A sees nothing of B in any tenant-bound table; the context dies with its transaction; the wrappers; the append-only audit record; `Mutate`'s acts, rollbacks and idempotency, concurrent duplicates included; the expiry job and its lock; the token lookup, refusal bound and last-used date; an act's token name beside its id, never on a system actor's act in the request |
| [`api_core_test.go`](../../backend/test/integration/api_core_test.go) | Unauthenticated meta routes, unknown routes and methods, authentication and the agent header, one tenant's token in another, `/me` and tokens, tenant settings, the audit view, the body limit, cursors, validation |
| [`api_boundary_test.go`](../../backend/test/integration/api_boundary_test.go) | Every tenant route refuses another tenant's token exactly like an unknown tenant (`TestEveryTenantRouteRefusesAnotherTenantLikeNoTenant`); the routes come from a walk over the document (`tenantRoutes`), shared with the session test below, so the account routes are covered the day they exist; `POST /tenants` has no tenant in its path and is tested by `TestOnlyAGlobalAdministratorCreatesATenant` |
| [`api_login_test.go`](../../backend/test/integration/api_login_test.go) | The login through the whole handler: the session and its cookie, every failure answering identically at the same cost, the lockout in both modes and its window, the per-address throttle — also behind a trusted proxy, where two clients are throttled apart and a spoofed entry moves nobody, and for a peer that is no proxy, whose header is ignored —, the init state, the options, both session limits on a moved clock, a session surviving a restart, logout, the expiry of the login's state |
| [`api_accounts_test.go`](../../backend/test/integration/api_accounts_test.go) | A temporary password gating the session, a password change counting and ending the other sessions, the account routes per role and across two tenants (what a tenant's administrator manages and does not), a token refused on creating an account and on resetting a password while it still lists, unlocks, deactivates and ends sessions (`TestAccountRoutesAnAdministratorsTokenMayStillCall`), deactivation ending tokens and sessions, a deactivation that waits for the tenant's lock the test holds and meets `last_admin` when the administrator acting was deactivated meanwhile, or goes through while another administrator remains (`TestADeactivationLeavesTheTenantAnAdministrator`), two administrators deactivating each other at once (`TestTwoAdministratorsCannotDeactivateEachOther`, eight rounds through `simultaneously`), a lock not inherited by a new account of the same name, a session's idempotency key scoped to its person |
| [`api_session_routes_test.go`](../../backend/test/integration/api_session_routes_test.go) | The routes only a session calls: a token created and shown once, its lifetime clamped, its idempotency; a tenant created by a global administrator only; the CSRF refusals (the `Referer` fallback, no origin, a second header, a cookie beside a token); the event stream ending with its session; the cross-tenant harness again with a cookie; no password, cookie or token in the log, the answers or the audit record |
| [`api_oversight_test.go`](../../backend/test/integration/api_oversight_test.go) | A global administrator without a role in a tenant ([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D2): the walk over every tenant route of the document, of which they reach the tenant, its members (without addresses), its mappings and the grant, and every other answers like an unknown tenant — a token of theirs, an agent-marked session and a person who is no global administrator reach nothing (`TestAGlobalAdministratorWithoutARoleSeesTheAdministrationOnly`); the grant to themselves, refused to a token, an agent and for anybody else, recorded and announced, after which the tenant answers as to an administrator; a tenant left without an administrator recovered, a grant below `admin` meeting no `last_admin`; a global administrator who holds `viewer` in such a tenant raising their own grant to `admin`, which a viewer who is none cannot (`TestAGlobalAdministratorWithALowerRoleRaisesTheirOwnGrant`); the list of every tenant, paged, refused to anybody else, to a token and to an agent; and the policies of migration 26 as the runtime role sees them — every tenant to a global administrator, their own grant in any role and its role changed, and nothing else of a tenant outside its transaction (`TestPoliciesOfTheGlobalAdministratorsReach`) |
| [`policy_login_test.go`](../../backend/test/integration/policy_login_test.go) | The policies of the persons, their accounts and their sessions as the runtime role sees them, with no handler in front — the identity provider's persons, memberships and mappings, a mapping made and changed by a global administrator who administers the tenant only, and the trigger that keeps a project's restriction to the tenant's administrators, among them; the session lookup finds the presented row only |
| [`bootstrap_test.go`](../../backend/test/integration/bootstrap_test.go) | The start-up synchronisation on an isolated database: created, left alone, re-hashed with the sessions ended, deactivated and reactivated, taken over from a tenant's account of the same name, four replicas at once |
| [`api_oidc_test.go`](../../backend/test/integration/api_oidc_test.go) | The login through Dex: its four users through the gate, the mapping and a grant — the person made by issuer and subject, the sealed refresh token, the acts and the source hash —, the first-tenant rule, leaving the allow-list (the sessions end, the token is refused and works again behind a wider gate), a spent refresh token, and the roles that hold: a viewer cannot write, a member of one tenant cannot list another, a member outside a restricted project cannot read it until an administrator puts them on its list |
| [`api_oidc_fake_test.go`](../../backend/test/integration/api_oidc_fake_test.go) | What only the fake issuer shows: the refresh that follows the groups, an unreachable issuer, a refused refresh token, no refresh token, the logout at an issuer with an end-session endpoint, every failure of the callback, a deactivated person, the mapping editor's own role and `last_admin`, no secret of the issuer in a log line or an audit row, leaving the gate stopping the tokens at once, groups older than the maximum age refusing the tokens until a sign-in or a refresh, no audit row naming a person's groups |
| [`api_members_test.go`](../../backend/test/integration/api_members_test.go) | The administration: a member added by address — any case, a verified one, one the issuer said nothing about only with `COWORK_OIDC_EMAIL_TRUSTED`, never an unverified one, ambiguous, deactivated — or by username, in a session only; grants and the last administrator; mappings that derive at once, change and go, made and changed by a global administrator who administers the tenant only, removed by any administrator; a project's restriction and access list; `membership.changed` reaching its audience; the source hash on every row of a request and none on a job's; the bootstrap tenant of the administrator group |
| [`api_review_test.go`](../../backend/test/integration/api_review_test.go) | The findings of the security review of 2026-10-04, one test each: a refresh that holds no connection or lock while the issuer hangs, on a pool of two (`TestARefreshWaitsForNoOneElse`); a refresh that read nothing keeping the person's newer groups; the issuer refusing cowork's client; a refreshed ID token that does not verify; a mapping's derivation leaving who cannot act; two administrators removing each other at once (`TestTwoAdministratorsCannotRemoveEachOther`, eight rounds through `simultaneously`); only an administrator who can log in counting for `last_admin`; a person of another issuer outside the gate; no address in an audit row; the address for administrators only; a project-restricted stream hearing only its project |
| [`api_repositories_test.go`](../../backend/test/integration/api_repositories_test.go) | Binding by the normalised identity, idempotent, the remote kept without credentials, another project's binding refused and named only to who sees it; who binds and unbinds; the list; the lookup — bound, the covering sub-directory, ambiguous across tenants, a restricted token's tenant —, the proposal (`only-tenant`, `remote-owner`, `choose`, a free key, none for a project-restricted token); a project created for a repository and the `200` for one bound already; `GET /me/token`; the public schema of `.cowork.yaml`; no credential in a lookup's answer |
| [`api_context_test.go`](../../backend/test/integration/api_context_test.go) | The context document of [ADR 0044](../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md) D2 over the real data: the sections, what the caller cannot see absent, the limits, every call recorded |
| [`api_chat_test.go`](../../backend/test/integration/api_chat_test.go) | The chat in the UI against the stub provider ([chat.md](chat.md#tests)): the availability with two providers; the person's pick and the default; a turn that files a ticket and ranks it to `now` with the chat's mark, the default set and a key on its acts; the person's capabilities over the Anthropic format — a close refused, chosen in a session only and recorded, then run at once; a token, a CSRF failure and an agent-marked session refused; a turn kept in its tenant; the turn's time, its comments and a failing provider; the agent header on a session; the turn limit and the shutdown; the stop that ends a slowly streaming turn within a second, its provider request cancelled, others untouched; a question asked of the person; the policies of `chat_capabilities` |
| [`api_token_marks_test.go`](../../backend/test/integration/api_token_marks_test.go) | Every act through a token marked with it ([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6): a plain token, an agent token with its header and a browser session each file a ticket, comment and edit the comment, upload, ask and answer, set a stake, book and correct time (not the agent) and change a field; every answer, revision and act of the activity, the filing and the stake, and every row in the database, carries the token's id and name and the agent mark exactly as the credential was — none for the session; the context document names the token where it names an agent; the tenant's audit view names the token in JSON and CSV; after the plain token's revocation another member reads its name on its acts, and no answer holds a part of a token (`TestEveryActThroughATokenIsMarkedWithIt`) |
| [`mcp_test.go`](../../backend/test/integration/mcp_test.go) | `cowork-mcp` against the real API ([ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D6): the working day from the proposal through `create_project`, filing, deciding, working, asking, answering and `finish_work`, every act the agent's with the client's name and every creating `POST` keyed; an assisted token's limits in the descriptions and `finish_work` stopping at `review`; the subcommands — `session-context` unbound and bound, `lookup`, `session-end` with and without changed files, `token check` and a revoked token —; and the binary itself, built with `go build` and run by its command line with its environment, the memory file under a temporary `HOME` ([ADR 0070](../adr/0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md) D6) |
| `api_projects_test.go`, `api_tickets_test.go`, `api_rank_test.go`, `api_links_test.go`, `api_prerequisites_test.go`, `api_transitions_test.go`, `api_stages_test.go`, `api_questions_test.go`, `api_comments_test.go`, `api_interest_test.go`, `api_progress_test.go`, `api_time_test.go`, `api_attachments_test.go`, `api_events_test.go`, `api_export_test.go` | The rules of [domain.md](domain.md), [storage.md](storage.md), [events.md](events.md) and [markdown-grammar.md](markdown-grammar.md), route by route, across tenants, restricted projects, confidential tickets, roles, scopes and agents; `api_stages_test.go` the state `review`, done by hand and its withdrawal, done by the stages and the reopen, their refusals for persons and agents, a parent's stages, an open ticket whose stages are full, what the release before the stages writes over this schema in a rollback (its statements verbatim), the override that holds, `done_after` and the `review` limit; `api_prerequisites_test.go` the tree and its upward reading, a ticket under two others, paging and its count, what lies behind a confidential ticket and a restricted project absent, and a graph of 5^8 paths answered at once |

## Frontend unit tests

`ng test` with the `@angular/build:unit-test` builder, vitest, jsdom. `CI=true` and
`--watch=false` make it run once; the Make targets set both. Coverage comes from
`@vitest/coverage-v8`, a dev dependency of the frontend (`make frontend-test-coverage`,
reports under `frontend/coverage/frontend/`: `text-summary` on the console, `lcov.info`,
`coverage-summary.json` for CI).

| Pattern | Where |
|---|---|
| `provideHttpClient()` + `provideHttpClientTesting()`, `HttpTestingController.expectOne(url).flush(...)`, `http.verify()` in `afterEach` | [`app.spec.ts`](../../frontend/src/app/app.spec.ts), [`version.service.spec.ts`](../../frontend/src/app/core/version.service.spec.ts) |
| `await fixture.whenStable()` after flushing, then read the DOM | `app.spec.ts` — the application is zoneless; `whenStable` settles the signal that `toSignal` feeds |
| `data-testid` attributes for assertions | [`layout/shell.html`](../../frontend/src/app/layout/shell.html) |
| A service against the generated client: the real `Api`, `HttpTestingController` answering each URL the client calls | [`tickets.service.spec.ts`](../../frontend/src/app/core/tickets.service.spec.ts), [`auth.service.spec.ts`](../../frontend/src/app/core/auth.service.spec.ts) |
| A component against mocked services: `{ provide: <Service>, useValue: {...} }` with signals and `vi.fn()` | [`ticket-detail.spec.ts`](../../frontend/src/app/features/ticket/ticket-detail.spec.ts) |
| An editor that belongs to its ticket: open it, set the `ticket` (or `ticketKey`) input to another ticket, and assert that it is closed and that nothing was written; set a newer version of the same ticket, and assert that it stays open | [`ticket-fields.spec.ts`](../../frontend/src/app/features/ticket/ticket-fields.spec.ts), [`ticket-moves.spec.ts`](../../frontend/src/app/features/ticket/ticket-moves.spec.ts), [`ticket-body.spec.ts`](../../frontend/src/app/features/ticket/ticket-body.spec.ts) |
| The event stream without a network: a fake `EventSource` through the `EVENT_SOURCE` token, its events pushed by the test | [`event-stream.service.spec.ts`](../../frontend/src/app/core/event-stream.service.spec.ts) |
| A chat turn without a network: a fake `fetch` through the `CHAT_FETCH` token that answers a `Response` over a `ReadableStream` the test writes the turn's events into, piece by piece | [`chat.service.spec.ts`](../../frontend/src/app/core/chat.service.spec.ts) |
| Time: `{ provide: Clock, useValue: { now } }` for what a page shows; `vi.useFakeTimers()` and `vi.advanceTimersByTimeAsync` for debounces, the polling fallback and retries | [`time-report.spec.ts`](../../frontend/src/app/features/time/time-report.spec.ts), [`tickets.service.spec.ts`](../../frontend/src/app/core/tickets.service.spec.ts) |
| A PrimeNG overlay (a select's panel) asks `matchMedia`, which jsdom lacks: `vi.stubGlobal('matchMedia', …)` in the test that opens one | [`new-token-dialog.spec.ts`](../../frontend/src/app/features/me/new-token-dialog.spec.ts) |
| A component that provides a service of its own (`TicketRelations`, `AccessList`): the real service over `HttpTestingController`, and `vi.spyOn` on its acts from `fixture.debugElement.injector` — not `TestBed.overrideComponent`, which compiles the component at test time and leaves its template out of the coverage | [`project-access.spec.ts`](../../frontend/src/app/features/project/project-access.spec.ts) |
| A request a test answers later: `fixture.whenStable()` waits for open requests in the zoneless test bed, so until the answer only change detection runs (`fixture.detectChanges()` after a macrotask) | [`project-access.spec.ts`](../../frontend/src/app/features/project/project-access.spec.ts) |

## Container check

Neither image has a unit test; what proves them is building and running them. CI builds each
`Containerfile` from its directory and scans the image (one `container-malware-scan` leg per
image). Locally, `make docker-build` builds both, and
[build-test-lint.md](build-test-lint.md#run-the-images-together) is the recipe for running them
together read-only behind the Ingress stand-in; `make verify-phase-2` scripts the API half of that
run by hand, and the nginx checks stay manual until the end-to-end tier exists.

## Chart tests

`helm lint --strict` on the default values and on each `ci/*-values.yaml`; `helm template` on
each `ci/*-values.yaml`. The default values name no database URL, no owner URL and no server-key
Secret, and the helpers `fail` on each — `helm lint` reports them as `[INFO]`, `helm template`
fails on the first. That is why the template target runs only with the `ci/` files, each of
which sets all three.

## Environment variables the suites read

| Variable | Read by | Meaning |
|---|---|---|
| `COWORK_TEST_DATABASE_URL` | the integration tier | An administrative URL to PostgreSQL 18 — a role that may create roles and databases and is not held by row-level security; required |
| `COWORK_TEST_S3_ENDPOINT`, `COWORK_TEST_S3_ACCESS_KEY_ID`, `COWORK_TEST_S3_SECRET_ACCESS_KEY` | the integration tier | The S3 server and its keys; required |
| `COWORK_TEST_OIDC_ISSUER` | the integration tier | The issuer of Dex, `http://localhost:5556/dex` by `make`'s default; required, and its discovery must answer |
| `DEX_PORT`, `DEX_IMAGE`, `DEX_CONTAINER` | `make dex-up`, `make test-integration` | Where and what to start locally; the issuer follows the port |
| `CONTAINER_BIND` | `make postgres-up`, `make minio-up`, `make dex-up` | The address the containers publish their ports on, `127.0.0.1` by default |
| `CI` | vitest through `ng test` | Non-interactive reporter and no watch |
| `POSTGRES_PORT`, `POSTGRES_IMAGE`, `POSTGRES_CONTAINER` | `make postgres-up` | Where and what to start locally |
| `MINIO_PORT`, `MINIO_IMAGE`, `MINIO_CONTAINER`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY` | `make minio-up`, `make test-integration` | Where and what to start locally, and the keys the tests are given |
| `COWORK_DEV_SEED_DATABASE_URL` | `test/devseed` | The administrative URL `make dev-seed` writes through |
