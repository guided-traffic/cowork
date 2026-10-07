# The API

How `/api/v1` is built: the document that is the contract, what `make generate` makes of it,
the pipeline every request runs before its handler, authentication — a token or a session —,
the CSRF check, the dashboard, the tenant boundary, authorization, errors, idempotency, versions,
paging, filters, the deprecated names a rename keeps for a release, and the media types beside
JSON. The decisions are [ADR 0046] (spec first), [ADR 0047] (errors), [ADR 0045] (idempotency),
[ADR 0048] (paging), [ADR 0049] (filters), [ADR 0050] (versions), [ADR 0028] (expand before
contract), [ADR 0031] (sessions), [ADR 0037] (CSRF), [ADR 0029] (the identity provider's login);
the reference table of routes and codes is [README.md, API](../../README.md#api-backend). Read
against the tree on 2026-10-05, GitHub's webhook ([below](#githubs-webhook)) on 2026-10-06.

## The document

[`backend/api/openapi.yaml`](../../backend/api/openapi.yaml) is the source (OpenAPI 3.1): the
`info`, the default security (`bearerToken` or `sessionCookie`), the tags, and one `$ref` per path
into the file of its path family.

| File | Paths |
|---|---|
| [`meta.yaml`](../../backend/api/meta.yaml) | `/version`, `/openapi.json`, `/schemas/cowork-yaml.json` — `security: []`, read before a client authenticates; the last answers [`cowork-yaml.schema.json`](../../backend/api/cowork-yaml.schema.json) |
| [`auth.yaml`](../../backend/api/auth.yaml) | the browser's login flows, **outside `/api/v1`**: `/auth/options`, `/auth/local`, `/auth/oidc/login`, `/auth/callback`, `/auth/logout` — see [the login flows](#the-login-flows) |
| [`me.yaml`](../../backend/api/me.yaml) | `/me`, `/me/password`, `/me/tokens`, `/me/tokens/{token_id}`, `/me/token` — the token a request presents —, `/me/chat`, and the person-level lists: `/me/next`, `/me/inbox` with `/me/inbox/read` and `/me/inbox/{notification}/read`, `/me/assigned`, `/me/decisions` ([the person-level routes](#the-person-level-routes)) |
| [`search.yaml`](../../backend/api/search.yaml) | `/tenants/{tenant}/search` and `/me/search` ([search.md](search.md)) |
| [`repositories.yaml`](../../backend/api/repositories.yaml) | a project's repositories (list, bind, unbind) and `/me/repositories/lookup` across the person's tenants ([domain.md](domain.md#repositories)) |
| [`tenants.yaml`](../../backend/api/tenants.yaml) | listing every tenant for a global administrator and creating one (`GET`, `POST /tenants`), the tenant, its audit record, projects, archiving, the sort of a project's rank by the score (`…/projects/{project}/rank`), the ticket lists, a ticket, its deletion, its body — read as Markdown and rendered ([rendered-markdown.md](rendered-markdown.md)), and replaced —, its horizon (`…/horizon`) and confidential flag, and the bin of deleted tickets with its restoration and purge |
| [`filters.yaml`](../../backend/api/filters.yaml) | the saved filters of a tenant: list, create, read, edit, delete ([filters](#filters)) |
| [`accounts.yaml`](../../backend/api/accounts.yaml) | the tenant's local accounts: list, create, reset the password, unlock, deactivate, end the sessions |
| [`members.yaml`](../../backend/api/members.yaml) | who belongs where: the members and their grants, the group mappings, a project's restriction and access list, and the tokens that can act in the tenant (`/tenants/{tenant}/tokens`) |
| [`tickets.yaml`](../../backend/api/tickets.yaml) | the key resolver `/tickets/{tenant}/{key}`, links, the prerequisite tree, transitions, the move in the rank, interest, the Markdown export and the context, and the pull requests GitHub's webhook linked (`…/pull-requests`, with a person's removal of one) |
| [`integrations.yaml`](../../backend/api/integrations.yaml) | `/tenants/{tenant}/integrations/github`: whether the tenant takes GitHub's webhook; `…/secret`: making, rotating and revoking its secret; `…/webhook`: the deliveries, public and signed ([GitHub's webhook](#githubs-webhook)) |
| [`questions.yaml`](../../backend/api/questions.yaml), [`comments.yaml`](../../backend/api/comments.yaml), [`time.yaml`](../../backend/api/time.yaml), [`attachments.yaml`](../../backend/api/attachments.yaml) | their entities; `comments.yaml` also the activity list, `attachments.yaml` also the tenant's attachment usage (`/tenants/{tenant}/attachment-usage`) and its consistency check with its two confirmations (`/tenants/{tenant}/attachment-consistency`, [storage.md](storage.md#the-consistency-check)) |
| [`events.yaml`](../../backend/api/events.yaml) | `/tenants/{tenant}/events`, with `me=true` the person-level stream ([events.md](events.md#the-person-level-stream)) |
| [`chat.yaml`](../../backend/api/chat.yaml) | `/tenants/{tenant}/chat`: the chat's availability and a turn of it, with the contract of the turn's stream in prose; `/tenants/{tenant}/chat/turns`: stopping the person's running turns ([chat.md](chat.md)) |
| [`imports.yaml`](../../backend/api/imports.yaml) | a project's import — the dry run (`…/projects/{project}/imports`), its job and report (`…/imports/{import}`), its execution (`…/imports/{import}/execution`) — and the project's and the tenant's export (`…/projects/{project}/export`, `/tenants/{tenant}/export`), with the report, the corrections and the manifests as `Import*` and `Export*` in `components/schemas.yaml` ([import-and-export.md](import-and-export.md)) |
| [`dashboard.yaml`](../../backend/api/dashboard.yaml) | `/tenants/{tenant}/dashboard`: the tenant's dashboard, each tile defined in its field of `components/schemas.yaml#/Dashboard` ([the dashboard](#the-dashboard)) |
| `components/schemas.yaml`, `parameters.yaml`, `responses.yaml`, `headers.yaml` | what the path files share; every operation answers `default` with `responses.yaml#/Problem` |
| `components/problem-codes.yaml` | the `ProblemCode` enum, **generated** from the code catalogue |

`make generate` turns it into code, in this order (the [`Makefile`](../../Makefile)):

1. [`tools/problemdoc`](../../backend/tools/problemdoc/main.go) writes `problem-codes.yaml` and
   the table of codes in the root README between `<!-- problem-codes:start -->` and `…:end -->`.
2. [`tools/specbundle`](../../backend/tools/specbundle/main.go) loads `openapi.yaml` with its
   external references, internalises each under the last segment of its JSON pointer, validates
   the result and writes `api/openapi.gen.json`.
3. oapi-codegen, configured by [`api/oapi-codegen.yaml`](../../backend/api/oapi-codegen.yaml),
   writes [`internal/api/apigen/api.gen.go`](../../backend/internal/api/apigen/api.gen.go): the
   models, the strict server interface on `net/http`'s mux, and the Go client the integration
   tests use. Nullable fields are `nullable.Nullable[T]`; every enum constant carries its type's
   name (`EffortS`); `streamEvents`, `runChatTurn` and `receiveGitHubWebhook` are excluded, and
   `skip-prune` keeps the models only their bodies and events name — the turn's body and the data of
   its events, which `chat.go` reads and writes.
4. `sqlc generate` (the data layer, [data-access.md](data-access.md)).

The generated files are committed and never edited; `make generate-check` fails CI on a diff or
an untracked generated file. [`api/embed.go`](../../backend/api/embed.go) (package `apispec`)
embeds `openapi.gen.json`; `api.New` replaces `info.version` with the backend's version — that
JSON is what `GET /api/v1/openapi.json` serves ([ADR 0046] D5) — and builds the kin-openapi
router over it with `servers` dropped, so the paths match whatever host a request names.

## The pipeline

[`handler.ServeHTTP`](../../backend/internal/api/api.go), behind the request id, the request
log and the panic recovery of [`httpserver`](../../backend/internal/httpserver/server.go):

1. `Cache-Control: no-store` on every answer but the event stream's, which sets `no-cache`.
2. **Route.** The kin router finds the operation in the document. No path: `404 not_found`. The
   path with other methods: `405 method_not_allowed`, `Allow` listing the methods the document
   declares there. The document declares no `HEAD`, so a `HEAD` is `405` too.
3. The `Accept` header is kept in the context for the routes that answer CSV, and the facts of
   the connection the login handlers need — the client's address, the session cookie presented,
   the `User-Agent` — in a `clientFacts` (`withClient`). The client's address is the TCP peer's,
   or, when the peer is inside `Options.TrustedProxies` (`COWORK_TRUSTED_PROXIES`), the first
   address of `X-Forwarded-For`, from the right, that is not one of them
   ([`clientaddr.go`](../../backend/internal/api/clientaddr.go) `clientAddress`; [ADR 0035] D2,
   [the rule](../security/local-accounts.md#the-client-address)): with no trusted network the
   header is never read, and an entry that is no address stops the walk.
4. **Authentication**, when the operation declares `bearerToken` or `sessionCookie` — all but
   the eight public operations (`getVersion`, `getOpenAPI`, `getCoworkYamlSchema`, `getAuthOptions`,
   `loginLocal`, `loginOidc`, `oidcCallback`, `receiveGitHubWebhook`). One resolver for both credentials
   ([Authentication](#authentication)); the `auth.Principal` and the `store.Caller` — with the
   keyed hash of the client's address every audit row of the request carries — go into the context. A public operation that writes and says
   `x-cowork-origin-check: true` — the login — gets the origin half of the CSRF check instead; one
   that says `x-cowork-signed` — GitHub's webhook — gets neither, its credential being the signature
   its handler verifies (`signed` in [`github.go`](../../backend/internal/api/github.go)).
   **For a request authenticated by a session** three more rules run here, before the tenant
   boundary: **the CSRF check** on an unsafe method (`403 csrf`); a session the agent header marks
   is refused an operation that takes a session only (`403 agent_forbidden`); and the gate of a
   temporary password (`403 password_change_required` for everything but `getMe`,
   `changeMyPassword` and `logout`) — `sessionRules` in [`api.go`](../../backend/internal/api/api.go).
5. **Tenant boundary**, when the path has `{tenant}`. The admitted `tenantScope` goes into the
   context. A signed operation meets no boundary, which admits persons: `admitTenant` hands it to
   `webhookTenant`, which reads the tenant by its slug and its sealed secret
   ([GitHub's webhook](#githubs-webhook)).
6. `streamEvents` leaves here: request validation, then `serveEvents` — no timeout, no body
   limit, no generated handler ([events.md](events.md)).
7. **Timeout:** the context gets `COWORK_REQUEST_TIMEOUT` (0 disables), and `bodyDeadline`
   holds reading the body to the same deadline — a read deadline on the connection, lifted once
   the body is read, so a body that trickles in fails instead of holding the request.
8. **Body limit** (`limitBody` in [`validate.go`](../../backend/internal/api/validate.go)): a
   JSON body `COWORK_MAX_JSON_BODY` (0 disables), a multipart upload
   `COWORK_ATTACHMENT_MAX_BYTES` plus 64 KiB of multipart overhead — an import's upload
   (`createImport`) `COWORK_MAX_IMPORT_BYTES` plus the same — (0 disables). A declared length above it is
   `413 payload_too_large` before anything is read; a longer body fails while it is read.
9. **Request validation** against the document (kin-openapi `openapi3filter`): every error is an
   `errors[]` entry of `400 validation_failed`; a query parameter the operation does not declare
   is refused (the validator would let it pass) — except on an operation marked
   `x-cowork-open-query`, the identity provider's callback, to which an issuer may add its own; a
   path parameter that breaks its schema is
   `404`, because it names nothing that can exist; `format: uuid` accepts any UUID version (the
   ids are UUIDv7); defaults are not written into the request — the handlers apply them; a
   multipart body is left to the handler, and so is a signed one, whose handler reads it unparsed
   until its signature holds. The validator sees the route without its security
   requirement (`unsecured`): step 4 has authenticated the caller, and the validator's own
   security check would read the whole body into memory before the handler checks anything.
10. The generated mux dispatches to the strict handler — or, with `Options.ValidateResponses`,
    `serveValidated` holds the response to the document as well ([testing.md](testing.md)); it
    reads the whole body before the handler runs, so a test of the body's timing switches it
    off. `runChatTurn` goes to `serveChat` instead, on the context from before step 7: the
    timeout bounded reading its body, and the turn has limits of its own
    ([chat.md](chat.md#a-turn)). `receiveGitHubWebhook` goes to `serveGitHubWebhook`.

A handler returns a `*problem.Error` or an error; `writeError` answers a problem as it is,
`store.ErrNotFound` as `404`, `store.ErrIdempotencyMismatch` as `422 idempotency_mismatch`, a
passed deadline as `504 timeout`, and anything else as `500 internal` with the details in the log
only. A body the strict server cannot decode is `400 validation_failed`.

## Authentication

[`authn.go`](../../backend/internal/api/authn.go) and [`session.go`](../../backend/internal/api/session.go)
with [`internal/auth`](../../backend/internal/auth/). **Two credentials, one resolver**
([ADR 0031] D6): `credentialsOf` reads from the document which of `bearerToken` and
`sessionCookie` the operation declares — the default is both, written once at the root; the
nineteen session-only operations (`createMyToken`, `createTenant`, `createAccount`,
`resetAccountPassword`, `changeMyPassword`, `logout`, `addMember`, `setMemberGrant`,
`createGroupMapping`, `updateGroupMapping`, `setProjectRestriction`, `setProjectAccess`,
`runChatTurn`, `stopChatTurns`, `setMyChat`, `listTenants`,
`purgeTicket`, `createGitHubSecret`, `removeOrphanedObjects`) declare `sessionCookie` alone, the eight public ones declare nothing — and
`authenticate` decides. What the first twelve make — a token, a tenant, an account, a password only
its setter knows, a role, a mapping, a way into a restricted project — would outlive the revocation
of a leaked token, which is why a token cannot call them, and so would the chat's capabilities
(`setMyChat`), what a purge destroys (`purgeTicket`, [ADR 0024] D7 as amended 2026-10-05), the
tenant's GitHub webhook secret, which writes into the tenant for whoever holds it
(`createGitHubSecret`, [ADR 0071] D1), and the removal of a consistency check's orphaned objects
(`removeOrphanedObjects`, ADR 0035 D5 as amended 2026-10-06); a turn of the chat acts with the person's session and its stop ends the session's
person's turns, and a token's agent has the MCP server; the list of every tenant is a global
administrator's view of the installation's clients, which a token of theirs does not get
([ADR 0033] D1, D5, [ADR 0035] D5, [ADR 0034] D2; the rule is
[tokens.md](../security/tokens.md#what-only-a-session-does)):

- **A request with an `Authorization` header is a token's**, whatever cookie it carries; the
  cookie is not looked at. A valid token on a session-only operation is `403 session_required`
  (an invalid one is the `401` it would be anywhere). Otherwise, where the operation takes a
  session, the cookie decides; neither is `401 unauthenticated`.
- **A token** is read from `Authorization: Bearer` only ([ADR 0035] D7). A value that does not
  match `^cwk_[0-9A-Za-z]{43}$` (`auth.WellFormedToken`) is refused before the database is
  asked; the lookup is by SHA-256 (`auth.HashToken`, `DB.LookupToken`).
  Missing, malformed or unknown: `401 unauthenticated`; revoked, or its person deactivated:
  `401 token_revoked`; expired: `401 token_expired`; its person one of the identity provider's whom
  the gate no longer admits — judged on their stored groups at most every
  `COWORK_OIDC_GROUPS_REFRESH`, and at every request when the person is not the configured
  issuer's or their groups are older than `COWORK_OIDC_GROUPS_MAX_AGE` (`tokenGate` in
  [`identity.go`](../../backend/internal/api/identity.go))
  — `401 not_allowed`. Every `401` carries `WWW-Authenticate: Bearer realm="cowork"`. A dead or
  gated token's use is recorded as an installation-level `refused` act, at most once per token,
  reason and hour ([ADR 0035] D9). The principal carries the token's id and name (`TokenID`,
  `TokenName`); `callerOf` hands both to the `store.Caller`, whose audit rows record them, and a
  handler writes them beside the agent mark on the rows that show an act — a comment and its
  revision, a file, a question asked and its answer, a time entry and its revision — through
  `actToken` in [`tickets.go`](../../backend/internal/api/tickets.go); `tokenMarkView` answers them
  as `token`, `{id, name}`, `null` for a session ([ADR 0036] D6).
- **A session** is the cookie `__Host-cowork-session` (`auth.SessionCookie`): 43 characters of
  base64url, the SHA-256 of which is `sessions.token_hash` (`auth.HashSession`,
  `DB.LookupSession`). Malformed, unknown, ended, past a limit (`sessionLive`: the absolute
  `expires_at`, the idle `last_seen_at` plus `COWORK_SESSION_IDLE`) or its person deactivated:
  the same `401`, with a `Set-Cookie` that clears the cookie. A session of the identity provider
  whose person is not the configured issuer's ends with every session of that person; one whose
  groups are due runs its groups refresh first — the request that claims it waits for the issuer,
  the session's others are served on its groups — and a refresh that ends it is that `401` too
  ([architecture.md](architecture.md#the-groups-refresh-in-the-request-path)). A live session moves
  its idle clock only for the person's activity — a write that passes the CSRF check, or a read
  that carries `X-Cowork-Activity: input` (`api.ActivityHeader`, `api.ActivityInput`), which the
  UI's keep-alive sends after the person's input; no other read, the event stream's included
  ([ADR 0031] D3; `movesIdleClock`) — and then at most once a minute (`DB.TouchSession`,
  bookkeeping outside `Mutate`; a failure is logged).
  The principal has `Session: true`, the cookie's hash in `SessionHash`, the scope `admin` — a
  session has no scope, the role decides — no agent mark but the header's, `GlobalAdmin` and
  `PasswordChangeRequired` from the person. `callerOf` puts the hash into `store.Caller`, which
  is how the session policies find the row; no audit row ever carries it.
- `X-Cowork-Agent: name/model/session` — three parts of 1 to 64 printable ASCII characters
  without a leading or trailing space; a malformed header is `400` on `header:X-Cowork-Agent`,
  never ignored. `auth.Mark` decides the agent mark: a token with the agent flag is an agent's
  with or without the header (recorded as the header or `unknown-agent`) and holds the token's
  capabilities; a plain token with the header is an agent's holding every capability; a plain
  token without it is the person ([ADR 0036], [ADR 0043] D4). A session is read the same way as a
  plain token: with the header its request is an agent's holding the capabilities its person chose
  for the chat, or `auth.DefaultChatCapabilities` ([ADR 0043] D5; `chatCapabilities` in
  [`session.go`](../../backend/internal/api/session.go), one read of `chat_capabilities` per such
  request), which `sessionRules` then refuses what only a session does — the chat in the UI marks its tool calls so,
  `chat/<model>/<conversation>` ([chat.md](chat.md#the-loopback)); without it the session is the
  person.
- The token's `last_used_on` is written at most once per UTC day (a process-local note, then
  the column), outside `Mutate`, and a failure never fails the request.

### The CSRF check

`csrf` in [`session.go`](../../backend/internal/api/session.go) ([ADR 0037] D1): on every
method but `GET`, `HEAD` and `OPTIONS` of a session-authenticated request, the `Origin` header —
or without one the origin of the `Referer` — must equal `Options.BaseOrigin`, the normalised
`COWORK_BASE_URL` (`config.Origin`), and `X-Requested-With` must be `cowork`, else `403 csrf`.
An empty `BaseOrigin` refuses every such write: the check fails closed. `checkOrigin` is the
first half alone, for the login. A token's request is never checked.

## The login flows

`/auth/options`, `/auth/local`, `/auth/oidc/login`, `/auth/callback` and `/auth/logout` are in the
API document, in [`auth.yaml`](../../backend/api/auth.yaml), so the pipeline validates their bodies
and parameters, the generated Go and Angular clients know them and the document says which
credential each takes — **with paths outside `/api/v1`**, as [ADR 0037] D5 names them. `httpserver.New` mounts the API handler
at `/auth/` as well as `/api/`; the Ingress routes `/auth/` to the backend like `/api/`, and the
dev proxy forwards it the same way. They are browser flows, but they are no secret: the served document lists them, and a
script that wants a session can read how.

[`login.go`](../../backend/internal/api/login.go): `LoginLocal` runs `throttled` (the limit per
client address), `NormaliseUsername`, `DB.LookupLogin`, **one** `passwordFits` — against the stored hash or
the dummy — and `DB.RecordLoginAttempt` (the lock and the counting in one transaction under the
username's advisory lock), then `DB.CreateSession` for a success; every failure is the same
`invalid_credentials`. `Logout` deletes the session's row, and for a session of the identity
provider whose issuer names an end-session endpoint answers `200` with its URL instead of `204`.
The store's side is [data-access.md](data-access.md#the-login-and-the-sessions); the security
design is [docs/security/local-accounts.md](../security/local-accounts.md),
[sessions.md](../security/sessions.md) and [csrf.md](../security/csrf.md).

[`oidc.go`](../../backend/internal/api/oidc.go): `LoginOidc` and `OidcCallback` are browser
navigations — the login page sets `window.location` — that answer redirects, never JSON: the start
`302` to the issuer, the callback `303` to the path the login began with or to
`/login?error=<code>`. The start's `silent=true` — the login page's own attempt after a session
ended ([ADR 0029] D6) — adds `prompt=none` to the authorization request (`oidc.Provider.AuthCodeURL`)
and `Silent` to the sealed `loginState`; `callbackRefusal` turns the issuer's `error` to such a login
into `login_required` when the cookie opens, and every other refusal into `oidc_failed`. The callback's answer sets two cookies, the session's and the cleared state
cookie, which the generated response type, with one `Set-Cookie`, cannot carry: `redirect`
implements the generated `VisitOidcCallbackResponse` itself. Its failures are redirects too, so
`OidcCallback` returns no `problem.Error` for them; the reason goes to the log. The relying party
itself is [`internal/oidc`](../../backend/internal/oidc/oidc.go), the decision
[`store.CompleteOIDCLogin`](../../backend/internal/store/identity.go)
([architecture.md](architecture.md#the-two-logins),
[identity-provider.md](../security/identity-provider.md)).

## The person-level routes

The routes under `/api/v1/me/` that list what spans tenants — the inbox, "next for me", the tickets
assigned to the person, the open decisions, the search ([ADR 0023] D2) — name no tenant in their
path, so no boundary admits them to one. `personTenants` in [`inbox.go`](../../backend/internal/api/inbox.go) reads the person's
memberships in an `Installation` transaction, keeps a token restricted to a tenant to that tenant
(`restricted`), narrows to the `tenant` query parameter — a slug that names none of the person's is
the boundary's `404 not_found`, whether or not it exists — and sorts them by slug. Each tenant is then
read in a transaction of its own (`InTenant`, [ADR 0021] D5), under the visibility predicates as the
tenant's own routes read it — a project-restricted token's `app.restricted_project_id` makes every
project of another tenant invisible — and the parts are merged in Go
([`inbox.go`](../../backend/internal/api/inbox.go), [`mylists.go`](../../backend/internal/api/mylists.go),
[`search.go`](../../backend/internal/api/search.go)).
"Next for me" (`ListMyNext`) and "assigned to me" (`ListMyAssigned`) share `listMine`: each tenant's
part is the list builder's `ByScore` page after the cursor, with the assignee filter — the person or
nobody for "next for me", the person for "assigned to me" — and the places of its tickets in their
project's rank (`ListRankPlaces`) in the same transaction; the parts are merged in the score's order
and cut to the page, which answers its weak `ETag` and `304` as the other polled lists do. `GET
/api/v1/me/next` also takes `project`, a project key within the tenant `tenant` names — without
`tenant` it is `400 validation_failed` at `query:project`.
A global administrator without a role in a tenant holds no membership there, and these lists leave it
out. Marking read needs `markRead` — any member, `write` scope, the agent baseline; the reads need no
authorization beyond the person's membership, as `GET /api/v1/me` does. One notification is found by
reading each tenant in turn (`FindNotification`); no query reads two tenants.

## The dashboard

`GET /api/v1/tenants/{tenant}/dashboard` ([`dashboard.go`](../../backend/internal/api/dashboard.go))
answers the nine tiles of [ADR 0018] D6, and beside them the open tickets updated last, in **one
route**: the page shows the tiles together under one set of filters, one read-only transaction
(`InTenant`) gives counts that agree with each other, and a reload after an event costs one
request, which a weak `ETag` answers with `304` while nothing the caller sees changed. Nine routes
would have cost nine requests per reload and nine snapshots that could disagree. **Each tile's
definition is its field's description in the document**, `components/schemas.yaml#/Dashboard` —
the one place a reader checks a number against — and the code follows it:

- `parseDashboardQuery` reads `project` as the ticket lists do ([ADR 0049] D6): repeatable, a `!`
  leaving a project out, a value that is no key `400` at `query:project`; the plain values become
  `projects`, the negated `without`, both empty arrays rather than `nil`, because the queries read
  an empty array as no filter and a `NULL` would match nothing. Without a plain value every project
  that is not archived counts; a key that names no project the caller sees counts nothing, and
  answers exactly like one that names no project at all.
- The period is `from` and `to`, UTC days, both inclusive, by default the thirty days that end
  today by `Options.Now` (the tests fix the clock); `from` after `to` is `400` at `query:from`. It
  bounds the time booked; throughput's eight ISO weeks end with the week of `to`, cut at `to`, and
  lead time's thirty days end with `to` ([ADR 0019] D2); the open tiles and the ages stand as the
  tickets do at the request. A week is named `2026-W40` by Go's `ISOWeek`, the Monday the query's
  `date_trunc('week', …)` gives.
- `dashboardRows.read` runs the ten queries of
  [`queries/read/dashboard.sql`](../../backend/internal/store/queries/read/dashboard.sql) in the
  transaction ([data-access.md](data-access.md#visibility-in-sql)); one small function per tile
  (`stateCounts`, `severityCounts`, `securityTile`, `blockedTile`, `ageTile`, `throughputTile`,
  `leadTimeTile`, `decisionsTile`, `timeTile`, `recentList`) turns its rows into the answer — the
  severities, the two classes, the five age buckets and the eight weeks always present, zero
  included, the rows of state and time only where they count something.

It takes `read`, any member and an agent; a global administrator without a role and a token
restricted to a project are refused by the boundary, the dashboard being neither of `oversight`
nor of `tenantWideForProjectTokens`. A deleted ticket counts in no tile, nor its questions and its
time, until it is restored ([ADR 0024] D1, [data-access.md](data-access.md#the-dashboards-queries)).
The time entries are not published to the event stream ([events.md](events.md#publication)), so
the browser's dashboard shows a booking made elsewhere at its next reload
([frontend.md](frontend.md#the-dashboard)) — settled so on the recommendation ([ADR 0018] D6).

## The tenant boundary

`boundary` in [`tenant.go`](../../backend/internal/api/tenant.go) admits a request to the tenant
in its path before any handler runs ([ADR 0023] D5). It reads the tenant by slug together with
the person's highest role there (`GetTenantForPerson`, an `Installation` read). Refused — all
with the same `404 not_found` "no such tenant", so the answer does not tell whether the tenant
exists ([ADR 0047] D5):

- an unknown slug, or a person without a membership;
- a token restricted to another tenant;
- a token restricted to a project, on a path without `{project}` — except `listProjects`,
  `listTenantTickets`, `searchTenant`, `resolveTicket` and `streamEvents` (`tenantWideForProjectTokens`), which
  the data layer narrows to the token's project through `app.restricted_project_id`.

**A global administrator without a role** ([ADR 0034] D2) is the one exception to the first rule:
where `GetTenantForPerson` finds no membership, `overseen` admits the request when `oversees` holds —
a global administrator, a session, no agent mark, and an operation of `oversight`: `getTenant`,
`listMembers`, `listGroupMappings`, `setMemberGrant` — and reads the tenant by slug
(`GetTenantBySlug`, which the `tenants` policy shows a global administrator since migration 26). The
`tenantScope` it hands on has no `Role` and `Oversight` set. The three reads authorize through
`administrationRead`, which takes the mark for the role; `SetMemberGrant` sends a grant to the
person themselves to `grantSelf` when `ownGrant` holds — a global administrator in a session no agent
marks who does not hold `admin`, with a role in the tenant or without — and anybody else's to
`auth.Authorize`, which a scope without a role fails (`403 forbidden`). Every other operation is refused like an unknown slug, before any handler;
a token, an agent-marked session and the event stream's heartbeat get no such admission.

Inside the tenant, `visibleProject` and `visibleTicket` read through the visibility predicates
([data-access.md](data-access.md#visibility-in-sql)): a restricted project or a confidential
ticket the caller cannot see is the same `404` as one that does not exist. `projectRole` lowers
the person's role on a restricted project to the role of their entry on its list; tenant
administrators keep theirs.

## Authorization

Every handler under a tenant calls `auth.Authorize(principal, role, need)`
([`authorize.go`](../../backend/internal/auth/authorize.go)) with the tenant role or the project
role it read; the `/me` routes act on the person's own rows and need none, except revoking
another token and marking notifications read (`write` scope,
[the person-level routes](#the-person-level-routes)). The checks run in this order; the first failure answers:

1. `Need.Role` above the person's role: `403 forbidden` ("the act needs the member role").
2. `Need.Scope` above the token's scope: `403 insufficient_scope`.
3. For an agent's request only: `Need.HardOff` set — `403 agent_forbidden`, detail
   `hard-off: <rule>`; `Need.Capability` not held — `403 agent_forbidden`, detail
   `missing capability: <name>` ([ADR 0043] D3–D5). Beyond `Authorize`, `mayClose` in
   [`transitions.go`](../../backend/internal/api/transitions.go) refuses an agent's done act —
   by hand, or by the `PATCH` that fills the last progress stage — from any state but
   `in-progress` and `review` with `403 agent_forbidden`, detail `close covers in-progress and
   review: …` (ADR 0043 D4), and `mayAssign` in [`tickets.go`](../../backend/internal/api/tickets.go)
   hands `Authorize` the hard-off rule `assigning a confidential ticket to anyone but the agent's
   person` for a filing or a `PATCH` that leaves a ticket confidential with an assignee who is
   neither the caller's person nor the one it had (ADR 0043 D3, [ADR 0065] D9).

| Need | Role, scope | Agent rule | Defined in |
|---|---|---|---|
| `read` | viewer, `read` | — | [`tenants.go`](../../backend/internal/api/tenants.go) |
| `administer` | admin, `admin` | hard-off `administration` | `tenants.go` |
| `adminRead` | admin, `read` | — | [`members.go`](../../backend/internal/api/members.go): the group mappings, a project's access list, and the bin of deleted tickets; the tokens that can act in the tenant ([`tenanttokens.go`](../../backend/internal/api/tenanttokens.go)); the tenant's attachment usage ([`attachments.go`](../../backend/internal/api/attachments.go)) and its consistency check ([`consistency.go`](../../backend/internal/api/consistency.go)) |
| `importRead` | admin, `read` | hard-off `administration` | [`imports.go`](../../backend/internal/api/imports.go): an import job and its report |
| `deletion` | admin, `admin` | hard-off `deleting, restoring or purging` | [`deletion.go`](../../backend/internal/api/deletion.go): deleting a ticket, restoring it, purging it ([ADR 0024] D7) — the tenant role, not a project's; the purge takes a session besides, which the document declares |
| `orphanRemoval` | admin, `admin` | hard-off `deleting, restoring or purging` | [`consistency.go`](../../backend/internal/api/consistency.go): removing the orphaned objects of a consistency check ([ADR 0059] D4); the document takes a session besides. Its acceptance of the missing files takes `administer` |
| `filterNeed` | viewer, `write` | baseline ([ADR 0043] D2) | [`filters.go`](../../backend/internal/api/filters.go): saving, changing, sharing and unsharing the person's own saved filter; another's shared one is `403 forbidden`, but to a tenant administrator, who unshares it with `administer` (`mayChangeFilter`) |
| `filterDeletion` | viewer, `write` | hard-off `deleting, restoring or purging` ([ADR 0043] D3) | `filters.go`: deleting the person's own saved filter, or — a tenant administrator, with `administer` besides (`mayChangeFilter`) — another person's shared one; an agent is refused before the filter is read |
| `work` | member, `write` | baseline; a transition adds `decide`, `close` or `drop`, the done act of the stages `close`, a horizon set `set-horizon` and of an agent a reason — on `setHorizon` for `later` too —, a filing into a horizon other than `later` `set-horizon` and with a place `rank`, an agent's answer `record-answer`; a confidential ticket's new assignee other than the agent's person is hard-off (`mayAssign`) | [`tickets.go`](../../backend/internal/api/tickets.go) |
| `edit` | member, `write` | — | [`projects.go`](../../backend/internal/api/projects.go) |
| `rankNeed` | member, `write` | `rank` | [`rank.go`](../../backend/internal/api/rank.go): a move; the sort by the score in [`score.go`](../../backend/internal/api/score.go) |
| `booking` | member, `write` | hard-off `booking time` | [`time.go`](../../backend/internal/api/time.go) |
| `uploadNeed` | member, `write` | `upload` | [`attachments.go`](../../backend/internal/api/attachments.go) |
| `interestNeed(weight)` | `watch`: viewer, `write`; `need`, `urgent`: member, `write` | `interest` for `need` and `urgent` | [`interest.go`](../../backend/internal/api/interest.go) |

An import's dry run and its execution use `administer`; the project export reads `read` on the
project's role, the tenant export `read` on the tenant's ([import-and-export.md](import-and-export.md)).
The handlers also build a few needs inline: `creating` for `createProject`, `bindRepository`
and `unbindRepository` (admin, or member while the tenant allows it; `write`; `create-project`
— [`repositories.go`](../../backend/internal/api/repositories.go), judged by the project role for
a binding), `setConfidential` (admin, `admin`, hard-off), the
done act's `close` and its prerequisite override (member, `write`; `close`, and hard-off for the
override — `mayClose`), `listAudit` (admin, `read`),
withdrawing another person's comment (admin, `admin`), and revoking another token of the person
(`write`, hard-off). The tenant's GitHub webhook ([`integrations.go`](../../backend/internal/api/integrations.go))
is read with `adminRead` and its secret made, rotated and revoked with `administer` — the making and
rotating in a session besides, which the document declares —, and a person removes a link of a pull
request with `work` by the project's role, as a ticket's link
([`pullrequests.go`](../../backend/internal/api/pullrequests.go)). The account routes of [`accounts.go`](../../backend/internal/api/accounts.go),
the writes of [`members.go`](../../backend/internal/api/members.go) and the revocation of a member's
token (`RevokeTenantToken`) use `administer`, the member list `read`; a change of a grant or a mapping, or the deactivation of an account
(`DeactivateAccount`), that would leave the tenant without an administrator who can log in is
`409 last_admin` (`lastAdmin`, checked in the transaction after the change, which took the
tenant's lock first);
creating a tenant (`CreateTenant`) and listing every tenant (`ListTenants`) need
`Principal.GlobalAdmin` and a session, which the pipeline has already settled; a global
administrator's grant to themselves where they do not hold `admin` (`grantSelf`, `setOwnGrant`) takes
the tenant's lock and meets no `lastAdmin`, since it takes no administrator away — unless it lowers a
grant of `admin` another administrator gave them meanwhile; making a group mapping or changing its role (`CreateGroupMapping`,
`UpdateGroupMapping`) needs `Principal.GlobalAdmin` after `administer`, else `403 forbidden` before
an idempotency key is kept or a row is written (`mapsGroups`, [ADR 0030] D7). A session passes every
scope check: its scope is `admin`. Rules about *whose* entity it is —
the asker, the author, the person asked — are checked after `Authorize`, in the handler.

## Problem details

[`internal/problem`](../../backend/internal/problem/problem.go) is the one place a code is
defined: `Code{Code, Status, Title, Meaning}`, listed in `Catalogue` in the order of the README
table ([ADR 0047] D4). A handler returns `problem.New(code, detail)`, `problem.Field(pointer,
message)` for one field, or a `&problem.Error{…}` with `Errors` and `Headers`. `problem.Write`
renders `application/problem+json; charset=utf-8` with `type`
(`https://cowork.dev/problems/<code-with-hyphens>`), `title`, `status`, `detail`, `instance`
(the path), `code`, `request_id` and `errors[]`. A field pointer is a JSON pointer into the body,
or `query:<name>`, `header:<name>`, `path:<name>`, and on `409 import_conflict` `file:<path>`, a
file of the upload; on a `412` an entry carries `current`. The
`detail` never carries a secret, SQL or an internal path; the cause goes to the log under the
request id.

## Idempotency

A creating `POST` — `createProject`, `bindRepository`, `createTicket`, `askQuestion`, `addComment`,
`bookTime`, `uploadAttachment`, `addMember`, `createGroupMapping`, `createSavedFilter` — calls
`keyed(ctx, key, op, scope, body)` in
[`server.go`](../../backend/internal/api/server.go):

- No `Idempotency-Key`: a person's request goes on unkeyed; an agent's is
  `400 idempotency_key_required` ([ADR 0045] D3). The key of a session, which has no token, is
  scoped to the person (`token_id` is `NULL`; migration 16); `createMyToken`, `createTenant` and
  `createAccount` take keys too, and `createMyToken` stores its answer **without the plaintext**,
  so a replay answers without `token`.
- With a key: the fingerprint is an HMAC-SHA-256 under a key derived from the server key
  (`Server.fingerprint`, `newFingerprintKey`) over the operation, the scope (the path's identities)
  and the JSON body — a body can carry a temporary password, and the row must be no plain hash of
  it; a key replayed after the server key changed meets `422 idempotency_mismatch` — for an upload, the file's SHA-256, name and comment instead of its bytes.
  `store.WithIdempotency` puts both into the context; inside `Mutate` the handler builds its
  `201` with `res, err := stored(view, headers)` and hands it over with `w.Respond(res)`, and a
  replay comes back as `*store.Result`, decoded
  with `replayed[T]` and `header`. A required nullable field the stored answer does not carry — one
  added after the release that stored it, such as an act's `token` — is answered as `null`
  (`nullUnstored`), never as its zero value; an optional one stays out, as it was stored
  (`TestAReplayAnswersARequiredFieldTheStoredAnswerLacksAsNull`). The same key with another request is
  `422 idempotency_mismatch`. A key is scoped to its token and kept twenty-four hours. The keys
  come from the client: the browser's creating forms hold one per content
  ([frontend.md](frontend.md#where-state-lives)), `cowork-mcp` draws a UUIDv7 per `POST`, and the chat in the UI derives them
  from the conversation and the call, so the same call sent again replays ([chat.md](chat.md#the-loopback)).

`createGitHubSecret` is a creating `POST` that takes no key on purpose: its answer is the one
sight of a secret, which a stored response must never hold (D6), and a repetition after a lost answer
makes a new secret, which is the one to give GitHub. GitHub's webhook takes none either: a delivery
is held to its delivery's id instead ([GitHub's webhook](#githubs-webhook)).

`PUT` and `DELETE` routes are idempotent by their address and take no key ([ADR 0045] D1). A
transition carries its `from` state instead; a key sent with it is recorded on the act, not
stored ([ADR 0045] D2, D7).

## Versions, ETag, If-Match

A mutable entity carries a `version`; its strong `ETag` is `"<version>"` (`etag`), on reads and
on write answers ([ADR 0050]). `ifMatch` reads the version an overwriting write was based on:
missing, empty or `*` is `428 precondition_required`; a weak or unreadable tag is
`412 precondition_failed`. The write is a compare-and-set in SQL (`WHERE … AND version = $n
RETURNING version`); a moved version is `stale(version, current)`: `412` with the current `ETag`
header and, per field the request tried to change, its current value in `errors[].current`,
`null` for an empty field ([ADR 0050] D5). A write that changes nothing answers as one that did, with the current state,
and records no act. A `PATCH` whose progress stages close or reopen the ticket raises the version
once: the state is written with `bump` false after the fields.

`If-Match` is required by `updateTenant`, `updateProject`, `updateTicket`,
`replaceTicketBody`, `setHorizon`, `setConfidential`,
`updateQuestion`, `answerQuestion` (changing an answer given), `editComment`,
`editTimeEntry`, `updateGroupMapping`, `updateSavedFilter` and `setProjectRestriction` (the project's version). A deletion, a restoration and a purge of a ticket take none: they overwrite no field, and the deletion and the restoration raise the version. A grant
and an access entry are addressed by their person and written without it, like a link. Links, interest and attachments are written without it and carry no version
([ADR 0050] D4); a move in the rank (`moveTicketRank`) is written without it — it names where
the ticket goes, so the last move wins — and raises the ticket's version.

The two ticket lists, and every list the UI loads again on a poll — `listProjects`,
`listMembers`, `listGroupMappings`, `listProjectAccess`, `listComments`, `listActivity`,
`listQuestions`, `listTicketLinks`, `listInterest`, `listAttachments`, `listTicketTime`,
`listPrerequisites`, `listMyInbox`, `listMyNext`, `listMyAssigned`, `listMyDecisions`,
`listDeletedTickets`, `listSavedFilters`, `listTenantTokens`, `listTicketPullRequests` — and the dashboard, `getDashboard`, and
the attachments' usage, `getAttachmentUsage`, answer a
weak `ETag` — `W/"…"`, 24 hex characters of the SHA-256 of the page as the caller reads it — and
`304` without a body for a matching `If-None-Match` (`weakETag`, `notModified` and `listTag` in
`tickets.go`; the document's `ListETag` header and `NotModified` response; [ADR 0054] D7). The tag
is the caller's: two callers who read the same list differently — an administrator the members'
addresses, a member not — get two tags. An
attachment's content answers its quoted hex SHA-256 and `304` likewise.

## Paging

[`cursor.go`](../../backend/internal/api/cursor.go): a cursor is
`base64url(payload).base64url(HMAC-SHA256)` with a key derived from `COWORK_SESSION_KEY` by HKDF
under the label `cowork cursor v1`. The payload binds the position to the operation and the scope
(the path's identities and, where it matters, the order); an altered cursor, or one from another
list or scope, is `400 invalid_cursor` ([ADR 0048] D5). Rotating the server key invalidates the
cursors clients hold.

A project's list seals its position, because a rank key is computed over tickets the caller
may not see ([ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D2):
`sealPosition` encrypts it with AES-256-GCM under a key derived under
`cowork cursor position v1`, padded to 144 bytes, with an HMAC of the padded position under a
key derived under `cowork cursor nonce v1` as the nonce — deterministic, so a page and its weak
`ETag` stay the same while the list does. `openPosition` refuses what it did not seal, and the
list answers that `invalid_cursor`.

- `limit` defaults to 50 and is clamped, not refused, at `COWORK_MAX_PAGE_SIZE`; the query
  fetches one row more than the page, which says whether `next_cursor` is set.
- The tables — `listProjectTickets`, `listTenantTickets`, `listTenantTime`, `listAudit`,
  `listMembers`, `listMyTokens`, `listTenantTokens` and `listProjects` — also take numbered pages: `page` with
  `per_page` (25, 50 or 100; 50 when absent, clamped like `limit`), answered with `total`, `page`
  and `per_page` and a `null` `next_cursor`; the query takes `LIMIT`/`OFFSET` and a count query
  beside it gives the total under the same filters and predicates. `page × per_page` above 10 000
  is `400 page_too_deep`; a numbered page with `cursor` or `limit`, or `per_page` without `page`,
  is `400 validation_failed` ([ADR 0048] D2). `tablePage` in [`cursor.go`](../../backend/internal/api/cursor.go)
  reads both modes for every table but the two ticket lists, whose `paging` in
  [`ticketlist.go`](../../backend/internal/api/ticketlist.go) also seals the rank's cursor.
- The sort is fixed per list ([ADR 0048] D6): a project's tickets by rank, the unranked after
  them by number — the position is `<key>.<number>` (`TicketOrder.Position`), sealed, and
  `ticketListScope` adds `/rank` to the scope, so a cursor of the number order before the rank
  is `invalid_cursor`; the tenant's tickets and time entries, the audit record and the person's
  tokens newest first; comments and activity oldest first unless `order=desc`; projects by key;
  questions by number; members, interest and a project's access list by person id; the group
  mappings by group; the installation's tenants by slug; the tenant's tokens newest first; a tenant's bin the last deleted first, its
  position the deletion's time and the id; a tenant's saved filters by id; the person's inbox newest
  first, merged across the tenants by the notifications' ids, which order by time; "next for me" and
  the tickets assigned to the person by the score, highest first, then the ticket's id, and the open
  decisions by the score of their ticket — a done or dropped ticket's after the others — then the
  ticket's id and the question's number ([ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md)
  D5); a search's hits by their rank, then the ticket's id, both descending — a search's position is
  `<rank>/<id>`, bound to a hash of its query as well ([search.md](search.md#the-cursor)); the other
  lists by id.
- A person-level list's cursor is bound to the person and the `tenant` it was narrowed to — "next
  for me"'s to its `project` as well. Its position is the notification's id, or for the lists in the
  score's order `<score key>/<ticket id>` (`store.ScorePosition`), for the open decisions
  `/<question>` after it (`decisionPosition` in [`mylists.go`](../../backend/internal/api/mylists.go)):
  the stored key, written so that it reads back to the same `float64` (`-Inf` for a ticket without a
  score), and the id, unique across tenants, so every tenant's part resumes at the same place of one
  order. A score is shown on the ticket, so the position is not sealed. Every tenant is read for a
  page after the position, and the parts are merged. The `cursor` parameter takes up to 1024
  characters.

## Filters

`parseTicketQuery` in [`ticketlist.go`](../../backend/internal/api/ticketlist.go) turns the
query of the two ticket lists into a `store.TicketFilter` ([ADR 0049]). A repeated parameter
combines with OR and `!` negates a value. Vocabulary values are checked against the generated
enums (`apigen.TicketState(v).Valid()` …); `assignee` and `reporter` take a person id or `me`,
`assignee` also `none`; `parent` takes a ticket key or `none`, resolved under the predicate —
a key the caller cannot see matches nothing; `interest` takes `me` or `any`; `blocked`,
`has_open_questions` and `include_terminal` are booleans; `q` is capped at
`COWORK_MAX_QUERY_LENGTH` characters. Every refused value is named in `errors[]`. `q` is a filter —
the title and body hold every word, the list keeps its order —; the ranked search with snippets over
comments, questions, file names and keys as well is the search routes' ([search.md](search.md#the-q-filter-and-the-mcp-tool)).
The checks of the filters themselves are `parseFilters`, which the saved filters share.

**Saved filters** ([`filters.go`](../../backend/internal/api/filters.go), [ADR 0018] D5, [ADR 0049]
D6, D7) store a filter's parameters as the JSON object `SavedFilterParameters` — the query's names,
each repeatable one an array —, whose schema refuses an unknown name (`additionalProperties:
false`, D4). Written, the parameters go through `parseFilters` and every refused value is `400` at
`/parameters/<name>` (`checkFilter`); `me` is stored as `me`. Read, they go through it again
(`filterView`): a value that no longer validates is a `warnings` entry, not an error (D7), and a
project or a parent ticket the reader cannot see — or that is gone — is one more for the owner;
another reader gets the filter `redacted`, its parameters and warnings withheld, as the activity
withholds an act that names a hidden ticket ([ADR 0065] D5). A filter is the owner's to change, and
another person's shared one a tenant administrator's to unshare or delete ([ADR 0018] D5 as amended
2026-10-06; `mayChangeFilter`): `administer` — `admin` scope, hard-off `administration` —, a patch of
`{"shared": false}` and nothing else, else `403 forbidden`; the unshare is a compare-and-set on the
version that sets `shared` alone, through `Writer.UnshareAnothersFilter`, and answers the filter the
administrator no longer reads; the deletion takes a shared filter only, one unshared meanwhile is
`404`. Each is recorded as the owner's acts are, under the administrator's name. An agent saves,
changes, shares and unshares its person's filter at the baseline and deletes none: `DeleteSavedFilter`
authorizes `filterDeletion`, the hard-off rule of a ticket's deletion, before it reads the filter
([ADR 0043] D2, D3 as amended 2026-10-06). The policies of
migrations 33 and 39 hold the same in the data layer
([data-access.md](data-access.md#the-settings-the-policies-read)). Saved filters are not published
on the event stream; their list answers a weak `ETag` and `304` like the other lists the UI loads
again on a poll ([above](#versions-etag-if-match)).

## GitHub's webhook

`POST /api/v1/tenants/{tenant}/integrations/github/webhook` ([ADR 0071]) is public and signed: the
document declares `security: []` and `x-cowork-signed: github`, and the pipeline takes it past the
three steps that serve persons — authentication, the origin check and the tenant boundary — and past
the body's validation. The handler, `serveGitHubWebhook` in
[`github.go`](../../backend/internal/api/github.go), and the code before it go in the order D3 makes
concrete:

1. **The tenant and its secret** (`webhookTenant`, from `admitTenant` in place of the boundary): the
   request becomes the system actor `system:github` (`store.Caller` with the request id and the
   source hash, no person), `DB.WebhookSecret` reads the tenant by slug and its sealed secret in a
   read-only transaction of the job `github-webhook`, and the handler's `webhookSealer` opens it with
   the tenant's id as the binding. An unknown tenant, a tenant without a secret and a secret that
   does not open — the server key changed — are the boundary's `404 not_found`, "no such tenant".
2. **The body**, bounded by `limitBody` and `bodyDeadline` as any JSON body, read whole (`413`).
3. **The signature**: `github.Verify`, `hmac.Equal` over the raw bytes; missing or wrong is
   `401 signature_invalid` with `WWW-Authenticate: X-Hub-Signature-256 realm="cowork"`, before
   anything is parsed or written.
4. **The delivery** (`readDelivery`): `X-GitHub-Delivery` must be a UUID (`400` at
   `header:X-GitHub-Delivery`), the type `application/json` (`415`), and the payload of
   `pull_request` or `push` what GitHub sends (`400`); every other event is a delivery with nothing
   to read.
5. **Taken once** (`DB.ReceiveDelivery`, [data-access.md](data-access.md#githubs-deliveries)): the
   delivery's id recorded for a day — no row when the tenant took it already, and then `200` with no
   body —, then `applyDelivery` ([`github_links.go`](../../backend/internal/api/github_links.go)),
   then `202` with no body, whatever it linked.

`applyDelivery` reads a `pull_request` of the actions `opened`, `edited`, `synchronize`, `reopened`
and `closed`, and a `push` whose `ref` is the default branch's; `boundIdentity` normalises
`repository.clone_url` and asks `RepositoryBoundInTenant`, any project and sub-directory; the keys of
`internal/github` resolve through `ResolveTicketKeys` (`resolveKeys`, `targetsOf`: a key of another
tenant, of no ticket or of a deleted one passed over, each ticket once by the first place its key
was read). A pull request is then linked to each ticket it names (`InsertTicketPullRequest`, an act
`linked`) and its facts written on every ticket it is linked to (`UpdatePullRequestFacts`, which
answers each row's state before: a state change is an act `merged`, `closed` or `reopened`, a change
of the title, page or author `updated`); a push links each commit to the tickets its message names
(`InsertTicketCommit`). The page of either is written from the identity (`pullRequestPage`,
`commitPage`), never taken from the payload. A merge's act carries `mergeNotices`, the watchers by
the reason `merged`. The rules themselves are [domain.md](domain.md#pull-requests-and-githubs-webhook).

The secret's routes are [`integrations.go`](../../backend/internal/api/integrations.go):
`GetGitHubIntegration` (`adminRead`) answers the secret's time and maker, never the secret, the
webhook's path and the events to choose; `CreateGitHubSecret` (`administer`, a session) draws
`newWebhookSecret` — 32 bytes of `crypto/rand` as hex —, seals it, upserts it
(`SetGitHubWebhookSecret`) and records `created` with `replaced`; `RevokeGitHubSecret`
(`administer`) deletes it, `204` also when there is none. A ticket's list and a person's removal are
[`pullrequests.go`](../../backend/internal/api/pullrequests.go): `ListTicketPullRequests` reads
through the ticket's predicate, pages by id and answers a weak `ETag`; `RemoveTicketPullRequest`
marks the link removed — `204` also when it is gone — and records `unlinked` on the link's kind.

## Deprecated names

`/api/v1` keeps what the clients of the release before read ([ADR 0046] D7), so a rename is an
expand and a later contract ([ADR 0028] D3):

- **The expand** keeps the old names in the document with `deprecated: true` and a description that
  names what replaces them. A property that is a `$ref` carries `deprecated` through an `allOf` of
  one: the bundler writes a `$ref` alone and would drop the keyword beside it. The generated Go
  carries `Deprecated:`, which staticcheck reports wherever the code uses them; each use is a
  function or a line with `//nolint:staticcheck` and why, and a test that proves the old surface
  still works says so the same way. On the way in an old name is taken as the new one; on the way
  out an answer names both.
- **The contract**, in a release after the expand and once no supported client reads the old names,
  takes them out of the document, the code and the tests, and a migration of its own rewrites what
  the database stored under them. Where the release before still writes an old name into a column
  whose check takes it, the check keeps the name until the first release whose release before no
  longer writes it; that release's migration rewrites again what an image rollback wrote in between
  and then narrows the check ([ADR 0028] D3).

The horizon went through both ([ADR 0010] D1 as amended 2026-10-05 and 2026-10-06). Release 0.5
answered `urgency`, `urgency_derived`, `urgency_rule` and `urgency_override` beside `horizon` and
`horizon_set`, served `…/urgency-override` beside `…/horizon`, took a filing's, a list's and a saved
filter's `urgency` and the capability `override-urgency`, and stored `override-urgency` beside
`set-horizon`. The release after it knows the new names only — an old one sent is `400`, a field or
a parameter the operation does not have —, and
[migration 38](../../backend/internal/store/migrations/000038_horizon_names_only.up.sql) rewrote
the stored capability sets and a saved filter's `urgency`. The checks of the capability sets took
`override-urgency` in 0.6 and 0.7, since 0.5 writes it beside `set-horizon` after an image rollback,
and those releases dropped it wherever a set was read;
[migration 40](../../backend/internal/store/migrations/000040_capability_checks_set_horizon_only.up.sql)
rewrote the three again and took the old name out of both checks, which refuse it now
(`TestTheNarrowingMigrationRewritesAgainAndRefusesTheOldName`). What keeps the old word is what no
client reads as API: the enum `urgency` and its columns (`TicketRow.UrgencyOverride` …, mapped in
`ticketView` and `setOverride`), and the audit record's act `overridden` with its payload
`urgency_override`, which the activity, the context and the session start read as setting the
horizon.

What stays deprecated in `/api/v1` today is a token's `restricted_project_id` beside
`restricted_project` ([ADR 0035] D2, `projectKeys` in [`me.go`](../../backend/internal/api/me.go)).

## Media types beside JSON

- **CSV.** `listAudit`, `listTenantTime` and `timeReport` answer `text/csv` when `Accept` names
  it (`wantsCSV` in [`content.go`](../../backend/internal/api/content.go)); a cell starting with
  `=`, `+`, `-`, `@`, a tab or a carriage return is prefixed with `'` (`neutralise`).
- **Uploads** are `multipart/form-data`, read by the handler, not by the validator
  ([storage.md](storage.md)).
- **The Markdown export** returns `markdownResponse` from
  [`export.go`](../../backend/internal/api/export.go), which implements the generated
  `VisitExportTicketResponse` itself: the generated response would send `text/markdown` without
  its charset. It sends `text/markdown; charset=utf-8`, the ticket's `ETag` and
  `Content-Length`, never `304`, and records every call as `exported`
  ([markdown-grammar.md](markdown-grammar.md)). The validator reads `text/markdown` with the
  plain-text body decoder registered in `validate.go`.
- **An import's upload** is `multipart/form-data` as well, read by `importer.ReadUpload` in the
  handler ([import-and-export.md](import-and-export.md#the-dry-run)).
- **The project and the tenant export** return `archiveResponse` from
  [`exports.go`](../../backend/internal/api/exports.go), which implements both generated visit
  methods: `application/gzip`, `Content-Disposition: attachment` with the archive's file name, and
  `Content-Length`. The validator reads `application/gzip` with kin-openapi's file body decoder,
  registered in `validate.go`.
- **The context** returns `contextResponse` from [`context.go`](../../backend/internal/api/context.go)
  for the same reason: `text/markdown; charset=utf-8` and `Content-Length`, no `ETag` — it is no
  one entity — and every call recorded as `exported` with the format `context v1`.
- **The event stream** is no response a handler returns: oapi-codegen excludes `streamEvents`,
  and the pipeline calls `serveEvents` in [`events.go`](../../backend/internal/api/events.go)
  ([events.md](events.md)).
- **A turn of the chat** is a `POST` answered with `text/event-stream` once it has begun: oapi-codegen
  excludes `runChatTurn` as well, and `serveOperation` calls `serveChat` in
  [`chat.go`](../../backend/internal/api/chat.go) after the body limit and the validation; a failure
  after the stream began is its `error` event, a problem body from `problem.BodyOf`
  ([chat.md](chat.md#a-turn)).

[ADR 0010]: ../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md
[ADR 0018]: ../adr/0018-the-views-of-the-first-release.md
[ADR 0019]: ../adr/0019-no-sprints-and-no-milestones-continuous-flow-with-optional-wip-limits.md
[ADR 0021]: ../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md
[ADR 0023]: ../adr/0023-the-tenant-is-in-the-path.md
[ADR 0024]: ../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md
[ADR 0028]: ../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md
[ADR 0029]: ../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md
[ADR 0030]: ../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md
[ADR 0031]: ../adr/0031-server-side-sessions-in-an-httponly-cookie.md
[ADR 0033]: ../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md
[ADR 0034]: ../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md
[ADR 0035]: ../adr/0035-personal-access-tokens.md
[ADR 0036]: ../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md
[ADR 0037]: ../adr/0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md
[ADR 0043]: ../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md
[ADR 0045]: ../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md
[ADR 0046]: ../adr/0046-spec-first-the-openapi-document-is-the-contract.md
[ADR 0047]: ../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md
[ADR 0048]: ../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md
[ADR 0049]: ../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md
[ADR 0050]: ../adr/0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md
[ADR 0054]: ../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md
[ADR 0059]: ../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md
[ADR 0065]: ../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md
[ADR 0071]: ../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md
