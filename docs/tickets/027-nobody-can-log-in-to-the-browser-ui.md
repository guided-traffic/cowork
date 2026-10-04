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
pages — "create the first tenant" only while `listTenants` finds none
([`home.ts`](../../frontend/src/app/features/home/home.ts#L97-L103)). The README reference,
[sessions.md](../security/sessions.md), [local-accounts.md](../security/local-accounts.md) and
[csrf.md](../security/csrf.md) describe it, and `make dev` logs the browser in through it over
HTTPS; the dev server's proxy holds no credential ([`proxy.conf.mjs`](../../frontend/proxy.conf.mjs)).
Its end-to-end paths run in the tier of T29 — the local form, a temporary password, *Sign in with
Dex*, the cookie, a write past the CSRF check and the sign-out, in Chromium and WebKit. Left:

- **A token names its project restriction by id.** `Token` carries `restricted_project_id`
  ([`schemas.yaml`](../../backend/api/components/schemas.yaml#L177-L180)) beside the tenant's slug,
  while `CurrentToken` names the project by its key, `restricted_project`
  ([`token.go`](../../backend/internal/api/token.go#L57-L68)). The tokens page loads every project
  of each restricting tenant to show a key, and shows the id where that fails
  ([`tokens.service.ts`](../../frontend/src/app/core/tokens.service.ts#L38-L76)).
- **The token form does not know the installation's longest lifetime.** It accepts up to the
  schema's 3650 days ([`new-token-dialog.ts`](../../frontend/src/app/features/me/new-token-dialog.ts#L46-L50));
  the server shortens a longer request to `COWORK_TOKEN_MAX_LIFETIME` and the page shows the expiry
  afterwards. No answer carries the bound — `GET /auth/options` carries the password policy
  (`password_min_length`) and nothing of the tokens.

## Required changes

1. `Token` names its project restriction by key, `restricted_project`, as `CurrentToken` does; the
   tokens page reads it and its lookup goes. The API document says what becomes of
   `restricted_project_id` ([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md)).
2. The installation's longest token lifetime reaches the form — an answer names
   `COWORK_TOKEN_MAX_LIFETIME` as `GET /auth/options` names `COWORK_PASSWORD_MIN_LENGTH` — and the
   dialog's bound and hint follow it instead of the schema's 3650 days.
3. Unit tests for the tokens page and the dialog; an integration test for each new field.

## Verified

- Playwright against `make dev`: Chromium and WebKit are sent to the login, log in as
  `dev`, keep `__Host-cowork-session` (Secure, HttpOnly, Lax), open the live stream, write a
  comment through the session (the CSRF check passes from `https://localhost:4200`), sign out and
  are sent to the login again. Over plain `http://localhost` WebKit dropped the cookie, which is
  why development serves HTTPS.
