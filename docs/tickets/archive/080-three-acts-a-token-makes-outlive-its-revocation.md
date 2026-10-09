---
id: T80
title: three acts a leaked token can make outlive its revocation, against the rule that such acts take a browser session
state: done
severity: medium
security: boundary
threat: the holder of a leaked admin-scope token of a tenant administrator turns members' sight of everyone's time on, lets members create projects or moves the time lock earlier (PATCH /api/v1/tenants/{tenant}); lifts a ticket's confidential flag (PUT …/confidential) or, with write scope, assigns a confidential ticket to an account of theirs; or unlocks a local account between guesses (DELETE …/accounts/{username}/lockout), so the per-username lockout never holds — each lasts after the token is revoked
urgency: next          # rule 3: severity medium, live; decided, waits for the build
effort: S
blocked-by:
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-09
shipped: 0.13.0: the unlock of a local account takes a session, and widening the tenant's settings, lifting the confidential flag and a token's assignment of a confidential ticket to another person take one in the handler
---

## Current state

ADR 0035 D5: an act whose effect outlives a leaked token's revocation takes a session. These do not:
`UpdateTenant` checks `administer` only (tenants.go:121-125, 146-149); the confidential flag's lift has no
session check (tickets.go:1103-1113) and the assignment rule of a confidential ticket binds agents only
(tickets.go:541-546, authorize.go:51-53); `unlockAccount` takes either credential (accounts.go:261-292).

## Open questions

### Q1: Do these three acts take a browser session?

- **(a) Yes, each in its giving direction**: the tenant settings that widen sight or rights, the confidential lift and a token's assignment of a confidential ticket to anyone but its own person or the current assignee, and the unlock — a token gets `403 session_required`; taking away stays open to a token. ADR 0035 D5, ADR 0065 D3/D9 and ADR 0033 amended in place, integration tests, the session-only count rising past nineteen.
- **(b) Keep the token's reach** and name each as an accepted gap in the security pages.

Recommended: **(a)** — the rule of ADR 0035 D5 already decides this; these three were missed when it was applied.

**Answer:** (a) — the owner, 2026-10-07. Each act takes a session in its giving direction; the
amendments of ADR 0035 D5, ADR 0065 D3/D9 and ADR 0033 land with the fix, in the same change, so no
tracked record names the gap before it is closed.
