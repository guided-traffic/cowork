# Logging in through an identity provider, and what its groups decide

How cowork logs a person in as an OpenID Connect relying party, what it checks of what the issuer
says, what the issuer's groups decide — who gets in, who administers the installation, which
tenants a person belongs to and in which role — how a session and a token keep up with the groups,
what cowork keeps of the issuer's tokens, and what is recorded, as built on 2026-10-04. What a
session is once the login has made one is [sessions.md](sessions.md); the local login beside this
one is [local-accounts.md](local-accounts.md); what a tenant's administrators do with the groups —
mappings, grants, a restricted project's access list — is [tenancy.md](tenancy.md); what a token
may do is [tokens.md](tokens.md); what cowork trusts from the issuer, in one table with everything
else it trusts, is [trust-boundaries.md](trust-boundaries.md).

## One issuer, discovered at start

An installation has one issuer
([ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D5), configured by `COWORK_OIDC_ISSUER` with a confidential client — `COWORK_OIDC_CLIENT_ID` and
`COWORK_OIDC_CLIENT_SECRET`; a client without a secret refuses the start — and the variables of
the gate ([README.md, Configuration](../../README.md#configuration);
[`config/oidc.go`](../../backend/internal/config/oidc.go)).

- **The issuer is `https://`, or `http://` on a loopback host** (`localhost`, `127.0.0.0/8`,
  `::1`) for a development issuer, with no user, query or fragment (`checkIssuer`): the browser
  carries the code over the issuer's redirects, and the backend carries the client secret to its
  token endpoint.
- **So are the endpoints its discovery names.** The authorization endpoint, the token endpoint,
  the keys (`jwks_uri`) and UserInfo must be `https`, or `http` on a loopback host, with no user and
  no fragment, or the start is refused; an `end_session_endpoint` that fails the rule is dropped
  with the warning `the issuer's end_session_endpoint is dropped: a logout ends no session at the
  issuer`, which names the issuer and not the endpoint
  ([`oidc/client.go`](../../backend/internal/oidc/client.go) `checkEndpoint`;
  `TestDiscoveryHoldsTheIssuerToItsRules`, `TestADroppedEndSessionIsLogged`).
- **It is kept as written.** cowork's own discovery
  ([`oidc/oidc.go`](../../backend/internal/oidc/oidc.go) `Discover`) reads
  `<issuer>/.well-known/openid-configuration`, which must answer `200` with JSON, and refuses a
  document whose `issuer` is not the configured string exactly, trailing slash included.
- **Only asymmetric signatures.** Of the algorithms the document names in
  `id_token_signing_alg_values_supported`, cowork accepts `RS256`, `RS384`, `RS512`, `PS256`,
  `PS384`, `PS512`, `ES256`, `ES384`, `ES512` and `EdDSA`; `RS256` when it names none, as the
  standard makes it mandatory. A document that names only others — `HS256`, say — refuses the start
  ([`oidc/keys.go`](../../backend/internal/oidc/keys.go) `acceptedAlgs`).
- **Discovery runs at every start** of `cowork serve`, after the database check and before the
  start-up synchronisation ([`cmd/cowork/main.go`](../../backend/cmd/cowork/main.go)
  `discoverIssuer`, at most thirty seconds), and an issuer that cannot be discovered refuses the
  start, like an invalid setting (ADR 0029 D4; `TestDiscoveryFailure`, whose error names the issuer
  and never the secret). The keys are not fetched then: the key set fetches them when a token first
  needs them.
- **Every call to the issuer** — discovery, the token endpoint, the keys, UserInfo — goes through
  one HTTP client ([`oidc/client.go`](../../backend/internal/oidc/client.go) `newClient`): ten
  seconds per call; **no redirect is followed** — an answer that redirects is no answer of the
  issuer's, and a redirected token request would carry the client secret elsewhere; **at most 1 MiB**
  of any answer is read, and a longer one fails rather than being cut; and no error of the relying
  party carries an answer's body.
- The libraries: [go-oidc](https://github.com/coreos/go-oidc) v3.21.0 for the ID token's claims and
  UserInfo, `golang.org/x/oauth2` v0.37.0 for the code exchange and the refresh grant, go-jose v4.1.4
  for the signatures ([`backend/go.mod`](../../backend/go.mod)). The discovery and the key set are
  cowork's own: a refresh must tell keys that could not be fetched — the issuer's trouble — from a
  signature no key verifies, and no error may carry the issuer's answer into a log.

## The login

### The start

`GET /auth/oidc/login?return_to=<path>` takes no credential
([`api/oidc.go`](../../backend/internal/api/oidc.go) `LoginOidc`). It draws a `state` and a
`nonce` of 256 random bits each and a PKCE verifier, and answers `302` to the issuer's
authorization endpoint for the code flow: `response_type=code`, the client id, the redirect URI
`COWORK_BASE_URL` + `/auth/callback`, the configured scopes, the state, the nonce and the `S256`
challenge of the verifier (`TestTheLoginStart`, `TestExchangeYieldsTheVerifiedPerson`).

The state, the nonce, the verifier, the path to return to and the time go into the cookie
`__Host-cowork-oidc`: `HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=600`, no `Domain` —
`Lax`, because the issuer's redirect back is a top-level navigation, which carries it. Its value is
sealed with AES-256-GCM under a key derived from `COWORK_SESSION_KEY` by HKDF-SHA256 with the label
`cowork oidc login v1`, the cookie's name as additional data
([`auth/seal.go`](../../backend/internal/auth/seal.go)): a browser can neither read nor forge it,
and the test asserts that the value carries neither the state nor the path.

- **Nothing is decided at the start that a forged link could use.** The session is made by the
  callback, and only for the browser that holds the cookie the start set; the `__Host-` prefix
  keeps a sibling subdomain from planting one. A link to the start makes a person log in as
  themselves at most — whether they see the issuer's form is the issuer's session's business
  (H-28).
- **`return_to` is a path of this installation or `/`:** it starts with one `/`, not `//` and not
  `/\`, holds no control character and no backslash, is valid UTF-8 and at most 2048 bytes; anything
  else becomes `/`, never a refusal, so a bad link still leads to a login (`safeReturnTo`,
  `TestReturnTo`). It is checked again when the cookie is opened. The login is no open redirect.
- Without a provider, or with a gate that admits nobody, the start answers `303` to
  `/login?error=oidc_unavailable` ([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
  D8; `TestOIDCRoutesWithoutAProvider`).

### The callback

`GET /auth/callback` (`OidcCallback`) is where the issuer sends the browser back, and every
outcome clears the state cookie. Before the issuer is asked anything, each of these is
`oidc_failed` (`callbackRefusal`; `TestTheCallbackRefusesWhatDoesNotHold`, `TestCallbackFailures`):

1. the state cookie is missing, longer than 8192 characters, does not open with the server key, or
   is older than ten minutes by the backend's clock — a minute ahead passes, for another replica's
   clock;
2. the issuer answered with `error`;
3. the `state` is not the cookie's — compared in constant time;
4. there is no code.

Then the code is redeemed at the token endpoint with the PKCE verifier and the client secret, and
the answer must carry an ID token. A code used twice, or redeemed without its verifier, is refused
by the issuer and is `oidc_failed` too (`TestExchangeNeedsTheVerifier`), and so is an answer that
redirects or exceeds 1 MiB. The issuer may add parameters of its own, such as RFC 9207's `iss` or
`session_state`: the route takes them and reads none (`x-cowork-open-query` in the API document).
The mix-up attack `iss` defends against needs a client of several issuers; cowork has one.

### What the ID token must hold

- **The signature** is checked by cowork's key set
  ([`oidc/keys.go`](../../backend/internal/oidc/keys.go)): one signature, of an accepted algorithm,
  by a key the issuer publishes at `jwks_uri` — the one the token's `kid` names when it names one,
  never a key marked `use: enc`. The cached keys are tried first; when none verifies, the keys are
  fetched again, one fetch at a time, which is how a key rotation reaches cowork, and only valid
  public keys are kept.
- **`iss`** equals the configured issuer, **`aud`** contains the client id, and **`exp`** has not
  passed by the backend's clock, with no leeway; `nbf`, when present, with five minutes — go-oidc's
  verifier, read from its source at v3.21.0.
- **`azp`**, cowork's own check (`verify`): a token that names an authorized party must name the
  client id, and a token for several audiences must name one — cowork (`TestAuthorizedParty`).
- **The `nonce`**, cowork's own check as well — go-oidc leaves it to its caller: it must equal the
  cookie's, compared in constant time, and is never empty.

The tests hold the failures cowork depends on: a signature of another key, another audience, an
expired token, another login's nonce (`TestExchangeRefusesAnIDTokenThatDoesNotVerify`), another
authorized party, keys larger than 1 MiB (`TestDiscoveryHoldsTheIssuerToItsRules`), and an ID token
signed with a key the issuer does not publish through the whole callback (`TestCallbackFailures`).

### The groups

The claim `COWORK_OIDC_GROUPS_CLAIM` (default `groups`) is read from the verified ID token; when
the token does not carry it, from UserInfo, with the access token of the same answer, whose `sub`
must be the ID token's (OIDC Core 5.3.2; ADR 0029 D2). A list of strings is the groups and a single
string is one group; empty names and repetitions are dropped; anything else — a number in the list,
an object — fails the login rather than reading as no groups (`oidc.Groups`, `TestGroupsClaimShapes`,
`TestGroupsFromUserInfoAndAsAString`). An issuer without a UserInfo endpoint, or whose answer lacks
the claim as well, leaves the person with no groups, and so outside the gate. A group name is
compared exactly, case and all, wherever cowork compares one.

## The decision

One transaction of the system actor `system:identity-provider` decides a verified login
([`store/identity.go`](../../backend/internal/store/identity.go) `CompleteOIDCLogin`), under the
person's advisory lock ([below](#one-decision-about-a-person-at-a-time)). In this order:

1. **The gate.** The person is of the configured issuer — which a login's always is — and one of
   their groups is in `COWORK_OIDC_ALLOWED_GROUPS` or is `COWORK_ADMIN_GROUP` (`gate` in
   [`api/identity.go`](../../backend/internal/api/identity.go); `TestTheGate`). Outside it:
   `not_allowed`. The gate is absolute — no mapping and no grant lets a person in
   ([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
   D4).
2. **A deactivated person:** `not_allowed` (`TestADeactivatedPersonIsRefused`).
3. **The init state.** While no tenant exists, a person who is not in the administrator group:
   `not_initialised` ([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
   D5).

A refusal sends the browser to `/login?error=<code>` without a session and is recorded as
`login_refused`; it makes no person of someone who was none (`TestLoginThroughDex`, whose `dan` is
outside the gate; `TestInitStateThroughDex`). An active person who exists and is refused at the gate
has left it, and that is acted on as the freshest word there is (`leftTheGate`): their groups are
stored as the issuer said them, their administrator flag and the gate's stamp are cleared, every
session of theirs ends (`revoked`, cause `gate`), and their tokens meet the gate at their next
request. Their memberships are left as they are and derived from nothing: they cannot use them,
and the tenants do not count them as an administrator
(`TestLeavingTheGateStopsTheTokensAtOnce`).

Otherwise:

- **The person** is found by issuer and subject, or made ([below](#the-identity-is-issuer-and-subject)).
- **Their memberships** are derived in every tenant ([below](#memberships-follow-the-groups)).
- **The session** is made as the local login makes one ([sessions.md](sessions.md)), with the
  method `oidc`, the groups of the login, the time they were read and the issuer's refresh token,
  sealed ([below](#what-cowork-keeps-of-the-issuers-tokens)); the session the request presented
  ends in the same transaction
  ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D5). An issuer that gives
  no refresh token makes the backend warn, once per process.
- **The answer** is `303` to the path the login began with, setting the session cookie and clearing
  the state cookie.

Every failure is `303` to `/login?error=<code>`, with `&return=<path>` when the state cookie still
opened, stale or not, and the path is not `/` — so the next attempt lands where the person wanted
(`TestAFailedCallbackKeepsThePathThePersonWanted`). The codes are `oidc_failed`, `not_allowed`,
`not_initialised` and `oidc_unavailable`; the reason is in the log, never on the page.

## The identity is issuer and subject

The pair (issuer, `sub`) is the person, unique in `users` (`users_oidc_identity_key`;
[migration 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql);
[ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D5). A person of the provider has no username — a check keeps the two kinds of person apart
(`users_oidc_or_local`) — so the local login never finds them, and no local account, the local
administrator least of all, becomes a person of the provider.

Every login refreshes what the issuer says of the person: the display name (`name`, else
`preferred_username`, else the address, else the subject, trimmed and cut to 200 characters), the
e-mail address and the issuer's word on it (`email_verified`, a boolean or a string such as `true`
or `false`; null when absent or anything else), the groups and the administrator flag. What changed
is recorded as `updated`, the changed fields only — **a changed address as `email_changed: true`,
never as the address, and changed groups as `groups_changed: true`, never as the groups**: the audit
record is append-only, and an address or a group's name in it could not be erased
(`TestAnAddressIsInNoAuditRow`, `TestNoAuditRowNamesAPersonsGroups`;
[ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D6). The address and the name are display attributes; the address is
also what an administrator grants by ([H-26](#h-26)), and the tenant's administrators alone see it
in the member list and a project's access list
([tenancy.md](tenancy.md#members-grants-and-group-mappings)).

## The administrator group

The members of `COWORK_ADMIN_GROUP` are global administrators, and the group is behind the gate by
definition. The flag is set and cleared from the groups whenever they are read or judged anew — a
login, a refresh that reads them, a token's gate check — and a change is recorded
(`TestRefreshFollowsTheIssuersGroups`). A global administrator creates tenants — becoming the new
tenant's first administrator by a marked grant — and has no role in any tenant they were not given:
they list every tenant, see the administration of one without a role — its members, mappings and
settings — and grant themselves a role there, in a browser session, recorded in the tenant
([tenancy.md](tenancy.md#a-global-administrator-without-a-role)); no route deletes a tenant
([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2). While no tenant exists they and the local administrator are the only ones who log in
(`TestInitStateThroughDex`). With `COWORK_BOOTSTRAP_TENANT_SLUG` and `_NAME` the start creates the
tenant while none exists and maps the group to its `admin` role, so the group's members administer
it from their first login and the installation needs no local administrator
([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
D6; `TestBootstrapSeedsTheAdministratorGroupsMapping`).

Whoever may change that group at the issuer administers the installation.

## Memberships follow the groups

A tenant's group mappings give a group a role in the tenant; a person's **mapped membership** in a
tenant is the highest role the tenant's mappings give their groups, and nothing else writes it
([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D2; the administration is [tenancy.md](tenancy.md#members-grants-and-group-mappings)). It is
derived in every tenant at once — made where a mapping now gives a role, changed where the highest
role moved, removed where none gives one any more (`deriveEverywhere`) — at four moments, each only
for a person the gate admits:

| Moment | Groups it derives from | Cause recorded |
|---|---|---|
| a login | the ones the issuer just said | `login` |
| a session's groups refresh that reads them | the ones the issuer just said — unless the person's stored groups are newer, a later login's or another session's ([below](#the-groups-refresh)) | `refresh` |
| a token's gate check | the person's stored groups ([below](#the-token-gate)) | `token` |
| an administrator makes, changes or removes a mapping | the stored groups of every person of the configured issuer whose groups hold the mapping's group, who is active and whom the gate admitted at their last login, refresh or check (`RederiveGroup`) | `mapping` |

- **A grant is never touched.** An administrator's marked grant lives beside the mapped membership,
  and the effective role is the higher of the two (D3, D4; `TestLoginThroughDex`, where a new login
  leaves bob's grant as it was).
- **Nothing is derived for a person outside the gate.** A login or a refresh that finds the person outside the
  gate stores their groups and derives nothing, and a mapping's change passes over a person who is
  deactivated, of another issuer, or was refused at their last login or refresh
  (`TestARederivationLeavesWhoCannotAct`). Such a person's memberships stay as they were: they
  cannot use them — no session, no working token — the member list still shows them, and
  `last_admin` does not count them ([tenancy.md](tenancy.md#members-grants-and-group-mappings);
  [H-29](#h-29)). The person's next admitted login brings them in line.
- **Every change is an act of `system:identity-provider` in its tenant**, with the cause, announced
  as `membership.changed` to the tenant's members
  ([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
  D2). A mapping's change derives in the administrator's transaction: the administrator's act and
  the derivations commit together or not at all.

### One decision about a person at a time

A login, the application of a refresh's answer, a token's gate check and a mapping's change each
take a transaction-level advisory lock of the person (`identityLockNamespace`, `cowi`, the person's
id) before they read anything the decision depends on, so two of them decide about one person one
after the other, each on what the one before committed. A mapping's change runs under the tenant's
lock (`cowt`, `LockTenant`), which every administrator's change of a grant or a mapping takes first,
and then takes the locks of its persons in the order of their ids; no transaction takes a tenant's
lock after a person's. The derivations of a login, a refresh and a token's gate check take the
person's lock and no tenant's.

## The groups refresh

Every `COWORK_OIDC_GROUPS_REFRESH` (fifteen minutes; one at least) a session of the identity
provider reads the person's groups again
([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D5; `checkProviderSession`, `refreshSession` and `askIssuer` in
[`api/identity.go`](../../backend/internal/api/identity.go); `ClaimSessionRefresh` and
`ApplySessionRefresh` in [`store/identity.go`](../../backend/internal/store/identity.go)):

- **when.** On the session's first request after the interval, inside its authentication — before
  the CSRF check, the tenant boundary and the request timeout — and at an open event stream's
  heartbeat, which does not move the session's idle clock. A person who is not the configured
  issuer's — another issuer's, or any person of a provider while none is configured — is not
  refreshed: every session of theirs ends at once (`EndProviderSessions`, `revoked`, cause `gate`;
  `TestAPersonOfAnotherIssuerIsOutsideTheGate`).
- **once, and holding nothing while the issuer is asked.** Three steps:
  1. *The claim.* A short transaction moves the session's `refresh_retry_at` thirty seconds ahead,
     where the refresh is due and nobody holds it. The one request whose statement does that goes
     on; every other request of the session — on this replica or another — finds the refresh not
     due and is served at once on the groups the session holds.
  2. *The question.* The issuer is asked with no database connection and no lock held, on a context
     the request's end does not cancel — a refresh token the issuer rotated is not lost with a
     client that went away — within twenty seconds for all the calls of the refresh together.
  3. *The answer* is applied by a second short transaction, within ten seconds, under the person's
     lock and the session's row lock, and only while the lease is still this request's; one that
     outlived its lease and finds it taken leaves the session to the other request.

  The request that claimed waits for the three; `TestARefreshWaitsForNoOneElse` holds the rest:
  with a pool of two connections and an issuer that does not answer, the session's other requests,
  another person's request and the readiness check are answered meanwhile.
- **how.** With the session's refresh token, a refresh grant at the token endpoint. The groups come
  from the refreshed ID token when the issuer sends one that verifies and carries the claim — its
  subject must be the person's — else from UserInfo with the new access token, whose subject must be
  the person's too. A refresh token the issuer rotated replaces the stored one, also when what
  followed failed, because the old one is spent.

| What the issuer does | What happens | Recorded |
|---|---|---|
| answers the groups | they become the session's, and the person's — groups, administrator flag, the gate's stamp — and the memberships are derived from them. Outside the gate the groups are stored, the stamp and the flag are cleared, the memberships are left as they are, **every session of the person ends at once**, and their tokens meet the gate at their next request. When the person's stored groups are newer than the moment this request began — a later login's, another session's refresh — those are judged instead, as in the next row | `updated` and membership acts, cause `refresh`; `revoked`, cause `gate` |
| nothing to read: the session holds no refresh token — none was given at the login — or the answer carries the claim in neither the refreshed ID token nor UserInfo | the person's groups as they stand — their last login's, or another session's last read — are judged against the gate as configured now and become the session's; **nothing of the person changes** ([H-25](#h-25)); outside the gate, every session of the person ends | `revoked`, cause `gate`, if so |
| refuses: a `4xx` other than `429` with an OAuth `error` other than the six below — `invalid_grant`, Dex's `invalid_request` for a spent or unknown token, `access_denied`, any other; a refreshed ID token that does not verify — a signature no published key makes, another audience or authorized party, an expiry passed — unless it failed because the keys could not be fetched; one that names another subject, or whose groups claim is no list of names; UserInfo about another subject | **that session ends**; the person's others stay | `revoked`, cause `identity-provider` |
| cannot be asked, or answers what is no word about the person: no answer, a timeout, a `5xx`, a `429`, an answer that redirects or exceeds 1 MiB, a `4xx` without an OAuth error; the OAuth errors `temporarily_unavailable`, `slow_down`, `server_error` and `invalid_scope`, and `invalid_client` and `unauthorized_client`, which say cowork's client is refused and are logged at error level; keys that cannot be fetched for a refreshed ID token; UserInfo that fails | the session is served on the groups it holds, and the next attempt waits a minute (`refresh_retry_at`) ([H-24](#h-24)) | nothing; the log says why |
| — the sealed token does not open: the server key changed | that session ends ([H-27](#h-27)) | `revoked`, cause `identity-provider` |

The tests: `TestRefreshFollowsTheIssuersGroups` (once per interval; a lost group takes its
membership and the administrator flag with it), `TestLeavingTheAllowList` (the sessions end, the
token is refused and not revoked, and works again behind a gate that admits the person),
`TestARefusedRefreshTokenEndsTheSession` (both refusals; the person's other session stays),
`TestASpentRefreshTokenEndsTheSession` (Dex rotates the token at every refresh and refuses a spent
one), `TestARefreshedIDTokenThatDoesNotVerify` (keys that cannot be fetched serve the session, a key
the issuer does not publish ends it), `TestAnUnreachableIssuerLeavesTheSessionServed` (a dropped
connection, a `503`, a `429` without an OAuth error; once per minute),
`TestAClientTheIssuerRefusesIsTheConfigurationsError`, `TestWithoutARefreshTokenTheLoginsGroupsHold`,
`TestARefreshThatReadNothingKeepsThePersonsNewerGroups`, and in the `oidc` package
`TestRefreshRefusalAndUnreachability` and `TestRefreshedIDTokens`, which run every class of answer
above.

## What cowork keeps of the issuer's tokens

- **The ID token and the access token** are verified, used and discarded; no column holds them, and
  the browser never sees them ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md)
  D1).
- **The refresh token** is the one secret of the issuer cowork keeps, in
  `sessions.refresh_token_sealed`: AES-256-GCM under a key derived from `COWORK_SESSION_KEY` by
  HKDF-SHA256 with the label `cowork oidc refresh token v1`, a random nonce per seal, and the
  session's cookie hash as additional data, so a sealed token opens for its own session only, and a
  copy moved to another row does not open (`auth.Sealer`, `TestSealer`). It is opened in the
  backend's memory for a refresh and deleted with the session's row. The integration test reads the
  stored value and finds no token the issuer handed out in it
  (`TestNoIssuerSecretIsLoggedOrRecorded`).
- **One server key, no old one.** A sealed token opens under the key it was sealed with; no
  previous key is kept to open what it sealed
  ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D1, the owner's answer of
  2026-10-04), so a changed `COWORK_SESSION_KEY` ends each session that holds a refresh token at its
  next refresh, and its person logs in again — the change fails closed ([H-27](#h-27)).
- **Not done:** cowork never revokes a refresh token at the issuer. When a session ends, the row and
  the sealed token go, and the token at the issuer lives as long as the issuer lets it
  ([H-27](#h-27)).

## The token gate

A personal access token of a person of the provider is held to the gate its person's login is held
to ([`api/identity.go`](../../backend/internal/api/identity.go) `tokenGate`,
`store.CheckTokenGate`; [ADR 0035](../adr/0035-personal-access-tokens.md) D8):

- **Another issuer is outside at once.** A person who is not the configured issuer's — another
  issuer's, or any person of a provider while none is configured — is refused at every request,
  whatever their last check (`TestAPersonOfAnotherIssuerIsOutsideTheGate`).
- **Groups no older than the maximum age.** Groups that the person's last sign-in, or the last
  session refresh that read them, read longer ago than `COWORK_OIDC_GROUPS_MAX_AGE` — a week by
  default, `users.oidc_groups_at` — judge no token: every request of the person's tokens is
  `401 not_allowed`, whose detail says to sign in to the browser once, recorded as the refusal below,
  and nothing else is written. A sign-in, or a refresh of one of their sessions that reads the groups,
  makes the tokens work again ([ADR 0035](../adr/0035-personal-access-tokens.md) D8 as amended
  2026-10-04; `groupsTooOld`; `TestGroupsOlderThanTheMaximumAgeRefuseTheTokens`).
- **Otherwise once per interval.** The first request after the person's last check plus
  `COWORK_OIDC_GROUPS_REFRESH` judges the person's stored groups — those of their last login or of
  the last refresh that read them, `users.oidc_groups` — against the gate as configured at the
  check, under the person's lock. The issuer is not asked ([H-23](#h-23)).
- **Admitted:** the check is stamped (`gate_checked_at`), the administrator flag follows the groups,
  and the memberships are derived (cause `token`).
- **Outside:** `401 not_allowed`, recorded as a `refused` act of the token with the reason
  `not_allowed`, at most once per token, reason and hour ([tokens.md](tokens.md)); nothing else is
  written. The token is not revoked: every request checks again, and the token works again once the
  person is admitted — by a login, a refresh, or a gate configured to admit them
  (`TestLeavingTheAllowList`).
- **At once after the issuer's word.** A refresh that reads groups outside the gate, or a refused
  login, clears the stamp, so the person's tokens are refused at their next request, not an interval
  later (`TestLeavingTheGateStopsTheTokensAtOnce`).
- An event stream opened with such a token runs the same check at its heartbeat
  (`streamStillAdmitted`).

## Logout

`POST /auth/logout` ends the session ([sessions.md](sessions.md)). For a session of the identity
provider, when the issuer's discovery names an `end_session_endpoint` that passed the endpoint rule,
it answers `200` with `end_session_url`: that endpoint with `client_id` and
`post_logout_redirect_uri=<COWORK_BASE_URL>/login`, and no `id_token_hint`, because no ID token is
kept ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D4;
`TestLogoutAtTheIssuer`, `TestEndSessionURL`). The browser goes there — the UI follows the URL only
when it is `https:` or `http:` ([frontend.md](../developer/frontend.md#where-state-lives)); cowork
does not call the issuer itself. Every other logout answers `204`. Dex names no
`end_session_endpoint` ([H-28](#h-28)).

## Who decides who gets in

The issuer decides who is in which group; the installation's configuration decides which groups
pass the gate and which administer the installation; a tenant's administrators decide who belongs
in their tenant by grant and a restricted project's access list, and remove mappings
([tenancy.md](tenancy.md#members-grants-and-group-mappings)); a mapping is made and its role changed
by a global administrator who administers the tenant only, because every tenant shares the issuer's
one namespace of groups and a mapping brings everyone in its group into the tenant at once
([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D7). Every administration act that can give access — adding a member, setting a grant, making or
changing a mapping, restricting or opening a project, putting a person on an access list — takes a
browser session, and a token, an administrator's `admin` token included, is `403 session_required`: what it
gives would outlive the revocation of a leaked token. An act that only takes access away — removing
a grant, a mapping, an entry of an access list — takes an administrator's token as well. No agent
makes any of them. The whole set of session-only operations, and the rule, is
[tokens.md](tokens.md#what-only-a-session-does).

## What is recorded and logged

| Action | Actor | Where | When |
|---|---|---|---|
| `login_refused` | `system:identity-provider` | installation | a verified login refused: reason `not_allowed` or `not_initialised`, the note `outside the gate`, `deactivated` or `not initialised`, the person when one exists |
| `created`, `updated` (entity `user`) | `system:identity-provider` | installation | the first login — name, identity `oidc`, administrator flag, nothing of the groups — and what a login, a refresh that read groups or a token's gate check changed, with the cause; an address as `email_changed` and the groups as `groups_changed`, never the address or a group's name |
| `revoked` (entity `user`) | `system:identity-provider` | installation | the sessions the provider ended: `sessions_ended`, the cause `gate` or `identity-provider` |
| `logged_in` | the person | installation | a login, note `oidc` |
| `created`, `updated`, `deleted` (entity `membership`) | `system:identity-provider` | the tenant | a derived membership, with the cause |

An installation-level row of a system actor names no person as actor and is readable in the
database only; the tenant's rows show in its audit view. Every row written for a request — the
identity provider's included — carries the keyed hash of the client's address
([tokens.md](tokens.md#what-is-recorded)). **`login_refused` is written only after an ID token
verified**: a stale state, the issuer's error, a failed exchange or verification and a claim of the
wrong shape fail before anybody is known, and `oidc_failed` is in the log only. **No row names a
person's groups** — their change is `groups_changed: true`, and the memberships they cause are
recorded tenant by tenant, with the cause
([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D6; `TestNoAuditRowNamesAPersonsGroups`). A mapping's own rows name its group: they are an
administrator's act on the tenant's configuration, not a word about a person.

**No code, ID token, access token or refresh token reaches a log line or an audit row.**
`TestNoIssuerSecretIsLoggedOrRecorded` records every log level through a login, a refresh, an
issuer that cannot be reached and one that refuses, and searches the log and every audit row for
each secret the fake issuer handed out. A failed login's line names the request id, the code, the
reason and, where there is one, the error, which never carries an answer's body: for an answer of
the token endpoint its status and its OAuth `error` code — letters, digits, `_`, `.` and `-`, at
most 64 of them — and never its `error_description`; for an ID token that does not verify, the
verifier's reason, cut to 200 characters; for a call that got no answer, the method, the URL and the
cause ([`oidc/client.go`](../../backend/internal/oidc/client.go) `safeCode`, `transportError`;
`TestRefreshRefusalAndUnreachability` asserts that no refusal's error carries the body). The
callback's own `error` parameter is cut to 64 characters, and its `error_description` is never
logged. The backend's request log carries no query string. Outside the backend, the callback's
query — the code and the state — is in nginx's error log for a request nginx itself failed
([trust-boundaries.md](trust-boundaries.md#h-14), H-14), and in the access log of an Ingress
controller that logs query strings. A code is redeemed once and only together with the PKCE
verifier, which is in the sealed cookie, and the client secret.

The log lines an operator meets are in [runtime.md](../operations/runtime.md#the-login-through-the-identity-provider).

## In the data layer

What the identity provider decides is written by the system actor `system:identity-provider`,
whose transactions name it in `app.job = 'identity-provider'`
([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D3,
migrations [20](../../backend/internal/store/migrations/000020_identity_provider.up.sql)–[22](../../backend/internal/store/migrations/000022_membership_administration.up.sql)):

- `users`: it reads every person — the derivation of a mapping finds the persons whose groups hold
  its group — and inserts and updates only persons of the provider (`oidc_issuer` set, no username):
  a local account, the local administrator above all, is never its to touch.
- `tenants`: it reads whether any exists, for the init state.
- `group_mappings`: it reads every tenant's, and writes none.
- `memberships`: a mapped membership is inserted, changed and removed by it alone; a grant by a
  tenant's administrators alone.

The refresh's claim is no act of the provider's: it runs as the person, with the session's hash,
and writes nothing but the lease. The settings, the restrictive policies of the administration and
the person lookup are [tenancy.md](tenancy.md#two-database-roles) and
[data-access.md](../developer/data-access.md).

## What this does not cover

<a id="h-23"></a>
### H-23 — A token judges its person by groups up to `COWORK_OIDC_GROUPS_MAX_AGE` old

Live today for every person of the provider who holds a token. The gate a token meets reads the
person's stored groups, never the issuer. Removing a person from the allowed groups at the issuer
reaches their tokens at their next browser login or at the next refresh of a session they hold that
reads the groups — and at the latest when the stored groups grow older than
`COWORK_OIDC_GROUPS_MAX_AGE`, a week by default: from then on every token of the person is refused
until they sign in to the browser, where the issuer's groups of that moment judge them
([above](#the-token-gate)). So a person who only uses tokens keeps working tokens — and the
memberships those groups map to — for up to the maximum age after the groups were last read, no
longer until the token expires. A person the issuer disables outright is no different: their
sessions end at their next refresh, when the issuer refuses the refresh token, but a refusal stores
no groups, so their tokens go on until the groups of their last read are too old, and a sign-in goes
through the issuer, which no longer lets them in. No route deactivates a person of the provider, and no route lets an
administrator revoke another person's token ([tokens.md](tokens.md)). What does reach a token within
one interval is the gate's configuration — the check judges the stored groups against
`COWORK_OIDC_ALLOWED_GROUPS` and `COWORK_ADMIN_GROUP` as they are at the check — and a change of the
configured issuer reaches it at once. Mitigation: a shorter `COWORK_OIDC_GROUPS_MAX_AGE` — a day asks
a daily sign-in in the browser; to cut a person off at once, the operator sets the person's
`users.deactivated_at` in the database, which every token and login of theirs then meets as
revoked — outside the API, and recorded nowhere.

<a id="h-24"></a>
### H-24 — While the issuer cannot be reached, sessions are served on their last groups

Live whenever the issuer is down, slow, or answers what is no word about the person — a `5xx`, a
`429`, a temporary OAuth error, keys it cannot serve. A refresh that cannot reach the issuer serves
the session on the groups it holds and tries again a minute later, for as long as the issuer stays
away: the refresh has no bound of its own, and the session's absolute lifetime,
`COWORK_SESSION_LIFETIME` (twelve hours by default), bounds how long a session that is in use lives
on its last groups — the idle limit, two hours, ends one that is not. A person removed from a group
meanwhile keeps what the group gave: the session, the memberships, the administrator flag. The same
holds when the issuer refuses cowork's own client — `invalid_client`, `unauthorized_client`, after a
client secret was rotated at the issuer and not in cowork — which is logged at error level and ends
no session, so a wrong secret does not log everyone out and does not refresh anyone's groups either.
Each attempt holds no database connection and no lock while it waits ([above](#the-groups-refresh)),
but the request that claimed it waits for the issuer up to twenty seconds, and each due session pays
that once a minute for as long as the issuer stays away. A discovery that fails refuses the start
instead ([installation.md](../operations/installation.md#the-identity-provider)). Mitigation: alert
on the warning `the issuer could not refresh a session's groups` and on the error `the issuer refuses
cowork's client`; a shorter `COWORK_SESSION_LIFETIME` bounds how long stale groups serve.

<a id="h-25"></a>
### H-25 — Without a refresh token or a groups claim at the refresh, a session never learns the person's groups anew

Live with an issuer that gives no refresh token — most give none without `offline_access` — or
whose refresh carries the groups claim in neither the refreshed ID token nor UserInfo. Such a
session never hears the issuer's groups again: at every interval it is judged on the person's groups
as they stand — those of the person's last login, or of another session's refresh that read them —
against the gate as configured now, so a changed gate still ends it, but a person removed from a
group at the issuer keeps what the group gave until they log in again or the session ends, twelve
hours at most by default. Such a refresh writes nothing of the person: their stored groups,
administrator flag and memberships stay as the last read left them, and an older session's groups
never go over a newer login's (`TestWithoutARefreshTokenTheLoginsGroupsHold`,
`TestARefreshThatReadNothingKeepsThePersonsNewerGroups`). The backend warns once per process
(`the issuer gave no refresh token …`, `… carries no groups claim …`). Mitigation: ask for
`offline_access` — the default scopes do — and have the issuer send the groups at a refresh.

<a id="h-26"></a>
### H-26 — Granting by e-mail address trusts the issuer's word on the address

Live wherever administrators add members by address. `POST …/members` finds a person among the
active persons of the configured issuer by the address the issuer asserted at that person's last
login, compared without regard to case. Only an address the issuer marked verified
(`email_verified: true`) matches; one about which it said nothing matches only while the operator
sets `COWORK_OIDC_EMAIL_TRUSTED` to `true`, `false` by default; one it marked unverified never
([ADR 0030](../adr/0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md)
D3; `TestAnAddressTheIssuerSaidNothingAboutIsTrustedOnlyWhenConfigured`). What remains is the
issuer's word itself: an issuer that marks an address verified it did not verify, and — dormant by
default, live once an operator sets `COWORK_OIDC_EMAIL_TRUSTED=true` for a provider whose users choose
their own address — a provider that lets its users choose one without verifying it and says nothing.
There a person can claim a colleague's address, and an administrator who grants that address before
the colleague has ever logged in grants the claimant — the grant is the person's, not the address's,
and stays when the address changes. Two active persons with one address answer
`409 person_ambiguous` and nobody is granted, which holds only once both have logged in. The identity
itself is the issuer and the subject, never the address. Mitigation: leave `COWORK_OIDC_EMAIL_TRUSTED`
`false` unless administrators issue the provider's addresses — persons of a provider that sends no
claim are then admitted by a group mapping; an administrator who checks the name in the answer; the
username for a local account.

<a id="h-27"></a>
### H-27 — Whoever holds the database and the server key can open the stored refresh tokens

Live wherever a provider session holds a refresh token. The seal protects a copy of the database, a
dump or a backup taken alone. Whoever also holds `COWORK_SESSION_KEY` opens every stored refresh
token, and with the client secret — both sit in the serving pod's environment — redeems them at the
issuer for tokens of those persons, with the scopes cowork was granted, for as long as the issuer
honours them; cowork never revokes one, so a token taken from a backup can outlive the session it
belonged to. Rotating the server key makes every stored token unopenable — no previous key is kept to
open them ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D1, amended
2026-10-04 to this): each provider session that holds one ends at its next refresh, and its person
logs in again; a session without a refresh token is not affected. The rotation also fails the logins
in flight (`oidc_failed`), invalidates the cursors, starts the throttle's count of an address over,
makes the audit rows' address hashes before and after it incomparable, and refuses an idempotent
retry across it as `422 idempotency_mismatch`
([installation.md](../operations/installation.md#the-secrets) lists each). It does not make a copy of
the database taken before it safe: the refresh tokens sealed in that copy open under the old key.
Mitigation: guard the database, its backups and the Secrets as one; after a suspected compromise of
both, rotate the server key and revoke the client's tokens at the issuer.

<a id="h-28"></a>
### H-28 — An issuer without an end-session endpoint keeps its own session after a logout

Live with Dex, with every issuer whose discovery names no `end_session_endpoint`, and with one whose
endpoint the start dropped because it is neither `https` nor `http` on a loopback host. A logout ends
the cowork session and answers `204`; the session the browser holds at the issuer stays, so the next
*Sign in with* on that browser may pass without a password — on a shared computer, as the person who
logged out. With an endpoint, the browser is sent there, and a browser that does not follow — closed,
offline — leaves the issuer's session as well; the URL carries no `id_token_hint`, and an issuer that
wants one may ask the person to confirm, or refuse. Not verified: whether Dex v2.45.1 keeps a session
of its own between two logins — the integration tier's browser keeps no cookie of Dex — and the
end-session URL against any issuer but the fake one (`TestLogoutAtTheIssuer`). Mitigation: an issuer
with an end-session endpoint; on a shared computer, log out at the issuer as well.

<a id="h-29"></a>
### H-29 — The issuer's word can leave a tenant without an administrator who can log in

Live in every tenant whose administrators hold the role through the identity provider. `409
last_admin` holds an administrator's acts — changing or removing a grant, changing or removing a
mapping, deactivating an account the tenant manages — to leaving an administrator who can log in:
active, and a local account or a person of the configured issuer whom the gate admitted at their
last login, refresh or check (`TestTheLastAdministratorMustBeAbleToAct`). Nothing holds the
issuer's word to it. A derivation at
a login, a refresh or a token's gate check is never refused: the tenant's last administrator by a
mapping who leaves the group loses the role at their next login or refresh. A person who leaves the
gate keeps their memberships but cannot log in, and their tenants keep no administrator who can. And
the derivations take no tenant's lock, so an administrator's change that counted such a person a
moment before can commit beside the derivation that takes their role — read from the code, run by no
test. Afterwards nobody of the tenant can change its settings, members, mappings or restrictions
until a global administrator acts. Recovery: a global administrator who holds no role in the tenant
grants themselves `admin` — a marked grant recorded in the tenant with them as its actor, which
takes no administrator away and so meets no `last_admin` — and gives the tenant an administrator of
its own ([tenancy.md](tenancy.md#a-global-administrator-without-a-role);
[ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2; `TestAStrandedTenantIsRecoveredByTheSelfGrant`); or put a person into the mapped group at the
issuer — the mapping stays, so their next login makes them administrator. What remains: nothing
tells anybody that a tenant has no administrator who can log in, so it stays without one until a
global administrator looks — one who holds no role there grants themselves `admin`, one who holds a
lower role raises their own grant to it; and an installation whose global administrators cannot log
in either — the administrator group
emptied at the issuer, no local administrator configured — has only a grant written into the
database, outside the API and recorded nowhere. Mitigation: keep one
administrator of every tenant by a grant to a local account the tenant manages itself, which neither
a derivation nor the gate touches and whose deactivation the tenant's `last_admin` holds — an
account another tenant manages, that tenant can still deactivate ([local-accounts.md](local-accounts.md#h-32)
H-32).
