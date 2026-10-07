# GitHub's webhook

How a tenant lets GitHub tell cowork about its pull requests: GitHub posts each delivery to the
tenant's endpoint, signed with the tenant's secret, and cowork links the pull requests and the
commits on a repository's default branch to the tickets their texts name, and tells a ticket's
assignee and watchers when one merges. cowork calls nothing at GitHub and changes no ticket's state
([ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)).
It is optional per tenant and on trial: a tenant without a secret takes no webhook. The routes are
[README.md, API](../../README.md#api-backend); what the endpoint lets in and what it leaves open is
[docs/security/github-webhook.md](../security/github-webhook.md).

```
GitHub ── POST <COWORK_BASE_URL>/api/v1/tenants/<slug>/integrations/github/webhook ──► the Ingress ──► backend
           X-Hub-Signature-256: sha256=<HMAC of the body under the tenant's secret>
           X-GitHub-Event: pull_request | push | ping | …
           X-GitHub-Delivery: <uuid>
       ◄── 202 taken · 200 taken before · 404 · 401 · 413 · 415 · 400 ─────────────────────────┘
```

## What it needs

- **GitHub reaches the installation.** The path is under `/api/`, which the Ingress routes to the
  backend like any other ([installation.md, expose it](installation.md#expose-it)), but GitHub's
  hooks come from outside the cluster: for `github.com` the Ingress host must be reachable from the
  internet, or at least from the addresses GitHub publishes for its hooks; for a GitHub Enterprise
  Server from that server. An installation only its people reach takes no webhook.
- **The repository is bound to a project of the tenant.** A delivery whose repository no project of
  the tenant binds is taken and passed over, without saying so — the binding is how cowork knows the
  repository is the tenant's
  ([ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)).
  `cowork-mcp`'s session start proposes the binding ([claude-code.md](claude-code.md)), and
  `POST …/projects/{project}/repositories` makes one; the repository's identity is its clone URL
  normalised, `github.com/<owner>/<repository>`.
- **A body limit GitHub's deliveries fit in.** A delivery above `COWORK_MAX_JSON_BODY` — 1 MiB by
  default — is answered `413` and links nothing; a push of many commits is the delivery that grows.
  The Ingress controller's own body limit must sit above it, as for every route
  ([runtime.md, what answers what](runtime.md#what-answers-what)).
- **No new setting.** The secret is the tenant's, made in the UI; nothing is configured for the
  installation.

## Setting it up, per repository

ADR 0071 D7 makes it the operator's step, per repository at GitHub, with no GitHub App and no
installation on an organisation.

1. **The secret.** A tenant administrator opens the tenant's *Settings* and, under *GitHub*, *Make
   the secret*. The secret — 64 hexadecimal characters — is shown once, with a copy button and the
   payload URL beside it; cowork keeps it sealed and cannot show it again
   ([docs/security/github-webhook.md](../security/github-webhook.md#the-secret-at-rest-and-who-can-open-it)).
   Making it takes a browser session: no token makes a secret (`403 session_required`), and no
   agent. The same secret serves every repository of the tenant.
2. **At GitHub**, in the repository's *Settings → Webhooks → Add webhook*:
   - *Payload URL*: `<COWORK_BASE_URL>/api/v1/tenants/<slug>/integrations/github/webhook`, which the
     settings page shows;
   - *Content type*: `application/json` — GitHub's default, `application/x-www-form-urlencoded`, is
     answered `415`;
   - *Secret*: the secret of step 1;
   - *SSL verification* on;
   - *Which events*: *Let me select individual events*, then *Pull requests* and *Pushes*; every other
     event is taken and passed over;
   - *Active*.
3. **The ping.** GitHub sends a `ping` when the webhook is saved; its delivery in the webhook's
   *Recent Deliveries* shows the answer, `202` when the endpoint took it — which also says the
   secret matched.

## What a delivery links

The keys are read where [ADR 0068](../adr/0068-commits-carry-a-component-scope-the-short-key-in-the-subject-and-the-full-key-in-a-trailer.md)
puts them ([`internal/github/keys.go`](../../backend/internal/github/keys.go)):

| Delivery | Read | Only when the first names no key |
|---|---|---|
| `pull_request`, the actions `opened`, `edited`, `synchronize`, `reopened`, `closed` | the pull request's body: a `Cowork-Ticket: <tenant>/<KEY>-<n>` trailer line, or a line that is a full key alone, as the body's first line is | the short keys in parentheses at the end of its title, `(VKO-12)` or `(VKO-12, VKO-13)`, GitHub's `(#34)` after them passed over |
| `push` to the repository's default branch | each commit's `Cowork-Ticket:` trailers | the short keys at the end of its subject |

A key in running text links nothing, a key of another tenant or of no ticket is passed over, and a
pull request's own commits are not fetched — cowork calls nothing at GitHub —: they reach a ticket
when the push brings them to the default branch. A pull request's title and state follow its later
deliveries on every ticket it is linked to; a merge tells the ticket's assignee and watchers in their
inbox and puts a hint on the ticket page. Nothing changes a ticket's state — moving it stays a
person's or a capable agent's act. A person removes a link a key made by mistake, and it stays
removed.

## What the answers mean

GitHub shows each delivery's answer in the webhook's *Recent Deliveries*, and can send a delivery
again from there.

| Answer | Means | Do |
|---|---|---|
| `202` | taken: linked what it named, or passed over a repository the tenant does not bind, an event or an action cowork does not read | nothing; a link missing on the ticket means the repository is not bound, or no key sat where the table above reads keys |
| `200` | the tenant took the same delivery in the last twenty-four hours; nothing happened again | nothing |
| `404 not_found` | no such tenant, or the tenant has no secret — or the server key changed since the secret was made (the backend's log says so) | check the slug in the payload URL; make the secret, or rotate it after a change of the server key |
| `401 signature_invalid` | the signature is not the tenant's secret's | set the secret at GitHub again, or rotate it and set the new one |
| `413 payload_too_large` | the body is above `COWORK_MAX_JSON_BODY`, or the Ingress controller's limit | raise both |
| `415 unsupported_media_type` | the content type is not `application/json` | set it at GitHub |
| `400 validation_failed` | the delivery id is no UUID, or the body is not what GitHub sends for its event | — this is not GitHub sending |

## Rotating the secret, revoking it

*Rotate the secret* makes a new one and refuses the old at once: every repository's webhook fails with
`401` until it holds the new one. Rotate, then set the new secret in each repository's webhook at
GitHub, then send the deliveries that failed in between again from *Recent Deliveries* — not verified:
whether GitHub signs a delivery it sends again with the secret it holds at that moment. A rotation is
recorded in the tenant's audit record, without the secret.

*Revoke* removes the secret: from then on every delivery is answered `404`, like an unknown tenant, and
the links made stay on their tickets. An administrator's `admin`-scope token may revoke; making and
rotating take the browser.

A change of `COWORK_SESSION_KEY` makes every tenant's sealed secret unopenable — each endpoint then
answers `404`, and each delivery logs `the tenant's GitHub webhook secret does not open: the server key
changed since it was made; rotate the secret` with the tenant's slug. Each tenant's administrator
rotates its secret and sets the new one at GitHub.

## Running it

- **Deliveries are kept a day.** Each replica removes the deliveries older than twenty-four hours at
  start and once an hour, the job `github delivery expiry`, which logs `job removed expired rows` with
  the count when there were any ([runtime.md](runtime.md#the-backend)). A delivery GitHub sends again
  after that is taken again and changes nothing, since nothing in it is new.
- **The request log** carries each delivery like any request: method, path, status, duration and the
  request id, never the body or a header. The acts a delivery records are the system actor
  `system:github`'s, with the delivery's id as their key, in the tenant's audit record.
- **Ending the trial** in a tenant is revoking its secret and removing the webhook at GitHub; the
  links stay as the record of what was linked.
