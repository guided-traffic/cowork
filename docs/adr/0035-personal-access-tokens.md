# ADR 0035: Personal Access Tokens — `cwk_` Prefixed, Hashed at Rest, Scoped `read`/`write`/`admin` With Optional Tenant and Project Restriction, Expiring by Default, Created Only by the Person in a Session

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"personal access token design?" at its three contested points: three hierarchical scopes
with optional tenant and project restriction; mandatory expiry with a ninety-day default and
a one-year maximum; creation only by the person themselves in a browser session, never by an
administrator for someone else and never through a token. The fixed part and the rules of
D7–D9 were put to the owner with the question and not objected to.

**Not built.** No `tokens` table, no bearer resolver.

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
stored as a hash in the audit row, not in the token), revoked at and by whom.

**D3 — Scopes are `read`, `write`, `admin`, hierarchical, and never exceed the person.**
`read` reads what the person may read; `write` additionally does what a `member` may;
`admin` additionally does what the person's `admin` role allows. A token's effective
permission is the intersection of its scope, its person's role in the tenant (ADR 0034) and
the agent restrictions (the next records). A token restricted to a tenant is invalid
elsewhere; one restricted to a project of that tenant is invalid outside it — which is the
token a repository binding wants.

**D4 — Expiry is mandatory.** Default ninety days, maximum one year, both configurable
(`COWORK_TOKEN_DEFAULT_LIFETIME`, `COWORK_TOKEN_MAX_LIFETIME`); no token without an expiry.
An expired token answers `401` with the reason.

**D5 — Only the person creates their tokens, in a browser session.** No endpoint creates a
token when the caller is a token; no administrator creates a token for someone else. An
administrator sees the tokens of their tenant's members — metadata, never plaintext — and may
revoke them.

**D6 — Revocation is immediate and keeps the row.** Revoked and expired tokens stay listed
with their state; a revoked token answers `401` with the reason. Deactivating a person
([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D5) revokes all their tokens.

**D7 — Transport.** `Authorization: Bearer cwk_…`, never a query parameter, never a cookie.
Token requests carry no cookie and are exempt from the CSRF check; the browser never holds a
token.

**D8 — The gate applies to tokens too.** A token's person is checked against the allow-list
and the mappings ([ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md))
on the first request after the refresh interval, with the person's stored groups snapshot;
a person who left the allow-list has no working token from that moment.

**D9 — Creation, use after expiry or revocation, and revocation are recorded acts;** use
itself is recorded through the audit rows of the acts the token performs (`token_id` in
each), not as a separate row per request.

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
