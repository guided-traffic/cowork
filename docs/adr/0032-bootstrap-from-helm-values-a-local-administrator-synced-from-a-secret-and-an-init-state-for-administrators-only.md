# ADR 0032: Bootstrap From Helm Values Alone — a Local Administrator Synced From a Secret at Start, an Init State Only Administrators May Enter, and an Optional Bootstrap Tenant

## Status

Accepted, amended 2026-10-01 (D3: the minimum length is the configurable one — the owner's
condition, in the answer to the catalog question "local accounts beyond the one administrator?",
that the minimum is configurable in the chart and no character class is required, a policy for
every local account that
[ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D3 holds),
2026-10-03 (D1, D2, D3, D5–D7 made concrete by the first implementation, which has no identity
provider yet) and 2026-10-04 (D1: every view names a local account by its plain username; D5: the
administrator group in the init state; D6: the bootstrap tenant needs the local administrator or an
administrator group, whose mapping is seeded), and 2026-10-07 by the owner's answer recorded in
[ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D4 (D2: a
changed password also revokes the account's tokens; built 2026-10-09), and 2026-10-10 by the
owner's rename of a tenant to a team
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1; D6: the
variables `COWORK_BOOTSTRAP_TEAM_SLUG` and `COWORK_BOOTSTRAP_TEAM_NAME` and the chart's
`bootstrap.team.slug` and `.name`, the names before read for one release; built the same day).
Date: 2026-10-01. Decided by the owner as the answer to the catalog question "how do
the first administrator and the first tenant come to exist?", reshaped by the owner's
requirements: no command-line step — pure Helm values must yield a usable installation — and
a local administrator account that exists without OIDC and is kept in step with a Kubernetes
Secret. The precisions of D2–D4 were put to the owner with the decision and confirmed. The
scope of local accounts beyond this one administrator is the next record's.

**Partly built** (phase 3, 2026-10-03): D1–D8 for the local administrator and the bootstrap
tenant — [`internal/bootstrap`](../../backend/internal/bootstrap/bootstrap.go) run by
`cowork serve` after the migrations, the init state in the login, the chart's `localAdmin` and
`bootstrap` values, and `POST /api/v1/tenants` for D7
([docs/security/local-accounts.md](../security/local-accounts.md),
[docs/operations/installation.md](../operations/installation.md)); in the UI, the start page
offers a global administrator without a membership the form that creates the first tenant
([`features/home/first-tenant.ts`](../../frontend/src/app/features/home/first-tenant.ts)). ~~Not built, because they
belong to the identity provider that does not exist yet: the administrator group of D5, and the
group mapping D6 seeds.~~

**Built** (phase 4, 2026-10-04): the administrator group of D5 in the init state of the identity
provider's login ([`store/identity.go`](../../backend/internal/store/identity.go)
`CompleteOIDCLogin`), and the group mapping D6 seeds
([`bootstrap.go`](../../backend/internal/bootstrap/bootstrap.go) `keepTenant`); the configuration and
the chart take a bootstrap tenant with the administrator group alone.

**Built** (2026-10-10): D6's variables and values under the team's names. `COWORK_BOOTSTRAP_TENANT_SLUG`
and `COWORK_BOOTSTRAP_TENANT_NAME` are still read for one release: alone with a warning in the log
that names the variable replacing each, and set to a different value a configuration error naming
both ([`config.go`](../../backend/internal/config/config.go) `renamedVariables`); the chart reads
`bootstrap.tenant.*` where `bootstrap.team.*` leaves a value empty and fails to render the two set to
different values (`cowork.bootstrapTeam`). Made concrete by the implementer the same day, open to the
owner's objection: the chart renders the variables under both names with the same value — in the pod
and in the migration Job —, so that an image rolled back to the release before, which reads the
names before alone, keeps the bootstrap team, and the backend reads a name before beside its
replacement at the same value without a warning. The removal of the names before is a later
release's.

## Context

[ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D6 asks that a
single-tenant installation be `helm install` and done. [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D8 admits nobody by default, and D1 makes the administrator group part of the gate. What was
missing: an installation without an identity provider at all (a person alone on a home lab,
an operator before the provider is wired), an operator locked out by a provider outage, and
the first tenant on a fresh database. A command-line step inside a distroless container is a
Job with the same image, which the owner rejected: the chart's values are the whole
interface.

## Decision

**D1 — One local administrator account, from configuration, optional.**
`COWORK_LOCAL_ADMIN_USERNAME` and `COWORK_LOCAL_ADMIN_PASSWORD` — in the chart from an
existing Secret (`localAdmin.existingSecret`, keys configurable) or rendered from values
(`localAdmin.username`, `localAdmin.password`; plain text in the release, the same warning
as `database.url`). The account is a global administrator ([ADR 0004](0004-cowork-is-a-team-product.md)
D4) and a **full account**: it may be a member of tenants and work with tickets like any
person; its identity is `local:<username>`, distinct from any OIDC identity. *(Amended
2026-10-03: ~~`local:<username>` is how an API view names the identity;~~ the stored value is the
plain `username`, unique in the installation. The account is a global administrator through
`users.global_admin`, which the start-up synchronisation sets and no route does, and no tenant's
administrator can manage it or reset its password
([ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D1).)*
*(Amended 2026-10-04: no view writes `local:` — every view names a local account by its plain
`username`, and a person of the identity provider by none; `POST …/members` takes the identity with
or without `local:`. The flag `users.global_admin` is set by the identity provider as well, for the
members of the administrator group, and by nothing else besides the synchronisation.)*

**D2 — The account is synchronised at every start, after the migrations, under an advisory
lock.** Both variables set: the account is created or updated; a password that differs from
the stored hash is re-hashed and **every session of the account is ended** ([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md)
D4). Both unset or empty: the account is deactivated ([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D5) and its sessions ended; it is never deleted. One set, one empty: the start is refused as
a configuration error. A changed Secret takes effect at the next pod start — the environment
is read once — which is the owner's intended "update the login at start". *(Amended 2026-10-03:
the synchronisation is `bootstrap.Sync`, run as the runtime role after the migrations and
before the server listens, as the system actor `system:bootstrap`, under the advisory lock of
`store.RunJob`; a replica that does not get the lock waits for it, two minutes at most, and then
finds nothing left to do. A start that finds everything as configured changes and records
nothing. A changed password also forgets the failed attempts and the lock of the username, which
is how a locked administrator is recovered, and deactivation also revokes the account's tokens
([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D5). Another username in the configuration leaves the previous account deactivated and makes the
new one; an account of the configured name that a tenant's administrator made first is taken
over — the configured password, no session, no token, no tenant that manages it; a deactivated
account is reactivated when the variables return, and its revoked tokens stay revoked.)*
*(Amended 2026-10-07 by the owner's answer recorded in
[ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D4: a
password that differs from the stored hash also **revokes every token of the account** — ~~a
take-over alone revoked them~~ —, so the rotation that recovers a leaked password leaves no token
made with it; the grants it made stay, for the operator to review.)*

**D3 — Password handling for the local administrator.** Argon2id with parameters recorded
in the security page; ~~minimum sixteen characters~~ *(amended 2026-10-01: the configured
minimum of ADR 0033 D3 — `COWORK_PASSWORD_MIN_LENGTH`, default 12, floor 8 — length only,
no character classes)*, enforced at start — a shorter password refuses the start with a
message naming the variable, never the value; the password appears in no log, no audit row
and no error text. Login attempts are rate-limited per account and
per source address; the limits are the next record's to set for all local accounts. *(Amended
2026-10-03: the parameters are 19 MiB of memory, two iterations and one lane, recorded in every
hash and named in [docs/security/local-accounts.md](../security/local-accounts.md); a password
is at most 1024 characters; the configured minimum is checked at start, and the error names
`COWORK_LOCAL_ADMIN_PASSWORD` and `COWORK_PASSWORD_MIN_LENGTH`.)*

**D4 — Exactly one local account comes from configuration.** Further local accounts, if
any, are the next record's.

**D5 — The init state is "no tenant exists", and only global administrators may log in
while it lasts.** A person who passes the OIDC gate but is not in `COWORK_ADMIN_GROUP`
receives "this installation is not initialised; contact an administrator" and no session.
The local administrator and the members of the administrator group log in and are taken to
"create the first tenant". *(Amended 2026-10-03: built for the local login as
`403 not_initialised` and no session. It is answered only after the password was verified, so
an attempt with a wrong password is the `401` every failure is and the state is not learnt
without a password. ~~The administrator group arrives with the identity provider.~~)* *(Amended
2026-10-04: built for the identity provider. A login whose ID token verified and whose groups pass
the gate but hold no `COWORK_ADMIN_GROUP` is sent back to the login page with `not_initialised`,
recorded as `login_refused`, and makes no person; the members of the administrator group are global
administrators and log in, and create the first tenant as the local administrator does.)*

**D6 — An optional bootstrap tenant from values.** ~~`COWORK_BOOTSTRAP_TENANT_SLUG` and
`COWORK_BOOTSTRAP_TENANT_NAME` (chart: `bootstrap.tenant.slug`, `.name`)~~ *(2026-10-10:)*
`COWORK_BOOTSTRAP_TEAM_SLUG` and `COWORK_BOOTSTRAP_TEAM_NAME` (chart: `bootstrap.team.slug`,
`.name`), the names before read for one release. When set and no
tenant exists, the start creates the tenant (slug validated by ADR 0005 D4), seeds one group
mapping `COWORK_ADMIN_GROUP → (tenant, admin)` (ADR 0030 D2) when an administrator group is
configured, and gives the local administrator a marked manual grant as `admin` of it
(ADR 0030 D3) when one is configured. When a tenant already exists the variables do nothing,
whatever they say; the operations page says so. Both the tenant and the grants are audit rows
with the actor `system:bootstrap` ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)).
*(Amended 2026-10-03: ~~built without the group mapping, which no mapping store exists for yet.
The bootstrap tenant needs the local administrator — a tenant without an administrator cannot
come to exist (D7) — and the configuration refuses the start without it.~~ The rows are two
installation-level `created` acts of `system:bootstrap`: the tenant and the marked grant.)*
*(Amended 2026-10-04: the mapping is seeded — `COWORK_ADMIN_GROUP` → (tenant, `admin`), a
`group_mapping` `created` act of `system:bootstrap` — when an administrator group is configured, and
the grant when a local administrator is. The bootstrap tenant needs one of the two: a tenant without
an administrator cannot come to exist (D7), and the configuration and the chart refuse a bootstrap
tenant with neither. With the group alone no grant is made, and the group's members administer the
tenant from their first login, when their membership is derived from the mapping.)*

**D7 — Whoever creates a tenant becomes its first administrator,** by a marked grant,
recorded. This holds for the bootstrap routine and for the UI and the API alike; a tenant
without an administrator cannot come to exist. *(Built 2026-10-03: `POST /api/v1/tenants` writes
the tenant and its creator's marked `admin` grant in one transaction with both acts; see
[ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D5.)*

**D8 — Nothing here needs a shell, a Job or a command.** `helm install` with the database,
the local administrator (or the OIDC gate with an administrator group) and optionally a
bootstrap tenant yields an installation a person can log in to and use.

## Consequences

- An installation without an identity provider is possible and usable; an installation with
  one can switch the local administrator off by emptying the Secret.
- Three to five Helm values stand between a fresh cluster and a usable cowork; the README's
  fast start shows exactly that path.
- The bootstrap routine is the second start-up step with side effects after the migration
  run; it runs under the same advisory-lock discipline, is idempotent, and logs what it did.
- The security page gains the local account: how the password is stored, why there is no
  MFA for it in the first release (an open gap with an `H-<n>` identifier), and how to switch
  it off.
- D5 means a half-configured installation fails closed: nobody but administrators can enter
  until a tenant exists.

## Alternatives Considered

- **Command-line subcommands for tenant and mapping creation** — the recommendation's
  addition. Explicit and scriptable; in the chart a Job, and in the owner's words not
  DevOps-capable. Lost.
- **A local administrator restricted to administration** (no tenant membership). Keeps the
  account small; leaves an installation without a provider unusable for work. Lost.
- **No local administrator; init through the OIDC administrator group only.** Lost for the
  same reason, and a provider outage would lock the operator out.
- **The first person to log in becomes administrator.** A race on every fresh installation.
  Lost.

## Residual risks

- The local administrator's password lives in a Kubernetes Secret and reaches the process as
  an environment variable; anyone who can read the pod spec or the Secret can read it. That
  is the standing property of Secret-backed environment in Kubernetes, shared with the
  database URL, and the operations page names it (and
  [docs/security/local-accounts.md](../security/local-accounts.md), H-20).
- No MFA for the local administrator in the first release. The gap is named in the security
  page; the mitigations are the length rule, the rate limit and the ability to switch the
  account off.
- D2's "ended at the next start" means a leaked password stays valid until the operator
  rotates the Secret **and** restarts the pod; the operations page says both steps.

## References

- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D4, D6 — the slug rule and the single-tenant installation
- [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md) D1–D3 — the gate, the mapping, the grant the bootstrap seeds
- [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D4 — ending sessions on a password change
- [ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md) D5 — deactivation, never deletion
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D5, D7 — the start-up sequence and the `COWORK_*` surface
- [ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) — the accounts, the lockout and the recovery this record points at
- [`backend/internal/bootstrap/bootstrap.go`](../../backend/internal/bootstrap/bootstrap.go) — the synchronisation
