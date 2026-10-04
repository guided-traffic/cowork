# ADR 0029: cowork Is a Standard OIDC Client With a Configurable Groups Claim; a Minimal Dex With a Static Client and Static Users Is the Test and Development Issuer

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "which
OIDC provider?": a generic OIDC client that assumes no particular provider, tested and
developed against a plain Dex in its minimal configuration — a static client and static
users — over Dex as the designated provider, over several issuers at once, and over the
recommendation's "Dex as default with a generic capability". The configuration surface of D4
was proposed with the question and not objected to.

Amended 2026-10-04 by the first implementation: D1 names the library and the flow's details, D2
the shapes of the claim, D3 and the Consequences the Make targets that run the fixture instead of a
compose file, D4 the default scopes — `offline_access` added —, `COWORK_OIDC_DISPLAY_NAME` and the
rule for the issuer's URL, D5 the display attributes; the first residual risk is answered. Amended
again on 2026-10-04 after the security review of the first implementation: D1 — the discovery and
the key set are cowork's own, the endpoints the discovery names are held to the issuer's rule,
every call to the issuer follows no redirect and reads at most 1 MiB, `azp` is checked; D4 — the
button reads "Sign in with".

~~**Not built.** No OIDC client, no session, no login route.~~ **Built** (phase 4, 2026-10-04):
D1–D5 — the relying party [`internal/oidc`](../../backend/internal/oidc/oidc.go), the start and the
callback ([`api/oidc.go`](../../backend/internal/api/oidc.go)), the configuration
([`config/oidc.go`](../../backend/internal/config/oidc.go)), discovery at start
([`cmd/cowork/main.go`](../../backend/cmd/cowork/main.go) `discoverIssuer`), the persons of the
provider ([migration 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)),
the Dex fixture ([`hack/dex/config.yaml`](../../hack/dex/config.yaml), `make dex-up`) and the
integration tier against it; the security page is
[docs/security/identity-provider.md](../security/identity-provider.md). The end-to-end tier's login
through Dex ([ADR 0056](0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md))
is not built.

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
provider-specific endpoint, parameter or claim is assumed. *(Made concrete 2026-10-04: ~~the library
is go-oidc v3.21.0 over `golang.org/x/oauth2` v0.37.0~~ go-oidc v3.21.0 verifies the ID token's
claims and reads UserInfo, `golang.org/x/oauth2` v0.37.0 redeems the code and the refresh token, and
— amended after the security review — the discovery and the key set are cowork's own, over go-jose
v4.1.4, so that a refresh tells keys that could not be fetched from a signature no key verifies, and
no error carries the issuer's answer into a log. The start draws a `state` and a `nonce` of
256 random bits and a PKCE verifier (`S256`) and seals them, with the path to return to and the
time, into the cookie `__Host-cowork-oidc` — AES-256-GCM under a key derived from
`COWORK_SESSION_KEY` with a label of its own, `HttpOnly; Secure; SameSite=Lax; Path=/`, ten
minutes. The callback compares the returned `state` with the cookie's in constant time, redeems the
code with the verifier and the client secret, has go-oidc verify the ID token's signature against
the published keys — ~~fetched again for an unknown key id~~ *(amended: through cowork's key set,
fetched again when no cached key verifies, of an asymmetric algorithm the discovery names, `RS256`
when it names none)* —, its issuer, its audience and its expiry, and compares the nonce itself,
because go-oidc leaves that to its caller. Parameters an issuer adds to the callback, such as `iss` and `session_state`, are taken
and not read. A failure of any of these is a redirect to the login page with `oidc_failed`, the
reason in the log, never a code or a token.)* *(Added after the security review, 2026-10-04: the
authorization, token, keys and UserInfo endpoints the discovery names are held to the issuer's own
rule of D4 — `https`, or `http` on a loopback host — or the start is refused, and an
`end_session_endpoint` that fails it is dropped with a warning; every call to the issuer goes
through one client that follows no redirect, reads at most 1 MiB of an answer and never puts an
answer's body into an error; and the ID token's `azp`, when present, must be the client id, and a
token for several audiences must name it.)*

**D2 — The groups claim is configuration, not an assumption.** The claim's name defaults to
`groups` and is configurable; the claim is read from the ID token and, when absent there,
from the UserInfo endpoint. A string-valued claim is treated as a single group. What the
groups mean — the gate and the mapping — is the next record's. *(Made concrete 2026-10-04: a list
of strings is the groups; empty names and repetitions are dropped; a claim of any other shape fails
the login rather than reading as no groups. UserInfo's `sub` must be the ID token's. A group name is
compared exactly, case and all.)*

