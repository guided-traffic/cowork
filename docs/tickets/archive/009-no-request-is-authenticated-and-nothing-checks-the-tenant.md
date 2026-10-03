---
id: T9
title: no request is authenticated, nothing checks the tenant before a handler, and nothing tells a viewer from an admin or an agent from its person
state: done
severity: high
security: hardening
threat: authenticates every API request by its token, answers an unknown tenant, a missing membership and a token restricted elsewhere with the same 404, and intersects scope, role, project visibility and the agent rules before a handler acts — additionally covering H-1 for the API, any peer that reaches either port being every user
urgency: later        # rule 4: decided fix (the ADRs)
effort: L
blocked-by: T8
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: bearer authentication, the agent mark, authorization by role, scope and capability, the tenant boundary, make dev-seed
---

## Current state

Decided by [ADR 0035](../adr/0035-personal-access-tokens.md) D1, D3, D4, D6, D7, D9,
[ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D1–D4, D7, [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D1–D5, D7, [ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D5, D6,
[ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D1, D3, D4, D6, D8, D9, [ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D6,
[ADR 0047](../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D5 and
[ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D3, D6, D7. On `main`:

- No route checks an identity ([`server.go`](../../backend/internal/httpserver/server.go));
  [trust-boundaries.md](../security/trust-boundaries.md) names it H-1, "There is no
  authentication and no tenancy", live.
- The plan put role checks and the project restriction into phase 4 and the agent rules into
  phase 5. ADR 0035 D3 (a token's permission is its scope ∩ its person's role ∩ the agent
  restrictions), ADR 0034 D4, ADR 0036 D7 and ADR 0043 D1, D5 bring all three into this phase;
  the restriction's administration routes stay in phase 4, so phase-2 tests seed
  `project_access`.
- No route creates a person, a tenant or a token in phase 2 (ADR 0038 D6): the fixture of T7
  and T8 does, over the administrative connection; `make dev-seed` (D7) does not exist.
- ADR 0036 D3 calls the header's parts "printable" without naming a character set.
- [trust-boundaries.md:3-7](../security/trust-boundaries.md#L3-L7) sends readers to "the open
  decisions in docs/planning/questions.md", which ADR 0074 made a tombstone.

## Required changes

1. **The resolver** (the token half of ADR 0031 D6's one resolver): `Authorization: Bearer
   cwk_…` is hashed with SHA-256 and looked up with `app.token_hash`, then `app.user_id` is set
   to its person; unknown or malformed → `401 unauthenticated`, expired → `401 token_expired`,
   revoked → `401 token_revoked` (ADR 0035 D4, D6 "with the reason"), each with
   `WWW-Authenticate: Bearer`. A refused use of an expired or revoked token is an act of its own
   — `refused`, the token's person, the token, the reason; tenant-level when the token is
   restricted to a tenant, installation-level otherwise — written in its own transaction at most
   once per token, reason and hour (ADR 0035 D9); every refusal stays in the request log.
   `last_used_on` through the bookkeeping writer, once per token and UTC day.
2. **Agent marking** (ADR 0036): a flagged token marks every request it makes;
   `X-Cowork-Agent: <name>/<model>/<session>` — three parts of 1–64 characters each, ASCII
   `0x20`–`0x7E` except `/`, no leading or trailing space — refines the mark, or marks a request
   of a plain token; a malformed header → `400 validation_failed` with the pointer
   `header:X-Cowork-Agent`; a flagged token without the header records `unknown-agent`; no
   header value unmarks a flagged token. A marked request of a plain token gets ADR 0043 D4's
   default set. Every agent act's audit row records the agent and the capabilities that applied
   (ADR 0043 D5). ADR 0036 D3 amended in place with the character set.
3. **The tenant boundary** (ADR 0023 D5), attached per operation from its security requirement,
   so a later route with a scheme of its own can opt out: the slug and the person's effective
   role in one query; an unknown slug, no membership, or a token restricted to another tenant →
   `404 not_found` with a body identical except `instance` and `request_id` (ADR 0047 D5); then
   `InTenant`.
4. **Authorization**, one helper: the person's role `>=` the route's → else `403 forbidden`;
   the scope — `read` safe methods, `write` member acts and project creation (ADR 0034 D9),
   `admin` admin acts — → else `403 insufficient_scope`; the visible projects (unrestricted, on
   the list with the lower of tenant role and entry, or tenant admin; ∩ the token's project
   restriction) → else `404`; the agent rule — baseline, a capability, or a hard-off rule (ADR
   0043 D2–D4) → else `403 agent_forbidden` with the capability or the rule in `detail`. Each
   operation declares its role, scope and agent rule in the document, and T5's unit test fails an
   operation that declares none. An agent act no record decides gets the open variant, named in
   the ticket that builds its route.
5. **Fixture and development entry:** each fixture identity gets a plain token and flagged ones
   with the "full" and the "assisted" set, plus a token restricted to a project; `make dev-seed`
   (ADR 0038 D7) creates a person, a tenant, an `admin` membership and a plain `write` token
   against `make postgres-up`'s database and prints the token once.
6. **The cross-tenant harness** in the API suite — the generated client against `httptest`,
   PostgreSQL as the runtime role: a table of route families every later child appends to (T10
   adds the first); for each, a person of A and a token restricted to A get the unknown-slug
   `404` on B's routes.
7. **Tests**, through `httptest` handlers behind the middleware until T10's routes exist: valid,
   expired, revoked and unknown tokens; a refused use recorded once per hour and reason;
   `last_used_on` once per day; the header table (0, 2 and 4 parts, an empty part, 65 characters,
   a control character, edge spaces); no header value removes a flag's effect (ADR 0036's
   residual risk); the audit cases of ADR 0036's Consequences that exist without sessions (plain
   token, plain token with the header, flagged with and without it); a project-restricted token
   sees one project. The scope × role matrix and, per capability, an allowed and a refused test,
   per hard-off rule a refused one (ADR 0043's residual risk), come with each route.
8. **Docs and records:** new docs/security/tokens.md — format and hash, scope ∩ role ∩ agent
   rules, restrictions and their `404`, expiry and revocation reasons, the refused-use bound, the
   agent flag, header and capabilities, the hard-off list; its gaps, numbered from `H-2` on in the
   order written: whoever holds the administrative database credential can mint a token for any
   person through the fixture (ADR 0038's residual risk); agent acts no record restricts are
   allowed, each listed by the ticket that builds it. New docs/security/tenancy.md — the two lines
   (the request layer's boundary and the policies), the settings and the named policies, the
   composite keys, the indistinguishable `404`, the project-restriction predicate and the fact
   that no phase-2 route restricts a project, so the predicate acts only on seeded rows until
   phase 4's administration; its gap: an owner credential given to `cowork serve` outside the
   chart keeps the split against defects but not against a compromise (ADR 0021's residual
   risk). trust-boundaries.md rewritten: its opening, H-1 narrowed to what remains (both Services
   stay reachable inside the cluster; a token is a bearer credential), the forwarded headers
   (still read by nothing — the address hash waits for a trust rule, ADR 0035 D2). New
   docs/developer/authentication.md and its row in the developer README; architecture.md (the
   request path); the README's naming (the token prefix, the agent header) and its fast start
   (`make dev-seed`); ADR 0023, 0031 (D6's token half), 0034, 0035, 0036, 0043 Status and index
   rows.

## Related

- T8 — the tokens and the audit row the resolver fills.
- T10 — the first routes behind the resolver and the boundary.
- T12 — the predicates and the agent gates the ticket routes apply.
- T22 — the agent acts built open, reviewed after experience.
