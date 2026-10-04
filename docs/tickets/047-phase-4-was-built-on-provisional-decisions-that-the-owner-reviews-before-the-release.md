---
id: T47
title: phase 4 (OIDC and authorization) was built in one night on provisional decisions — the owner reviews them before the release
state: in-progress
severity: medium
security: hardening
threat: the remaining phase close would additionally cover development containers that still listen on every interface until they are recreated
urgency: release      # rule 2: gates the release — merging the branch releases phase 4
effort: S
blocked-by: human
filed-from: docs/planning/project-plan.md phase 4, converted and built 2026-10-04 (ADR 0074 D2)
opened: 2026-10-04
decided: 2026-10-04
done:
---

## Current state

The family ticket of phase 4 ([ADR 0074](../adr/0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D2). **Goal:** people sign in through the identity provider; only allowed groups get in; roles
hold. Built on the branch `feat/phase-4-and-5` on the owner's instruction of 2026-10-03: build to
best knowledge, leave a gate open when in doubt, file what the owner should look at.

- **Built:** the OIDC relying party with the code flow, PKCE, the sealed state cookie and a hardened
  issuer client ([ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md));
  the gate, the administrator group, the mappings, the grants and their precedence
  ([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md));
  the groups snapshot and its refresh outside the database's connections and locks, with a sealed
  refresh token ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md)); the init
  state and the bootstrap mapping ([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md));
  the administration of memberships, mappings and a restricted project's access list with
  `membership.changed` ([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md),
  [ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md));
  the source-address hash and the token gate ([ADR 0035](../adr/0035-personal-access-tokens.md) D2,
  D8); the Dex fixture (`make dex-up`, `dev-up`, the integration tier, `make dev`); the chart's
  `auth.oidc.*`; the UI — the sign-in button, members with origins, e-mail for administrators and
  grants, the group mappings, a project's restriction and access list;
  [identity-provider.md](../security/identity-provider.md).
- **Decided without the owner**, each recorded in the ADR it amends (2026-10-04): `offline_access`
  in the default scopes and `COWORK_OIDC_DISPLAY_NAME` (ADR 0029 D4); twelve session-only
  operations by the rule "giving access needs a session, taking it away does not" (ADR 0035 D5,
  ADR 0031 D6); at a refresh, an OAuth error answer ends the session unless it names cowork's own
  configuration or a temporary state — a rotated client secret logs nobody out (ADR 0030 D5); a
  session stays while the issuer is unreachable, bounded by its absolute lifetime — ending it would
  lock everyone out until the issuer is back; logout at the issuer is the browser's redirect
  (ADR 0031 D4); a restricted project's entries live on its access list, not on the member list,
  which viewers read (ADR 0034 D7); e-mail addresses are shown to administrators only and never
  written to audit rows; agents are refused on every administration route; the client secret
  comes from a Secret only (ADR 0058 D3); the development containers bind to the loopback interface
  (ADR 0038 D4).
- **The owner's answers (Q1–Q7), built:** only a global administrator who administers a tenant
  creates a mapping or changes its role, in the handler and in the data layer (ADR 0030 D7,
  ADR 0021); tokens stop once their person's groups are older than `COWORK_OIDC_GROUPS_MAX_AGE`
  (ADR 0035 D8); a global administrator sees a tenant's administration without a role and grants or
  raises their own role, by session only (ADR 0034 D2); only `email_verified: true` finds a person
  unless `COWORK_OIDC_EMAIL_TRUSTED` (ADR 0030 D3); audit rows record only that the groups changed
  (ADR 0026 D1); a change of the server key signs provider persons out once (ADR 0031 D1); the
  deactivation asks only the managing tenant (ADR 0034 D1).
- **Not built:** the administrators' view of their members' tokens (T40); the sign-in through Dex
  in the end-to-end tier (T29).

## Required changes

