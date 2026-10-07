# ADR 0033: Local Accounts Are Created by Administrators, Never by Registration — Length-Only Password Policy With a Configurable Minimum, Rate Limits, Lockout, No MFA in the First Release

## Status

Accepted, amended 2026-10-03 (D1, D4, D5, D6, D8 made concrete by the first implementation; D1,
D5 and D6 again by the owner's answers of the same day, below; D6 once more the same day: an
IPv6 client counts by its /64), amended 2026-10-04 (D6: behind the Ingress the TCP peer is a
controller pod, since the Ingress routes `/api/` and `/auth/` to the backend,
[ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3), amended 2026-10-06 (D3 and the References: they say that
[ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
D3 names this minimum in place instead of claiming to amend it, by the owner's rule that every
amendment is made in place in the record it changes; no rule changes), amended 2026-10-06 (D8: the
page that offers the identity provider's button starts that sign-in by itself after a session
ended, by
[ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) D6;
what it offers does not change). Date: 2026-10-01. Decided
by the owner as the answer to the catalog question "local accounts beyond the one administrator?": administrator-managed local accounts, over none,
over self-registration with e-mail reset, and over global-administrator-only creation. The
owner set two conditions: the minimum password length is configurable in the chart, and
there is no special-character requirement. The remaining rules of D3–D7 were put to the
owner with the question and not objected to.

The owner's answers of 2026-10-03 to two questions the first implementation raised: creating a
local account and resetting its password take a browser session only (D1, D5), over leaving them
open to an administrator's token — a leaked `admin`-scope token must not become access that
survives its revocation —; and the login throttle counts the client address, found by walking
`X-Forwarded-For` from the right through configured trusted proxies (D6), over counting the TCP
peer, which ~~behind the frontend is nginx~~ *(since 2026-10-04 behind the Ingress is a controller
pod)*, and over trusting the header as it comes.

**Partly built** (phase 3, 2026-10-03): D1–D8 for the browser login — `local_accounts`,
`login_attempts` and `login_locks` (migration 15), Argon2id
([`auth/password.go`](../../backend/internal/auth/password.go)), `POST /auth/local`, the
temporary-password gate, the lockout and the throttle, the accounts routes of a tenant, and
`GET /auth/options`
([docs/security/local-accounts.md](../security/local-accounts.md)). Not built: a global
administrator's creation of an account for a tenant they do not administer or for none (D1);
the origin on the member list (D2), which shows the username that marks a local account. The UI
has the login page with the local form as `/auth/options` offers it (D8), the password page a
temporary password leads to first (D4), and the accounts page of a tenant's administrators —
create, reset, unlock, deactivate, end the sessions
([`features/auth/`](../../frontend/src/app/features/auth/),
[`features/tenant/accounts.ts`](../../frontend/src/app/features/tenant/accounts.ts)).

## Context

[ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
gives an installation one local administrator and leaves further local accounts open. The
owner names a real case: a person who has no identity in the provider and wants none. The
alternative — operating an identity provider for that one login — is work the owner should
not have to do. A local account bypasses the OIDC gate of [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D1 by nature, so something else has to be the gate; and a password store without a second
factor has to be defensible by the rules around it. Password composition rules (classes of
characters) are known not to help and to produce worse passwords; length does help.

## Decision

**D1 — A local account is created by an administrator and by nothing else.** A tenant
administrator creates an account for their tenant and the account receives a marked grant
there (ADR 0030 D3); a global administrator creates an account for any tenant or none. There
is no self-registration and no invitation link. The creation is the gate for local accounts
and is a recorded act ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)).
*(Amended 2026-10-03: built as `POST /api/v1/tenants/{tenant}/accounts`, for a tenant's
administrators with `admin` scope — never an agent — with a username, a display name, a role and
a temporary password. **It takes a browser session only:** a token, an administrator's included,
is `403 session_required` before anything is written, because an account made with a leaked
token would outlive the token's revocation, and so would the password the administrator chose.
The account gets a marked grant with that role in the tenant and is **managed by** it, which D5
relies on. A global administrator has no role in a
tenant until they grant themselves one
([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2), so they create accounts in the tenants they administer; creating one for another tenant,
or for none, is not built.)*

**D2 — Identity and separation.** A local account's identity is `local:<username>`; the
username is unique in the installation. A local account and an OIDC identity are two
persons; no linking in the first release. The member list shows the origin ("local").

**D3 — The password policy is length only, and the minimum is configuration.**
`COWORK_PASSWORD_MIN_LENGTH` (chart `auth.local.passwordMinLength`), default 12, hard floor
8: a configured value below 8 refuses the start. No required character classes, no required
rotation, no reuse history. The policy applies to every local account, the administrator of
ADR 0032 included, whose D3 names this minimum in place.

**D4 — Storage and change.** Argon2id with parameters recorded in the security page and
raised by amendment as hardware moves. A password set by an administrator is temporary: the
person must change it at first login before anything else. A change of password ends every
other session of the account ([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md)
D4). A password appears in no log, no audit row, no error text. *(Amended 2026-10-03: the
parameters in force are 19 MiB of memory, two iterations and one lane — OWASP's minimum —
recorded in every PHC-encoded hash, which is verified with the parameters it records;
a malformed or oversized hash is a password that does not fit, never a `500`. At most two
computations run at once, so a flood of attempts holds no more than twice 19 MiB. A password is
at most 1024 characters, counted in characters, and an administrator's temporary password is
held to the same policy.)*

**D5 — Reset is an administrator's act.** A tenant administrator (for accounts of their
tenant) or a global administrator sets a new temporary password; there is no e-mail flow
([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) keeps e-mail out). The act
is recorded; the person changes the password at the next login. *(Amended 2026-10-03: built as
`PUT /api/v1/tenants/{tenant}/accounts/{username}/password`; every session of the account ends.
A tenant's administrators reset only the accounts their tenant created. A person is one account
across the whole installation — its password and its sessions are not a tenant's — so an
administrator who could reset the password of a person they merely share a tenant with could
enter every other tenant of that person, and a global administrator's account in particular;
any other account answers `404`, like a username nobody has. The same rule holds for unlocking,
for deactivating and for ending the sessions, in the API and in the policies of the tables. An
administrator never resets, unlocks or deactivates their own account: their own password is
`PUT /api/v1/me/password`, which asks for the current one and counts a wrong one toward the
lockout. The local administrator's password is not changeable through the API at all
([ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
D2). **The reset takes a browser session only**, for the reason of D1: a password reset with a
leaked token is access that survives the token's revocation. Listing the accounts, unlocking one,
deactivating one and ending its sessions remove or restrict access, leave nothing behind, and
stay open to an administrator's token.)*

**D6 — Rate limits and lockout.** Five failed attempts per account within fifteen minutes
lock the account until an administrator unlocks it or the window passes, whichever the
installation configures (`COWORK_LOGIN_LOCKOUT`, default: window); twenty attempts per source
address per minute are throttled with `429`. Failed and locked attempts are recorded without
the attempted password. The administrator of ADR 0032 is subject to the same limits, and the
operations page says how to recover it when it is locked (rotate the Secret, restart).
*(Amended 2026-10-03: the failures are counted by the username **as presented**, known or not,
so an unknown username is counted and locked exactly like a known one and neither the answer
nor the lockout says whether an account exists. Every failure — an unknown username, a wrong
password, a locked or a deactivated account — is the same `401 invalid_credentials` after one
Argon2id computation, against the account's hash or a dummy. `COWORK_LOGIN_MAX_FAILURES`
(default 5; `0` locks never) failures within the fixed window of fifteen minutes lock the
username; with `COWORK_LOGIN_LOCKOUT=window` the lock ends when the window passes, with `admin`
the lock of an existing account stays until an administrator of its managing tenant unlocks it
(`DELETE …/lockout`) — the lock of an unknown name still ends with the window, so nobody can
make permanent locks for names at will. `COWORK_LOGIN_ADDRESS_LIMIT` (default 20; `0` switches
the throttle off) attempts per client address and minute *(amended 2026-10-03: an IPv6 address
counts by its /64, the network one subscriber is given — per address, its 2^64 addresses would
each be a fresh bucket)* are `429 too_many_attempts` with
`Retry-After`, before any hash is computed, and a throttled attempt is not counted. **The
address is the client's**, found under the trust rule of
[ADR 0035](0035-personal-access-tokens.md) D2: the TCP peer, or — when the peer is inside
`COWORK_TRUSTED_PROXIES`, a list of CIDRs that is empty by default — the first address of
`X-Forwarded-For`, from the right, that is not a trusted proxy; entries to its left are never
read. It is hashed with a key derived from `COWORK_SESSION_KEY` and never stored. With the list
empty, ~~behind the frontend's nginx, the peer is nginx and the throttle is one for the whole
installation~~ *(amended 2026-10-04: behind the Ingress the peer is a controller pod, and the
throttle is one for every browser behind it)*; a list that is too wide lets a client choose its
address (open gap H-17 of the security page). The failed and locked attempts, the locks and the unlocks are audit rows of the
system actor `system:login`, without the attempted password and without a username that names no
account.)*

**D7 — No second factor for local accounts in the first release, and this is said aloud.**
The security page carries it as an open gap with an `H-<n>` identifier; the mitigations are
D1 (no public registration), D3 (length), D6 (limits and lockout), and the ability to
deactivate any local account ([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D5).

**D8 — The login page offers both ways only when both exist.** The OIDC button appears when
an issuer is configured; the local form appears when at least one active local account
exists; an installation with neither shows the "not configured" notice of
[ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D4. *(Amended 2026-10-03: built as `GET /auth/options`, which answers `local` when at
least one active local account exists and `oidc: false`, since no identity provider is built.
The login page itself belongs to the frontend.)* *(Amended 2026-10-06: where the browser remembers
that the person signs in through the identity provider, the page that offers its button starts that
sign-in by itself at the person's first input after a session ended, and still shows the button and
the local form while it waits
([ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D6); a local sign-in that succeeds forgets the provider, so the page waits for the person again.
What the page offers does not change.)*

## Consequences

- A person without an identity provider can work in cowork; the operator does not run a
  provider for them.
- Password code enters the backend: hashing, change, temporary flag, lockout, limits — each
  with tests in the integration tier, each described in the security page.
- The tenant boundary holds for local accounts as for everyone: a tenant administrator can
  only grant into their own tenant (ADR 0030 D3), and row-level security does not care how a
  person logged in.
- Two configuration values more (`COWORK_PASSWORD_MIN_LENGTH`, `COWORK_LOGIN_LOCKOUT`); the
  README's table grows in the change that builds them.

## Alternatives Considered

- **No further local accounts; the minimal Dex holds static users.** One password entry in
  cowork, no reset or lockout code; one identity provider to run for one person. Lost.
- **Self-registration with e-mail reset.** A public identity provider with SMTP, templates,
  token links and abuse handling. Lost.
- **Global administrators only may create local accounts.** Tighter; a client's
  administrator would have to ask the owner to add a colleague, the wrong bottleneck in a
  team product. Lost.
- **A fixed minimum length and character classes.** Classes are known not to help; a fixed
  length the owner found too strict. Lost to D3's configurable, length-only rule with a floor.

## Residual risks

- D7, by name. A phished or reused local password is a full account until it is changed or
  the account deactivated; D6 slows guessing, not phishing
  ([docs/security/local-accounts.md](../security/local-accounts.md), H-16).
- *(Added 2026-10-03.)* The throttle is as good as `COWORK_TRUSTED_PROXIES` is right: empty, it
  is one limit for the installation; too wide, a client picks its own bucket; and one address is
  one bucket, so a client with many addresses is not slowed by it (H-17).
- *(Added 2026-10-03.)* An administrator knows the temporary password they set, and the account
  stays the creating tenant's for good: a person who belongs to several tenants has one
  password that one tenant's administrators manage (H-19). A lockout is a lever against its
  owner: anyone who knows a username can lock it (H-18). Both are named on the security page.
- D3's floor of 8 is the lowest defensible value; an installation that sets it has accepted
  that in its values file.
- Two persons for one human (D2) when someone has both a local and an OIDC account; linking
  is an amendment if it becomes a nuisance.

## References

- [ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md) — the first local account, whose password takes this record's minimum (D3 there)
- [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md) D3 — the marked grant a local account enters a tenant with
- [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D4 — sessions ended on a password change
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) — why there is no e-mail reset
- [ADR 0035](0035-personal-access-tokens.md) D2 — the trust rule for forwarded addresses the throttle counts under
- [`backend/internal/api/login.go`](../../backend/internal/api/login.go), [`backend/internal/api/accounts.go`](../../backend/internal/api/accounts.go), [migration 15](../../backend/internal/store/migrations/000015_local_accounts.up.sql) — the implementation
