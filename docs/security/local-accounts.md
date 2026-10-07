# Local accounts and the local login

How a person without an identity provider logs in: the local account and the local
administrator, how a password is stored and checked, what the login answers and how it counts
and locks, who may create, reset and deactivate an account, and what is recorded — as built on
2026-10-03. What a session is once the login has made one is [sessions.md](sessions.md); what
protects its writes is [csrf.md](csrf.md); what a token may do is [tokens.md](tokens.md); how a
request is kept inside its tenant is [tenancy.md](tenancy.md).

## What an account is

A local account is a person with a row in `local_accounts`
([migration 15](../../backend/internal/store/migrations/000015_local_accounts.up.sql);
[ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)
D1, D2): the Argon2id hash of the password, whether it is temporary
(`password_change_required`), where the account comes from (`origin`) and which tenant manages
it (`managing_tenant_id`). Its identity is `local:<username>`; the username is the person's
`users.username`, unique in the installation. A person without such a row — a fixture person, or a
person of the identity provider, who has no username at all
([identity-provider.md](identity-provider.md#the-identity-is-issuer-and-subject)) — cannot log in
with a password.

| Origin | Made by | Managed by | Is |
|---|---|---|---|
| `config` | the start-up synchronisation, from `COWORK_LOCAL_ADMIN_USERNAME` and `_PASSWORD` | nobody: the Secret is its source | a global administrator, a full account |
| `tenant` | `POST …/accounts`, by an administrator of the tenant | that tenant's administrators | whatever role the administrator granted, by a marked grant in that tenant |

There is no registration and no invitation link: the creation route and the configuration are
the only gates (D1). The `global_admin` flag is set by the synchronisation alone — the policy
on `users` refuses it to every request — and a global administrator has no role in any tenant
until they grant themselves one ([ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2).

## How a password is stored and checked

- **Argon2id**, in the PHC string form with its parameters recorded:
  `$argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>` — 19 MiB of memory, two passes, one lane, a
  16-byte salt of its own and a 32-byte key
  ([`auth/password.go`](../../backend/internal/auth/password.go)
  `ArgonMemoryKiB`, `ArgonIterations`, `ArgonParallelism`; ADR 0033 D4). These are OWASP's
  minimum; raising them is an amendment, and a stored hash is verified with the parameters it
  records, so it keeps working. Nothing re-hashes a password at login after such an amendment.
- **Verification is bounded** twice. `parseHash` holds a row's parameters to sane bounds, so a
  damaged or foreign hash cannot ask the server for gigabytes or minutes; and at most two
  hashes are computed at once, so a flood of attempts holds at most 38 MiB, which waits behind
  the request's own timeout. A damaged hash is a password that does not fit, not a `500`.
- **The policy is length only**, counted in characters: `COWORK_PASSWORD_MIN_LENGTH`, 12 by
  default, 8 at the lowest — below it the backend refuses to start — and at most 1024, which
  the API document also holds every password field to. No character classes, no history, no
  expiry; it holds for the local administrator too (`CheckPassword`; ADR 0033 D3).
- **A password is in no log, no audit row, no stored answer and no error text.** A validation
  message names the bound ("must be at least 12 characters"), never the value; a configuration
  error names the variable. `TestNoPasswordCookieOrTokenIsLoggedOrRecorded` records every log
  level through the whole flow and searches the log, every answer and every table of the login
  for each password it used. The fingerprint a creation with an `Idempotency-Key` keeps for a
  day covers the body, temporary password included, as an HMAC under a key derived from the
  server key ([ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
  D4): a backup of the database alone gives nobody a hash to test guesses against
  (`TestIdempotencyFingerprintIsKeyedByTheServerKey`).

## What the login answers

`POST /auth/local` ([`api/login.go`](../../backend/internal/api/login.go) `LoginLocal`):

1. **The origin check** ([csrf.md](csrf.md)) — before anything else, `403 csrf` when the
   request is not from `COWORK_BASE_URL`.
2. **The address throttle**: `COWORK_LOGIN_ADDRESS_LIMIT` (20) attempts of one client address
   within a minute, whatever their outcome, and the next is `429 too_many_attempts` with
   `Retry-After: 60` — before any hash is computed, so a flood costs the server a read. A
   throttled attempt is not counted, so the minute slides. The client address is the one
   found under [the rule below](#the-client-address) — an IPv6 client by its /64, the network
   one subscriber is given, whose addresses would otherwise each be a fresh bucket — keyed-hashed
   with a key derived from the server key; the address itself is stored nowhere
   ([H-17](#h-17)); `TestAddressHash` holds the notation, the /64 and the key.
3. **The name**, trimmed and lower-cased; a value that still cannot be a username names no
   account and is treated like any name that names none.
4. **One Argon2id computation, always**: the presented password against the account's hash —
   or against a dummy hash, made at start with the current parameters, when the username names
   no account, a deactivated one, or a person without a password. The integration test counts
   the computations for an unknown username, a malformed one, a person without an account, a
   deactivated account, a wrong password and a success: one each
   (`TestEveryLoginFailureIsTheSame`).
5. **One transaction under the username's advisory lock decides the outcome**
   (`store.RecordLoginAttempt`): the lock of the username is read, the attempt is counted, and
   the outcome follows — success, failure, locked, or refused for the init state. Concurrent
   attempts at one name are decided one after the other, so a burst of parallel guesses cannot
   make more guesses than the lock allows.
6. **Every refusal is the same `401 invalid_credentials`** — same status, same body, same
   headers, no cookie — for an unknown username, a wrong password, a locked account, a
   deactivated one and a username that cannot be one. Neither the answer nor the time says
   whether an account exists.
7. **The init state** ([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
   D5): while no tenant exists, a person who is not a global administrator and whose password
   fitted is `403 not_initialised` and gets no session. Only after the password fitted: a wrong
   password stays `401`, so the `403` tells nothing to anyone who does not know the password.
8. **A session** is made in a transaction of its own, as the person
   ([sessions.md](sessions.md)); the answer says whether the password is temporary.

## The client address

The throttle counts the address of the client, and the backend has to take it from somewhere:
the TCP peer cannot be forged, but behind the Ingress it is a controller pod, and every browser
behind it would share it. `COWORK_TRUSTED_PROXIES` names the networks of the proxies that stand
in front of the backend — a comma-separated list of CIDRs, IPv4 and IPv6, validated at start,
empty by default — and the client is found by walking `X-Forwarded-For` from the right
([`api/clientaddr.go`](../../backend/internal/api/clientaddr.go) `clientAddress`;
[ADR 0035](../adr/0035-personal-access-tokens.md) D2):

1. The walk starts at the TCP peer.
2. While the current address is inside a trusted network, the entry to its left in the header
   becomes the current address.
3. The first address that is not trusted is the client. What stands to its left was written
   by the client or by someone beyond it, and is never read.

An entry that is no address stops the walk, and the hop before it is the client; a header that
runs out while every hop is trusted leaves the leftmost one. With the list empty — the default —
and for a peer outside it, the peer is the client and the header is not looked at, so nobody
chooses their bucket with a header. The address is made canonical before it is hashed —
IPv4-mapped IPv6 unmapped, no zone, compressed — so a client is one bucket however a proxy
writes it. `TestClientAddressWalksFromTheRight` and `TestClientAddressStopsAtAMalformedEntry`
run the table (no header, one hop, chains, spoofed entries to the left, malformed entries, IPv6,
IPv4-mapped IPv6, the empty list; `FuzzClientAddress` holds that an untrusted peer is always the
client); `TestAddressThrottleCountsTheClientBehindTrustedProxies` puts two clients behind one
trusted proxy, throttled apart, and shows that a spoofed entry moves nobody to another bucket;
`TestAddressThrottleIgnoresTheHeaderOfAnUntrustedPeer` holds the other half.

What the rule rests on: every proxy in the list writes the address it saw as the rightmost entry,
in place of what the client sent or appended to it. The one proxy of a chart installation is the
Ingress controller — the frontend proxies nothing
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3); ingress-nginx writes `X-Forwarded-For $remote_addr`, another controller does by its own
setting ([installation.md](../operations/installation.md#the-client-address-and-the-trusted-proxies)
names the chain and what to set). It also rests on the list being the proxies and no more, and on
no other pod inside it reaching the backend ([H-17](#h-17)).

## Lockout

Five failures of one username within fifteen minutes lock it
(`COWORK_LOGIN_MAX_FAILURES`, `store.LoginWindow`; ADR 0033 D6). The count is by the username
as presented, in `login_attempts`, whether or not an account has it — **an unknown username is
counted and locked exactly like a known one** — so the lockout, like the answer, reveals
nothing about which accounts exist. The fifth failure sets the lock; an attempt that meets a
lock is refused whatever its password and counts as one more failure.

- `COWORK_LOGIN_LOCKOUT=window` (the default): the lock ends when the window has passed since
  it was set.
- `COWORK_LOGIN_LOCKOUT=admin`: a lock on an account stays until an administrator unlocks it
  (`DELETE …/accounts/{username}/lockout`), or — for the local administrator — until the Secret
  is rotated and the backend restarted. A lock on a username nobody has ends with the window
  all the same; nobody could unlock it, and nothing can show the difference.
- **What an unlock forgets** is the failures and the lock of the username; the creation of an
  account of a name forgets those of that name, so an account does not begin locked by what
  someone tried before it existed.
- **What the record keeps**: each failure is an audit row `login_failed` with the reason
  (`wrong_password`, `unknown_account`, `account_deactivated`), the lock a row `locked`, and an
  attempt against a lock a row `login_failed` with the reason `locked` at most once an hour per
  lock; a refusal for the init state is `login_failed` with `not_initialised`. They are
  installation-level rows of the system actor `system:login` that name the person when there is
  one — never the attempted password and never an unknown username, which could be a password
  typed into the wrong field. A throttled attempt writes no row; the request log has it.
- The attempts older than the window and the locks of the `window` mode that ended are removed
  by the job `login-expiry`, hourly, which records one `expired` act per run that removed any.
- A wrong **current password** in `PUT /api/v1/me/password` is a failed attempt of the account
  as well, so a stolen session cannot guess the password through it.

## Temporary passwords, changes and resets

- An administrator creates the account with a temporary password and the person changes it at
  the first login before anything else. The UI shows the temporary password in a plain text
  field that password managers are told to leave alone, so none offers to save it as the
  administrator's own login or fills the administrator's password into it, and it generates
  `max(24, COWORK_PASSWORD_MIN_LENGTH)` characters
  ([frontend.md](../developer/frontend.md#where-state-lives)) ([sessions.md](sessions.md) "What a session may do"):
  `PUT /api/v1/me/password` verifies the current password, applies the length policy, refuses
  a new password equal to the current one, ends every other session of the account and clears
  the flag (ADR 0033 D4).
- An administrator's **reset** (`PUT …/accounts/{username}/password`, a session only) sets a
  new temporary password, ends every session of the account and sets the flag again. It does not unlock the
  account (an unlock is its own act) and does not revoke its tokens (revoking is the person's
  or a deactivation's).
- **The local administrator's password is the Secret's.** `PUT /api/v1/me/password` refuses
  it (`403 forbidden`, naming the variable): the next start would put the configured password
  back and end the session that changed it. The password changes where it comes from.

## Who may manage which account

An account is a person across the whole installation — its password and its sessions are
nobody's tenant's — so a tenant's administrators manage the accounts **their own
administrators created** (`managing_tenant_id`) and no others. The tenant's list
(`GET …/accounts`) shows those; another tenant's account, a global administrator and the local
administrator answer `404 not_found`, indistinguishable from a username nobody has
(`TestAccountAdministration` compares the bodies). Without this rule an administrator of a
tenant could reset the password of a person who is also a member of another tenant — a global
administrator, say — log in as them and read what that tenant holds. The rule is in the
policies of migration 15 as well as in the handlers: `app_manages_account` is true only for an
administrator of the current tenant and an account that tenant manages, and the policies on
`users`, `local_accounts`, `sessions`, `tokens`, `login_attempts` and `login_locks` use it
(`TestPoliciesOfThePersonsAndTheirAccounts`).

- **Not their own account.** An administrator may not reset their own password, unlock
  themselves or deactivate themselves through these routes (`403 forbidden`): a reset would
  change the password without the current one a stolen session lacks, an unlock would lift the
  lock on guessing, and a deactivation would lock the administrator out for good. Ending their
  own sessions is allowed.
- **Who may.** The tenant's administrators with `admin` scope, never an agent: account
  administration is the hard-off rule "administration" ([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
  D3). A member and a viewer are `403`.
- **Creating an account and resetting a password take a browser session only.** A token — an
  administrator's, with `admin` scope — is `403 session_required` before anything is written
  ([ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)
  D1, D5; `TestAccountAdministration`), because what these two routes make outlives the token:
  an account, or a password only the administrator and the person know, would stay with whoever
  held a leaked token after the token was revoked. The API document declares the two with the
  session cookie alone, and the unit test over the document holds that set. Listing the
  accounts, unlocking one, deactivating one and ending its sessions remove or restrict access,
  leave nothing behind, and stay open to an administrator's token
  (`TestAccountRoutesAnAdministratorsTokenMayStillCall`).
- **A deactivation** ([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
  D5) revokes every token of the person, ends every session and refuses the login; the person,
  the grants and every act they made stay. No route reactivates a person, and the memberships
  of a deactivated person are not marked inactive: they stay as they were.
- **A deactivation is held to `last_admin` in the managing tenant.** A deactivated
  person counts as no tenant's administrator, so the deactivation is a change of who administers
  the tenant and is held to the rule the changes of grants and mappings are held to
  ([tenancy.md](tenancy.md#members-grants-and-group-mappings);
  [ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
  D1): it takes the tenant's lock first (`LockTenant`), deactivates, and is refused with
  `409 last_admin` when no administrator who can log in remains — the whole act rolls back, so no
  token is revoked, no session ended and no act recorded (`DeactivateAccount`, `lastAdmin`;
  `TestADeactivationLeavesTheTenantAnAdministrator`). The administrator acting counts unless
  something took their own account or role away meanwhile, so what the refusal decides is the
  race: two administrators who deactivate each other at the same moment are decided one after the
  other, and the second, whose own account the first has just deactivated, meets `last_admin` —
  or, authenticated only after the first committed, finds its session ended
  (`TestTwoAdministratorsCannotDeactivateEachOther`, eight rounds). The other tenants the person
  administers are not asked ([H-32](#h-32)).
- **A username exists once in the installation**, so `409 username_taken` tells an
  administrator that a name is taken, whichever tenant has it.

## The local administrator

`COWORK_LOCAL_ADMIN_USERNAME` and `COWORK_LOCAL_ADMIN_PASSWORD` name one account the
configuration keeps ([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
D1–D4, [`bootstrap/bootstrap.go`](../../backend/internal/bootstrap/bootstrap.go)). Both or neither: one alone
refuses the start, naming the missing variable. At every start of `cowork serve`, after the
migrations and under an advisory lock, as `system:bootstrap` — and, in the chart's job mode, in the
migration Job after the schema step as well, under the same lock
([ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md) D4):

- the account is **created** — a global administrator, display name its username;
- a password that does not verify against the stored hash is **re-hashed**, every session of
  the account ends, and the failures and the lock of its username are forgotten — rotating the
  Secret and restarting is how a locked or leaked administrator is recovered;
- an account that was **deactivated** because the variables went is reactivated; a revoked
  token stays revoked;
- a **tenant administrator's account of the same name** is taken over: the configured password,
  no session, no token, no managing tenant — the person behind the name is the operator's;
- an account the configuration kept under **another username**, or when both variables are
  empty, is deactivated, its tokens revoked and its sessions ended — never deleted;
- the **bootstrap tenant** (`COWORK_BOOTSTRAP_TENANT_SLUG` and `_NAME`) is created only while no
  tenant exists, with the administrator as its first administrator by a marked grant; once a
  tenant exists the variables do nothing. They need the local administrator, who becomes the
  first administrator: a tenant without an administrator cannot come to exist (ADR 0032 D7).

A start that finds everything as configured changes nothing and records nothing
(`TestBootstrapKeepsTheConfiguredAdministrator`); replicas that start together agree
(`TestConcurrentBootstrapsAgree`).

## What this does not cover

<a id="h-16"></a>
### H-16 — There is no second factor

Live today, said aloud by ADR 0033 D7. A local account is its password: a phished or reused
password is a full account until the person changes it or an administrator deactivates the
account, and the local administrator — a global administrator, the one account that can
create tenants — is no exception. The mitigations are the ones the record names: no public
registration, the length rule, the lockout and the address throttle (which slow guessing, not
phishing), and the ability to deactivate any managed account. Keep the local administrator
switched off (both variables empty) where an identity provider with a second factor does the
work.

<a id="h-17"></a>
### H-17 — The address throttle is as good as the trusted proxies it is given

Live today. The throttle counts the client address under [the rule above](#the-client-address),
and the rule is only as sound as `COWORK_TRUSTED_PROXIES` is right — and as the network behind it
is closed:

- **Empty, which is the default.** The client is the TCP peer, which behind the Ingress is a
  controller pod: `COWORK_LOGIN_ADDRESS_LIMIT` is then one limit for every browser behind that
  pod, not one per client — twenty attempts a minute from anyone, which is also the pace at which
  a guesser can try passwords across many usernames, and a way for one client to keep every other
  from logging in for as long as it keeps trying. The chart notes say so at install time while a
  local administrator is configured. Nothing that reaches the backend can choose its address: a
  pod that calls the backend Service directly is its own client.
- **Too narrow.** An Ingress controller whose address is not listed stops the walk at the
  controller: every client behind it shares the controller's address, one bucket per
  controller pod.
- **Too wide.** A trusted network that holds clients — an entry such as `0.0.0.0/0`, or all of a
  private range for an installation whose users sit in it — makes those clients hops themselves:
  the walk goes on into the entries they wrote, and they choose their own address. They dodge the
  throttle by changing it with every attempt, and they can fill another client's bucket to keep
  that person from logging in.
- **A trusted network that holds other pods.** The Ingress controller commonly runs as an ordinary
  Deployment, so its address comes from the pod network at every start, and the list has to name
  that network — and with it every pod in the cluster. Any of those pods that reaches the backend
  directly — the backend Service answers every pod of the cluster — is a trusted peer, and the walk
  reads what it wrote into `X-Forwarded-For`: it chooses its client address, for the throttle's
  bucket and for the source hash of every audit row of its requests. Verified on 2026-10-04 in a
  kind cluster behind ingress-nginx v1.15.1, the list naming the pod network and the limit three:
  a pod that sent each attempt to the backend Service with another forged header was never
  throttled, the same pod without the header was at its fourth attempt, and the same forgery
  through the controller moved nothing, because ingress-nginx writes the address it saw in place
  of the header. The chart ships no NetworkPolicy
  ([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
  D3): restricting who reaches the backend's pods — the controller, and the scripts that must —
  is the cluster administrator's network policy, enforced only by a network plugin that implements
  NetworkPolicy ([installation.md](../operations/installation.md#network-policies-are-the-clusters)).
  A narrower list helps only where the controller's addresses are narrower than the pod network:
  the host network, a fixed range. The chart's notes warn whenever the list is set.
- **One address is one bucket.** An IPv6 client counts by its /64, but a client that holds
  many networks or addresses — a shorter IPv6 prefix, which some providers give out, or a
  botnet — has a bucket for each and is slowed by this limit only that much; the lockout of
  the username is what bounds the guesses against one account, at the price of H-18.

The keyed address hash every audit row of a request carries is found by the same rule and is as
good as the list in the same way ([tokens.md](tokens.md#what-is-recorded)). Rate limits at the
Ingress, which sees the real client, do what this limit cannot;
`COWORK_LOGIN_ADDRESS_LIMIT=0` switches the throttle off.

<a id="h-18"></a>
### H-18 — A lockout is a lever against its owner

Live today. Anyone who knows a username can fail five times a quarter of an hour and keep its
account locked out of its own login; in `admin` mode until an administrator unlocks it. The
local administrator is no exception: its recovery is rotating the Secret and restarting, which
the attacker cannot undo. Failures from many clients and addresses are not slowed by the
address throttle ([H-17](#h-17)). The throttle and the lockout are the price of limiting
guesses without a second factor; `COWORK_LOGIN_MAX_FAILURES=0` switches the lockout off and
leaves the guesses to the throttle and to the Argon2id cost.

<a id="h-19"></a>
### H-19 — An administrator knows the temporary password, and the account stays their tenant's

Live today. An administrator who creates an account or resets a password chooses a password
only they and the person know, and can log in as the person before the person does: the
temporary password forces a change at the next login, not before it. Every act is recorded —
`password_reset` and `logged_in` rows — and a login that consumed the flag shows as the person
changing their password, but nothing tells the person that someone else changed it first. The
same administrators keep that power for as long as the account exists: the tenant that created
an account manages it even once the person is a member of other tenants, so an administrator of
the creating tenant can reset the password and enter the person's other tenants as them. The
mitigation is ADR 0033 D1's: the creation is the gate, the creating administrator is
accountable, and deactivation is final for the account. A password reset does not revoke the
person's tokens.

<a id="h-20"></a>
### H-20 — The local administrator's password lives in a Secret and in the pod's environment

Live whenever it is configured. The password reaches the process as an environment variable —
the serving container's and, in the chart's job mode, the migration Job's —: anyone who can read
the pod's spec or the Secret reads it, as with the database URL. A leaked
password stays valid until the Secret is rotated **and** the backend restarted — the
synchronisation reads the environment once, at start, and the migration Job's only at the next
install or upgrade
([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
residual risks). The inline chart values `localAdmin.username` and `localAdmin.password` put
the credential in plain text into the release Secret and into `helm get values`
([trust-boundaries.md](trust-boundaries.md) "Where the credentials live"); use
`localAdmin.existingSecret`.

Not built: a way for a person to recover their own password without an administrator — there
is no e-mail flow ([ADR 0020](../adr/0020-notifications-are-an-in-app-inbox-per-person.md)); a
route that creates an account for no tenant or lists the accounts of other tenants for a global
administrator; the reactivation of a deactivated person.

<a id="h-32"></a>
### H-32 — Deactivating an account does not ask the other tenants it administers

Live in every tenant one of whose administrators is a local account another tenant manages. Any
tenant's administrator grants a role to a local account by its username
([tenancy.md](tenancy.md#members-grants-and-group-mappings)), so an account one tenant manages may
be an administrator of another — the only one there who can log in. The deactivation holds the
managing tenant to `last_admin` under that tenant's lock
([above](#who-may-manage-which-account)) and asks no other tenant: the managing tenant's
administrators deactivate the account without seeing that it leaves another tenant without an
administrator, and that tenant's own changes, which take its lock and not the managing tenant's,
count the account until the deactivation has committed. What stands in the way of the check is
the boundary itself: the deactivation runs in the managing tenant's transaction, where row-level
security admits neither the person's memberships in other tenants nor those tenants'
administrators ([tenancy.md](tenancy.md)), and reading them would open the boundary to a request of
another tenant ([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D3, D7, D8).

Read from the code; no test runs it. The tenant is then where
[identity-provider.md](identity-provider.md#h-29) H-29 leaves one: without an administrator until a
global administrator who does not hold `admin` there grants themselves `admin` and gives it one of
its own
([tenancy.md](tenancy.md#a-global-administrator-without-a-role);
`TestAStrandedTenantIsRecoveredByTheSelfGrant`), and no route reactivates the person. Mitigation:
give every tenant an administrator
whose account it manages itself, or a person of the identity provider, so no other tenant can
deactivate its last one.

**The start-up's deactivation of the local administrator asks no tenant either.** Dormant until the
operator acts: when `COWORK_LOCAL_ADMIN_USERNAME` and `COWORK_LOCAL_ADMIN_PASSWORD` are emptied, or
name another username, the next start deactivates the account the configuration kept — its tokens
revoked, its sessions ended ([above](#the-local-administrator)) — as `system:bootstrap`, without a
tenant's lock and without `last_admin` in any tenant
([`bootstrap/bootstrap.go`](../../backend/internal/bootstrap/bootstrap.go) `run`, `deactivate`). Its
memberships stay, but a deactivated person counts as no tenant's administrator, so a tenant whose
only administrator who can log in is the local administrator — the bootstrap tenant made with it and
without `COWORK_ADMIN_GROUP`, say — is left without one. Read from the code; no test runs it.
Recovery: name the same username again, and the next start reactivates the account with its
memberships; or the self-grant of
[ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2 by a global administrator — a member of `COWORK_ADMIN_GROUP`, or the local administrator under
its new username. Mitigation: before the variables change, grant every tenant the local
administrator administers another administrator.
