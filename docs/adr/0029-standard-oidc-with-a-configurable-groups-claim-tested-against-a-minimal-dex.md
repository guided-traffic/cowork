# ADR 0029: cowork Is a Standard OIDC Client With a Configurable Groups Claim; a Minimal Dex With a Static Client and Static Users Is the Test and Development Issuer

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "which
OIDC provider?": a generic OIDC client that assumes no particular provider, tested and
developed against a plain Dex in its minimal configuration — a static client and static
users — over Dex as the designated provider, over several issuers at once, and over the
recommendation's "Dex as default with a generic capability". The configuration surface of D4
was proposed with the question and not objected to.

**Not built.** No OIDC client, no session, no login route.

## Context

The founding brief wants people to log in through OIDC and their access gated by groups.
Identity providers agree on the code flow and on discovery and disagree on everything around
groups: the claim is `groups` here, `roles` there, absent above a size limit elsewhere. A
client that bakes one provider's habits in works for that provider and surprises the next;
a client that speaks the standard and names the one thing that varies — the groups claim —
works for any of them. Development and tests still need a running issuer, and that issuer
should be the smallest thing that speaks the standard honestly: Dex with a static client and
static users in one YAML file, which is also what a continuous-integration job can start.

## Decision

**D1 — cowork is a standard OpenID Connect relying party.** Authorization Code Flow with
PKCE, issuer discovery (`/.well-known/openid-configuration`) at start, ID-token signature
verification against the issuer's JWKS with key rotation, `state` and `nonce` checked. No
provider-specific endpoint, parameter or claim is assumed.

**D2 — The groups claim is configuration, not an assumption.** The claim's name defaults to
`groups` and is configurable; the claim is read from the ID token and, when absent there,
from the UserInfo endpoint. A string-valued claim is treated as a single group. What the
groups mean — the gate and the mapping — is the next record's.

**D3 — The test and development issuer is a plain Dex in minimal configuration:** one static
client (cowork's client id and secret), static users with passwords and group memberships
declared in Dex's configuration file, no connectors. It runs in `docker compose` beside
PostgreSQL for development and as a service container in the integration tier. Nothing in
cowork depends on it being Dex; it is the fixture, not the design.

**D4 — Configuration.**

| Variable | Default | Meaning |
|---|---|---|
| `COWORK_OIDC_ISSUER` | — (required to enable login) | the issuer URL discovery is fetched from |
| `COWORK_OIDC_CLIENT_ID` | — | the relying party's client id |
| `COWORK_OIDC_CLIENT_SECRET` | — (Secret) | the client secret; a public client without secret is not supported in the first release |
| `COWORK_OIDC_SCOPES` | `openid profile email groups` | the scopes requested |
| `COWORK_OIDC_GROUPS_CLAIM` | `groups` | the claim that carries the groups (D2) |

The redirect URI is `COWORK_BASE_URL` + `/auth/callback`, which makes `COWORK_BASE_URL`
required when the issuer is configured. A configured issuer that cannot be discovered at
start refuses the start, like an invalid configuration value; an unconfigured issuer leaves
the installation without a login and says so on the login page.

**D5 — The identity of a person is the issuer's `sub`, scoped by the issuer.** The pair
(issuer, `sub`) is the stable key of a person; e-mail and name are display attributes
refreshed on every login, never the identity. One installation has one issuer in the first
release.

## Consequences

- Any conformant issuer works by configuration; the one that is proven to work is the
  minimal Dex of D3, and the operations page documents it as the reference.
- Providers that cap the groups claim (large directories) are a configuration problem of
  that provider, surfaced by D2's UserInfo fallback and documented; cowork does not work
  around it with provider-specific graph calls.
- A single issuer per installation (D5) keeps account merging out of cowork; a tenant with
  its own identity provider federates in front of cowork or runs its own installation.
- Development needs `docker compose` with PostgreSQL and Dex; `make dev-up` is the target
  that brings both.

## Alternatives Considered

- **Dex as the designated provider.** In-house and capable; would have invited Dex-specific
  shortcuts and bound a client-tenant's login to the owner's Dex. Lost as the design; kept as
  the fixture.
- **Dex as default with a generic capability** — the recommendation. Functionally close to
  this record; the owner removed the "default" and any mention of operating Dex. Lost in
  wording, not in substance.
- **Several issuers at once, one per tenant.** Issuer selection on the login page and
  account merging across issuers — federation rebuilt inside cowork. Lost.

## Residual risks

- D1 relies on a Go OIDC library for discovery, JWKS and token verification; the choice is
  made when the login is built and recorded in the security page, with the verification
  tests that prove `state`, `nonce`, expiry, audience and signature are checked.
- D4's refusal to start without a reachable issuer makes an identity-provider outage a
  backend restart failure; the operations page says so, and the amendment — start, but
  disable login until discovery succeeds — is one if the owner prefers it.

## References

- [ADR 0023](0023-the-tenant-is-in-the-path.md) D5 — the membership check this login feeds
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D7 — `COWORK_*` configuration and `COWORK_BASE_URL`
- [ADR 0003](0003-test-and-ci-policy.md) D2 — the integration tier the Dex fixture joins
