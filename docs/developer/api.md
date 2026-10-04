# The API

How `/api/v1` is built: the document that is the contract, what `make generate` makes of it,
the pipeline every request runs before its handler, authentication — a token or a session —,
the CSRF check, the tenant boundary, authorization, errors, idempotency, versions, paging,
filters, and the media types beside JSON. The decisions are [ADR 0046] (spec first), [ADR 0047]
(errors), [ADR 0045] (idempotency), [ADR 0048] (paging), [ADR 0049] (filters), [ADR 0050]
(versions), [ADR 0031] (sessions), [ADR 0037] (CSRF), [ADR 0029] (the identity provider's login);
the reference table of routes and codes is [README.md, API](../../README.md#api-backend). Read
against the tree on 2026-10-04.

## The document

[`backend/api/openapi.yaml`](../../backend/api/openapi.yaml) is the source (OpenAPI 3.1): the
`info`, the default security (`bearerToken` or `sessionCookie`), the tags, and one `$ref` per path
into the file of its path family.

| File | Paths |
|---|---|
| [`meta.yaml`](../../backend/api/meta.yaml) | `/version`, `/openapi.json`, `/schemas/cowork-yaml.json` — `security: []`, read before a client authenticates; the last answers [`cowork-yaml.schema.json`](../../backend/api/cowork-yaml.schema.json) |
| [`auth.yaml`](../../backend/api/auth.yaml) | the browser's login flows, **outside `/api/v1`**: `/auth/options`, `/auth/local`, `/auth/oidc/login`, `/auth/callback`, `/auth/logout` — see [the login flows](#the-login-flows) |
| [`me.yaml`](../../backend/api/me.yaml) | `/me`, `/me/password`, `/me/tokens`, `/me/tokens/{token_id}`, `/me/token` — the token a request presents |
| [`repositories.yaml`](../../backend/api/repositories.yaml) | a project's repositories (list, bind, unbind) and `/me/repositories/lookup` across the person's tenants ([domain.md](domain.md#repositories)) |
| [`tenants.yaml`](../../backend/api/tenants.yaml) | listing every tenant for a global administrator and creating one (`GET`, `POST /tenants`), the tenant, its audit record, projects, archiving, the ticket lists, a ticket, its body, urgency override and confidential flag |
| [`accounts.yaml`](../../backend/api/accounts.yaml) | the tenant's local accounts: list, create, reset the password, unlock, deactivate, end the sessions |
| [`members.yaml`](../../backend/api/members.yaml) | who belongs where: the members and their grants, the group mappings, a project's restriction and access list |
| [`tickets.yaml`](../../backend/api/tickets.yaml) | the key resolver `/tickets/{tenant}/{key}`, links, transitions, the move in the rank, interest, the Markdown export and the context |
| [`questions.yaml`](../../backend/api/questions.yaml), [`comments.yaml`](../../backend/api/comments.yaml), [`time.yaml`](../../backend/api/time.yaml), [`attachments.yaml`](../../backend/api/attachments.yaml) | their entities; `comments.yaml` also the activity list |
| [`events.yaml`](../../backend/api/events.yaml) | `/tenants/{tenant}/events` |
| [`chat.yaml`](../../backend/api/chat.yaml) | `/tenants/{tenant}/chat`: the chat's availability and a turn of it, with the contract of the turn's stream in prose; `/tenants/{tenant}/chat/turns`: stopping the person's running turns ([chat.md](chat.md)) |
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
   name (`EffortS`); `streamEvents` and `runChatTurn` are excluded, and `skip-prune` keeps the
   models only their bodies and events name — the turn's body and the data of its events, which
   `chat.go` reads and writes.
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
   the seven public operations (`getVersion`, `getOpenAPI`, `getCoworkYamlSchema`, `getAuthOptions`,
   `loginLocal`, `loginOidc`, `oidcCallback`). One resolver for both credentials
   ([Authentication](#authentication)); the `auth.Principal` and the `store.Caller` — with the
   keyed hash of the client's address every audit row of the request carries — go into the context. A public operation that writes and says
   `x-cowork-origin-check: true` — the login — gets the origin half of the CSRF check instead.
   **For a request authenticated by a session** three more rules run here, before the tenant
   boundary: **the CSRF check** on an unsafe method (`403 csrf`); a session the agent header marks
   is refused an operation that takes a session only (`403 agent_forbidden`); and the gate of a
   temporary password (`403 password_change_required` for everything but `getMe`,
   `changeMyPassword` and `logout`) — `sessionRules` in [`api.go`](../../backend/internal/api/api.go).
5. **Tenant boundary**, when the path has `{tenant}`. The admitted `tenantScope` goes into the
   context.
6. `streamEvents` leaves here: request validation, then `serveEvents` — no timeout, no body
   limit, no generated handler ([events.md](events.md)).
7. **Timeout:** the context gets `COWORK_REQUEST_TIMEOUT` (0 disables), and `bodyDeadline`
   holds reading the body to the same deadline — a read deadline on the connection, lifted once
   the body is read, so a body that trickles in fails instead of holding the request.
8. **Body limit** (`limitBody` in [`validate.go`](../../backend/internal/api/validate.go)): a
   JSON body `COWORK_MAX_JSON_BODY` (0 disables), a multipart upload
   `COWORK_ATTACHMENT_MAX_BYTES` plus 64 KiB of multipart overhead (0 disables). A declared length above it is
   `413 payload_too_large` before anything is read; a longer body fails while it is read.
9. **Request validation** against the document (kin-openapi `openapi3filter`): every error is an
   `errors[]` entry of `400 validation_failed`; a query parameter the operation does not declare
   is refused (the validator would let it pass) — except on an operation marked
   `x-cowork-open-query`, the identity provider's callback, to which an issuer may add its own; a
   path parameter that breaks its schema is
   `404`, because it names nothing that can exist; `format: uuid` accepts any UUID version (the
   ids are UUIDv7); defaults are not written into the request — the handlers apply them; a
   multipart body is left to the handler. The validator sees the route without its security
   requirement (`unsecured`): step 4 has authenticated the caller, and the validator's own
   security check would read the whole body into memory before the handler checks anything.
10. The generated mux dispatches to the strict handler — or, with `Options.ValidateResponses`,
    `serveValidated` holds the response to the document as well ([testing.md](testing.md)); it
    reads the whole body before the handler runs, so a test of the body's timing switches it
    off. `runChatTurn` goes to `serveChat` instead, on the context from before step 7: the
    timeout bounded reading its body, and the turn has limits of its own
    ([chat.md](chat.md#a-turn)).

A handler returns a `*problem.Error` or an error; `writeError` answers a problem as it is,
`store.ErrNotFound` as `404`, `store.ErrIdempotencyMismatch` as `422 idempotency_mismatch`, a
passed deadline as `504 timeout`, and anything else as `500 internal` with the details in the log
only. A body the strict server cannot decode is `400 validation_failed`.

## Authentication

[`authn.go`](../../backend/internal/api/authn.go) and [`session.go`](../../backend/internal/api/session.go)
with [`internal/auth`](../../backend/internal/auth/). **Two credentials, one resolver**
([ADR 0031] D6): `credentialsOf` reads from the document which of `bearerToken` and
`sessionCookie` the operation declares — the default is both, written once at the root; the
sixteen session-only operations (`createMyToken`, `createTenant`, `createAccount`,
`resetAccountPassword`, `changeMyPassword`, `logout`, `addMember`, `setMemberGrant`,
`createGroupMapping`, `updateGroupMapping`, `setProjectRestriction`, `setProjectAccess`,
`runChatTurn`, `stopChatTurns`, `setMyChat`, `listTenants`) declare `sessionCookie` alone, the seven public ones declare nothing — and
`authenticate` decides. What the first twelve make — a token, a tenant, an account, a password only
its setter knows, a role, a mapping, a way into a restricted project — would outlive the revocation
of a leaked token, which is why a token cannot call them, and so would the chat's capabilities
(`setMyChat`); a turn of the chat acts with the person's session and its stop ends the session's
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
  reason and hour ([ADR 0035] D9).
- **A session** is the cookie `__Host-cowork-session` (`auth.SessionCookie`): 43 characters of
  base64url, the SHA-256 of which is `sessions.token_hash` (`auth.HashSession`,
  `DB.LookupSession`). Malformed, unknown, ended, past a limit (`sessionLive`: the absolute
  `expires_at`, the idle `last_seen_at` plus `COWORK_SESSION_IDLE`) or its person deactivated:
  the same `401`, with a `Set-Cookie` that clears the cookie. A session of the identity provider
  whose person is not the configured issuer's ends with every session of that person; one whose
  groups are due runs its groups refresh first — the request that claims it waits for the issuer,
  the session's others are served on its groups — and a refresh that ends it is that `401` too
  ([architecture.md](architecture.md#the-groups-refresh-in-the-request-path)). A live session moves
  its idle clock at most once a minute (`DB.TouchSession`, bookkeeping outside `Mutate`; a failure
  is logged).
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
at `/auth/` as well as `/api/`; the frontend's nginx and the dev proxy forward `/auth` like
`/api`. They are browser flows, but they are no secret: the served document lists them, and a
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
`/login?error=<code>`. The callback's answer sets two cookies, the session's and the cleared state
cookie, which the generated response type, with one `Set-Cookie`, cannot carry: `redirect`
implements the generated `VisitOidcCallbackResponse` itself. Its failures are redirects too, so
`OidcCallback` returns no `problem.Error` for them; the reason goes to the log. The relying party
itself is [`internal/oidc`](../../backend/internal/oidc/oidc.go), the decision
[`store.CompleteOIDCLogin`](../../backend/internal/store/identity.go)
([architecture.md](architecture.md#the-two-logins),
[identity-provider.md](../security/identity-provider.md)).

## The tenant boundary

`boundary` in [`tenant.go`](../../backend/internal/api/tenant.go) admits a request to the tenant
in its path before any handler runs ([ADR 0023] D5). It reads the tenant by slug together with
the person's highest role there (`GetTenantForPerson`, an `Installation` read). Refused — all
with the same `404 not_found` "no such tenant", so the answer does not tell whether the tenant
exists ([ADR 0047] D5):

- an unknown slug, or a person without a membership;
- a token restricted to another tenant;
- a token restricted to a project, on a path without `{project}` — except `listProjects`,
  `listTenantTickets`, `resolveTicket` and `streamEvents` (`tenantWideForProjectTokens`), which
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
another token. The checks run in this order; the first failure answers:

1. `Need.Role` above the person's role: `403 forbidden` ("the act needs the member role").
2. `Need.Scope` above the token's scope: `403 insufficient_scope`.
3. For an agent's request only: `Need.HardOff` set — `403 agent_forbidden`, detail
   `hard-off: <rule>`; `Need.Capability` not held — `403 agent_forbidden`, detail
   `missing capability: <name>` ([ADR 0043] D3–D5). Beyond `Authorize`, `mayClose` in
   [`transitions.go`](../../backend/internal/api/transitions.go) refuses an agent's done act —
   by hand, or by the `PATCH` that fills the last progress stage — from any state but
   `in-progress` and `review` with `403 agent_forbidden`, detail `close covers in-progress and
   review: …` (ADR 0043 D4).

| Need | Role, scope | Agent rule | Defined in |
|---|---|---|---|
| `read` | viewer, `read` | — | [`tenants.go`](../../backend/internal/api/tenants.go) |
| `administer` | admin, `admin` | hard-off `administration` | `tenants.go` |
| `adminRead` | admin, `read` | — | [`members.go`](../../backend/internal/api/members.go): the group mappings, a project's access list |
| `work` | member, `write` | baseline; a transition adds `decide`, `close` or `drop`, the done act of the stages `close`, an override `override-urgency` and of an agent a reason, an agent's answer `record-answer` | [`tickets.go`](../../backend/internal/api/tickets.go) |
| `edit` | member, `write` | — | [`projects.go`](../../backend/internal/api/projects.go) |
| `rankNeed` | member, `write` | `rank` | [`rank.go`](../../backend/internal/api/rank.go) |
| `booking` | member, `write` | hard-off `booking time` | [`time.go`](../../backend/internal/api/time.go) |
| `uploadNeed` | member, `write` | `upload` | [`attachments.go`](../../backend/internal/api/attachments.go) |
| `interestNeed(weight)` | `watch`: viewer, `write`; `need`, `urgent`: member, `write` | `interest` for `need` and `urgent` | [`interest.go`](../../backend/internal/api/interest.go) |

The handlers also build a few needs inline: `creating` for `createProject`, `bindRepository`
and `unbindRepository` (admin, or member while the tenant allows it; `write`; `create-project`
— [`repositories.go`](../../backend/internal/api/repositories.go), judged by the project role for
a binding), `setConfidential` (admin, `admin`, hard-off), the
done act's `close` and its prerequisite override (member, `write`; `close`, and hard-off for the
override — `mayClose`), `listAudit` (admin, `read`),
withdrawing another person's comment (admin, `admin`), and revoking another token of the person
(`write`, hard-off). The account routes of [`accounts.go`](../../backend/internal/api/accounts.go)
and the writes of [`members.go`](../../backend/internal/api/members.go) use `administer`, the
member list `read`; a change of a grant or a mapping, or the deactivation of an account
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
or `query:<name>`, `header:<name>`, `path:<name>`; on a `412` an entry carries `current`. The
`detail` never carries a secret, SQL or an internal path; the cause goes to the log under the
request id.

## Idempotency

A creating `POST` — `createProject`, `bindRepository`, `createTicket`, `askQuestion`, `addComment`,
`bookTime`, `uploadAttachment`, `addMember`, `createGroupMapping` — calls
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
  with `replayed[T]` and `header`. The same key with another request is
  `422 idempotency_mismatch`. A key is scoped to its token and kept twenty-four hours. The keys
  come from the client: `cowork-mcp` draws a UUIDv7 per `POST`, and the chat in the UI derives them
  from the conversation and the call, so the same call sent again replays ([chat.md](chat.md#the-loopback)).

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
`replaceTicketBody`, `overrideUrgency`, `withdrawUrgencyOverride`, `setConfidential`,
`updateQuestion`, `answerQuestion` (changing an answer given), `editComment`,
`editTimeEntry`, `updateGroupMapping` and `setProjectRestriction` (the project's version). A grant
and an access entry are addressed by their person and written without it, like a link. Links, interest and attachments are written without it and carry no version
([ADR 0050] D4); a move in the rank (`moveTicketRank`) is written without it — it names where
the ticket goes, so the last move wins — and raises the ticket's version.

The two ticket lists answer a weak `ETag` — `W/"…"`, 24 hex characters of the SHA-256 of the
page — and `304` for a matching `If-None-Match` (`weakETag`, `notModified` in `tickets.go`). An
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
- `listProjectTickets`, `listTenantTickets` and `listTenantTime` also take numbered pages:
  `page` with `per_page` (25, 50 or 100; 50 when absent), answered with `total`. `page ×
  per_page` above 10 000 is `400 page_too_deep`; a numbered page with `cursor` or `limit`, or
  `per_page` without `page`, is `400 validation_failed` ([ADR 0048] D2).
- The sort is fixed per list ([ADR 0048] D6): a project's tickets by rank, the unranked after
  them by number — the position is `<key>.<number>` (`TicketOrder.Position`), sealed, and
  `ticketListScope` adds `/rank` to the scope, so a cursor of the number order before the rank
  is `invalid_cursor`; the tenant's tickets and time entries, the audit record and the person's
  tokens newest first; comments and activity oldest first unless `order=desc`; projects by key;
  questions by number; members, interest and a project's access list by person id; the group
  mappings by group; the installation's tenants by slug; the other lists by id.

## Filters

`parseTicketQuery` in [`ticketlist.go`](../../backend/internal/api/ticketlist.go) turns the
query of the two ticket lists into a `store.TicketFilter` ([ADR 0049]). A repeated parameter
combines with OR and `!` negates a value. Vocabulary values are checked against the generated
enums (`apigen.TicketState(v).Valid()` …); `assignee` and `reporter` take a person id or `me`,
`assignee` also `none`; `parent` takes a ticket key or `none`, resolved under the predicate —
a key the caller cannot see matches nothing; `interest` takes `me` or `any`; `blocked`,
`has_open_questions` and `include_terminal` are booleans; `q` is capped at
`COWORK_MAX_QUERY_LENGTH` characters. Every refused value is named in `errors[]`.

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

[ADR 0023]: ../adr/0023-the-tenant-is-in-the-path.md
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
