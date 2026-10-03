# What a token can do, and what an agent can do

What a personal access token is, how a request presenting one is checked, what it may then do
— through its scope, its person's role, its restriction and, for an agent, the capabilities
and the hard-off list — how a dead token is answered and what is recorded, as built on
2026-10-02. Which tenants, projects and tickets a person can see at all is
[tenancy.md](tenancy.md); how a token comes to exist while there is no login is
[trust-boundaries.md](trust-boundaries.md) H-1.

## A token is a bearer secret, stored as a hash

- **Form.** `cwk_` followed by 43 base62 characters that encode 32 bytes from `crypto/rand`,
  left-padded so every token has one length and matches `^cwk_[0-9A-Za-z]{43}$` — a pattern
  secret scanners and push protection can look for
  ([`backend/internal/auth/token.go`](../../backend/internal/auth/token.go);
  [ADR 0035](../adr/0035-personal-access-tokens.md) D1).
- **Storage.** The database keeps the SHA-256 of the whole token and nothing else of it
  (`tokens.token_hash`, unique, 32 bytes); the plaintext exists once, where the token is
  written. A plain hash is enough for a secret of 256 random bits: there is no dictionary of
  likely tokens to precompute. Nothing limits the rate of failed attempts
  ([ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
  D1); at 256 bits, guessing is not a practical attack.
- **Transport.** Only `Authorization: Bearer cwk_…`; a value of another shape is refused
  before the database is asked. A token is never read from a query parameter or a cookie, and
  an unknown query parameter is refused anyway (ADR 0035 D7).
- **Lookup.** The resolver's transaction names the presented hash in `app.token_hash`, and
  the tokens policy admits that one row and no other; the person is read once the row names
  it ([`store/tokens.go`](../../backend/internal/store/tokens.go) `LookupToken`;
  `TestTokenLookupSeesThePresentedRowOnly`).
- **Origin.** No route creates a token, and the runtime role cannot insert one; the test
  fixture writes tokens over an administrative connection, and `make dev-seed` prints the
  plaintext of the one it writes once ([trust-boundaries.md](trust-boundaries.md) H-1).
  Whatever writes a token,
  the schema holds it to its rules: an expiry later than its creation, no `admin` scope on an
  agent token, capabilities only on an agent token and only the nine names, a project
  restriction only together with the tenant restriction and only for a project of that
  tenant ([migration 4](../../backend/internal/store/migrations/000004_tokens.up.sql)). The
  lifetime default and maximum of ADR 0035 D4 belong to the creation route, which does not
  exist: nothing caps the expiry a writer chooses.
- **Listing.** `GET /api/v1/me/tokens` shows the person's tokens with their metadata — name,
  scope, agent flag, capabilities, restriction, dates, state — never the hash or the
  plaintext; revoked and expired tokens stay listed (ADR 0035 D6).

## What a request with a token may do

An act is allowed when the person's role, the token's scope and, for an agent's request, the
agent rules all allow it. `auth.Authorize` checks them in that order — role, scope, hard-off
list, capability — and answers `403` with `forbidden`, `insufficient_scope` or
`agent_forbidden`, the last with a detail naming the rule (`hard-off: …`) or the capability
(`missing capability: …`) ([`auth/authorize.go`](../../backend/internal/auth/authorize.go);
ADR 0035 D3,
[ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D5). The role is the person's membership role in the tenant — in a restricted project the
lower of it and their entry on the project's list — read on every request, so a token never
reaches further than its person does at that moment.

| Scope | Reaches |
|---|---|
| `read` | every read of what the person may see; for a tenant administrator also the tenant's audit view |
| `write` | additionally what a member does: filing and editing tickets, transitions, links, comments, questions and answers, stakes, progress, uploads, booking time; creating a project where the person may ([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D9); revoking another of the person's tokens |
| `admin` | additionally the administration acts that exist: the tenant's settings including the time lock, archiving a project, setting or lifting the confidential flag, withdrawing another person's comment ([`api/comments.go`](../../backend/internal/api/comments.go) `mayChangeComment`) |

## Restrictions

- A token restricted to a tenant answers `404` on every other tenant, exactly as for an
  unknown slug ([`api/tenant.go`](../../backend/internal/api/tenant.go) `boundary`).
- A token restricted to a project — always inside its tenant restriction — carries the
  project into the visibility predicate. It reaches its project's routes, the project list,
  the tenant-wide ticket list, the key resolver and the event stream, each narrowed to the
  project, and answers `404` on every other tenant route ([`api/tenant.go`](../../backend/internal/api/tenant.go)
  `tenantWideForProjectTokens`; [tenancy.md](tenancy.md) "The project restriction"). It is the
  token a repository binding wants (ADR 0035 D3).
- The person's own routes under `/api/v1/me` name no tenant, so the boundary does not run for
  them; the handlers hold a restricted token to its tenant instead. It sees the membership of
  its tenant only, lists only itself among the person's tokens, and revokes no token but itself
  ([`api/me.go`](../../backend/internal/api/me.go) `restricted`;
  `TestARestrictedTokenSeesItsTenantAndItselfOnly`).

## Expiry, revocation and refusals

- **Answers.** An unknown or malformed token: `401 unauthenticated`. A revoked token, or one
  whose person is deactivated: `401 token_revoked`. An expired token: `401 token_expired` with
  the date. Revocation is checked first. Every `401` carries
  `WWW-Authenticate: Bearer realm="cowork"` ([`api/authn.go`](../../backend/internal/api/authn.go)).
- **Refusals.** Every use of a revoked or expired token is logged with the token's id and the
  reason, never the token. It is also recorded as an installation-level `refused` act of the
  token's person, unless one with the same token and reason was recorded within the past hour
  (ADR 0035 D9), so a forgotten configuration that retries with a dead token cannot grow the
  append-only record without bound. The count and the insert are not serialised: two
  refusals in the same instant can both be recorded
  ([`store/tokens.go`](../../backend/internal/store/tokens.go) `RecordTokenRefusal`).
- **Revocation.** `DELETE /api/v1/me/tokens/{token_id}` is immediate, keeps the row and
  records the `revoked` act and who revoked. A token may always revoke itself, whatever its
  scope; revoking another of the person's tokens needs `write` scope and is hard-off for
  agents ([`api/me.go`](../../backend/internal/api/me.go) `RevokeMyToken`). Revoking twice,
  or twice at the same moment, answers `204` and records one act. A revocation is final: a
  trigger refuses any update that clears or changes `revoked_at` or `revoked_by`, whoever
  writes it ([migration 4](../../backend/internal/store/migrations/000004_tokens.up.sql)
  `tokens_revocation_is_final`; `TestRevocationIsFinal`). The `refused`
  and `revoked` acts are installation-level rows, readable in the database only: no route
  shows them, and the per-token view of
  [ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D6 is not
  built. Not built either: an administrator's view and revocation of their members' tokens
  (ADR 0035 D5), the deactivation of a person, and the allow-list check of ADR 0035 D8.
- **Last use.** The last-used day is written at most once per token and UTC day — a note per
  replica and a conditional update — as bookkeeping, not as an act (ADR 0035 D2).
- **Open streams.** An event stream is not a next request: at every heartbeat it checks the
  token again — not revoked, not expired, its person not deactivated — and the tenant
  boundary, and ends when either fails ([`api/events.go`](../../backend/internal/api/events.go)
  `stillAdmitted`; [ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
  D5) — H-7.

## What is recorded

- Every act is an audit row written in the act's transaction; it names the person, the token,
  the request id, the agent mark and, for an agent, the capability set that applied
  ([`store/tx.go`](../../backend/internal/store/tx.go)). Using a token is recorded through its
  acts, not per request (ADR 0035 D9).
- Reads are not recorded, with two exceptions that mean data left the system (ADR 0026 D5):
  every download of an attachment's bytes — a `304` is not one — and every Markdown export of
  a ticket, which is never answered with `304`
  ([`api/export.go`](../../backend/internal/api/export.go)). The CSV forms of the time lists,
  the time report and the audit view are reads like any other.
- The answer to an abused token is the record and revocation (ADR 0039 D4): an administrator
  filters the tenant's audit view (`GET …/audit`) by token, person, action, entity type and
  period, as JSON or CSV. In CSV, a cell a spreadsheet would read as a formula — one starting
  with `=`, `+`, `-`, `@`, a tab or a carriage return — is prefixed with an apostrophe
  ([`api/tenants.go`](../../backend/internal/api/tenants.go) `neutralise`).

## Which request is an agent's

- The agent flag is written with the token and never changes; the runtime role cannot update
  it. A flagged token's request is an agent's whatever it sends; without an `X-Cowork-Agent`
  header it is recorded as `unknown-agent`
  ([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
  D2, D4).
- A plain token's request becomes an agent's when it sends the header, and then holds every
  capability: the header adds the agent rules and takes nothing away. No header value turns a
  flagged token's request into a person's
  ([`auth/principal.go`](../../backend/internal/auth/principal.go) `Mark`; ADR 0036 D3).
- The header is `name/model/session`, each part one to 64 printable ASCII characters that
  neither start nor end with a space; a malformed header is refused with `400`, never ignored.
- An agent token's scope is at most `write`. An administration act therefore refuses an agent
  token on its scope (`insufficient_scope`) before the hard-off rule is reached; the hard-off
  rule is what refuses it to a plain `admin` token marked by the header, and what refuses the
  hard-off acts that need only `write` to every agent.

## Capabilities, the baseline and the hard-off list

| Capability | Guards |
|---|---|
| `decide` | `analysed → decided` |
| `close` | `in-progress → done`; the verification note stays required, and the open prerequisites the agent can see still refuse |
| `drop` | a move to `dropped` from any state but `done` and `dropped` |
| `override-urgency` | setting and withdrawing an urgency override |
| `interest` | a `need` or `urgent` stake |
| `upload` | uploading an attachment |
| `create-project` | creating a project |
| `record-answer` | answering a question, which for an agent writes its person's answer down: the answer is marked `recorded_by_agent`, and an agent changes only an answer an agent recorded |
| `rank` | nothing yet; no route moves a rank |

Without a capability, an agent with `write` scope whose person is a member has the baseline
(ADR 0043 D2, as the handlers build it): filing a ticket and editing its fields and its body,
comments, links, questions, progress, a `watch` stake, the transitions `filed → analysed`,
`decided → in-progress`, into `blocked` and back, and the acts of H-6.

| Hard-off rule ([`auth/authorize.go`](../../backend/internal/auth/authorize.go)) | Refuses |
|---|---|
| administration | the tenant's settings, archiving a project |
| booking time | booking, editing and voiding time entries — refused before the `Idempotency-Key` is looked at |
| overriding the prerequisite refusal | `override_prerequisites` on a transition to `done` |
| setting or lifting the confidential flag | `PUT …/confidential` |
| token administration | revoking another token of the person |

Three rules live in the handlers and answer `agent_forbidden` with their own detail: an agent
edits or withdraws only comments an agent of the same person wrote
([ADR 0015](../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D4), it
withdraws only questions an agent asked, and it changes only an answer an agent recorded.

## An agent's POST carries an Idempotency-Key

Every `POST` of an agent except a transition must carry an `Idempotency-Key`, or it is refused
with `400 idempotency_key_required` ([`api/server.go`](../../backend/internal/api/server.go)
`keyed`): filing a ticket, creating a project, a comment, a question, an upload — and booking
time, which is refused earlier. A transition is idempotent by the state it names as `from`; a
key sent with one is recorded on the act and not stored
([ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D2, D7). The key belongs to the token and is bound to a fingerprint of the operation, its path
and its body — for an upload the file's SHA-256, its name and its comment. The response is
stored with the act in the same transaction for twenty-four hours; a repetition replays it,
and the same key with a different request answers `422 idempotency_mismatch` (ADR 0045 D3,
D4). A person's `POST` may carry a key and need not.

## What this does not cover

<a id="h-6"></a>
### H-6 — Some agent acts are open by default and await review

Live today. ADR 0043 gives an agent a baseline, nine capabilities and a hard-off list; the
acts below are on none of them and are built allowed, each recorded with the agent mark and
the capability set that applied:

- **Reassigning a confidential ticket.** Assignment admits the new assignee
  ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
  D9). An agent of a person who sees the ticket can assign it to anyone in the tenant who sees
  its project, and the disclosure cannot be taken back
  ([`api/tickets.go`](../../backend/internal/api/tickets.go) `applyRelations`).
- **Removing a `blocks` link.** An agent with `close` cannot override the prerequisite
  refusal, but it can remove the open `blocks` links into its ticket and then close it: two
  calls around a hard-off rule ([`api/links.go`](../../backend/internal/api/links.go)
  `UnlinkTickets`).
- **Backward moves and reopens.** `in-progress → decided` or `→ analysed`,
  `decided → analysed`, and `done` or `dropped → filed` need no capability, so a token without
  `decide` and `close` — the "assisted" set — can undo what its person kept for themselves
  ([`api/transitions.go`](../../backend/internal/api/transitions.go) `checkTransition`).
- **Removing its person's stake**, or lowering it to `watch`, whatever weight the person gave
  it; only setting `need` or `urgent` needs `interest`
  ([`api/interest.go`](../../backend/internal/api/interest.go)).
- **Editing an open question its person asked** — its text, options, recommendation and the
  person asked — also one the person asked without an agent, while withdrawing a question is
  limited to what an agent asked ([`api/questions.go`](../../backend/internal/api/questions.go)
  `mayEdit`).
- **Editing a project's** name, description and WIP limits, while creating one needs
  `create-project` ([`api/projects.go`](../../backend/internal/api/projects.go) `edit`).

The integration tests assert all six as allowed, so closing one is a deliberate change. Nobody can switch them off per installation; a person who wants none of them gives an
agent a `read` token, and an administrator finds them in the tenant's audit view by token.

<a id="h-7"></a>
### H-7 — An open event stream outlives a revocation by up to one heartbeat

Live today. A stream checks its token and its person's membership at every heartbeat, every
twenty seconds; the interval is not configurable. Between two heartbeats a stream whose token
was revoked or expired, whose person left the tenant, or whose person lost a project still
receives the events its filter admitted at the last heartbeat — the keys, versions and kinds of
the acts, no content. Every request the client makes
with the dead token is refused at once; the window is the stream's alone.
