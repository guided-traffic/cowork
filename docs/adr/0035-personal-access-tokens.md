# ADR 0035: Personal Access Tokens — `cwk_` Prefixed, Hashed at Rest, Scoped `read`/`write`/`admin` With Optional Tenant and Project Restriction, Expiring by Default, Created Only by the Person in a Session

## Status

Accepted, amended 2026-10-02 (D2: the address hash comes with the trust rule for forwarded
addresses; D3: creating a project is a `write` act, and what a restricted token reaches on the
person's own routes and on the tenant-level reads; D6: a revocation is final; D9: refused uses
are recorded at most once per token, reason and hour) and 2026-10-03 (D2: the trust rule for
forwarded addresses is decided; D4, D5: how creation is built, and what else only a session
makes) and 2026-10-04 (D2: the address hash in the audit row built; D5: twelve operations take a
session only, by one rule; D8: the gate built; D9: its refusal recorded), and again on 2026-10-04
after the security review (D8: a person of another issuer is outside at once), and for the chat
in the UI (D5: thirteen operations, and the tenant's consent to an outside provider of the chat
switched on in a session only), and on 2026-10-04 by the owner's answer to "how old may the groups
be that judge a token?" (D8: groups older than `COWORK_OIDC_GROUPS_MAX_AGE`, a week by default,
refuse the person's tokens until they sign in to the browser once — built the same day), and for
the global administrator's view of the installation's tenants (D5: fourteen operations, the list of
every tenant among them,
[ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2), and on 2026-10-04 by the owner's answers on the chat recorded in
[ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md) (D5:
sixteen operations — stopping the person's turns of the chat and choosing the chat's capabilities
among them —, and the tenant's consent field gone with the consent; built the same day), and on
2026-10-04 by the owner's decision on the routing recorded in
[ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3
(D2: the hop in front of the backend is the Ingress controller, and the chart ships no
NetworkPolicy; built the same day), and on 2026-10-05 by the decision on the purge recorded in
[ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D7, built on the recommendation (D5: seventeen operations, purging a deleted ticket among them), and
on 2026-10-05 by the answer to "which of a member's tokens does a tenant's administrator see and
revoke?" (D5). The options were (a) every token of every member, also those restricted to the
member's other tenants; (b) the tokens that can act in the tenant — the members' unrestricted tokens
and those restricted to it; (c) only the tokens restricted to the tenant. (b) was the
recommendation, and it was built on the owner's instruction of 2026-10-05 to build the recommended
option, the owner reviewing the result. Amended 2026-10-06 for GitHub's webhook of
[ADR 0071](0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md),
by D5's rule and built the same day (D5: eighteen operations, making or rotating the tenant's
webhook secret the eighteenth), and again by the rule of D5 itself for the consistency check of
[ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4, made concrete by the implementer, open to the owner's objection (D5: nineteen operations,
removing the orphaned objects of a consistency check the nineteenth; built the same day), and on
2026-10-07 by the owner's answer to "do these three acts take a browser session?" — each in its
giving direction, over keeping the token's reach and naming each as a gap (D5: twenty operations,
unlocking a local account the twentieth; widening the tenant's settings, lifting the confidential
flag and a token's assignment of a confidential ticket to another person take a session in the
request layer; built 2026-10-09). And amended 2026-10-09 with the removal of GitHub's webhook, which the owner
dropped before its trial (ADR 0071 Status; D5: ~~twenty~~ nineteen operations, `createGitHubSecret`
gone with the webhook). Date: 2026-10-01. Decided by the owner as the answer to the
catalog question "personal access token design?" at its three contested points: three hierarchical scopes
with optional tenant and project restriction; mandatory expiry with a ninety-day default and
a one-year maximum; creation only by the person themselves in a browser session, never by an
administrator for someone else and never through a token. The fixed part and the rules of
D7–D9 were put to the owner with the question and not objected to. The
amendments of 2026-10-02 are the owner's answers to three questions of the first build phase: when the
source address is hashed (with the login, over now with a proxy trust rule and a
NetworkPolicy, and over hashing the TCP peer); how many refused uses of a dead token are
recorded (bounded, over every one and over once per token); and whether an agent's `write`
token may create a project (yes, through the tenant setting of
[ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D9 that the owner asked for).

The owner's answer of 2026-10-03 settles the first of those for the rule itself: whose
`X-Forwarded-For` entry is the client's is decided by a list of trusted proxies and a walk from the
right (D2), over trusting the header as it comes and over counting the TCP peer for good.

The creation route of D5 arrived with the sessions (phase 3, 2026-10-03); the test-only fixture
of [ADR 0038](0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D6, D7 stays for the tests and `make dev-seed`.

**Partly built** (phase 2, 2026-10-02): D1, D3–D9 — `tokens` (migration 4), the bearer resolver
with its distinct answers for unknown, revoked and expired tokens, scopes and the tenant and
project restriction, the person's listing and revocation, the last-used day, the bounded
refusal rows, and the event stream's re-check at every heartbeat.

**Built** (phase 3, 2026-10-03): D5's creation and D4's configurable lifetimes —
`POST /api/v1/me/tokens`, for a browser session only
([`api/me.go`](../../backend/internal/api/me.go) `CreateMyToken`;
[docs/security/tokens.md](../security/tokens.md)) — and D2's trust rule for forwarded addresses,
which the login throttle counts under
([`api/clientaddr.go`](../../backend/internal/api/clientaddr.go) `clientAddress`,
`COWORK_TRUSTED_PROXIES`, ~~the chart's NetworkPolicy~~ *(gone 2026-10-04, D2)*;
[docs/security/local-accounts.md](../security/local-accounts.md)); in the UI, the person's tokens
page lists, creates — the plaintext shown once, in a dialog that forgets it when it closes — and
revokes ([`features/me/tokens.ts`](../../frontend/src/app/features/me/tokens.ts)). Not built: ~~D5's administrator
view of the tokens of their tenant's members and their revocation of them~~ *(built 2026-10-05, below)*; ~~D2's address hash in
the audit row, which waits for the phase that builds the identity provider; D8 (the gate), which
belongs to the identity provider.~~

**Built** (phase 4, 2026-10-04): D2's address hash in every audit row of a request
([`api/login.go`](../../backend/internal/api/login.go) `sourceHash`,
[migration 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)); D8, the
gate on the tokens of the identity provider's persons
([`api/identity.go`](../../backend/internal/api/identity.go) `tokenGate`); D5's rule extended to
the administration acts that give access ([docs/security/tokens.md](../security/tokens.md#what-only-a-session-does)).
~~Still not built: D5's administrator view and revocation of the members' tokens.~~ *(Built
2026-10-05, below.)*

**Built** (phase 3, 2026-10-04): the person's token list names D2's project restriction by key,
`restricted_project`, as the token's own answer does — only while the person sees the project in a
tenant they belong to — and keeps the project's id beside it, deprecated, for the clients of
`/api/v1` ([`api/me.go`](../../backend/internal/api/me.go) `projectKeys`); D4's maximum reaches the
token form before it asks: `GET /auth/options` names it in whole days, `token_max_lifetime_days`.

**Built** (phase 3, 2026-10-05): D5's administrator view and revocation as amended that day —
`GET` and `DELETE /api/v1/tenants/{tenant}/tokens`
([`api/tenanttokens.go`](../../backend/internal/api/tenanttokens.go)), the tokens policy of
[migration 35](../../backend/internal/store/migrations/000035_tenant_tokens.up.sql), and the
tenant's page *Tokens* ([`features/tenant/tenant-tokens.ts`](../../frontend/src/app/features/tenant/tenant-tokens.ts));
the person's token list in the browser reads numbered pages
([ADR 0048](0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D4).

Amended 2026-10-06 (the Context: the project plan it cited is consumed and deleted,
[ADR 0074](0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D4; no rule changes).

Amended 2026-10-10 by the owner's rename of a tenant to a team ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1): D5 names the list of every
team by its operation, `listTeams`, which the rename renamed; `createTeam` is the other session-only
operation it renamed, and each deprecated twin takes a session only as its operation does
([ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) D8). Still nineteen
operations; no rule changes. A token's restriction is answered as `restricted_team`, and as
`restricted_tenant` beside it for one release; a token is created restricted by `team`, or by
`tenant` in its place.

## Context

An LLM operates cowork before any UI exists (phase 2 of the project plan, which was consumed into
the phase tickets on 2026-10-06), and it does so with a token that must say who is accountable
([ADR 0004](0004-cowork-is-a-team-product.md) D1). One resolver serves cookies and tokens
([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D6), every audit row carries
the token id ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)
D1), the agent's acts are bounded by role and by the agent rules ([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D6). A token that lives in a configuration file on a laptop is the credential most likely to
leak and least likely to be noticed; its form has to make leaks scannable, bounded in time
and bounded in reach.

## Decision

**D1 — Format and storage.** A token is `cwk_` followed by 32 random bytes in base62. Only
its SHA-256 is stored; the plaintext is shown exactly once, at creation. The prefix exists
for secret scanners and push protection.

**D2 — Metadata.** Name, owner (the person), scope, optional tenant restriction, optional
project restriction, created at, expires at, last used at (date only; the source address is
stored as a hash in the audit row, not in the token *(amended 2026-10-02: from the moment a
trust rule decides which forwarded address is the client's, which the per-address login
throttle of [ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md)
D6 needs as well; audit rows written before carry no hash)* *(amended 2026-10-03: the rule is
decided. `COWORK_TRUSTED_PROXIES` is a list of CIDRs, IPv4 and IPv6, empty by default; the client
address of a request is found by walking `X-Forwarded-For` from the right, starting at the TCP
peer: while the current address is inside a trusted network the entry to its left becomes the
current address, and the first address that is not trusted is the client — entries to its left
are never read, an entry that is no address stops the walk at the hop before it, and with the
list empty the peer is the client. ~~The chart's NetworkPolicy admits only the frontend pods to the
backend, so no other pod can write the header to it.~~ *(Amended 2026-10-04 by the owner,
[ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3: the Ingress routes `/api/` and `/auth/` to the backend, so the hop the list names is the
Ingress controller, and the chart ships no NetworkPolicy. Every pod inside a trusted network that
reaches the backend can write the header and choose its address; only a policy of the cluster's
that admits the controller alone prevents it, and with the list empty nobody can
([docs/security/local-accounts.md](../security/local-accounts.md#h-17) H-17).)* The login throttle of
[ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D6 counts
that address — an IPv6 address by its /64 — keyed-hashed, in its own table; ~~the hash in the audit row stays for the phase that
builds the identity provider, and no audit row carries an address yet~~)* *(amended 2026-10-04:
every audit row written for a request carries `source_hash`, the HMAC-SHA-256 of the client's
address under a key derived from `COWORK_SESSION_KEY` by HKDF-SHA256 with the label
`cowork audit address v1` — the whole address, an IPv6 address too, found by the rule above. The
rows of the jobs and of the start-up carry none, and neither do the rows written before. No route
shows it. Whoever holds the server key can test addresses against it, and so reverse every IPv4
hash ([docs/security/tokens.md](../security/tokens.md) H-30))*), revoked at and by whom.

**D3 — Scopes are `read`, `write`, `admin`, hierarchical, and never exceed the person.**
`read` reads what the person may read; `write` additionally does what a `member` may;
`admin` additionally does what the person's `admin` role allows. *(Amended 2026-10-02:
creating a project is a `write` act wherever the person may create projects, also where the
tenant reserves it to administrators — ADR 0034 D9.)* A token's effective
permission is the intersection of its scope, its person's role in the tenant (ADR 0034) and
the agent restrictions (the next records). A token restricted to a tenant is invalid
elsewhere; one restricted to a project of that tenant is invalid outside it — which is the
token a repository binding wants. *(Amended 2026-10-02: a project-restricted token reaches the
tenant-level reads its project narrows — the project list, the tenant-wide ticket list, the
key resolver and the event stream — and nothing else of the tenant. On the person's own
routes, which name no tenant, a restricted token sees only its tenant's membership and only
itself among the person's tokens, and revokes no token but itself.)*

**D4 — Expiry is mandatory.** Default ninety days, maximum one year, both configurable
(`COWORK_TOKEN_DEFAULT_LIFETIME`, `COWORK_TOKEN_MAX_LIFETIME`); no token without an expiry.
An expired token answers `401` with the reason. *(Amended 2026-10-03: the creation request
names `lifetime_days`, or omits it for the default; a longer lifetime is shortened to the
maximum, not refused, and the answer shows the `expires_at` that applies.)*

**D5 — Only the person creates their tokens, in a browser session.** No endpoint creates a
token when the caller is a token; no administrator creates a token for someone else. An
administrator sees the tokens of their tenant's members — metadata, never plaintext — and may
revoke them. *(Amended 2026-10-03: built as `POST /api/v1/me/tokens`, which takes a session
only — a token answers `403 session_required` — and returns the plaintext once, in the answer
that creates it. A restriction names a tenant the person belongs to and a project of it they
see, and anything else is "no such"; an agent token has at most `write` scope. The route accepts
an `Idempotency-Key`, and the response stored for it omits the plaintext, so a replay answers
the token's metadata without the secret
([ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D6). The same rule — a session only — holds for creating a tenant, creating a local account and
resetting its password
([ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D1, D5:
what they make would outlive the revocation of a leaked token), changing one's own password and
logging out; the API document declares them with the session cookie alone and a unit test holds
the set to those ~~six~~. The administrator's view of their tenant's members' tokens is not built.)*
*(Amended 2026-10-04: the rule is that an act that can give access, or make something that outlives
a leaked token's revocation, takes a session, and an act that only takes access away does not. It
adds the administration of [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
and [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D3: adding a member, setting a grant, making or changing a group mapping, restricting or opening a
project, putting a person on its access list — ~~twelve~~ operations in all *(thirteen since the
chat, below)*, which the unit test over the document holds. Removing a grant, a mapping or an access entry stays open to an administrator's
`admin`-scope token, as listing, ~~unlocking,~~ *(2026-10-07: unlocking takes a session, below)*
deactivating a local account and ending its sessions do. A route that does both — a grant or a mapping raised or lowered, a project restricted or opened
— takes a session for both.)* *(Amended 2026-10-04 for the chat in the UI,
[ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md):
~~twelve~~ thirteen operations — a turn of the chat, `runChatTurn`, takes a session too, for a
reason of its own: its tool calls act with the person's session, and an agent that holds a token
has the MCP server ([ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)).
~~Beside the thirteen, one field follows the rule: switching the tenant's `chat_external_allowed` on
in `PATCH /api/v1/tenants/{tenant}` takes a session — the consent lets the tenant's data leave the
installation, which outlives a leaked token's revocation — and a token that tries is
`403 session_required`; switching it off only takes something away and stays open to an
administrator's `admin`-scope token ([`api/tenants.go`](../../backend/internal/api/tenants.go)
`UpdateTenant`).~~ *(The field is gone with the tenant's consent, 2026-10-04, ADR 0076.)* A session
the agent header marks is refused all ~~thirteen~~
([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D7).)* *(Amended 2026-10-04 for the global administrator's view,
[ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2: ~~thirteen~~ fourteen operations — listing every tenant of the installation, ~~`listTenants`~~ `listTeams` *(2026-10-10)*,
takes a session as well, for a reason of its own: it shows a global administrator the
installation's clients beyond the person's memberships, and a token of theirs keeps the reach of
those memberships, so a leaked one lists no other client. The same holds, in the request layer and
not in the document, for the reach into a tenant in which the global administrator holds no role:
a token's request there answers like an unknown tenant. A session the agent header marks is refused
all ~~fourteen~~.)* *(Amended 2026-10-04 by the owner's answers on the chat, ADR 0076, and built the
same day: ~~fourteen~~ sixteen operations. Stopping the person's running turns of the chat,
`stopChatTurns`, takes a session as a turn does — the turns are the session's person's, and a token
starts none. Choosing the chat's capabilities, `setMyChat` (`PUT /api/v1/me/chat`), takes a session
by the rule itself: the set is what the person's agent in the browser may do in every tenant of the
person, access that would outlive a leaked token's revocation; reading it, `getMyChat`, takes either
credential. A session the agent header marks is refused all ~~sixteen~~ — the chat cannot stop turns or
widen its own capabilities.)* *(Amended 2026-10-05 by the decision recorded in
[ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D7, built on the recommendation: ~~sixteen~~ seventeen operations. Purging a deleted ticket,
`purgeTicket`, takes a session by the rule itself: nothing undoes a purge, so what a leaked token did
there would outlive its revocation. Deleting a ticket and restoring it, which the bin undoes, stay
open to an administrator's `admin`-scope token. A session the agent header marks is refused all
~~seventeen~~.)* *(Amended 2026-10-05, the owner's answer to "which of a member's tokens does a tenant's
administrator see and revoke?": the tokens of their tenant's members are **the tokens that can act in
the tenant** — every token of a member that is unrestricted or restricted to this tenant. A token
restricted to another tenant is not shown, not even by its name or by the fact that it exists, and
neither is a token of a person who is no member here
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3). The
administrator reads the token's person and metadata — name, scope, agent flag, capabilities,
restriction, created, expires, last-used day, state — never its secret, and revokes it, immediately
and finally (D6), as an act of the tenant recorded in its audit (D9). Revoking an unrestricted token
ends it in every tenant of its person: that is what an unrestricted token is, the person makes a new
one in a session, and the page says so before it acts. Listing takes the administrator role and
`read` scope; revoking takes `admin` scope and no agent, and a token may, because it only takes
access away (the rule above) — neither is among the ~~seventeen~~ eighteen. An unrestricted token's name and
last-used day are its person's across their tenants, so they can say something of the person's work
elsewhere ([docs/security/tokens.md](../security/tokens.md#h-57) H-57). Built the same day.)*
*(Amended 2026-10-06 by the rule itself, for GitHub's webhook of
[ADR 0071](0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
D1, and built the same day: ~~seventeen~~ eighteen operations. Making the tenant's webhook secret,
or rotating it — one operation, `createGitHubSecret` —, takes a session: whoever learns the secret
writes links into the tenant and tells its people of merges, long after a leaked token that made it
was revoked. Revoking the secret only takes access away and stays open to an administrator's
`admin`-scope token. A session the agent header marks is refused all ~~eighteen~~.)*
*(Amended 2026-10-06 by the rule itself, for the consistency check of
[ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4, made concrete by the implementer, open to the owner's objection: ~~eighteen~~ nineteen
operations. Removing the orphaned objects of a consistency check, `removeOrphanedObjects`, takes a
session: nothing brings a removed object back, so what a leaked token did there would outlive its
revocation, as with the purge of a ticket. Reading the check takes either credential with `read`
scope, and accepting the loss of its missing files an administrator's `admin`-scope token as well:
the acceptance removes nothing, and a file whose bytes come back is whole again. A session the agent
header marks is refused all ~~nineteen~~. Built the same day.)*
*(Amended 2026-10-07 by the owner's answer to "do these three acts take a browser session?", over
keeping the token's reach and naming each as a gap, and built 2026-10-09: the rule decided them,
and they were missed when it was applied. **Each takes a session in its giving direction; taking
away stays open to a token.**
- **Unlocking a local account**, `unlockAccount` (`DELETE …/accounts/{username}/lockout`), takes a
  session — ~~nineteen~~ ~~**twenty operations**~~, which the unit test over the document holds. A
  token that could unlock an account between guesses would keep its lockout from ever holding,
  long after the token's revocation
  ([ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D5, D6).
  Listing, deactivating and ending the sessions stay open to an administrator's token.
- **Widening the tenant's settings** in `PATCH /api/v1/tenants/{tenant}` — switching
  `time_visible_to_members` or `members_create_projects` on, or moving `time_locked_until` earlier
  or clearing it, which opens closed days to writes again — is `403 session_required` for a token
  ([`tenants.go`](../../backend/internal/api/tenants.go) `gives`, `sessionToGive`). The other
  direction and the name stay open to an administrator's `admin`-scope token; the operation
  declares either credential, so the rule is the handler's, as the switched-on consent of the chat
  was before it went.
- **Lifting the confidential flag** (`PUT …/confidential` with `false`) takes a session; setting it
  stays open to a token ([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
  D3).
- **A token's assignment of a confidential ticket** to anyone but its own person or the assignee as
  it was — at a filing or on a change — is `403 session_required`; assigning nobody stays open
  (ADR 0065 D9, `mayAssign`).
A session the agent header marks is refused all ~~twenty~~, and the three acts by their hard-off rules.
Held by `TestWideningTheTenantSettingsTakesASession`,
`TestATokenAssignsAConfidentialTicketOnlyToItsPerson` (unit), and
`TestWideningTheTenantSettingsTakesASession`, `TestConfidentialTickets` and
`TestAccountRoutesAnAdministratorsTokenMayStillCall` (integration).)*
*(Amended 2026-10-09 with the removal of GitHub's webhook, [ADR 0071](0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md) Status:
~~twenty~~ **nineteen operations**. `createGitHubSecret` is gone with the webhook, and so is the
revocation that stayed open to a token. A session the agent header marks is refused all nineteen.)*

**D6 — Revocation is immediate and keeps the row.** Revoked and expired tokens stay listed
with their state; a revoked token answers `401` with the reason. *(Amended 2026-10-02: a
revocation is final — the database refuses an update that clears or changes it, whoever
writes it — and revoking a revoked token, also at the same moment, answers like revoking it
once.)* Deactivating a person
([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D5) revokes all their tokens.

**D7 — Transport.** `Authorization: Bearer cwk_…`, never a query parameter, never a cookie.
Token requests carry no cookie and are exempt from the CSRF check; the browser never holds a
token. *(Amended 2026-10-03: a request with an `Authorization` header is a token's whatever
else it carries, and its cookie is not looked at; the check and the temporary-password gate
belong to cookie requests
([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D6).)*

**D8 — The gate applies to tokens too.** A token's person is checked against the allow-list
and the mappings ([ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md))
on the first request after the refresh interval, with the person's stored groups snapshot;
a person who left the allow-list has no working token from that moment. *(Built 2026-10-04: for a
token of a person of the identity provider, on its first request after the person's last check plus
`COWORK_OIDC_GROUPS_REFRESH`, under the person's lock. The snapshot is the person's groups as of
their last login or session refresh, judged against the gate as configured at the check. Admitted,
the check is stamped and the memberships are derived from the snapshot; outside, the request is
`401 not_allowed` and nothing is written but the refusal of D9 — the token is refused, not revoked, and works again once
the person is admitted. A refresh or a login refused at the gate clears the stamp, so the person's
next token request meets the gate at once. A local account meets no gate. The snapshot is only as
fresh as the person's last browser login or refresh: a person who uses tokens only is judged on old
groups ~~until the token expires~~ *(amended 2026-10-04, below: until they are older than the
maximum age)* — [docs/security/identity-provider.md](../security/identity-provider.md) H-23.)* *(Amended after the
security review, 2026-10-04: a person who is not the configured issuer's — of another issuer, or of
a provider no longer configured — is outside the gate at every request, whatever their last check;
and the snapshot is the groups of the person's last login or of the last refresh that read them.)*
*(Amended 2026-10-04, the owner's answer to "how old may the groups be that judge a token?", over
any age as built and over a refresh token of the person's own that the gate would use to ask the
issuer: the snapshot judges a token only while it is no older than `COWORK_OIDC_GROUPS_MAX_AGE` — a
duration, seven days (`168h`) by default, which the configuration refuses unless it is longer than
`COWORK_OIDC_GROUPS_REFRESH`. Groups older than that — `users.oidc_groups_at` is when the person's
last sign-in, or the last session refresh that read them, read them — refuse every token of the
person at every request with `401 not_allowed`, whose detail says to sign in to the browser once, and
end an open event stream of such a token at its heartbeat; the refusal is recorded as D9 says. A
sign-in, or a refresh that reads the groups, makes the tokens work again. It is one comparison and no
further stored credential, and a weekly sign-in in the browser is part of the working day. A local
account has no groups and is not affected. Built in
[`api/identity.go`](../../backend/internal/api/identity.go) `tokenGate`, `streamStillAdmitted`,
`groupsTooOld`; `TestGroupsOlderThanTheMaximumAgeRefuseTheTokens`.)*

**D9 — Creation, use after expiry or revocation, and revocation are recorded acts;** use
itself is recorded through the audit rows of the acts the token performs (`token_id` in
each), not as a separate row per request. *(Amended 2026-10-02: a refused use is recorded at
most once per token, reason — expired or revoked — and hour; every refusal stays in the
request log. A forgotten configuration that retries with a dead token cannot grow the
append-only table without bound.)* *(Amended 2026-10-04: the gate's refusal of D8 is recorded the
same way, with the reason `not_allowed`.)*

## Consequences

- Claude's session gets one `write` token restricted to a tenant and, where the binding is
  one project, to that project; a leak of that token reaches one project for at most ninety
  days and is scannable by its prefix.
- No "service account" tokens: every token has a human owner, which is what makes the audit
  trail name a person (the attribution record follows).
- The owner renews tokens every ninety days; `last used` shows which ones are dead.
- Four configuration values and a token management page in the person's settings.

## Alternatives Considered

- **Fine-grained resource scopes** (`tickets:read`, `comments:write`, …). Precise; a scope
  list to maintain, a matrix in the UI, and in practice "everything" gets picked. Lost.
- **Optional unlimited lifetime.** An unlimited token in a forgotten MCP configuration file
  is the typical leak. Lost.
- **A thirty-day default.** Friction for daily sessions without a gain `last used` does not
  already give. Lost.
- **Administrators creating tokens for others.** A token an administrator has seen is not
  personal, and attribution breaks. Lost.
- **Re-authentication before creating a token.** Reasonable hardening; not in the first
  release, the session suffices. An amendment if the owner wants it.

## Residual risks

- A token is a bearer credential: whoever holds it is the person, within scope and lifetime.
  The mitigations are D1 (scannable), D3 (bounded reach), D4 (bounded time), D6 (revocable).
- D8's refresh means a token keeps working for up to the refresh interval after its person
  left the allow-list; the interval is the session record's fifteen minutes. *(Amended 2026-10-04:
  that holds for a change of the allow-list in the configuration. A change at the issuer reaches the
  tokens at the person's next browser login or session refresh — and never while the person uses
  tokens only ([docs/security/identity-provider.md](../security/identity-provider.md) H-23).)*

## References

- [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D6 — one resolver
- [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D6 — the role the token inherits
- [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md) — the gate that applies to tokens
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D1 — `token_id` in every audit row
- [ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md) D3 — the repository binding a project-restricted token serves
