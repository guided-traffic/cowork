# ADR 0030: A Global Allow-List of Groups Gates Login, Group Mappings Derive Membership and Role per Tenant, and a Marked Manual Grant Adds to Them — Never Over Them

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "how
do OIDC groups gate access?": the three-layer form — a global gate, per-tenant mappings, and
an additive manual grant — over a gate alone, over mappings alone, and over a grant that
overrides the mapping. The rules of D6–D8 were put to the owner with the question and not
objected to.

Amended 2026-10-04 by the first implementation: D1 and D8 (the gate is the allowed groups and the
administrator group, and both empty admit nobody), D2 (a mapping's change derives at once; a global
administrator's editing of any tenant is not built), D3 (how a person is found), D5 (where a refresh
reads the groups, and which answers of the issuer end a session — the coordinator's rule after Dex,
2026-10-04), D6 (the causes recorded). Amended again on 2026-10-04 after the security review of the
first implementation: D1 and D8 (the gate compares the issuer), D2 (only a person the gate admits is
derived; the changes of a tenant's administrators are ordered by the tenant's lock), D3 (an address
is looked up among the configured issuer's persons only), D5 (the refresh holds nothing while it
asks the issuer; the classes of the issuer's answers; a refresh that read nothing changes nothing of
the person).

~~**Not built.** No `memberships`, `group_mappings` or session table exists.~~ **Built** (phase 4,
2026-10-04): D1–D8 but a global administrator's editing of the mappings of a tenant they do not
administer — the gate in [`api/identity.go`](../../backend/internal/api/identity.go), the
derivation and the refresh in [`store/identity.go`](../../backend/internal/store/identity.go), the
administration in [`api/members.go`](../../backend/internal/api/members.go) and the UI,
`group_mappings` ([migration 21](../../backend/internal/store/migrations/000021_group_mappings.up.sql))
and the policies of [migration 22](../../backend/internal/store/migrations/000022_membership_administration.up.sql);
the security page is [docs/security/identity-provider.md](../security/identity-provider.md), the
administration's [docs/security/tenancy.md](../security/tenancy.md#members-grants-and-group-mappings).

## Context