1. After the merge, when the development data may go: `make postgres-down minio-down dex-down`,
   then `make dev` — containers made before the loopback rule keep listening on every interface
   until they are recreated.
2. Phase close: the remaining items here or in T29/T40, the phase-4 lines
   of [project-plan.md](../planning/project-plan.md) gone, this ticket archived.

## Open questions

### Q1: May a tenant administrator map any group of the identity provider?

Every tenant shares the provider's one group namespace. An administrator of client tenant X who
maps `cowork-users` (or a guessed department group) admits every person of that group into X at
once — their names in X's member list, X in their tenant lists — and with grants by e-mail
(`404` against `201`/`409`) learns who exists in the installation, across the client boundary of
[ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md).

- **(a) As built** (ADR 0030 D7): tenant administrators map any group.
- **(b) Only a person who is a global administrator** (and an administrator of the tenant) creates
  and changes mappings; tenant administrators see them, remove them and grant by hand.
- **(c) A group prefix per tenant**, set by a global administrator: the tenant's administrators map
  only groups that start with it (`client-x-`).

Recommended: **(b)** now — one check on two routes, and the owner maps groups for client tenants
anyway; **(c)** when a client wants self-service for its own groups.

**Answer:** (b) — only a global administrator who administers the tenant creates a mapping or
changes its role; tenant administrators see, remove and grant by hand. Recorded in ADR 0030 D7.

### Q2: How old may the groups be that judge a token?

The token gate (ADR 0035 D8) judges the groups stored at the person's last sign-in or refresh. A
person who only uses tokens never refreshes them: someone removed from the provider's groups keeps
working tokens until they expire, up to the token lifetime (H-23).

- **(a) As built:** any age.
- **(b) A maximum age**, `COWORK_OIDC_GROUPS_MAX_AGE` (for instance seven days): older groups refuse
  the token with `401 not_allowed` until the person signs in to the browser once.
- **(c) A person-level refresh token**, stored sealed, that the token gate uses to ask the issuer.

Recommended: **(b)** — one comparison and no further stored credential, and a weekly sign-in in the
browser is part of the working day.

**Answer:** (b) — `COWORK_OIDC_GROUPS_MAX_AGE`, seven days by default: older groups refuse the
person's tokens with `401 not_allowed` until they sign in to the browser once.

### Q3: What keeps a tenant from losing its last administrator?

A sign-in, a refresh or a mapping change can remove a tenant's only administrator — the provider's
word is never refused — and no route lets a global administrator grant themselves a role in an
existing tenant (H-29).

- **(a) As built:** recovery by the local administrator, if the tenant has it, or in the database.
- **(b) A derivation never removes the last administrator**; it keeps the membership and logs —
  against "the provider is the truth".
- **(c) Build ADR 0034 D2's self-grant:** a global administrator grants themselves `admin` in any
  tenant, a recorded act the tenant sees.

Recommended: **(c)** — decided already, and it recovers a tenant whatever removed its last
administrator, the deactivation of an account another tenant manages (Q7) included.

**Answer:** (c) — build the self-grant of ADR 0034 D2: a global administrator sees every tenant's
administration and grants themselves `admin`, a recorded grant the tenant sees, by session only.

### Q4: Does a missing `email_verified` count as verified when an administrator grants by e-mail?

An unverified address is never matched; an address without the claim is (H-26). Providers such as
Entra do not send the claim, and a person of the provider has no username to be found by.

- **(a) As built:** a missing claim counts as verified.
- **(b) Only `email_verified: true` counts** — persons of such providers can then be admitted by a
  mapping only.
- **(c) A setting** that says whether the configured provider's addresses are verified.

Recommended: **(a)** while the installation's provider is one whose addresses an administrator
issues; **(c)** before a provider where people choose their own address is configured.

**Answer:** (c), secure by default — `COWORK_OIDC_EMAIL_TRUSTED`, `false` by default: only
`email_verified: true` matches; an operator sets `true` only for a provider whose addresses
administrators issue.