**D3 — The test and development issuer is a plain Dex in minimal configuration:** one static
client (cowork's client id and secret), static users with passwords and group memberships
declared in Dex's configuration file, no connectors. ~~It runs in `docker compose` beside
PostgreSQL for development and as a service container in the integration tier.~~ *(Amended
2026-10-04: it runs as the container `cowork-dex` of `make dex-up` — `docker create`, the
configuration [`hack/dex/config.yaml`](../../hack/dex/config.yaml) copied in with `docker cp`, then
`docker start` — on `localhost:5556`, published on the loopback address only (`CONTAINER_BIND`),
beside the PostgreSQL and MinIO of `make postgres-up` and `make minio-up`; `make dev-up` starts all
three. The integration tier's job runs `make dex-up` on the runner's Docker daemon, because a
service container cannot take a configuration file. No compose file exists. The configuration has
four static users with the groups the gate's cases need —
[docs/developer/testing.md](../developer/testing.md#the-identity-provider-in-the-tests) lists them —
and an issuer of the tests' own, in the test's process, does what Dex cannot be made to do: a wrong
key, another audience, an expired token, a refused or failing refresh.)* Nothing in
cowork depends on it being Dex; it is the fixture, not the design.

**D4 — Configuration.**

| Variable | Default | Meaning |
|---|---|---|
| `COWORK_OIDC_ISSUER` | — (required to enable login) | the issuer URL discovery is fetched from |
| `COWORK_OIDC_CLIENT_ID` | — | the relying party's client id |
| `COWORK_OIDC_CLIENT_SECRET` | — (Secret) | the client secret; a public client without secret is not supported in the first release |
| `COWORK_OIDC_SCOPES` | ~~`openid profile email groups`~~ `openid profile email groups offline_access` *(amended 2026-10-04)* | the scopes requested |
| `COWORK_OIDC_GROUPS_CLAIM` | `groups` | the claim that carries the groups (D2) |
| `COWORK_OIDC_DISPLAY_NAME` *(added 2026-10-04)* | `single sign-on` | the provider's name on the login page's button, ~~"Log in with `<name>`"~~ "Sign in with `<name>`" *(amended 2026-10-04)*; at most 64 characters |

The redirect URI is `COWORK_BASE_URL` + `/auth/callback`, which makes `COWORK_BASE_URL`
required when the issuer is configured. A configured issuer that cannot be discovered at
start refuses the start, like an invalid configuration value; an unconfigured issuer leaves
the installation without a login and says so on the login page. *(Amended 2026-10-04: the default
scopes ask for `offline_access`, because without a refresh token the groups refresh of
[ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D5 cannot run once the access token has expired, and Dex, Entra and Okta issue one only for that
scope; scopes are separated by spaces or commas and must contain `openid`. The issuer is
`https://`, or `http://` on a loopback host for a development issuer — the browser carries the
code over its redirects and the backend the client secret to its token endpoint — with no user,
query or fragment, and is kept exactly as written, because discovery compares it with the issuer
the document names, trailing slash included. Every other variable of the identity provider set
without the issuer refuses the start, naming itself; the client id and secret are required with
it, and the secret is read as it is and never echoed. An unconfigured issuer leaves the login page
without the provider's button; whether there is a login at all is the local login's
([ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)).
The gate's variables are ADR 0030's.)*

**D5 — The identity of a person is the issuer's `sub`, scoped by the issuer.** The pair
(issuer, `sub`) is the stable key of a person; e-mail and name are display attributes
refreshed on every login, never the identity. One installation has one issuer in the first
release. *(Made concrete 2026-10-04: the pair is unique in `users`; a person of the provider has no
username, so no local account becomes one and the local login never finds one. The display name is
the `name` claim, else `preferred_username`, else the address, else the subject, at most 200
characters; the address is kept with the issuer's `email_verified`, null when it says nothing, and
is what an administrator may grant a role by — ADR 0030 D3.)*

## Consequences

- Any conformant issuer works by configuration; the one that is proven to work is the
  minimal Dex of D3, and the operations page documents it as the reference.
- Providers that cap the groups claim (large directories) are a configuration problem of
  that provider, surfaced by D2's UserInfo fallback and documented; cowork does not work
  around it with provider-specific graph calls.
- A single issuer per installation (D5) keeps account merging out of cowork; a tenant with
  its own identity provider federates in front of cowork or runs its own installation.
- ~~Development needs `docker compose` with PostgreSQL and Dex; `make dev-up` is the target
  that brings both.~~ *(Amended 2026-10-04: development needs Docker; `make dev-up` starts
  PostgreSQL, MinIO and Dex, `make dex-up` and `make dex-down` Dex alone, and `make dev` logs the
  browser in through it.)*

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
  tests that prove `state`, `nonce`, expiry, audience and signature are checked. *(Answered
  2026-10-04: go-oidc v3.21.0 and `golang.org/x/oauth2` v0.37.0, recorded in
  [docs/security/identity-provider.md](../security/identity-provider.md);
  `TestExchangeRefusesAnIDTokenThatDoesNotVerify` holds the signature, the audience, the expiry and
  the nonce, `TestCallbackFailures` and `TestTheCallbackRefusesWhatDoesNotHold` the state and the
  cookie. What stays is the library's own correctness, and the trust in the issuer the security
  page names.)* *(After the security review, 2026-10-04: the discovery and the key set are cowork's
  own code, held by `TestDiscoveryHoldsTheIssuerToItsRules`, `TestRefreshedIDTokens` and
  `TestAuthorizedParty`; their correctness is cowork's to keep now, the claims' checks still
  go-oidc's.)*
- D4's refusal to start without a reachable issuer makes an identity-provider outage a
  backend restart failure; the operations page says so, and the amendment — start, but
  disable login until discovery succeeds — is one if the owner prefers it.

## References

- [ADR 0023](0023-the-tenant-is-in-the-path.md) D5 — the membership check this login feeds
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D7 — `COWORK_*` configuration and `COWORK_BASE_URL`
- [ADR 0003](0003-test-and-ci-policy.md) D2 — the integration tier the Dex fixture joins
- [`backend/internal/oidc/oidc.go`](../../backend/internal/oidc/oidc.go), [`backend/internal/api/oidc.go`](../../backend/internal/api/oidc.go), [`backend/internal/config/oidc.go`](../../backend/internal/config/oidc.go), [`hack/dex/config.yaml`](../../hack/dex/config.yaml) — the implementation and the fixture
- [docs/security/identity-provider.md](../security/identity-provider.md) — what the login checks, and what it leaves open