The founding brief says that OIDC groups decide whether a person may log in at all.
[ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D2 delivers the groups as a claim; [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
D5 says a person joins a tenant through the identity provider's mapping or an administrator's
explicit grant; [ADR 0004](0004-cowork-is-a-team-product.md) D4 makes the global
administrator an explicit grant. A gate alone makes every membership manual work; mappings
alone leave no room for a guest who is in no group and lock everyone out on a mapping
mistake; a grant that overrides a mapping creates two truths for one role and an unclear
winner at the next login.

## Decision

**D1 — The gate: a global allow-list of groups, in configuration.**
`COWORK_OIDC_ALLOWED_GROUPS` is a list of group names; a person whose groups intersect it may
log in; ~~an empty or unset list means nobody may~~ *(amended 2026-10-04: an empty or unset list
admits the members of the administrator group only, and both empty admit nobody, D8)*.
`COWORK_ADMIN_GROUP` names the group whose members are global administrators; it is part of the
gate by definition. The gate is evaluated on every login and nowhere else than in configuration.
*(Amended 2026-10-04: the gate is configuration only, and is evaluated at every login, at every
groups refresh of a session (D5), and at a token's check of
[ADR 0035](0035-personal-access-tokens.md) D8. The list is comma-separated, a name compared exactly,
case and all; the administrator flag of a person follows the group at each of the three.)*
*(Amended after the security review, 2026-10-04: the gate admits only a person of the configured
issuer. A person of another issuer, or any person of a provider while none is configured, is outside
it whatever their groups — at once: their sessions end at the first request of any, and their
tokens are refused at every request.)*

**D2 — The mapping: group → (tenant, role), per tenant.** A table of rows (group name,
tenant, role) that a tenant administrator edits for their tenant and a global administrator
for any. On every login the person's groups are matched against the mappings; a matching
row creates or updates the membership with that role; a membership that was derived from a
mapping and whose group is no longer in the claim is removed. Several matches for one tenant
yield the highest role. *(Amended 2026-10-04: built as `group_mappings`, one mapping per group and
tenant. A tenant's administrators edit it, in a browser session for a mapping made or changed
([ADR 0035](0035-personal-access-tokens.md) D5); a global administrator's editing of a tenant they
do not administer is not built — no route lets one in (ADR 0034 D2). The derivation runs at a login,
at a groups refresh (D5), at a token's gate check, and **at once** when an administrator makes,
changes or removes a mapping: in the administrator's transaction, for every person whose groups as
of their last login or refresh hold the group, so the member list shows the effect immediately and
not at each person's next login. ~~It follows the groups whatever the gate says.~~ A change of a
mapping that would leave the tenant without an ~~active~~ administrator *(amended: who can log in)*
is refused, a derivation never
([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D1).)* *(Amended after the security review, 2026-10-04: the derivation runs only for a person the
gate admits. A login or a refresh that finds the person outside the gate stores their groups and
derives nothing — their memberships stay as they were, unusable, and count for no tenant as an
administrator — and a mapping's change passes over a person who is deactivated, of another issuer,
or was refused at their last login or refresh. An administrator's change of a grant or a mapping
takes the tenant's lock before any person's, so two such changes are decided one after the
other.)*

**D3 — The grant: a tenant administrator may add a membership by hand, and it is marked as
manual.** A manual grant gives a person who is behind the gate but in no mapped group a
membership and a role in one tenant. It never changes a derived membership's role: a person
whose role comes from a mapping has that role until the mapping or the groups change. A
manual grant stays until an administrator removes it; it is shown as manual in the member
list. *(Made concrete 2026-10-04: the grant is a membership row of its own beside the mapped one,
`source` `grant`. A person is added by the e-mail address the issuer asserted at their last login —
compared without regard to case, never one the issuer marked unverified, *(amended after the security
review: only among the persons of the configured issuer,)* and refused as ambiguous
when two active persons share it — or by a local account's username; the person must exist, having
logged in once or holding a local account. Adding a person and setting a grant take a browser
session; removing a grant takes an administrator's token as well.)*

**D4 — Precedence is gate, then mapping, then grant, and the gate is absolute.** A person
outside the allow-list has no login, whatever mappings or grants exist. Behind the gate, a
derived membership and a manual grant for the same tenant coexist; the effective role is the
higher of the two, and the list shows both sources.

**D5 — Groups are re-evaluated on every login and watched during a session.** The session
stores the groups of the login; a refresh every `COWORK_OIDC_GROUPS_REFRESH` (default fifteen
minutes) re-reads the claim ~~from the UserInfo endpoint~~ and re-runs D1–D2; a person who left
the allow-list is logged out at the next request after the refresh; a person who left a
mapped group loses that membership at the same moment. *(Amended 2026-10-04, the coordinator's
rule after Dex: the refresh is a refresh grant with the session's sealed refresh token, on the
session's first request after the interval and at an open event stream's heartbeat, once per
session ~~under its row's lock~~. It reads the claim from the refreshed ID token when the issuer sends
one that verifies and carries it — its subject the person's — and else from UserInfo with the new
access token. ~~**Any OAuth error answer of the token endpoint** — a 4xx with an `error` field:
`invalid_grant`, and `invalid_request`, which Dex answers for a spent or unknown token — and a
refreshed ID token of another subject **end that session**. **No answer, a timeout, a 5xx, a 4xx
that is no OAuth error**, a refreshed ID token that does not verify and a failing UserInfo are the
issuer's trouble, not the session's: it is served on the groups it holds and refreshed again a
minute later.~~ ~~A session without a refresh token — the issuer gave none —, or whose refresh carries
the claim in neither place, is judged on the groups it holds, against the gate as configured now.~~ A
person outside the gate at a refresh loses **every** session at once, and their tokens meet the gate
at their next request. The interval is at least one minute.)* *(Amended after the security review,
2026-10-04 — the rule as built: **once, and holding nothing while the issuer is asked.** A short
transaction claims the refresh by a thirty-second lease on the session's row; another request of the
session meanwhile finds it taken and is served on the groups the session holds, without waiting. The
issuer is asked with no database connection and no lock held, on a context the request's end does not
cancel, within twenty seconds; a second short transaction applies the answer under the person's lock
and the session's row lock while the lease is still the claimant's. **A `4xx` other than `429` with an
OAuth `error` ends that session** — `invalid_grant`, Dex's `invalid_request`, `access_denied`, any other — **except
`temporarily_unavailable`, `slow_down`, `server_error`, `invalid_scope`, `invalid_client` and
`unauthorized_client`**, which are the issuer's state or cowork's configuration: the last two are
logged at error level, so a rotated client secret logs nobody out. A refreshed ID token that does not
verify ends it too, unless fetching the keys failed, and so does one of another subject and UserInfo
about another subject. **No answer, a timeout, a `5xx`, a `429`**, an answer that redirects or exceeds
1 MiB, a `4xx` without an OAuth error and those six errors serve the session on its groups and refresh
it a minute later. **A refresh that read nothing** — no refresh token, or the claim in neither place —
judges the person's groups as they stand against the gate as configured now and changes the session's
row only: the person's groups are written only from groups the issuer has just returned, and never
over groups newer than the refresh, a later login's or another session's. How long a session lives on
its last groups while the issuer cannot be reached is bounded by its absolute lifetime,
`COWORK_SESSION_LIFETIME`.)*

**D6 — Every derivation and every grant is a recorded act** ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)):
derived memberships with the actor `identity-provider` and the login as the cause, grants
and removals with the administrator. *(Made concrete 2026-10-04: the actor is
`system:identity-provider`; the cause is `login`, `refresh`, `token` (a token's gate check) or
`mapping` (an administrator's change of a mapping, recorded in the administrator's request); the
persons it makes and changes and the sessions it ends are its rows as well, a refused login is
`login_refused`, and every change of a membership is announced as `membership.changed`
([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D2).)*

**D7 — Mappings are editable in the UI by tenant administrators; the allow-list and the
administrator group are configuration only.** An installation's operator decides who may
enter; a tenant's administrator decides who belongs where.

**D8 — No default admits anyone.** An installation without `COWORK_OIDC_ALLOWED_GROUPS`
has no login; the login page says so. Bootstrapping the first administrator and tenant is
the next record's. *(Amended 2026-10-04: without an allowed group and without an administrator
group the provider admits nobody — the login page offers no button, the start route sends the
browser back with `oidc_unavailable`, and the start warns; the local login is not affected. A
person of a provider that is no longer configured is refused the same way.)* *(Amended after the
security review, 2026-10-04: at once, as D1 now says — their sessions and tokens with them.)*

## Consequences

- The brief's sentence is one configuration line (D1); the identity provider stays the truth
  for who belongs where (D2); the guest is a visible exception (D3); precedence is testable
  (D4).
- A mapping mistake cannot lock everyone out of the installation — the gate is separate —
  but it can empty a tenant's memberships at the next login; D6 makes that visible and the
  mapping editor warns when a change would remove the editor's own membership.
- D5's refresh is a UserInfo call per session per fifteen minutes; the session record decides
  how the session stores and compares the snapshot. *(Amended 2026-10-04: a refresh grant, and a
  UserInfo call where the refreshed ID token lacks the groups, per active session per interval,
  inside the session's request — the one request that claims the refresh waits for it, holding no
  connection, and the session's others do not.)*
- Three mechanisms to document and test: the integration tier needs a user in the gate with a
  mapping, one with a grant only, one with both, and one outside the gate.

## Alternatives Considered

- **A gate alone, membership managed in cowork.** The brief's sentence and nothing more;
  every membership manual, roles in two places. Lost.
- **Mappings alone.** The identity provider as the only truth; no guest, and a mapping error
  locks everyone out. Lost.
- **A grant that overrides the mapping.** Convenient; two truths for one role and an unclear
  winner at the next login. Lost to D3's "adds, never overrides".

## Residual risks

- D2's "several matches yield the highest role" is a choice; a tenant that wants the lowest
  (deny wins) needs an amendment.
- D5 depends on the issuer's UserInfo endpoint returning groups; an issuer that puts groups
  only in the ID token makes the refresh a no-op, and the session then lives on its login
  snapshot until it expires. The session record sets that expiry short enough. *(Amended
  2026-10-04: the refresh reads the refreshed ID token first, so such an issuer works when it sends
  one at a refresh. One that sends the groups at neither place, or no refresh token, leaves the
  session on its login's groups ~~— and the refresh writes them back onto the person at every
  interval~~ *(amended after the security review: such a refresh writes nothing of the person)*;
  while the issuer cannot be reached a session is served on its groups ~~without a bound of its
  own~~ until its absolute lifetime ends; ~~a derivation can remove a tenant's last administrator~~
  the issuer's word — a derivation, a person leaving the gate — can leave a tenant without an
  administrator who can log in; ~~a mapping shows its administrator every person of the installation
  in its group~~ a tenant's administrator may map any group, and so bring its people into the tenant
  and learn who exists in the installation. The security pages name each:
  [identity-provider.md](../security/identity-provider.md) H-24, H-25, H-29,
  [tenancy.md](../security/tenancy.md) H-31.)*

## References

- [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) D2 — the groups claim
- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D2, D5 — memberships and how they are created
- [ADR 0004](0004-cowork-is-a-team-product.md) D4 — the global administrator as a grant
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) — the recorded acts