### Q5: Do the groups belong in audit rows?

A sign-in or refresh that changes a person's groups records the group lists in the append-only
audit table, where an erasure request cannot be honoured; e-mail addresses are no longer recorded.

- **(a) As built:** the group lists are recorded.
- **(b) Only that the groups changed**; the memberships they caused are recorded anyway, tenant by
  tenant.

Recommended: **(b)** — the memberships carry the accountability, and group names can say more about
a person than their role in a tenant.

**Answer:** (b) — an audit row records only that the groups changed; the memberships they caused
carry the accountability.

### Q6: Does a change of the server key end the sessions of the identity provider?

[ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D1 keeps the old key for
decrypting until every session that used it is gone; that is not built. A changed
`COWORK_SESSION_KEY` makes every stored refresh token unreadable, so each provider session ends at
its next refresh (fails closed), and every list cursor and login-throttle hash starts over.

- **(a) Amend D1 to what is built:** a key change signs every provider person out once.
- **(b) Build the rotation:** `COWORK_SESSION_KEY_PREVIOUS` opens what the old key sealed until the
  sessions that hold it have ended.

Recommended: **(a)** — a session lives twelve hours at most, a rotation is rare and deliberate, and a
second key is one more Secret to handle.

**Answer:** (a) — ADR 0031 D1 is amended to what is built: a key change signs every provider person
out once, and the operations pages name every consequence.

### Q7: Does the deactivation of an account ask the other tenants it administers?

Any tenant's administrator grants a role to a local account by its username, so an account one
tenant manages can be another tenant's only administrator who can log in. The deactivation holds the
managing tenant to `last_admin` under its lock and asks no other tenant, which is then left without
an administrator (H-32). The check needs that tenant's memberships and persons, and row-level
security keeps them out of the managing tenant's transaction. Each of (b)–(d) also takes several
tenants' locks, so it needs one order among them — by the lock's key, since two tenant ids can share
one — and must hold the set of tenants still while it decides: a grant of `admin` in a further
tenant can commit in between.

- **(a) As built:** not asked; a stranded tenant is recovered by Q3 (c).
- **(b) The deactivation steps into the other tenants:** the store names the person in
  `app.user_id` to find the tenants they administer, then each tenant in `app.tenant_id` to take its
  lock and count its administrators, and returns only a number — no migration, but for that moment
  a request of one tenant reads another tenant's rows past the second line, an exception to ADR 0021
  D3 like the identity provider's.
- **(c) A migration with a dedicated read (ADR 0021 D7):** a setting-gated policy that admits, to
  the deactivation's transaction, the `admin` memberships of the tenants the person administers and
  those administrators' person rows — names and addresses the check does not need, readable for
  good under that setting.
- **(d) A function of a dedicated database role** (`SECURITY DEFINER`, owned by a role that may only
  read those rows) that takes the tenants' locks and returns only how many tenants would be left
  without an administrator — no row of another tenant reaches the request, but the migration must
  create a third role (`CREATEROLE` for the owner) and every deactivation runs code as it.

Recommended: **(a) with Q3 (c)** — the self-grant recovers a tenant whatever left it without an
administrator, opens no boundary and is decided already; **(d)** if a stranded tenant must never
happen, since it is the one option that keeps every row of the other tenant out of the request.

**Answer:** (a) — the deactivation asks only the managing tenant; a tenant it leaves without an
administrator is recovered by the self-grant of Q3. Recorded in ADR 0034 D1.

## Related

- T29 — the end-to-end tier; the sign-in through Dex joins it
- T40 — the administrators' view of their members' tokens
- T45 (medium, boundary, XS, in-progress) — embargoed security finding, open - details in its own ticket file until it is fixed
- T46 (medium, boundary, XS, in-progress) — embargoed security finding, open - details in its own ticket file until it is fixed
