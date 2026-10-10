---
id: T90
title: two first logins of the same person at once answer one of them with a 500
state: done
severity: low         # the second login fails once and works when repeated; no data is lost
security: none
threat:
urgency: later        # rule 4: a cheap known fix
effort: XS
filed-from: the end-to-end run of the relations across teams, 2026-10-10
opened: 2026-10-10
decided:
done: 2026-10-10
shipped: 0.16.1 — a login takes a lock of the issuer and the subject before it reads the person, so the second of two first logins at once finds the person the first made
---

## Current state

A person of the identity provider is made at their first login, in one transaction under the person's
advisory lock ([ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md),
[ADR 0027](../adr/0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D5). But the lock is keyed on the person's id, and a new person has none until the insert:
`keepPerson` reads the person by issuer and subject, finds none, and `createPerson`
([identity.go:467-485](../../backend/internal/store/identity.go#L467-L485)) inserts the row and only
then takes the lock (`forPerson`, [identity.go:222-229](../../backend/internal/store/identity.go#L222-L229)).
Two logins of the same new person at the same moment — two tabs, a double click on the sign-in, two
end-to-end tests signing in one fresh identity — both find no row and both insert; the second hits
`users_oidc_identity_key` ([000020_identity_provider.up.sql:33](../../backend/internal/store/migrations/000020_identity_provider.up.sql#L33))
and the callback answers `500`.

Seen once in the end-to-end tier on 2026-10-10 (the Dex sign-in path in chromium-light: two tests signed
in the same new person at once); the code is the same on main.

## Required changes

- The login takes a lock keyed on the identity — the issuer and the subject — before it reads the person
  (or inserts with `ON CONFLICT (oidc_issuer, oidc_subject) DO NOTHING` and reads the row back), so the
  second login finds the person the first made and carries on as a returning person's login.
- The order of the locks stays as docs/developer/data-access.md states it (the identity's lock before
  the tenants' and the persons'); the page says where the new lock stands.

### The tests that prove it

- The integration tier: two logins of the same new identity at once both succeed, one person exists,
  one `created` act — failing before the fix.
