# A public endpoint whose credential is a signature: GitHub's webhook

What the inbound webhook of
[ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
lets into a tenant and what keeps everything else out, as built on 2026-10-06: the boundary of a
route that takes no person's credential, what somebody without the tenant's secret can do, what a
holder of it can do, the replay window, and the secret at rest and who can open it. How a person's
request is authenticated is [trust-boundaries.md](trust-boundaries.md) and
[tokens.md](tokens.md); how the rows it writes stay inside the tenant and under a ticket's sight is
[tenancy.md](tenancy.md).

## The boundary

`POST /api/v1/tenants/{tenant}/integrations/github/webhook` answers whoever reaches the Ingress —
GitHub's hooks, which is why an installation that takes them is reachable from GitHub's addresses.
It is the one public route that writes into a tenant, and the API document says so with
`security: []` and `x-cowork-signed: github`
([`backend/api/integrations.yaml`](../../backend/api/integrations.yaml),
[`backend/api/document_test.go`](../../backend/api/document_test.go) holds it to being the only
one). For it, the pipeline ([`api.go`](../../backend/internal/api/api.go) `ServeHTTP`,
`admitTenant`) does what it does for no other operation:

- **No person's credential is resolved.** A cookie or an `Authorization` header on the request is
  never looked at, so a delivery can neither borrow a session nor fail on one, and none of its acts
  names a person, a token or an agent: they are the system actor `system:github`'s, with the
  request's id, the keyed hash of the client's address ([ADR 0035](../adr/0035-personal-access-tokens.md)
  D2) and the delivery's id as their key.
- **No origin check.** GitHub sends no `Origin`. The check of
  [ADR 0037](../adr/0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md)
  defends a credential a browser sends by itself; this route has none, and a signature no browser
  can compute without the secret is its own defence ([csrf.md](csrf.md)).
- **No tenant boundary.** The boundary admits persons. The webhook's handler finds the tenant by
  its slug and its sealed secret itself, in a read-only transaction of the job `github-webhook`
  that names no person ([`store/github.go`](../../backend/internal/store/github.go)
  `WebhookSecret`); the `tenants` policy admits that job to every tenant's row, of which the query
  reads the id, the slug and the name of the one the path names.
- **No validation of the body.** Validating parses it; the body is read whole and unparsed until
  its signature holds.

The handler ([`github.go`](../../backend/internal/api/github.go)) then goes in this order: the
tenant and its secret — an unknown tenant, one without a secret and a secret that no longer opens
are one `404 not_found`, the boundary's answer —; the body, bounded by `COWORK_MAX_JSON_BODY`
(`413 payload_too_large`); `X-Hub-Signature-256`, the HMAC-SHA256 of the raw body under the
secret, compared with `hmac.Equal`, which takes the same time wherever two values differ
(`401 signature_invalid`, nothing read, nothing written —
[`internal/github`](../../backend/internal/github/signature.go) `Verify`, checked against GitHub's
own documented example in `TestSignAndVerifyMatchGitHubsExample`); the delivery's id and the
body's type; the delivery kept for a day; the event. Every delivery taken answers `202` and the
same empty body, whatever it linked, whether its repository is bound and whatever event it was; a
repetition answers `200`.

## Without the secret

Somebody who does not hold a tenant's secret gets `404`, `413` or `401` and nothing else: no row
is written before the signature holds — not the delivery, not an audit row —, and nothing of the
payload is parsed (`TestASignatureThatDoesNotHoldIsRefusedAndWritesNothing`). What the three
answers tell is [H-64](#h-64).

## With the secret

The secret is the whole credential: whoever holds it is GitHub for the tenant. What that person
can do is bounded by what a delivery can do (`TestAPullRequestLinksTheTicketsItsTitleAndBodyName`
and its neighbours in [`api_github_test.go`](../../backend/test/integration/api_github_test.go)):

- **Link a pull request or a commit to a ticket of the tenant**, if the delivery names a repository
  a project of the tenant binds — its `repository.clone_url`, normalised as a binding is
  ([ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
  D1) — and a key of the tenant's tickets, which are sequential per project. The link holds a title
  and an author login the payload chose, shown as text everywhere and quoted in the context
  document, and a page at GitHub that cowork writes from the bound repository's identity —
  `https://<identity>/pull/<n>` or `/commit/<sha>` — never from the payload, so no link to a page
  outside the bound repository reaches a ticket (`TestAPayloadsPageIsNotWhatATicketLinks`). A key of
  another tenant is passed over; a delivery reaches the tenant of its path and no other, and its
  secret is that tenant's.
- **Report a merge, a close or a reopening** of a pull request it linked, an act on the ticket, and
  with a merge tell the ticket's assignee and watchers in their inbox, each only if they may see the
  ticket (`person_sees_ticket`, [ADR 0020](../adr/0020-notifications-are-an-in-app-inbox-per-person.md)
  D2). The ticket page then hints that the work may be ready to move.
- **Nothing else.** No state, field, body, comment, question, stake or link of a ticket changes; no
  delivery deletes anything; nothing is read back — every answer is the same `202`. A confidential
  ticket's links are written like any other's and read only by those who see the ticket
  ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
  D1, `TestAConfidentialTicketsPullRequestsAreItsReadersOnly`).

Only the webhook's job inserts a link — a restrictive policy of migration 41 holds every insert of
`ticket_pull_requests` to it, so no person's request writes one —, a person removes a wrong one for
good ([ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
Residual risks), and only the purge of a deleted ticket deletes them, under migration 32's rule.
A delivery reads at most fifty keys per text and a hundred commits per push.

## The replay window

The delivery's id, `X-GitHub-Delivery`, is kept twenty-four hours per tenant
(`github_deliveries`, swept by the job `github-delivery-expiry`), and the same id again is `200`
without effect. Two copies at once meet at the table's unique key, where the second waits for the
first's transaction — PostgreSQL's rule for `INSERT … ON CONFLICT`; not verified by a test that
races two. GitHub's signature covers the body only: the event and the delivery id are not signed,
so a captured delivery — a body and its signature, which whoever sees a delivery on its way, or in
GitHub's log of a repository's deliveries, holds — can be sent again under a new id. It changes
nothing: a link that exists is not
made twice, a pull request's facts are written only from a delivery whose `updated_at` is not older
than what a link holds, so an old delivery sent after a newer one writes nothing, and a merge tells
once, at the change of state (`TestARepeatedDeliveryChangesNothing`,
`TestAnEditOrASynchronizeUpdatesThePullRequest`, `TestAMergeTellsTheAssigneeAndTheWatchersAndMovesNothing`).
A link a person removed stays removed (`TestAPersonRemovesAWrongLinkForGood`). A replay costs the
server the work of a delivery, as any request does ([H-65](#h-65)).

## The secret at rest, and who can open it

The server draws the secret — 256 bits of `crypto/rand`, answered as 64 hexadecimal characters —
and answers it once, in the `201` of `POST …/integrations/github/secret`
([`integrations.go`](../../backend/internal/api/integrations.go) `CreateGitHubSecret`). The
operation takes no `Idempotency-Key`, so no stored answer could ever hold it
([ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D6); its act records whether a secret was replaced and nothing of the secret; no log line carries
it (`TestTheWebhookSecretIsAnAdministratorsAct` searches the log and the audit record for it). The
browser holds it in one signal of the settings page until *I have stored it*.

It is stored **sealed, not hashed**: the server must recompute the HMAC of every delivery, so it
must be able to open what it stored. The seal is AES-256-GCM under a key derived from
`COWORK_SESSION_KEY` by HKDF-SHA256 with the label `cowork github webhook secret v1`
([`auth/seal.go`](../../backend/internal/auth/seal.go) `LabelGitHubWebhookSecret`), a key no other
use of the server key shares, with the tenant's id as the additional data: a sealed secret copied
into another tenant's row does not open there (`TestTheSecretIsSealedForItsTenant`,
`TestTheWebhookSecretIsSealedForItsTenant`). So:

- **A copy of the database alone opens nothing.** The plaintext needs the row and the server key
  together — the backend, or whoever holds both.
- **The runtime role reads the sealed value only for the tenant's administrators and the webhook's
  job**: restrictive policies of migration 41 hold reading, writing and deleting
  `github_webhook_secrets` to them, and the administrators' route reads when and by whom, never
  the column.
- **A change of `COWORK_SESSION_KEY` makes every sealed secret unopenable**: from then on each
  tenant's endpoint answers `404` and the log says to rotate the secret, until an administrator
  does — the server key admits no second key that would keep the old one
  ([ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D1).

## Who makes, rotates and revokes it

Making a secret and rotating it — one route, which replaces the old secret at once — lets whoever
learns the new secret write into the tenant after a leaked token's revocation, so it takes a
browser session ([ADR 0035](../adr/0035-personal-access-tokens.md) D5, one of the session-only
operations): a token is `403 session_required`, a session the agent header marks `403
agent_forbidden`, and the role is a tenant administrator's with `admin` scope, never an agent's
([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D3). Revoking only takes access away: an administrator's `admin`-scope token may, an agent may not.
Each is recorded on `github_webhook_secret` — `created`, with `replaced`, and `revoked` — in the
tenant's audit record.

## What this does not cover

<a id="h-64"></a>
### H-64 — The answers tell which tenants take the webhook

Live, by D3's answers. A tenant without a secret answers like an unknown one, `404`; a tenant with a
secret answers a delivery without a valid signature `401`, and a body above the limit `413`. So
anybody can learn, slug by slug, that a tenant exists and takes GitHub's webhook — the one route
under `/tenants/{tenant}` whose refusal differs from an unknown tenant's
([tenancy.md](tenancy.md)). The payload URL is no secret anyway: it sits in every bound
repository's webhook settings at GitHub. An installation that must not reveal its tenants' slugs
revokes the secret of every tenant, which makes all of them `404` again.

<a id="h-65"></a>
### H-65 — Nothing throttles the endpoint

Live. Like every route of cowork ([ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)),
the webhook has limits of size and time, not a budget: anybody can make the server read up to
`COWORK_MAX_JSON_BODY` and compute an HMAC per request, and a holder of the secret can write links,
acts and notifications without a bound over time — each delivery is bounded, to fifty keys per text
and a hundred commits per push. The login's throttle per address does not apply here. An
installation that wants one sets it at its Ingress controller, for the webhook's path.

<a id="h-66"></a>
### H-66 — A leaked secret writes until it is rotated, and nothing says it leaked

Live whenever a secret leaks — from GitHub's webhook settings, a repository administrator's
clipboard, a person's screen. Whoever holds it can link any ticket whose key they know or guess to a
pull request in a bound repository with a title of their choosing, report it merged, and so put a
merge hint on the ticket and a notification into its watchers' inboxes — a lure to move a ticket
that nobody's work finished. They cannot change a state themselves, read anything, or reach another
tenant, and the hint says that nothing moved. Nothing in cowork tells a leaked secret's deliveries
from GitHub's: both are signed alike; the audit rows carry the keyed hash of the sender's address
and the delivery's id, which GitHub's own delivery log can be held against. Not verified, and this is
the gap: whether the time a delivery takes tells a holder which repositories are bound or which keys
exist — a delivery that links writes rows, one passed over does not. The remedy is the rotation,
which refuses the old secret at once, and a person's removal of each link it made.

<a id="h-67"></a>
### H-67 — One secret for every repository of a tenant, kept by GitHub

By design: one tenant, one secret
([ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
D1). cowork holds no GitHub credential (D2), but GitHub holds a cowork credential: every webhook the
secret is set up in keeps it to sign its deliveries, and how GitHub protects it is GitHub's. Whoever
reads it out of one place — the settings of one repository's webhook, the person who pasted it, a
script that set the webhooks up — holds what writes links for every repository the tenant binds. A
secret per repository would narrow that to one repository; it is not built.
