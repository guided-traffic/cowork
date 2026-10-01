# ADR 0030: A Global Allow-List of Groups Gates Login, Group Mappings Derive Membership and Role per Tenant, and a Marked Manual Grant Adds to Them — Never Over Them

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "how
do OIDC groups gate access?": the three-layer form — a global gate, per-tenant mappings, and
an additive manual grant — over a gate alone, over mappings alone, and over a grant that
overrides the mapping. The rules of D6–D8 were put to the owner with the question and not
objected to.

**Not built.** No `memberships`, `group_mappings` or session table exists.

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
log in; an empty or unset list means nobody may. `COWORK_ADMIN_GROUP` names the group whose
members are global administrators; it is part of the gate by definition. The gate is
evaluated on every login and nowhere else than in configuration.

**D2 — The mapping: group → (tenant, role), per tenant.** A table of rows (group name,
tenant, role) that a tenant administrator edits for their tenant and a global administrator
for any. On every login the person's groups are matched against the mappings; a matching
row creates or updates the membership with that role; a membership that was derived from a
mapping and whose group is no longer in the claim is removed. Several matches for one tenant
yield the highest role.

**D3 — The grant: a tenant administrator may add a membership by hand, and it is marked as
manual.** A manual grant gives a person who is behind the gate but in no mapped group a
membership and a role in one tenant. It never changes a derived membership's role: a person
whose role comes from a mapping has that role until the mapping or the groups change. A
manual grant stays until an administrator removes it; it is shown as manual in the member
list.

**D4 — Precedence is gate, then mapping, then grant, and the gate is absolute.** A person
outside the allow-list has no login, whatever mappings or grants exist. Behind the gate, a
derived membership and a manual grant for the same tenant coexist; the effective role is the
higher of the two, and the list shows both sources.

**D5 — Groups are re-evaluated on every login and watched during a session.** The session
stores the groups of the login; a refresh every `COWORK_OIDC_GROUPS_REFRESH` (default fifteen
minutes) re-reads the claim from the UserInfo endpoint and re-runs D1–D2; a person who left
the allow-list is logged out at the next request after the refresh; a person who left a
mapped group loses that membership at the same moment.

**D6 — Every derivation and every grant is a recorded act** ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)):
derived memberships with the actor `identity-provider` and the login as the cause, grants
and removals with the administrator.

**D7 — Mappings are editable in the UI by tenant administrators; the allow-list and the
administrator group are configuration only.** An installation's operator decides who may
enter; a tenant's administrator decides who belongs where.

**D8 — No default admits anyone.** An installation without `COWORK_OIDC_ALLOWED_GROUPS`
has no login; the login page says so. Bootstrapping the first administrator and tenant is
the next record's.

## Consequences

- The brief's sentence is one configuration line (D1); the identity provider stays the truth
  for who belongs where (D2); the guest is a visible exception (D3); precedence is testable
  (D4).
- A mapping mistake cannot lock everyone out of the installation — the gate is separate —
  but it can empty a tenant's memberships at the next login; D6 makes that visible and the
  mapping editor warns when a change would remove the editor's own membership.
- D5's refresh is a UserInfo call per session per fifteen minutes; the session record decides
  how the session stores and compares the snapshot.
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
  snapshot until it expires. The session record sets that expiry short enough.

## References

- [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) D2 — the groups claim
- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D2, D5 — memberships and how they are created
- [ADR 0004](0004-cowork-is-a-team-product.md) D4 — the global administrator as a grant
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) — the recorded acts
