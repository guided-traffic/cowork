---
id: T27
title: a token names its project restriction by id, and the token form does not know the installation's longest lifetime
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: two cheap known fixes
effort: S
blocked-by:
filed-from: T26, the owner's answer of 2026-10-03 (the local login moves from phase 4 into phase 3)
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The login is released: sessions ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md)),
the local administrator ([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)),
local accounts ([ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)),
CSRF ([ADR 0037](../adr/0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md)),
the creation of tenants and tokens, and the UI's login, password, tokens, accounts and first-tenant
pages. Its end-to-end paths are T29's.

Built on this branch, both items of the work list:

- **A token names its project restriction by key.** `Token` carries `restricted_project`, the key,
  `null` where the person no longer sees the project in a tenant they belong to
  ([`me.go`](../../backend/internal/api/me.go) `projectKeys`); `restricted_project_id` stays,
  marked `deprecated` in the API document, because `/api/v1` keeps what a client reads
  ([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md) D7). The tokens page
  shows the key and no longer loads the projects of each restricting tenant
  ([`tokens.service.ts`](../../frontend/src/app/core/tokens.service.ts)).
- **The token form knows the installation's longest lifetime.** `GET /auth/options` answers
  `token_max_lifetime_days`, `COWORK_TOKEN_MAX_LIFETIME` in whole days rounded down
  ([`login.go`](../../backend/internal/api/login.go) `GetAuthOptions`), and the dialog's bound and
  hint follow it within the schema's 3650 days
  ([`new-token-dialog.ts`](../../frontend/src/app/features/me/new-token-dialog.ts)).

The README reference, [tokens.md](../security/tokens.md), [frontend.md](../developer/frontend.md)
and ADR 0035's Status carry both.

## Required changes

None left. The ticket closes by extraction — done — and moves to the archive once this work is
merged.

## Verified

- `TestATokenNamesItsProjectByKey`, `TestTokenCreationRules` and `TestAuthOptions` (integration);
  `tokens.service.spec.ts`, `tokens.spec.ts` and `new-token-dialog.spec.ts` (the installation's
  maximum, a day more refused, an answer that arrives late, a maximum under a day).
- Playwright against `make dev` (before this branch): Chromium and WebKit are sent to the login,
  log in as `dev`, keep `__Host-cowork-session` (Secure, HttpOnly, Lax), open the live stream,
  write a comment through the session (the CSRF check passes from `https://localhost:4200`), sign
  out and are sent to the login again. Over plain `http://localhost` WebKit dropped the cookie,
  which is why development serves HTTPS. Not repeated against this branch: the browser was not
  run.
