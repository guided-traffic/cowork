# Testing

The test tiers, what each needs, the fixtures a test builds on, and the environment variables
that steer the suites. **The rules — what belongs in which tier, what may be skipped (nothing),
what a fix has to prove — are [ADR 0003](../adr/0003-test-and-ci-policy.md) and are not restated
here.** The Make targets themselves are listed in [build-test-lint.md](build-test-lint.md).

Read against the tree on 2026-10-04.

## The tiers

| Tier | Command | Build tag | Needs | What it is for |
|---|---|---|---|---|
| Backend unit | `make test-unit` | none | nothing running | Configuration, the domain rules, tokens and authorization, passwords and sessions, the sealing, the CSRF rule, the client address, the relying party and the identity provider's routes against an issuer in the test's process, the event hub, the Markdown grammar and the context document, the rendering and sanitising of the Markdown people write against hostile input and the allow-list, the search's key, snippet and cursor, the remote identity, the outer handler and the server lifecycle, the command dispatch, the tool catalogue against a fake API, the MCP server over an in-memory transport, `cowork-mcp`'s command line and hooks, the chat's loop against a scripted model and its gateway against the stub provider, the API document's completeness (`TestEveryOperationIsDeclaredCompletely`, which holds the session-only operations to a set of nineteen) and its examples (`examples_test.go`), and the lints over the migration set and the query files |
| Backend integration | `make test-integration` | `integration` | PostgreSQL 18 at `COWORK_TEST_DATABASE_URL`, one that serves TLS under a private authority at `COWORK_TEST_DATABASE_TLS_URL` and `COWORK_TEST_DATABASE_TLS_CA`, an S3 server at `COWORK_TEST_S3_*` and an OpenID Connect issuer at `COWORK_TEST_OIDC_ISSUER` (`make dev-up postgres-tls-up` provides all four); `git` on the `PATH` | What only the database and the whole handler decide: migrations, roles, isolation, the wrappers, every API route, the local login with its lockout and sessions, the login through the identity provider with its gate, refresh and derivation, the administration of members and mappings, the start-up synchronisation, `cowork-mcp` against the real API, the chat's turns through the whole server against the stub provider — no real model |
| Frontend unit | `make frontend-test` | — | Node.js and `frontend/node_modules` (`make frontend-install`) | Components and services, vitest on jsdom, no browser |
| Chart | `make helm-lint`, `make helm-template` | — | Helm | Strict lint and a render per `deploy/helm/cowork/ci/*-values.yaml`; the inline credentials leave no checksum in the backend pod template |
| Release tooling | `make test-release-tooling` | — | Node.js and `npm ci` at the root | The semantic-release plugins still render notes |
| End-to-end | `make e2e`, after `make docker-build` | — | Docker, the two images of one commit, Chromium and WebKit (`make e2e-browsers`), `curl`, `openssl` | The images together, read-only, behind the Ingress stand-in with TLS, against a PostgreSQL, a Silo and a Dex of its own: the login in a browser — the local form, a temporary password, Dex, and Dex again without a click after the session ended —, the session cookie and the CSRF check, filing, editing and moving a ticket, the board and its drag, the backlog's drag, the score's marker and sort, the conversation and the prerequisite tree in a second browser, the rendered Markdown and the search under the shell's content-security policy, the start page, a shared saved filter, deletion and the bin, the dashboard with two identities, in Chromium and WebKit and both colour schemes, and coarse screenshots of the board and the dashboard ([ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md), [below](#end-to-end-tests)) |

Per-tier timeouts: the integration target passes `-timeout=10m`; the end-to-end suite gives a
test 30 seconds and an assertion 10, and its CI job has ten minutes; the others use the Go default.

## Backend unit tests

They sit next to the code under `backend/`. Three `httpserver` tests listen on `127.0.0.1:0`
(`TestServeShutsDownOnContextCancel`, `TestListenAndServeReportsBindError`,
`TestServeAllSharesOneLifecycle`), the identity
provider's tests start the fake issuer and the gateway's tests the stub provider on `httptest`'s
loopback listener (below), `TestTheBinaryContainsNoTestPackage` and `TestTheBinaryIsAClientOnly`
run `go list -deps`, and `TestOnlyThisPackageImportsTheClientLibrary` runs `go list` over the module;
nothing else opens a socket or needs a tool — the tools and the chat's loop reach
their fake API in process and read no git.

