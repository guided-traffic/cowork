# ADR 0035: Personal Access Tokens — `cwk_` Prefixed, Hashed at Rest, Scoped `read`/`write`/`admin` With Optional Tenant and Project Restriction, Expiring by Default, Created Only by the Person in a Session

## Status

Accepted, amended 2026-10-02 (D2: the address hash comes with the trust rule for forwarded
addresses; D3: creating a project is a `write` act, and what a restricted token reaches on the
person's own routes and on the tenant-level reads; D6: a revocation is final; D9: refused uses
are recorded at most once per token, reason and hour) and 2026-10-03 (D2: the trust rule for
forwarded addresses is decided; D4, D5: how creation is built, and what else only a session
makes). Date: 2026-10-01. Decided by the owner as the answer to the
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
`COWORK_TRUSTED_PROXIES`, the chart's NetworkPolicy;
[docs/security/local-accounts.md](../security/local-accounts.md)); in the UI, the person's tokens
page lists, creates — the plaintext shown once, in a dialog that forgets it when it closes — and
revokes ([`features/me/tokens.ts`](../../frontend/src/app/features/me/tokens.ts)). Not built: D5's administrator
view of the tokens of their tenant's members and their revocation of them; D2's address hash in
the audit row, which waits for the phase that builds the identity provider; D8 (the gate), which
belongs to the identity provider.

## Context

An LLM operates cowork before any UI exists ([project plan](../planning/project-plan.md),
phase 2), and it does so with a token that must say who is accountable
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
list empty the peer is the client. The chart's NetworkPolicy admits only the frontend pods to the
backend, so no other pod can write the header to it. The login throttle of
[ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D6 counts
that address — an IPv6 address by its /64 — keyed-hashed, in its own table; the hash in the audit row stays for the phase that
builds the identity provider, and no audit row carries an address yet)*), revoked at and by whom.

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
the set to those six. The administrator's view of their tenant's members' tokens is not built.)*

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
a person who left the allow-list has no working token from that moment.

**D9 — Creation, use after expiry or revocation, and revocation are recorded acts;** use
itself is recorded through the audit rows of the acts the token performs (`token_id` in
each), not as a separate row per request. *(Amended 2026-10-02: a refused use is recorded at
most once per token, reason — expired or revoked — and hour; every refusal stays in the
request log. A forgotten configuration that retries with a dead token cannot grow the
append-only table without bound.)*

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
  left the allow-list; the interval is the session record's fifteen minutes.

## References

- [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D6 — one resolver
- [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D6 — the role the token inherits
- [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md) — the gate that applies to tokens
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D1 — `token_id` in every audit row
- [ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md) D3 — the repository binding a project-restricted token serves
