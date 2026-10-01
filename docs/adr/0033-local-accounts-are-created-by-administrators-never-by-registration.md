# ADR 0033: Local Accounts Are Created by Administrators, Never by Registration — Length-Only Password Policy With a Configurable Minimum, Rate Limits, Lockout, No MFA in the First Release

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "local
accounts beyond the one administrator?": administrator-managed local accounts, over none,
over self-registration with e-mail reset, and over global-administrator-only creation. The
owner set two conditions: the minimum password length is configurable in the chart, and
there is no special-character requirement. The remaining rules of D3–D7 were put to the
owner with the question and not objected to.

**Not built.** No `users` table, no password code.

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

**D2 — Identity and separation.** A local account's identity is `local:<username>`; the
username is unique in the installation. A local account and an OIDC identity are two
persons; no linking in the first release. The member list shows the origin ("local").

**D3 — The password policy is length only, and the minimum is configuration.**
`COWORK_PASSWORD_MIN_LENGTH` (chart `auth.local.passwordMinLength`), default 12, hard floor
8: a configured value below 8 refuses the start. No required character classes, no required
rotation, no reuse history. The policy applies to every local account, the administrator of
ADR 0032 included; ADR 0032 D3 is amended accordingly.

**D4 — Storage and change.** Argon2id with parameters recorded in the security page and
raised by amendment as hardware moves. A password set by an administrator is temporary: the
person must change it at first login before anything else. A change of password ends every
other session of the account ([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md)
D4). A password appears in no log, no audit row, no error text.

**D5 — Reset is an administrator's act.** A tenant administrator (for accounts of their
tenant) or a global administrator sets a new temporary password; there is no e-mail flow
([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) keeps e-mail out). The act
is recorded; the person changes the password at the next login.

**D6 — Rate limits and lockout.** Five failed attempts per account within fifteen minutes
lock the account until an administrator unlocks it or the window passes, whichever the
installation configures (`COWORK_LOGIN_LOCKOUT`, default: window); twenty attempts per source
address per minute are throttled with `429`. Failed and locked attempts are recorded without
the attempted password. The administrator of ADR 0032 is subject to the same limits, and the
operations page says how to recover it when it is locked (rotate the Secret, restart).

**D7 — No second factor for local accounts in the first release, and this is said aloud.**
The security page carries it as an open gap with an `H-<n>` identifier; the mitigations are
D1 (no public registration), D3 (length), D6 (limits and lockout), and the ability to
deactivate any local account ([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D5).

**D8 — The login page offers both ways only when both exist.** The OIDC button appears when
an issuer is configured; the local form appears when at least one active local account
exists; an installation with neither shows the "not configured" notice of
[ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D4.

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
  the account deactivated; D6 slows guessing, not phishing.
- D3's floor of 8 is the lowest defensible value; an installation that sets it has accepted
  that in its values file.
- Two persons for one human (D2) when someone has both a local and an OIDC account; linking
  is an amendment if it becomes a nuisance.

## References

- [ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md) — the first local account, amended here
- [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md) D3 — the marked grant a local account enters a tenant with
- [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D4 — sessions ended on a password change
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) — why there is no e-mail reset