| Fixture | Where | What it gives you |
|---|---|---|
| `envOf(map[string]string)` | [`config_test.go`](../../backend/internal/config/config_test.go), [`main_test.go`](../../backend/cmd/cowork/main_test.go) | A lookup with the contract of `os.LookupEnv` over a map; the way every test sets configuration without touching the process environment |
| `do(t, handler, method, target)`, `decode(t, res)`, `problemOf(t, res)` | [`server_test.go`](../../backend/internal/httpserver/server_test.go) | One request through the handler, one JSON body as a map, a problem body with its envelope checked: media type, `type` from `code`, `status`, `request_id` equal to `X-Request-Id` |
| `newRecordingLogger(&lines)` | [`logger_test.go`](../../backend/internal/httpserver/logger_test.go) | A `slog.Logger` that collects records as maps, for asserting on the request log |
| `Options.Ready`, `Options.API` | [`server.go`](../../backend/internal/httpserver/server.go) | Inject a readiness check, or a stand-in for the API handler |
| `newFixtures()`, `note`, `filter`, `Hub.now` | [`hub_test.go`](../../backend/internal/events/hub_test.go) | Notifications and filters without a database; a fixed clock for the replay window |
| `metrics.New()`, `Samples`, `Sum`, `Has`; `exercise` | [`metrics/samples.go`](../../backend/internal/metrics/samples.go), [`metrics_test.go`](../../backend/internal/metrics/metrics_test.go) | A registry of the test's own — the instruments are never global — and what a scrape would answer of it, summed over the labels a test names; `exercise` records through every method once ([metrics.md](metrics.md#tests)) |
| `Options.Now` | [`api.go`](../../backend/internal/api/api.go) | The clock every session and login window reads; a test that moves it ages a session or a lock without waiting ([`session_test.go`](../../backend/internal/api/session_test.go) `sessionLive`, the integration tier's `clock`) |
| `auth.Computations()` | [`password.go`](../../backend/internal/auth/password.go) | A counter of the Argon2id computations this process made, to assert that a refusal costs what a success costs and that a throttled attempt computes nothing — exported for the integration tier and read by nothing else |
| the golden files, `-update` | [`markdown_test.go`](../../backend/internal/markdown/markdown_test.go), [`context_test.go`](../../backend/internal/markdown/context_test.go) | `TestRender` compares `Render` with `testdata/*.md`, `TestRenderContext` `RenderContext` with `testdata/context-*.md`; `cd backend && go test ./internal/markdown -update` rewrites them after a deliberate change ([markdown-grammar.md](markdown-grammar.md#changing-the-grammar)) |
| `fakeAPI` — `newFake(t)`, `on`, `text`, `refuse`, `calls`, `writes`, `session(bound)`, `call(t, s, name, args)` | [`tools/fake_test.go`](../../backend/internal/tools/fake_test.go) | The API as the tools see it, reached through `HandlerDoer` in process: a canned answer per route, a `not_found` problem for any other, every request recorded with its headers and body; a session bound to `acme/COW` or unbound; one tool call with its arguments as JSON, held to the tool's schema as the MCP server holds it |
| `fakeWorkspace`, `startSession` | [`tools/start_test.go`](../../backend/internal/tools/start_test.go) | A working directory without git: its remotes, its path in the repository, a `.cowork.yaml`, whether files changed |
| `fakeAPI(t, routes)`, `noRepo`, `run(t, e, args…)`, `envOf` | [`mcpcli/cli_test.go`](../../backend/internal/mcpcli/cli_test.go) | `cowork-mcp` by its command line through `mcpcli.Env`: the exit code, standard output and standard error, against a mux, outside any repository |
| `testdata/tickets/`, `fixture(t, path)` | [`importer/parse_test.go`](../../backend/internal/importer/parse_test.go) | Ticket files as a repository holds them: copies of this repository's own as of 2026-10-06 and three made for the shapes it lacks — a record without frontmatter, a `README.md` and an embargoed file, named `embargoed-099-…` and read as `local_099-…`, since a `local_` name is ignored by many a git configuration; test data, not references to tickets ([import-and-export.md](import-and-export.md#tests)) |
| `tarGz`, `tarGzOfType`, `zipped`, `manyEntries`, `form` | [`importer/upload_test.go`](../../backend/internal/importer/upload_test.go) | An upload as a multipart reader: archives and files of chosen names, types and bytes; a zip of many empty entries written byte by byte |
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
| `TestEveryReadOfTicketsCarriesTheDeletionFilter`, `TestTicketListLeavesTheDeletedOut` (`queries_test.go`) | the deletion lint: every query that reads a ticket leaves the deleted ones out once per ticket it reads, or names its exemption; the list builder does too ([data-access.md](data-access.md#visibility-in-sql)) |
| `TestNoInstrumentCarriesAForbiddenLabel` ([`metrics_test.go`](../../backend/internal/metrics/metrics_test.go)) | no instrument carries a label for a person, a ticket, a key, a token or a request id, nor `tenant` outside the consistency family's two counts — where it is an id and the one label —, and every family of the backend's is exercised ([ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md) D5) |
| `TestOnlyThisPackageImportsTheClientLibrary`, `TestTheDashboardNamesOnlyInstrumentsThatExist` (`metrics_test.go`) | no package but `internal/metrics` imports `github.com/prometheus/…`, the integration tier included; every metric the Grafana dashboard names exists |
| `TestEverySecurityDefinerFunctionIsFencedIn` (`policy_test.go`) | a function that runs with its owner's rights fixes its `search_path`, `pg_temp` last, and is revoked from `PUBLIC` |
| `TestTicketListSelectsWhatTheQueriesSelect` | the list builder's columns and joins equal `GetTicketByNumber`'s |
| `TestTheBinaryContainsNoTestPackage` ([`main_test.go`](../../backend/cmd/cowork/main_test.go)) | `cmd/cowork` depends on nothing under `backend/test/` |
| `TestTheBinaryIsAClientOnly` ([`cmd/cowork-mcp/main_test.go`](../../backend/cmd/cowork-mcp/main_test.go)) | `cmd/cowork-mcp` depends on nothing under `backend/test/`, not on `internal/store`, `internal/api`, the PostgreSQL driver or the S3 client ([ADR 0040](../adr/0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md) D1) |
| `TestEveryOperationOfAToolIsInTheDocument` ([`tools_test.go`](../../backend/internal/tools/tools_test.go)) | every operation a tool declares is in the API document — what `cowork-mcp` compares with the server's at start |
| `TestDescriptionsNameTheLimits` | `transition`'s description names the capability each move needs and, once the token is read, what an agent's token holds and lacks; a person's token is not bounded by capabilities ([ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D3) |
| `TestEveryOperationIsDeclaredCompletely` ([`document_test.go`](../../backend/api/document_test.go)) | every operation has an id, a tag, the problem response and a security requirement of one of three shapes: public (empty: `getVersion`, `getOpenAPI`, `getCoworkYamlSchema`, `getAuthOptions`, `loginLocal`, `loginOidc`, `oidcCallback`), the session cookie alone (exactly the nineteen of `sessionOnly`, `unlockAccount` the last), or the bearer token and the session cookie; a public write carries `x-cowork-origin-check`, and only `oidcCallback` takes query parameters it does not declare (`x-cowork-open-query`); `x-cowork-recorded-read` is on exactly the five reads of `recordedRead`, each a `GET` |
| `TestEveryBodyHasAnExample`, `TestEveryExampleValidates`, `TestTheExamplesWalkReachesEveryKindOfBody` ([`examples_test.go`](../../backend/api/examples_test.go)) | every request body and every response with a body has an example, its own or its schema's — bytes none, a multipart body its parts described instead —, each missing one named by operation, status and media type; every example of a body and of a schema validates under JSON Schema 2020-12 with `format: uuid` as the server checks it, an event stream's as server-sent events with JSON data; the walk reaches a body of every media type ([api.md](api.md#examples)) |
| `TestABodyIsTheTypeTheOperationDeclares` ([`api/validate_test.go`](../../backend/internal/api/validate_test.go)) | every operation of the document that takes a body refuses one of a type it does not declare — multipart to a JSON route, JSON to an upload, none, `text/plain` — with `415` before a byte is read, takes each declared type with a parameter, and leaves a request without a body alone; a multipart `Content-Type` exempts no JSON route's body from the validation ([api.md](api.md#the-pipeline)) |
| `TestEveryToolIsNamed` ([`chat/chat_test.go`](../../backend/internal/chat/chat_test.go)) | every tool of the shared catalogue is named in the chat's `offered`, offered or left out — a tool the chat does not name is not offered, and a new one fails this until it is ([chat.md](chat.md#the-capabilities)) |

The `run(ctx, args, lookup, stdout, stderr)` tests in `backend/cmd/cowork` cover the command
dispatch and the configuration errors without a database: `migrate` without
the runtime role's connection or without the owner role's, `serve` without the database's or
— while it migrates on start — without the owner's, and with a local administrator's
username but no password or the other way round (`TestServeRefusesAHalfConfiguredLocalAdministrator`),
each exit 1 naming the variables; `serve` without the identity provider's client id and secret,
which `migrate` reads the administrator group without
(`TestTheIdentityProvidersClientIsServesRequirementAlone`), and `migrate` with the owner as
components (`TestMigrateTakesTheOwnerAsComponents`); an unparsable URL exits 1 with "migration
failed", before any network.

[`config/database_test.go`](../../backend/internal/config/database_test.go) holds the composition of
a database role's components ([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
D4): the composed URL is read back with pgx's own `pgconn.ParseConfig`, and every part — a password
with `@`, `:`, `/`, `%`, `?`, `#`, `&`, `=`, `+`, a space and brackets, a user and a name with
reserved characters, an IPv6 host — comes back as it was given; the refusals name the variables and
never quote the password or a URL. `backend/tools/crdschema` has its own tests: one schema per served
version, nested objects closed, the root, a map, an object that keeps unknown fields and the
branches of `anyOf` left open.

The unit tests of the login's pieces: [`password_test.go`](../../backend/internal/auth/password_test.go)
(the recorded parameters, the salts, verification by the parameters a hash records, the refusal of a
malformed or oversized hash, the context, the dummy hash, the policy),
[`session_test.go`](../../backend/internal/auth/session_test.go) (the cookie value and its shape, the
username rule), [`api/session_test.go`](../../backend/internal/api/session_test.go) (the cookie's
attributes, the CSRF table of headers, the fail-closed rule, what moves the idle clock — every
request but a write the CSRF check refuses (`TestWhatMovesTheIdleClock`) —, the page a session's
recorded read comes from by `Sec-Fetch-Site` (`TestARecordedReadComesFromTheInstallationsOwnPages`),
both limits of a session, the keyed hash
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
with PKCE and its silent form with `prompt=none` (`TestASilentLoginAsksForNoPage`), an ID token that does not verify — another key, another audience, expired, another
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
silent start and the issuer's error to it as `login_required`
(`TestASilentLoginTheIssuerCannotCompleteAsksForASignIn`), the gate and its issuer check),
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
memory files — the start's time and the model per project directory —, the parsing of `git` output and of `.cowork.yaml`, the compatibility check;
[`mcpserver/server_test.go`](../../backend/internal/mcpserver/server_test.go) the catalogue as an
MCP client lists and calls it, and a server whose start was refused answering every tool with the
reason; [`mcpcli/cli_test.go`](../../backend/internal/mcpcli/cli_test.go) the configuration
(`https` except on loopback, the token's shape, no value echoed), the subcommands, the hooks
silent outside a repository, `token check`, `serve` refusing an API it does not know, and the
model of the `SessionStart` hook's input — the one Claude Code's hook reference shows — in the
mark of the server of the same project directory, and the starts without one; the `to_model` of
the `PostModelSwitch` hook's input in the same mark, a subagent's switch and one without
`to_model` leaving it, and the switch hook's standard output empty in every case, a malformed
configuration and a failed write included.
[`domain/repository_test.go`](../../backend/internal/domain/repository_test.go) is the table of
remote identities, sub-directories and proposed keys; [`mcpcli/export_test.go`](../../backend/internal/mcpcli/export_test.go)
the unpacking of an export, which stays inside its directory and never overwrites.

The import without a database ([import-and-export.md](import-and-export.md#tests)):
[`importer/`](../../backend/internal/importer/) reads the four shapes of a repository's ticket files
over the fixtures, every error class of a file with its line, the questions outside fenced code, an
upload in its three forms and every bound; every golden file of grammar v1 back to its own bytes
(`TestParseReadsTheGoldenFilesOfGrammarV1`); and the analysis — conflicts, duplicates, purged
numbers, the corrections and their refusals, an export with its links, blocks and parents.

The consistency check without a store or a database ([storage.md](storage.md#the-consistency-check)):
[`store/consistency_test.go`](../../backend/internal/store/consistency_test.go) holds the schedule, `judge`,
the question for an unlisted attachment, the order of the missing files and the summary;
[`store/consistency_compare_test.go`](../../backend/internal/store/consistency_compare_test.go) the comparison
a batch at a time — the keys sort as their ids, the comparison finds over laid-out tenants and at several
batch sizes what the check found when it read the whole listing and every row (`judgedAtOnce`, the judgement
as it was), the rows of a range are read only once the listing has passed it, a listing out of byte order
fails the run —, and `TestTheComparisonHoldsABoundedMemoryWhateverTheNumberOfObjects`, which runs it over
a synthetic tenant of a million objects, named by their rows and by none, and holds the growth of the live
heap, sampled after a collection, below 8 MiB; it takes about a second.

The rules of the states and the progress stages without a database:
[`domain_test.go`](../../backend/internal/domain/domain_test.go) (the move matrix, what a write of
the stages does, which done is by the stages) and
[`api/transitions_test.go`](../../backend/internal/api/transitions_test.go) (the transition and
`PATCH` checks for persons and agents over `store.TicketRow` values built by `in`, the stages and
the done by hand a ticket shows — rows the release before the stages left included) and
[`api/tickets_test.go`](../../backend/internal/api/tickets_test.go) (`mayAssign`: whom an agent may
assign a confidential ticket to).

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
binary, and `metrics_test.go` runs `go build` for `cmd/cowork`; without either on the `PATH` those
tests fail and say so.

Every store and API test then connects as the runtime role, as `cowork serve` does; the
administrative connection only creates and seeds. The tests share the run's database and keep
apart by unique slugs, so nothing resets between them.

```
make dev-up postgres-tls-up           # postgres:18 on :5432 and, serving TLS, on :5433, Silo on :9000, Dex on :5556
make test-integration                 # exports COWORK_TEST_DATABASE_URL, COWORK_TEST_DATABASE_TLS_*, COWORK_TEST_S3_* and COWORK_TEST_OIDC_ISSUER for them
COWORK_TEST_DATABASE_URL=… make test-integration   # any other PostgreSQL 18, as a role that may create roles and databases
make postgres-down postgres-tls-down minio-down dex-down
```

`POSTGRES_PORT=55432 make postgres-up test-integration` moves the container off a port that is
taken; `POSTGRES_TLS_PORT=` does the same for the PostgreSQL that serves TLS, `MINIO_PORT=` for
Silo, and `DEX_PORT=` for Dex and its issuer, which `make test-integration` follows when it is
given the same variables.

`COWORK_TEST_DATABASE_TLS_URL` and `COWORK_TEST_DATABASE_TLS_CA` are read by
[`database_tls_test.go`](../../backend/test/integration/database_tls_test.go) alone, which fails
without them and says how to set them: the administrative URL of the PostgreSQL that serves TLS,
without an `sslmode` — the test chooses one per connection —, and the certificate of the authority
that issued the server's.

### Fixtures of the integration tier

| Fixture | Where | What it gives you |
|---|---|---|
| `fixture.DB` — `fixtures(t)` | [`test/fixture/fixture.go`](../../backend/test/fixture/fixture.go), [`helpers_test.go`](../../backend/test/integration/helpers_test.go) | Rows written over the administrative connection, past row-level security and without audit rows: `Person`, `Tenant`, `Member` (a marked grant), `Account` (a local account with an Argon2id hash of the password, managed by a tenant, optionally with a temporary password), `GlobalAdmin`, `Project` (with its counter), `Ticket` (a plain task with the next number and no rank, as a release before the rank files one), `Token(TokenSpec)` returning the plaintext once, `Session` (a local session of twelve hours, as a login makes one) returning its cookie value once; `Exec`, `Query`, `QueryRow`, `QueryCount` for a state no route reaches. A zero `TokenSpec` is a write token of ninety days; `Agent` without capabilities holds every one, `fixture.AssistedCapabilities` is the "assisted" set |
| `world`, `newWorld(t)` | `helpers_test.go` | Two tenants with unique slugs; an admin, a member and a viewer of A, a member of B, a person in both; the projects `ALPHA` in A and `BETA` in B |
| `seedEveryTenantTable(t, w)`, `tenantBoundTables(t)` | `helpers_test.go` | A row of each tenant in every table with `tenant_id`, and the list of those tables from the catalog. `TestUnfilteredQueryUnderTenantSeesNothingOfAnother` walks the catalog and fails for a table the seed leaves empty |
| `openRuntime(t)`, `openStore(t, url)`, `as(person)` | `helpers_test.go` | The store as the runtime role (or another URL); a context whose store calls act for a person |
| `issueTokens(t, w)` | [`api_helpers_test.go`](../../backend/test/integration/api_helpers_test.go) | A token per person of the world, the administrator's with admin and with write scope, an agent token with every capability and an assisted one |
| `newAPI(t, opts…)` → `apiServer` | `api_helpers_test.go` | The whole handler on `httptest`: health, request id, log and pipeline, the runtime role, a real event listener, the run's bucket (1 MiB files, five per ticket) and response validation on; `opts` change `api.Options` |
| `apiServer.client(t, caller)`, `apiServer.do(…)`, `assertProblem(t, res, status, code)`, `sessionOf(t, person)` | `api_helpers_test.go` | The generated Go client acting as a token — or a session cookie, with `Origin` and `X-Requested-With` on its unsafe requests, for a server with `withLogin` — and an agent header; a raw request for what the client cannot express; a problem's status, code and envelope; a session of a person made through `fixture.DB.Session`, as a login makes one, for a test whose world has no local accounts |
| `withLogin`, `testOrigin`, `testPassword` | [`login_helpers_test.go`](../../backend/test/integration/login_helpers_test.go) | What a cookie login needs on `api.Options`: the base origin, the lockout, the password minimum — and the per-address throttle **off**, because every test of the run reaches the server from `127.0.0.1` and one shared limit would make the rest depend on speed; the tests of the throttle switch it on with a server key of their own, and with `Options.TrustedProxies` naming `127.0.0.0/8` the test server plays the proxy and `X-Forwarded-For` names the clients |
| `browser`, `apiServer.browser(t)`, `login`, `mustLogin`, `get`, `request` | `login_helpers_test.go` | A client that holds one session cookie and sends `Origin` and `X-Requested-With` on unsafe methods, as the frontend does; `withHeader`, `without` and `withBearer` take one away or add one to see a refusal |
| `clock`, `newClock()`, `withClock` | `login_helpers_test.go` | A clock a test moves, installed as `api.Options.Now`: hours of a session and minutes of a lock pass without waiting |
| `withAccounts(t, w)` | `login_helpers_test.go` | Local accounts with `testPassword` for the persons of a world, managed by tenant A (B's member by B), and their usernames |
| `isolated`, `newIsolated(t)` | `login_helpers_test.go` | A database of its own, migrated and empty, with the runtime role's store open on it, for what needs an installation that has no tenant and no person: the init state, the start-up synchronisation |
| `recordingLogger` | `login_helpers_test.go` | A logger that collects every record at every level as text, for searching a run for a secret |
| `simultaneously(sends…)`, `times(n, send)` | `api_helpers_test.go` | Starts requests at the same instant and collects their status codes, for the races a conditional write can lose: the request that comes second must answer as a later one would, never `500`. A send runs in its own goroutine, so it builds nothing with `require` |
| `ticketEnv`, `newTicketEnv(t)`, `task(…)`, `file(…)` | [`api_tickets_test.go`](../../backend/test/integration/api_tickets_test.go) | A world with its tokens and a running API with `withLogin`, so a `sessionOf` caller passes the CSRF check; a plain ticket body; filing as a caller, with a key for an agent |
| `openStream`, `openStreamAt`, `next` | [`api_events_test.go`](../../backend/test/integration/api_events_test.go) | An event stream read message by message; `openStreamAt` takes the path with its query |
| `fileIn`, `send`, `inbox`, `reasonsAbout`, `openMeStream`, `until`, `unreadOf`, `ticketPath` | [`api_inbox_test.go`](../../backend/test/integration/api_inbox_test.go) | Filing in either tenant, a write that must answer one status, a person's inbox and the reasons it holds for a ticket, a person-level stream read until a message matches — what came before it returned, for asserting what must not have — and an `inbox.changed`'s count |
| `assigned`, `decisions`, `walk` | [`api_me_lists_test.go`](../../backend/test/integration/api_me_lists_test.go) | The person-level lists as keys, and a list walked one item per page by its cursor |
| `next`, `sortRank`, `scoreOf`, `storedScore` | [`api_score_test.go`](../../backend/test/integration/api_score_test.go) | "Next for me" as keys; the sort of a project's rank by the score; a ticket's score, and the stored one held to `domain.ScoreKey` |
| `search`, `hitKeys`, `snippetText` | [`api_search_test.go`](../../backend/test/integration/api_search_test.go) | A search of a tenant or of the person's tenants as c, its hits as keys, a hit's snippet as one text with the found words in brackets ([search.md](search.md#tests)) |
| `sessionEnv`, `newDeletedScene`, `bin`, `binPath`, `binKeys`, `short` | [`api_deletion_test.go`](../../backend/test/integration/api_deletion_test.go) | A world whose persons have local accounts, with its tokens, a running API and tenant A's administrator in a browser session, which the purge takes; a deleted ticket's scene with everything that hangs off it or points at it; the bin as its items and as keys; a ticket's short key |
| `dashboardEnv`, `newDashboardEnv(t)`, `ago`, `ticket`, `hide`, `done`, `dashboard` | [`api_dashboard_test.go`](../../backend/test/integration/api_dashboard_test.go) | A world with the server's clock fixed on a Wednesday at noon and a project restricted away from the member; a ticket filed at a time, made confidential or done at a time through the fixture; the dashboard read as a caller |
| `adminWorld`, `newAdminWorld(t)`, `members`, `nextMembership` | [`api_members_test.go`](../../backend/test/integration/api_members_test.go) | A world whose persons have local accounts, tenant A's administrator in a session and an administrator's token beside it; the member list as a map; the next `membership.changed` of a stream |
| `repoEnv`, `newRepoEnv(t)`, `bind`, `lookup` | [`api_repositories_test.go`](../../backend/test/integration/api_repositories_test.go) | A world with its tokens and a running API; a binding with a fresh key; a lookup's answer |
| `contextOf` | [`api_context_test.go`](../../backend/test/integration/api_context_test.go) | A ticket's context document and its response |
| `mcpEnv`, `newMCPEnv(t)`, `run`, `serve`, `callTool`, `mustCall` | [`mcp_test.go`](../../backend/test/integration/mcp_test.go) | A git repository on disk whose `origin` lies under tenant A's slug, the API, and `cowork-mcp`'s environment against both: a subcommand by its command line with its exit code and output, the MCP server on an in-memory transport with a client that names itself `claude-code`, a tool call's text |

### The identity provider in the tests

Two issuers, for two purposes ([ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D3):

- **Dex**, the reference: [`hack/dex/config.yaml`](../../hack/dex/config.yaml) in the container
  `cowork-dex` of `make dex-up`, at `COWORK_TEST_OIDC_ISSUER`. One static client, `cowork`
  ([its secret](development-credentials.md#the-containers)), whose redirect URIs are `make dev`'s, the tests'
  `http://cowork.test/auth/callback` — a name that never has to resolve, because the test intercepts
  the redirect and replays it against its own server — and the end-to-end tier's
  `https://localhost:18443/auth/callback`, whose Dex is a container of its own made from the same
  file ([below](#end-to-end-tests)). Four static users
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
| [`migrate_test.go`](../../backend/test/integration/migrate_test.go) | A fresh database reaches the embedded version, a second run applies nothing, the runtime role reads the version; PostgreSQL 18 or newer; a schema ahead of the binary is served; tenant ids are UUIDv7; migration 17 ranks every project's open tickets in number order and restores the force it lifts (`TestRankMigrationKeepsNumberOrder`, on a database of its own that `migrateTo` brings to version 16 first); migrations 18 and 19 backfill the three progress stages, `done_from` and `done_by_hand` on the tickets a release before them left, derive the parents' new stages a level at a time, grant the new columns and restore the force (`TestStagesMigrationBackfill`, from version 17); migration 37 lets the capability sets of the tokens and of the chat take `set-horizon` beside `override-urgency`, rewrites no row and still refuses a name outside the catalogue (`TestTheCapabilityMigrationTakesBothNamesAndRewritesNothing`, from version 36 to 37); migration 38 rewrites every `override-urgency` of those sets to `set-horizon`, each name once in the order first named — a revoked token's too —, and a saved filter's `urgency` to `horizon`, a horizon already there winning, moves no version and no time, leaves both checks taking the old name — release 0.5 writes it after a rollback — and refusing one outside the catalogue, and restores the force it lifts (`TestTheContractMigrationRewritesTheNamesBefore`, from version 37 to 38); migration 39 changes no saved filter and lets an administrator of the tenant unshare and delete another person's shared one, which version 38 refused, never one that is not shared (`TestTheModerationMigrationWidensTheFilterPoliciesAndChangesNoRow`, from version 38); migration 40 rewrites again what 0.5 stores after a rollback — `override-urgency` beside `set-horizon` in the tokens' and the chat's sets, each name once in the order first named, a revoked token's too, and a saved filter's `urgency` to `horizon`, a horizon already there winning, a shared filter's through the moderation guard of migration 39 —, moves no version and no time, and then both checks refuse the old name with SQLSTATE `23514`, take every name of `auth.AllCapabilities` and refuse one outside the catalogue, and restores the force it lifts (`TestTheNarrowingMigrationRewritesAgainAndRefusesTheOldName`, from version 39); migration 44 fails on a body longer than 200,000 characters and leaves no check behind, applies once the row is shortened and the version set back, passes the rows at the bounds, and then refuses a longer body, options or answer with SQLSTATE `23514` naming its check (`TestTheLengthMigrationHoldsTheTextsToTheLengthsOfTheAPI`, from version 43) |
| [`command_test.go`](../../backend/test/integration/command_test.go) | The binary itself, built with `go build` and run with exactly the environment a container has, against a database of its own that no migration has touched, both roles given as components ([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D4): `cowork migrate` with `COWORK_MIGRATE_BOOTSTRAP=true`, as the chart's migration Job runs it, leaves the schema current and the bootstrap done — the local administrator, the bootstrap tenant with its grant and the administrator group's mapping, no client secret given —, a second run changes and records nothing, and a run without the switch, as the init container's, leaves the administrator active (`TestMigrateInJobModeLeavesTheBootstrapDone`, [ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md) D4); `cowork serve` exits 1 with `pending migrations: 1` on a schema one version behind, before it listens, and `cowork migrate` then applies that one (`TestServeRefusesAStaleSchema`, D3) |
| [`database_tls_test.go`](../../backend/test/integration/database_tls_test.go) | The database's private authority, `COWORK_DATABASE_CA` ([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D3), against the PostgreSQL of `make postgres-tls-up`: the built binary's `cowork migrate` and the runtime pool `store.Open` reads from `config.Load` hold `verify-full` with the authority, over TLS, and both refuse the same server through an authority that did not issue its certificate; `cowork migrate` refuses it through the system pool too |
| [`store_test.go`](../../backend/test/integration/store_test.go) | The runtime role check; an unfiltered query under tenant A sees nothing of B in any tenant-bound table; the context dies with its transaction; the wrappers; the append-only audit record; `Mutate`'s acts, rollbacks and idempotency, concurrent duplicates included; the expiry job and its lock; the token lookup, refusal bound and last-used date; an act's token name beside its id, never on a system actor's act in the request |
| [`api_core_test.go`](../../backend/test/integration/api_core_test.go) | Unauthenticated meta routes, unknown routes and methods, authentication and the agent header, one tenant's token in another, `/me` and tokens, tenant settings, the audit view, the body limit, a JSON body sent as multipart to the login and to a write refused with `415` before it is read (`TestABodyOfATypeTheRouteDoesNotTakeIsRefusedBeforeItIsRead`), cursors, validation |
| [`api_boundary_test.go`](../../backend/test/integration/api_boundary_test.go) | Every tenant route refuses another tenant's token exactly like an unknown tenant (`TestEveryTenantRouteRefusesAnotherTenantLikeNoTenant`); the routes come from a walk over the document (`tenantRoutes`), shared with the session test below, so the account routes are covered the day they exist; `POST /tenants` has no tenant in its path and is tested by `TestOnlyAGlobalAdministratorCreatesATenant` |
| [`api_login_test.go`](../../backend/test/integration/api_login_test.go) | The login through the whole handler: the session and its cookie, every failure answering identically at the same cost, the lockout in both modes and its window, the per-address throttle — also behind a trusted proxy, where two clients are throttled apart and a spoofed entry moves nobody, and for a peer that is no proxy, whose header is ignored; a parallel burst of one address held to the limit, one row per attempt, and the current password of a change held to it with the lockout off (`TestTheAddressThrottleHoldsForParallelAttemptsAndThePasswordChange`) —, the init state, the options, both session limits on a moved clock — a read extending it —, what moves the idle clock: a read, the event stream's connection and a write at most once a minute, never a write the CSRF check refuses, and a session whose stream alone reconnects lives to its absolute limit (`TestEveryRequestButARefusedWriteMovesTheIdleClock`), a session surviving a restart, a login that waits for its username's lock the test holds while an administrator resets the password, and makes no session (`TestALoginInFlightMakesNoSessionAfterThePasswordChanged`), logout, the expiry of the login's state |
| [`api_accounts_test.go`](../../backend/test/integration/api_accounts_test.go) | A temporary password gating the session, a password change counting and ending the other sessions, the account routes per role and across two tenants (what a tenant's administrator manages and does not), a token refused on creating an account, on resetting a password and on unlocking one while it still lists, deactivates and ends sessions (`TestAccountRoutesAnAdministratorsTokenMayStillCall`), deactivation ending tokens and sessions, a deactivation that waits for the tenant's lock the test holds and meets `last_admin` when the administrator acting was deactivated meanwhile, or goes through while another administrator remains (`TestADeactivationLeavesTheTenantAnAdministrator`), two administrators deactivating each other at once (`TestTwoAdministratorsCannotDeactivateEachOther`, eight rounds through `simultaneously`), a lock not inherited by a new account of the same name, a session's idempotency key scoped to its person |
| [`api_session_routes_test.go`](../../backend/test/integration/api_session_routes_test.go) | The routes only a session calls: a token created and shown once, its lifetime clamped, its idempotency, its project restriction named by key while the person sees the project (`TestATokenNamesItsProjectByKey`); a tenant created by a global administrator only; a widening of the tenant's settings refused to a token, which narrows them, and made in a session (`TestWideningTheTenantSettingsTakesASession`); the five recorded reads refused to a session from a sibling host or another site, served and recorded from the installation's own pages, the address bar and a browser without `Sec-Fetch-Site`, and a token's not looked at (`TestARecordedReadOfASessionComesFromTheInstallationsOwnPages`); the CSRF refusals (the `Referer` fallback, no origin, a second header, a cookie beside a token); the event stream ending with its session; the cross-tenant harness again with a cookie; no password, cookie or token in the log, the answers or the audit record |
| [`api_oversight_test.go`](../../backend/test/integration/api_oversight_test.go) | A global administrator without a role in a tenant ([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D2): the walk over every tenant route of the document, of which they reach the tenant, its members (without addresses), its mappings and the grant, and every other answers like an unknown tenant — a token of theirs, an agent-marked session and a person who is no global administrator reach nothing (`TestAGlobalAdministratorWithoutARoleSeesTheAdministrationOnly`); the grant to themselves, refused to a token, an agent and for anybody else, recorded and announced, after which the tenant answers as to an administrator; a tenant left without an administrator recovered, a grant below `admin` meeting no `last_admin`; a global administrator who holds `viewer` in such a tenant raising their own grant to `admin`, which a viewer who is none cannot (`TestAGlobalAdministratorWithALowerRoleRaisesTheirOwnGrant`); the list of every tenant, paged, refused to anybody else, to a token and to an agent; and the policies of migration 26 as the runtime role sees them — every tenant to a global administrator, their own grant in any role and its role changed, and nothing else of a tenant outside its transaction (`TestPoliciesOfTheGlobalAdministratorsReach`) |
| [`policy_login_test.go`](../../backend/test/integration/policy_login_test.go) | The policies of the persons, their accounts and their sessions as the runtime role sees them, with no handler in front — the identity provider's persons, memberships and mappings, a mapping made and changed by a global administrator who administers the tenant only, and the trigger that keeps a project's restriction to the tenant's administrators, among them; the session lookup finds the presented row only |
| [`bootstrap_test.go`](../../backend/test/integration/bootstrap_test.go) | The start-up synchronisation on an isolated database: created, left alone, re-hashed with the sessions ended and the tokens revoked, deactivated and reactivated, taken over from a tenant's account of the same name, four replicas at once |
| [`api_oidc_test.go`](../../backend/test/integration/api_oidc_test.go) | The login through Dex: its four users through the gate, the mapping and a grant — the person made by issuer and subject, the sealed refresh token, the acts and the source hash —, the first-tenant rule, a silent start, whose `prompt=none` Dex ignores and answers with its form (`TestASilentStartThroughDex`), leaving the allow-list (the sessions end, the token is refused and works again behind a wider gate), a spent refresh token, and the roles that hold: a viewer cannot write, a member of one tenant cannot list another, a member outside a restricted project cannot read it until an administrator puts them on its list |
| [`api_oidc_fake_test.go`](../../backend/test/integration/api_oidc_fake_test.go) | What only the fake issuer shows: the refresh that follows the groups, an unreachable issuer, a refused refresh token, no refresh token, the logout at an issuer with an end-session endpoint, every failure of the callback, the silent sign-in — a code from an issuer that holds the session, `login_required` with the path from one that holds none (`NoSession`), also stale, and `oidc_failed` for the same error to a login the person started (`TestASilentSignIn`) —, a deactivated person, the mapping editor's own role and `last_admin`, no secret of the issuer in a log line or an audit row, leaving the gate stopping the tokens at once, groups older than the maximum age refusing the tokens until a sign-in or a refresh, no audit row naming a person's groups |
| [`api_members_test.go`](../../backend/test/integration/api_members_test.go) | The administration: a member added by address — any case, a verified one, one the issuer said nothing about only with `COWORK_OIDC_EMAIL_TRUSTED`, never an unverified one, ambiguous, deactivated — or by username, in a session only; grants and the last administrator; mappings that derive at once, change and go, made and changed by a global administrator who administers the tenant only, removed by any administrator; a project's restriction and access list; `membership.changed` reaching its audience; the source hash on every row of a request and none on a job's; the bootstrap tenant of the administrator group |
| [`api_review_test.go`](../../backend/test/integration/api_review_test.go) | The findings of the security review of 2026-10-04, one test each: a refresh that holds no connection or lock while the issuer hangs, on a pool of two (`TestARefreshWaitsForNoOneElse`); a refresh that read nothing keeping the person's newer groups; the issuer refusing cowork's client; a refreshed ID token that does not verify; a mapping's derivation leaving who cannot act; two administrators removing each other at once (`TestTwoAdministratorsCannotRemoveEachOther`, eight rounds through `simultaneously`); only an administrator who can log in counting for `last_admin`; a person of another issuer outside the gate; no address in an audit row; the address for administrators only; a project-restricted stream hearing only its project |
| [`api_repositories_test.go`](../../backend/test/integration/api_repositories_test.go) | Binding by the normalised identity, idempotent, the remote kept without credentials, another project's binding refused and named only to who sees it; who binds and unbinds; the list; the lookup — bound, the covering sub-directory, ambiguous across tenants, a restricted token's tenant —, the proposal (`only-tenant`, `remote-owner`, `choose`, a free key, none for a project-restricted token); a project created for a repository and the `200` for one bound already; `GET /me/token`; the public schema of `.cowork.yaml`; no credential in a lookup's answer |
| [`api_context_test.go`](../../backend/test/integration/api_context_test.go) | The context document of [ADR 0044](../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md) D2 over the real data: the sections, what the caller cannot see absent, the limits, every call recorded |
| [`api_chat_test.go`](../../backend/test/integration/api_chat_test.go) | The chat in the UI against the stub provider ([chat.md](chat.md#tests)): the availability with two providers; the person's pick and the default; a turn that files a ticket and ranks it to `now` with the chat's mark, the default set and a key on its acts; the person's capabilities over the Anthropic format — a close refused, chosen in a session only and recorded, then run at once; a token, a CSRF failure and an agent-marked session refused; a turn kept in its tenant; the turn's time, its comments and a failing provider; the agent header on a session; the turn limit and the shutdown; the stop that ends a slowly streaming turn within a second, its provider request cancelled, others untouched; a question asked of the person; the policies of `chat_capabilities` |
| [`api_token_marks_test.go`](../../backend/test/integration/api_token_marks_test.go) | Every act through a token marked with it ([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6): a plain token, an agent token with its header and a browser session each file a ticket, comment and edit the comment, upload, ask and answer, set a stake, book and correct time (not the agent) and change a field; every answer, revision and act of the activity, the filing and the stake, and every row in the database, carries the token's id and name and the agent mark exactly as the credential was — none for the session; the context document names the token where it names an agent; the tenant's audit view names the token in JSON and CSV; after the plain token's revocation another member reads its name on its acts, and no answer holds a part of a token (`TestEveryActThroughATokenIsMarkedWithIt`) |
| [`mcp_test.go`](../../backend/test/integration/mcp_test.go) | `cowork-mcp` against the real API ([ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D6): the working day from the proposal through `create_project`, filing, deciding, working, asking, answering and `finish_work`, every act the agent's with the client's name and every creating `POST` keyed; an assisted token's limits in the descriptions and `finish_work` stopping at `review`; the subcommands — `session-context` unbound and bound, `lookup`, `session-end` with and without changed files, `token check` and a revoked token, `export` into a new directory and refused a full one (`TestTheExportSubcommand`) —; and the binary itself, built with `go build` and run by its command line with its environment, the memory file under a temporary `HOME` and the model file `model-switch` writes there ([ADR 0070](../adr/0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md) D6); an administrator's token refused the deletion, the restoration and the purge through the escape hatch, and a deleted ticket missing to `get_ticket` and `search` (`TestTheToolsNeverDeleteAndMissADeletedTicket`); the context `get_ticket` answers naming the horizon by its word — the key `horizon:`, the act as `set the horizon to now`, neither urgency nor overridden |
| [`metrics_test.go`](../../backend/test/integration/metrics_test.go) | `cowork serve` as the image runs it — the binary built with `go build`, an isolated database migrated beforehand, its own free ports — with the metrics listener: a scrape after a few requests answers the routes by their pattern, the acts by their actor, a refused token, the pool, the schema version, every job's run, the Go runtime, and no forbidden label; the API's port has no `/metrics` and the metrics port nothing else; a dirty flag set in the database shows within ten seconds while it serves; `SIGTERM` ends both listeners cleanly; an empty `COWORK_METRICS_ADDR` opens none (`TestServeAnswersAScrapeOnItsMetricsListener`, [metrics.md](metrics.md#tests)) |
| [`api_imports_test.go`](../../backend/test/integration/api_imports_test.go) | The import of [import-and-export.md](import-and-export.md): a dry run of the fixtures and its execution with corrections — who may make and read it, another writer not —, the report, every ticket, question and link with its act naming the job, one `project.changed`, the sequence past the highest number, a second execution `409` (`TestImportADryRunAndItsExecution`); a conflict and an error left out and the rest imported, a conflict filed after the dry run among them, an exclusion naming the project's ticket (`TestImportLeavesOutWhatItCannotImport`); a member's agent token without any capability importing and setting a parent afterwards (`TestAWriterAndTheirAgentImport`); a plain token's import assigning a confidential ticket only to its person, a browser session's as the file says (`TestATokenImportAssignsAConfidentialTicketToItsPersonOnly`); a purged number given back and a `/context` document skipped (`TestImportGivesAPurgedNumberBack`); a member added between a browser session's dry run and its execution not assigned (`TestTheExecutionAssignsWhomTheDryRunNamed`); the bounds, a broken upload, an archived project and the expiry of a dry run; the policies of `import_jobs` as the runtime role meets them; the purge job taking an imported ticket's file out of its report (`TestThePurgeTakesAnImportedTicketOutOfItsReport`); this repository's whole `docs/tickets/` read without an error, its open tickets after the execution as many as its open `state:` lines (`TestImportThisRepositorysTickets`) |
| [`api_exports_test.go`](../../backend/test/integration/api_exports_test.go) | The export: the round trip of [ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D5 — a project exported, imported into an empty one and exported again is the same archive up to the keys and the times (`TestTheExportRoundTripsThroughTheImport`) —; the archive as each reader sees it, the confidential tickets left out and counted, a link to a hidden ticket absent, a restricted project absent from the tenant's archive, the attachments' metadata, every export recorded (`TestTheExportFollowsItsReader`); the archive streamed a page of tickets at a time, the heap growing by a fraction of what it carries (`TestTheExportStreamsALargeProjectWithinAMemoryBound`) |
| [`api_inbox_test.go`](../../backend/test/integration/api_inbox_test.go) | The inbox of [ADR 0020](../adr/0020-notifications-are-an-in-app-inbox-per-person.md): every event of D2 telling its recipient and the actor and the actor's agent nothing; the inbox across two tenants, newest first, narrowed, paged and not another person's, a tenant-restricted token's tenant only; a ticket made confidential, a project restricted away and a tenant left taking theirs out of the list and the count; marking one and every one read, as acts, refused to a read-scope token and to another person, and by a browser with and without the CSRF header; the ninety days of the job; a notification of the reason `merged` that GitHub's webhook of a release up to 0.12.0 made, out of the list, the count and the marking (`TestAMergeNotificationOfTheRemovedWebhookIsLeftOut`); the restrictive policy (`TestTheInboxPolicyHoldsAPersonToTheirOwn`); the person-level stream's count, a question of another tenant without an id, and what it never carries — another person's question, a hidden project's, a confidential ticket's, a left tenant's, and anything of another tenant on a tenant-restricted token |
| [`api_me_lists_test.go`](../../backend/test/integration/api_me_lists_test.go) | "Assigned to me" and "open decisions" across two tenants and a restricted project: the score's order with the place in the rank beside it, done tickets and other people's questions absent, the cursor walking the same order, the narrowing and its `404`, the restricted tokens |
| [`api_score_test.go`](../../backend/test/integration/api_score_test.go) | The score following its inputs — a filing, the severity, the horizon, the stakes, the age —, none while done; the sort by the score around a hidden ticket, its one act in the activity of the tickets it moved, its event, its refusals; 800 moves into one gap through the rebalancing; "next for me" across two tenants, a restricted project and confidential tickets, its narrowing, its cursor and its restricted tokens; migration 34's scores held to the function |
| [`api_dashboard_test.go`](../../backend/test/integration/api_dashboard_test.go) | The tenant's dashboard ([api.md](api.md#the-dashboard)), one test per tile and its definition, with the server's clock fixed (`Options.Now`) and the timestamps written by the fixture: each read as the administrator and as a member who sees neither a restricted project nor a confidential ticket — the member's numbers are the tile without them, and no hidden ticket is ever named; the edges of the definitions — a ticket exactly seven days old, a Monday at midnight, a week the period cuts, a reopened ticket, the window's first instant, the latest act into `blocked` and the update standing in for a missing one, the period's first and last booking day, the visibility of time; the filter's archived and negated projects and a hidden key answering like a missing one; the default period, the refusals, the weak `ETag` unmoved by a ticket the member cannot see, and the boundary's `404` for another tenant's member and a project-restricted token |
| [`api_consistency_test.go`](../../backend/test/integration/api_consistency_test.go) | The consistency check of [ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md) D4, D5: a lost file, two orphans, one of them gaining metadata before the removal, and a stray object in tenant A, nothing in tenant B — the counts and the lists, the summary without a file name, the counts on a second store's registry, the acceptance, the removal in a session that keeps the object that gained metadata, a stale check `409`, the next checks, the bytes put back (`TestTheConsistencyCheckFindsWhatARestoreLeftAndTheAdministratorSettlesIt`); a member refused, another tenant's administrator `404`, no agent and no token removing anything, row-level security reading nothing for a member (`TestTheConsistencyCheckIsTheTenantAdministratorsAndNoAgents`); `cowork check-consistency` on the built binary against an isolated database (`TestTheCheckRunsAtOnceFromTheCommandLine`). The run checks every tenant of the shared database; the assertions look at their own |
| [`consistency_order_test.go`](../../backend/test/integration/consistency_order_test.go) | The two orders the consistency check compares as streams ([storage.md](storage.md#the-consistency-check)): PostgreSQL reads a tenant's attachments, UUIDv7s and UUIDv4s, range by range in the byte order of their keys, above the one id and up to the other (`TestTheAttachmentsAreReadInRangesInTheOrderOfTheirKeys`); the S3 server, Silo, lists the backend's keys, the same ids in capitals and keys the backend never writes in byte order, and a listing whose context ended ends with the error (`TestTheListingHandsTheKeysOnInByteOrderAndEndsWithItsContext`) |
| [`api_deletion_test.go`](../../backend/test/integration/api_deletion_test.go) | The deletion of [ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md): a deleted ticket — with a parent, a child, a blocker, a block, a link, a comment, a question, a file, time, a stake and a notification — answering like a missing one to an administrator, a member and a viewer on every route of it and under it, its rendered body included, in every list, the full text, the trees, the links, the activity, the context, the person-level lists, the inbox and the time report, and in the bin only; the restoration bringing all of it back; the deletion, the restoration and the purge on the event stream (`TestADeletedTicketAnswersLikeAMissingOne`); the search of the tenant and of the person, the person-level lists and the inbox's count across two tenants, and the deletion on a person-level stream opened in the other tenant (`TestADeletedTicketLeavesSearchAndThePersonLevelLists`); who may — role, scope, agent, another tenant — and the bins of two tenants apart; the purge in a browser session only — every token `403 session_required`, an agent-marked session `agent_forbidden` (`TestPurgingTakesABrowserSession`); the purge removing every row and the object, keeping the audit rows without content, making the children roots and the block an external reference, the key never handed out again; the job after thirty days in its tenant; the restrictive policies and the owner's function as the runtime role meets them; racing deletions, purges and a restoration |
| [`api_filters_test.go`](../../backend/test/integration/api_filters_test.go) | Saved filters across two persons and two tenants: private and shared, the owner beside a shared one, the owner changing or deleting with `If-Match`, the acts; a tenant administrator unsharing and deleting another person's shared filter — one whose owner left the tenant among them — as recorded acts, and refused anything else of it, a filter that is not shared, a token with less than `admin` scope and another tenant's (`TestAnAdministratorUnsharesOrDeletesAnotherPersonsSharedFilter`); the parameters refused as the lists refuse them, the warning of a value that no longer holds, a filter that names a restricted project, a confidential or a deleted ticket withheld from another reader; the idempotent creation, an agent's marked; an agent's five acts on its person's filter, each allowed or refused — saving, changing, sharing and unsharing at the baseline, a token with no capability included, and deleting refused as a deletion, to a token's agent, to an administrator's agent on another person's shared filter and to a session the header marks (`TestAnAgentKeepsItsPersonsSavedFilterAndDeletesNone`); a filter that names `horizon`, and one that names `urgency`, the name `horizon` had before, refused as a parameter the filters do not have; the restrictive policies as the runtime role meets them, an administrator's unshare reading its row back only for the filter the transaction names and refused by the trigger when it renames the filter or changes its conditions as well (`TestTheSavedFilterPoliciesAdmitAnAdministratorToASharedFilter`); the owner of a filter whose owner left the tenant answered by its id alone |
| [`api_paging_test.go`](../../backend/test/integration/api_paging_test.go) | The numbered pages of the tables ([ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D2): the projects, the audit view — its CSV too —, the members and the person's tokens answer a page with `total`, `page` and `per_page`, clamped like `limit`; the total counts what the caller sees and the filters select, a restricted token itself; a numbered page with a cursor or a limit, `per_page` alone and a page past row 10 000 refused (`TestTheTablesTakeNumberedPages`) |
| [`api_list_etag_test.go`](../../backend/test/integration/api_list_etag_test.go) | Every list the client polls — the person-level ones, the prerequisite tree, the bin and the saved filters among them — answers a weak `ETag` and `304` without a body to it, a change of the list a new tag, and another caller's tag is not this caller's page (`TestThePolledListsAnswerNotModified`, [ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md) D7) |
| `api_projects_test.go`, `api_tickets_test.go`, `api_rank_test.go`, `api_links_test.go`, `api_prerequisites_test.go`, `api_transitions_test.go`, `api_stages_test.go`, `api_questions_test.go`, `api_comments_test.go`, `api_interest_test.go`, `api_progress_test.go`, `api_time_test.go`, `api_attachments_test.go`, `api_events_test.go`, `api_export_test.go` | The rules of [domain.md](domain.md), [storage.md](storage.md), [events.md](events.md) and [markdown-grammar.md](markdown-grammar.md), route by route, across tenants, restricted projects, confidential tickets, roles, scopes and agents; `api_stages_test.go` the state `review`, done by hand and its withdrawal, done by the stages and the reopen, their refusals for persons and agents, a parent's stages, an open ticket whose stages are full, what the release before the stages writes over this schema in a rollback (its statements verbatim), the override that holds, `done_after` and the `review` limit; `api_prerequisites_test.go` the tree and its upward reading, a ticket under two others, paging and its count, what lies behind a confidential ticket and a restricted project absent, and a graph of 5^8 paths answered at once |

## Frontend unit tests

`ng test` with the `@angular/build:unit-test` builder, vitest, jsdom. `CI=true` and
`--watch=false` make it run once; the Make targets set both. Coverage comes from
`@vitest/coverage-v8`, a dev dependency of the frontend (`make frontend-test-coverage`,
reports under `frontend/coverage/frontend/`: `text-summary` on the console, `lcov.info`,
`coverage-summary.json` for CI). A test may take fifteen seconds, not Vitest's five:
[`vitest-base.config.mts`](../../frontend/vitest-base.config.mts), which the builder merges
(`runnerConfig` in `angular.json`), because a component test that renders a page took 5.4 s on a
shared CI runner under coverage and failed a run that was otherwise green.

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

## End-to-end tests

Playwright Test in [`frontend/e2e/`](../../frontend/e2e/) against the two built images
([ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)).
Nothing of the dev server is in it: the browser talks to the Ingress stand-in in front of the
images, as it talks to the Ingress of an installation.

```bash
make e2e-browsers                 # once: Chromium and WebKit of the @playwright/test version
make docker-build e2e             # both images of this commit, then the stack, the suite and its removal
make e2e E2E_ARGS="--project=chromium-dark board.spec.ts"   # a part; E2E_ARGS reaches playwright test

make e2e-up                       # the stack alone, kept, to write tests against it:
cd frontend && npx playwright test -c e2e [--ui | --headed | file]
make e2e-down
```

`make e2e` uses `BACKEND_IMG` and `FRONTEND_IMG` as `make docker-build` names them and refuses
two images whose `org.opencontainers.image.revision` labels differ. A failed run leaves
`frontend/e2e/test-results/` — per failed test its trace, video and screenshot
(`npx playwright show-trace <trace.zip>`), and the containers' logs in `containers/` — and the HTML
report in `frontend/e2e/playwright-report/`.

### The stack

[`hack/e2e.sh`](../../hack/e2e.sh) makes everything it runs on a Docker network of its own,
`cowork-e2e` (`E2E_NAME=` renames all of it), and removes it afterwards — never a container of
`make dev-up`, never the development database. Two ports are published, on `CONTAINER_BIND`:

| Container | What it is |
|---|---|
| `cowork-e2e-postgres` | `POSTGRES_IMAGE`; the database `cowork_e2e`, owned by `cowork_owner`, served as `cowork_app`; no published port |
| `cowork-e2e-minio` | `MINIO_IMAGE`; the bucket `cowork-e2e`, made through a port Docker chooses, since the server never makes its bucket |
| `cowork-e2e-dex` | `DEX_IMAGE` with [`hack/dex/config.yaml`](../../hack/dex/config.yaml), its issuer moved to `http://localhost:5557/dex` (`E2E_DEX_PORT`); it holds the network namespace below and publishes both ports |
| `cowork-e2e-backend` | `BACKEND_IMG`, read-only, migrating on start; the local administrator `e2e-admin`, Dex as identity provider with `make dev`'s gate, `COWORK_BASE_URL=https://localhost:18443` (`E2E_PORT`), a server key per run, and the per-address login throttle off (`COWORK_LOGIN_ADDRESS_LIMIT=0`): every browser reaches the backend through the one stand-in, so all of them are one address to it, as in the integration tier |
| `cowork-e2e-frontend` | `FRONTEND_IMG`, read-only, as the chart runs it |
| `cowork-e2e-ingress` | `INGRESS_IMAGE` with [`hack/ingress/default.conf`](../../hack/ingress/default.conf) as it is, but listening with TLS on `E2E_PORT`, and a certificate for `localhost` made with `openssl` for the run and never stored |

**Why the backend and the stand-in share Dex's network namespace** (`--network container:`, as
containers of one pod share `localhost`): an issuer is one URL for the browser and for the backend,
and the backend takes plain `http` only on a loopback host
([`config/oidc.go`](../../backend/internal/config/oidc.go) `checkIssuer`). Inside the namespace,
`http://localhost:5557/dex` is Dex and `https://localhost:18443` the stand-in; outside, the
browser reaches both through the published ports. **Why TLS**: WebKit stores no `Secure` cookie from
`http://localhost`, and the session cookie is `Secure` everywhere
([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D2); the suite sets
`ignoreHTTPSErrors` for the run's certificate.

### The suite

| File | What it walks |
|---|---|
| [`playwright.config.ts`](../../frontend/e2e/playwright.config.ts) | Four projects — `chromium-light`, `chromium-dark`, `webkit-light`, `webkit-dark`, by `colorScheme` —; WebKit runs the tests tagged `@smoke` (all of today's); a test tagged `@dark` — the board's screenshot — runs in the dark projects only; no retries (D7); trace and video kept for a failed test; four workers, two under `CI` |
| [`global-setup.ts`](../../frontend/e2e/global-setup.ts) | Once per run, through the API: the local administrator signs in, makes the tenant `e2e` (the installation's first, so that Dex's people may sign in), maps `team-red` to `member` in it, makes the tenant `e2e-other` — the administrator's second, for what spans a person's tenants —, makes the token `e2e-seed` the workers seed with (`COWORK_E2E_TOKEN`), seeds the visual board, and keeps its session in `e2e/.auth/admin.json` (ignored) |
| [`login.spec.ts`](../../frontend/e2e/login.spec.ts) | The local administrator through the form: the session cookie stored with `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/` and invisible to the page's script, kept across a reload, a write through it (filing a ticket), the same write without `X-Requested-With` refused `403 csrf`, the sign-out, the session gone; a local account the administrator makes signing in with its temporary password, sent to the password page, choosing its own, signing in again with it; `bob@example.com` through *Sign in with Dex*, Dex's form, back as a member of `e2e` by the mapping; the same person's session cookie removed, the login page saying that it signs them in again, and a pointer move — no click — leading to Dex's form through `silent=true` |
| [`tickets.spec.ts`](../../frontend/e2e/tickets.spec.ts) | A ticket filed with the dialog of the project's header, in the horizon `later` of the backlog, moved `filed → analysed` on its page, the move in the API and back in the backlog |
| [`board.spec.ts`](../../frontend/e2e/board.spec.ts) | A project's address opening its board, and the navigation's link too; a card dragged from Refinement to Ready — the transition `analysed → decided`, in the column, the count, the live region and the API, and after a reload |
| [`tenant-board.spec.ts`](../../frontend/e2e/tenant-board.spec.ts) | Passes in CI (run 37285901009 of commit `65337eb`, 2026-10-05) and locally: two projects' swimlanes through the address's filter; a card dragged from Refinement to Ready in its swimlane — the transition `analysed → decided`, in the column, the live region and the API —, then dragged onto the other swimlane, which says no while it is over it; let go there, the toast that says why, the card where it was and the ticket unchanged in the API |
| [`ticket-list.spec.ts`](../../frontend/e2e/ticket-list.spec.ts) | Passes in CI (run 37285901009 of commit `65337eb`, 2026-10-05) and locally: the tenant's ticket list over two projects through the address's `project`, each row with its project; the address's `severity` narrowing it, and the same after a reload; a ticket filed through the API meanwhile at the top without a reload; a row opening its ticket; *Clear filters* leaving the address without a filter |
| [`backlog.spec.ts`](../../frontend/e2e/backlog.spec.ts) | A row dragged by its handle to the top of `later` — the order in the page, the live region and the project's rank in the API —, and one dragged into the empty `next` — the horizon in the page and the API, and the reason field a person may leave with Escape; a ticket of a higher score that the rank puts last, marked *score* with its arrow up and its sentence, the only mark; *Sort by score* asked and confirmed — the rows by score, no mark left, the live region's count of the tickets that changed their place, and the rank in the API |
| [`visual.spec.ts`](../../frontend/e2e/visual.spec.ts) | The board of the tenant `e2e-visual` in the dark scheme, and its dashboard in both schemes, against their pictures ([below](#the-screenshots)) |
| [`assigned.spec.ts`](../../frontend/e2e/assigned.spec.ts) | Passes in CI (run 37285901009 of commit `65337eb`, 2026-10-05) and locally: the phase's path with two identities, each in a browser context of its own — the administrator makes a local account (its temporary password changed through the API, `signInWithNewPassword`) and files a ticket with the dialog, assigned to it; the account's page, open on "Assigned to me" with its stream live before the ticket exists, shows it and the bell counts it without a reload; its inbox says "assigned it to you"; it opens the ticket from there, edits the title in place and the body as Markdown, moves it to `analysed` and closes it by hand with a verification note; the administrator's page of the ticket, open all along, shows the new title, the body as the server rendered it, and the ticket done |
| [`conversation.spec.ts`](../../frontend/e2e/conversation.spec.ts) | Two identities on one ticket: a member's page of it open, its stream live; the administrator writes a comment, edits it and withdraws it after the question, edits the text of the question they asked, and from another ticket's page links that ticket as blocking this one — the member's page shows the comment, its new text and on request its earlier one, then *withdrawn* without either text, the question's new text, and the prerequisite tree with the blocker and `1 open` |
| [`rendered.spec.ts`](../../frontend/e2e/rendered.spec.ts) | A body with a heading, a table, code, a link, the ticket's own PNG attachment as an image and two hostile lines (a raw `<img>` with a handler, a `javascript:` link), seeded through the API: the page shows the markup, the link with its `rel` and `target`, the image from its attachment's path and loaded, the hostile lines as text with nothing of them run, and the shell's content-security policy refuses nothing (`policyViolations`) |
| [`start.spec.ts`](../../frontend/e2e/start.spec.ts) | A member of one tenant with a critical ticket in `now` assigned to them: `/` is "Next for me" with the ticket, its tenant and *yours*; no tenant switcher, the tenant's name in the top bar a link to its front page |
| [`search.spec.ts`](../../frontend/e2e/search.spec.ts) | A word of the test's own in a title and a body, in a comment of a long ticket and in a ticket of `e2e-other`: the top bar's box inside `e2e` lists that tenant's two hits with where each was found and the word marked in its snippet, and not the other tenant's; *Search all your tenants* lists all three with their tenants; the comment's hit opens the ticket scrolled to the comment, which the plain address leaves below the fold; the policy refuses nothing |
| [`filters.spec.ts`](../../frontend/e2e/filters.spec.ts) | Two identities on one backlog: the member narrows it by its search, saves the filter under a name and shares it; the administrator picks it among the saved filters by its name and its owner, and the backlog applies it — the search field, the rows — with *by* and the owner beside it, *Stop sharing* and *Delete* of the member's filter, and no share toggle |
| [`deletion.spec.ts`](../../frontend/e2e/deletion.spec.ts) | The administrator deletes a ticket from its page after the question and lands on the backlog without it; a member gets *No such ticket* at its address and an open backlog without it; restored from *Deleted tickets*, it is back in the member's backlog without a reload; deleted again, it is purged after the two questions and is not in the bin after a reload |
| [`dashboard.spec.ts`](../../frontend/e2e/dashboard.spec.ts) | A tenant of the test's own, so that the tiles count its tickets alone: two projects, one restricted away from a member, a confidential ticket in the other; the administrator's front page counts all three, the member's one — the open tickets, the severities, the recent tickets, the projects' cards; the member's project filter lands in the address and survives a reload; a ticket the member moves on its page in a second tab moves the administrator's state tile within five seconds; a ticket the administrator deletes leaves both dashboards |
| [`import.spec.ts`](../../frontend/e2e/import.spec.ts) | Not run yet (written 2026-10-07 without the stack): a member of the fixture tenant reads a fresh project with its export and its import, a writer's (edited 2026-10-09, not run either); the administrator chooses two ticket files of a repository on the import page, makes the dry run, reads its report at the job's address after a reload — two to create, no conflict, no error —, executes after the question and finds both tickets in the backlog's `later` with the numbers of their files; the project's export downloads under `<tenant>-<KEY>-<YYYYMMDD>.tar.gz`; the policy refuses nothing |

Every path checks that the page shows the scheme its project emulates (`.app-dark` on `<html>`, or
not).

| Fixture | Where | What it gives you |
|---|---|---|
| `test`, `asAdmin` | [`support/fixtures.ts`](../../frontend/e2e/support/fixtures.ts) | A test without a session (the login's paths), and one that starts as the local administrator from `.auth/admin.json` |
| `project` | `support/fixtures.ts` | A project of `e2e` with a key of its own (`E` and seven random characters), made per test, so tests run in parallel without a reset (D7) |
| `member`, `Person` | `support/fixtures.ts` | A member of `e2e` made for the test — a local account, its temporary password changed — signed in in a browser context of its own in the project's scheme: its `page`, its person's `id` (to assign a ticket to) and the `name` the pages show; the second identity of a path, closed after the test |
| `newAccount(browser, role, slug)`, `newContext(browser, storageState)` | `support/fixtures.ts` | The same for any role and tenant, which the caller closes; a browser context beside the test's own in its project's scheme, with the administrator's session when given `adminState` |
| `policyViolations(page)` | `support/fixtures.ts` | What the page's content-security policy refuses from then on, across navigations: every document's `securitypolicyviolation` events and the console errors that name the policy (WebKit reports through the console); a test asserts it empty at its end |
| `seed` (per worker) | `support/fixtures.ts`, [`support/api.ts`](../../frontend/e2e/support/api.ts) `Seed` | The administrator's token: `project`, `file` (a ticket at the end of its horizon, `later` by default, with a `body` and an `assignee` when given), `transition` (with a reason or a block), `ticket`, `horizon` (a horizon's open tickets in the rank), `replaceBody` and `confidential` (over the version just read), `attach` (a file), `comment`, `ask` (a question open in the tenant), `remove` (into the bin); `Seed.create(baseURL, token, slug)` seeds another tenant, such as `otherTenant` |
| `Session`, `sessionContext`, `signIn` | `support/api.ts` | A request context that holds a session and writes with the origin and `X-Requested-With: cowork`, for what only a session does: a tenant, a mapping, a token, a local account of any tenant, a project's restriction |
| `signInWithNewPassword` | `support/api.ts` | A browser context's own request context signed in as a local account, its temporary password changed: the context's pages are then that person's — the second identity of a path |
| `expectScheme(page)` | `support/fixtures.ts` | The page's scheme is the emulated one |
| `drag(page, handle, target, at)` | `support/fixtures.ts` | A drag the Angular CDK takes: press, a few pixels past its threshold, twenty steps to the point `at` of the target, release |
| identities | [`support/identities.ts`](../../frontend/e2e/support/identities.ts) | `COWORK_BASE_URL`, the administrator (`COWORK_E2E_ADMIN`, `COWORK_E2E_ADMIN_PASSWORD`), Dex's `bob`, `freshPassword()` |

A path finds what it touches by its role and accessible name first (`getByRole`), by its label
next, and by `data-testid` (`getByTestId`) where the page names an element in no accessible way or
no role and name tell it apart; a component gets a test id or an `aria-label` for a test only then
([ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)
D7 as amended 2026-10-06). A confirmation is the `alertdialog` its header names. The paths written
before that rule find things by `data-testid`, but for a PrimeNG menu item (*Sign out*) and Dex's
own login form, found by its inputs' names.

### The screenshots

[`visual.spec.ts`](../../frontend/e2e/visual.spec.ts) compares two pages of the tenant `e2e-visual`
— the project `VIEW` with a card in every column, seeded by the global setup
([`support/visual.ts`](../../frontend/e2e/support/visual.ts)) so that the navigation lists the same
one project in every run — with one picture per browser and scheme,
[`screenshots/visual.spec.ts/<page>-<project>.png`](../../frontend/e2e/screenshots/visual.spec.ts/):
the board in the dark projects (`board`, tagged `@dark`) and the tenant's dashboard in all four
(`dashboard`), at most 2 % of the pixels different (`maxDiffPixelRatio`), animations stopped.
[`screenshot.css`](../../frontend/e2e/screenshot.css) takes out what differs by build or by the day of
the run, not by change: PrimeNG's license notice of a build without the PrimeUI key (CI builds
without it), the version line, the dashboard's period and how long its longest blocked ticket has
waited. The bell's count stays in: it counts the administrator's unread notifications, which the
paths that ran before made, and its badge is too small a share of the page to fail the comparison.

One picture serves every platform, and the threshold is what that costs. Measured on 2026-10-04
with Playwright 1.63: the renderings of macOS and of Linux (`mcr.microsoft.com/playwright:v1.63.0-noble`
on arm64) differ in about 1.2 % of the pixels at Playwright's per-pixel threshold, before its
anti-aliasing exclusion, and pass against each other's picture; a sidebar, the cards or the top bar
turned light fail it (8 to 22 % of the pixels, both browsers); card titles turned dark on the dark
cards **pass** — text is too small a share of the page for this comparison. The dashboard's pictures,
made on 2026-10-06 in the same image, pass on macOS as well. The committed pictures are the Linux
renderings, the platform CI runs on; amd64, the runners' architecture, has not been compared for the
dashboard. After a deliberate change, make them again in that image, against the stack of
`make e2e-up`, whose namespace it joins:

```bash
docker run --rm --network container:cowork-e2e-dex -v "$PWD/frontend:/work" -w /work \
  mcr.microsoft.com/playwright:v1.63.0-noble \
  npx playwright test -c e2e visual.spec.ts --update-snapshots=all
```

`--update-snapshots=missing` instead writes only the pictures that are not there, after the changed
page's old ones are deleted, and leaves the others as they were. The image's tag is the version of
`@playwright/test` in `frontend/package.json`, and moves with it. A picture made on macOS
(`--update-snapshots=all` without the container) passes as well, by the measurement above, with the
same margin the other way.

### In CI

The `e2e` job ([ci-and-release.md](ci-and-release.md)) loads the images the container scan built
and scanned, installs the browsers with their system packages, and runs `make e2e` — the same
script, on the job's Docker daemon. Its artefact `e2e-results`, kept for a failed or cancelled run,
holds the traces, which record the run's requests with their cookies and the seed token: they belong
to a stack that is gone when the job ends.

The suite reads `COWORK_BASE_URL`, `COWORK_E2E_ADMIN` and `COWORK_E2E_ADMIN_PASSWORD`, so it can be
pointed at another stack ([ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)
D1 allows it for iteration, never as the gate). There it makes the tenants `e2e` and `e2e-visual`;
**not tried**: against `make dev`, whose login throttle is on.

## Container check

Neither image has a unit test; what proves them is building and running them. CI builds each
`Containerfile` from its directory and scans the image (one `container-malware-scan` leg per
image). Locally, `make docker-build` builds both, and
[build-test-lint.md](build-test-lint.md#run-the-images-together) is the recipe for running them
together read-only behind the Ingress stand-in; `make verify-phase-2` scripts the API half of that
run by hand. The end-to-end tier runs both images behind the stand-in on every push and walks the
UI through it ([above](#end-to-end-tests)); the nginx checks it does not make — the cache headers,
the two body limits, the stream past the stand-in's read timeout, `SIGTERM` with a stream open —
stay manual.

## Chart tests

`helm lint --strict` on the default values and on each `ci/*-values.yaml`; `helm template` on
each `ci/*-values.yaml`, and once more on `ci/inline-url-values.yaml`, whose backend pod template
must carry `cowork/inline-credentials-revision` and no `checksum/` annotation — no hash of an
inline credential ([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
D3; the `helm-template` target in the [`Makefile`](../../Makefile)). The default values name no database URL, no owner URL and no server-key
Secret, and the helpers `fail` on each — `helm lint` reports them as `[INFO]`, `helm template`
fails on the first. That is why the template target runs only with the `ci/` files, each of
which sets all three. The two migration modes each have a file
([ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)
D5): `ci/components-values.yaml` names `onStart` and reads both database roles as components with a
ConfigMap and the storage from a ConfigMap; `ci/migrations-job-values.yaml` renders the Job with the
owner as the components of a Secret alone. What these targets do not show — that a shape fails, and
that a change leaves the manifests of the existing files as they were — is checked by hand: render
each `ci/` file with the chart before and after and compare, once with the new `values.yaml` and
once with the previous release's in its place, as `helm upgrade --reuse-values` has it
([installation.md](../operations/installation.md#upgrade)). `make examples-lint` checks the examples of
`deploy/examples/` — the CloudNativePG manifest against its CRD schema, Silo's chart rendered with
the example's values
([build-test-lint.md](build-test-lint.md#targets)). `ci/metrics-values.yaml` switches every monitoring resource on — the
`PodMonitor` with the nginx exporter's entry, the `ServiceMonitor` with its headless Service, the
`PrometheusRule`, the dashboard's ConfigMap — and renders them with the Prometheus Operator's CRDs
assumed present; nothing applies them to a cluster. `ci/reuse-values-values.yaml` sets `metrics` and
`frontend.metrics` to null — the values of a release upgraded with `--reuse-values` from one before
the metrics have neither — and renders through the defaults the helpers `cowork.metrics` and
`cowork.frontendMetrics` repeat; its manifests equal those of the defaults (compared by hand).

## Environment variables the suites read

| Variable | Read by | Meaning |
|---|---|---|
| `COWORK_TEST_DATABASE_URL` | the integration tier | An administrative URL to PostgreSQL 18 — a role that may create roles and databases and is not held by row-level security; required |
| `COWORK_TEST_DATABASE_TLS_URL`, `COWORK_TEST_DATABASE_TLS_CA` | the integration tier | An administrative URL to a PostgreSQL 18 that serves TLS, without an `sslmode`, and the PEM of the private authority that issued its certificate, for `localhost`; `make postgres-tls-up` starts one and copies the authority out; required |
| `COWORK_TEST_S3_ENDPOINT`, `COWORK_TEST_S3_ACCESS_KEY_ID`, `COWORK_TEST_S3_SECRET_ACCESS_KEY` | the integration tier | The S3 server and its keys; required |
| `COWORK_TEST_OIDC_ISSUER` | the integration tier | The issuer of Dex, `http://localhost:5556/dex` by `make`'s default; required, and its discovery must answer |
| `DEX_PORT`, `DEX_IMAGE`, `DEX_CONTAINER` | `make dex-up`, `make test-integration` | Where and what to start locally; the issuer follows the port |
| `CONTAINER_BIND` | `make postgres-up`, `make postgres-tls-up`, `make minio-up`, `make dex-up` | The address the containers publish their ports on, `127.0.0.1` by default |
| `CI` | vitest through `ng test`; the end-to-end suite | Non-interactive reporter and no watch; in the suite two workers and `test.only` refused |
| `POSTGRES_PORT`, `POSTGRES_IMAGE`, `POSTGRES_CONTAINER` | `make postgres-up` | Where and what to start locally |
| `POSTGRES_TLS_PORT`, `POSTGRES_TLS_CONTAINER`, `POSTGRES_TLS_CA` | `make postgres-tls-up`, `make test-integration` | Where to start the PostgreSQL that serves TLS — the image is `POSTGRES_IMAGE` — and where its authority's certificate is copied, which the tests are given |
| `MINIO_PORT`, `MINIO_IMAGE`, `MINIO_CONTAINER`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY` | `make minio-up`, `make test-integration` | Where and what to start locally, and the keys the tests are given |
| `COWORK_DEV_SEED_DATABASE_URL` | `test/devseed` | The administrative URL `make dev-seed` writes through |
| `E2E_PORT`, `E2E_DEX_PORT`, `E2E_NAME` | `make e2e`, `make e2e-up`, `hack/e2e.sh` | The stand-in's HTTPS port (`18443` `# default`), Dex's (`5557` `# default`), and the prefix of the stack's containers and network (`cowork-e2e` `# default`); a moved port moves the issuer and the redirect URI in the copy of `hack/dex/config.yaml` |
| `E2E_ARGS` | `make e2e` | Arguments of `playwright test`, e.g. `--project=webkit-dark login.spec.ts` |
| `BACKEND_IMG`, `FRONTEND_IMG`, `INGRESS_IMAGE`, `POSTGRES_IMAGE`, `MINIO_IMAGE`, `DEX_IMAGE` | `make e2e`, `make e2e-up` | The images of the stack, the Makefile's |
| `COWORK_BASE_URL`, `COWORK_E2E_ADMIN`, `COWORK_E2E_ADMIN_PASSWORD` | the end-to-end suite | The stand-in's origin (`https://localhost:18443` `# default`) and the local administrator's credentials (`e2e-admin` / `e2e-only-cowork` `# default`); `make e2e` sets all three |
| `COWORK_E2E_TOKEN` | the end-to-end suite | Set by its global setup for the workers; never set by hand |
| `PLAYWRIGHT_INSTALL_FLAGS` | `make e2e-browsers` | `--with-deps` installs the browsers' system packages as well (CI) |
