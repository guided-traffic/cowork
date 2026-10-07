# ADR 0071: An Inbound, Signed GitHub Webhook Links Pull Requests to Tickets and Notifies on Merge — Optional per Tenant, Nothing Outbound, No Automatic Transition, Built in Phase 7 on Trial

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"GitHub integration?": the inbound webhook, over nothing, over an automatic transition on
merge, and over an outbound integration with a GitHub credential in cowork — with the owner's
qualification that it is **not mandatory**: it is tried, and may be dropped if it does not
earn its place. The rules of D5–D8 were put to the owner with the question and not objected
to.

~~**Not built.** Phase 7.~~ **Built** (phase 7, 2026-10-06), **on trial**: D1–D7 —
`POST /api/v1/tenants/{tenant}/integrations/github/webhook`
([`api/github.go`](../../backend/internal/api/github.go),
[`api/github_links.go`](../../backend/internal/api/github_links.go), the pure parser
[`internal/github`](../../backend/internal/github/)), the tenant's secret
(`GET …/integrations/github`, `POST` and `DELETE …/integrations/github/secret`,
[`api/integrations.go`](../../backend/internal/api/integrations.go)), a ticket's pull requests
(`GET …/tickets/{number}/pull-requests`, `DELETE …/pull-requests/{pull_request}`,
[`api/pullrequests.go`](../../backend/internal/api/pullrequests.go)),
[migration 41](../../backend/internal/store/migrations/000041_github_webhook.up.sql), the settings'
GitHub section and the ticket page's card and hint in the UI; the operator's page is
[docs/operations/github.md](../operations/github.md), the boundary and its gaps
[docs/security/github-webhook.md](../security/github-webhook.md). Whether the feature earns its place
is the owner's trial on a real repository, outstanding.

*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)*

- **D1 — the secret.** The server draws 256 random bits and answers them once as 64 hexadecimal
  characters; it keeps them sealed — AES-256-GCM under a key derived from `COWORK_SESSION_KEY` by
  HKDF-SHA256 with the label `cowork github webhook secret v1`, the tenant's id as the additional
  data —, since the server must recompute every delivery's HMAC and a hash would not let it. One
  secret per tenant, for every repository. Making it and rotating it are one operation,
  `createGitHubSecret`, which replaces a secret at once; it takes a browser session by the rule of
  [ADR 0035](0035-personal-access-tokens.md) D5 — a secret lets whoever holds it write into the
  tenant after a leaked token's revocation —, the eighteenth such operation, and no
  `Idempotency-Key`, so no stored answer can hold the secret. Revoking only takes access away: an
  administrator's `admin`-scope token may. Never an agent
  ([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
  D3). Recorded `created`, with whether one was `replaced`, and `revoked`, on
  `github_webhook_secret`. A change of the server key leaves every sealed secret unopenable: the
  endpoints answer `404` and the log says to rotate.
- **D2 — the route.** The path names `{tenant}`, as every route of a tenant does
  ([ADR 0023](0023-the-tenant-is-in-the-path.md)). The document declares it `security: []` with
  `x-cowork-signed: github`: no session or token is resolved — a cookie or an `Authorization`
  header on a delivery is ignored —, no origin is checked, since GitHub sends none and a signature
  needs no CSRF defence ([ADR 0037](0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md)
  D5 names the origin check for the login, whose credential a browser can send by itself), and the
  tenant boundary, which admits persons, does not run: the handler reads the tenant and its secret
  itself. It is served outside the generated server, which would parse the body.
- **D3 — the order.** The tenant and its secret — an unknown tenant and one without a secret, the
  same `404` —; the body, bounded while it is read, before the signature, which is computed over the
  bytes read (`413`); `X-Hub-Signature-256` compared in constant time before anything is parsed
  (`401 signature_invalid`, a code of its own); the delivery's id, a UUID, and the type,
  `application/json` (`400`, `415`); the delivery kept twenty-four hours per tenant, a repetition
  `200` — every delivery taken is kept, whatever its event; then the event. Every delivery taken
  answers `202` with the same empty body, whether it linked something, named an unbound repository
  or was another event, so the answer says nothing of what is bound. A captured body sent again under
  a new id changes nothing: a link is made once, and a pull request's facts are written only from a
  delivery whose `updated_at` is not older than what a link holds. The job `github-delivery-expiry`
  removes the deliveries past their day.
- **D4 — what is read.** `reopened` is read beside the four actions; a push is read when its `ref` is
  `refs/heads/` and the repository's `default_branch`. The repository is bound when any project of
  the tenant binds its identity, whatever the sub-directory, and the keys resolve anywhere in the
  tenant, not only in the bound project.
- **D5 — the keys.** In a pull request's body, a `Cowork-Ticket:` trailer line — its name read
  without regard to case, its key full or short — and a line that is a full key alone, as ADR 0068
  D5 puts the full key on the body's first line; the short keys in parentheses at the end of the
  title only where the body names none, GitHub's `(#n)` after them passed over. In a pushed commit,
  its trailers, else its subject's short keys. A key in running text is not read; a key of another
  tenant, of no ticket or of a deleted one is passed over; at most fifty keys per text and a hundred
  commits per push.
- **D6 — what a ticket gains.** `ticket_pull_requests`, one row per ticket and pull request or
  commit, the pull request's facts repeated on each, read under the ticket's predicate
  ([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
  D1), the route `GET …/tickets/{number}/pull-requests` a child of the ticket like its links and
  files. A commit that reaches the default branch is an entry of its own, merged, which tells
  nobody. The page a ticket links is written from the bound repository's identity, never taken from
  the payload. The acts are `linked`, `merged`, `closed`, `reopened`, `updated` — a title's or an
  author's change, which the activity leaves out — and a person's `unlinked`, of the system actor
  `system:github` with the delivery's id as their key and no title or body; a merge tells the
  watchers, the assignee among them, by the reason `merged`
  ([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D2 as amended 2026-10-06), also
  when the merge is the first the webhook hears of the pull request. The ticket page hints that the
  work may be ready to move while the ticket is open and a pull request or a commit of it merged; the
  context document names them in `## Pull requests`, written only when there are some
  ([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
  D2 as amended 2026-10-06), and `/markdown` does not. The stream says `pull_request.changed`
  ([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
  D2 as amended 2026-10-06). A person removes a wrong link like any link (the Residual risks), a
  member's act with `write` scope in the agent baseline, and the removal stays: the row is kept,
  marked removed, so a later delivery does not bring the link back.
- **D7 — the setup.** The tenant's settings make, rotate and revoke the secret and show the payload
  URL with what to set at GitHub; [docs/operations/github.md](../operations/github.md) is the
  operator's page.

Four of these choices await the owner's answer, the recommended option of each built: what a push to
the default branch adds to a ticket, whether a removal stays and an agent may make one, whether a key
that leaves a pull request's text unlinks it, and whether a title's short keys are read where the
body names full keys.

## Context

[ADR 0068](0068-commits-carry-a-component-scope-the-short-key-in-the-subject-and-the-full-key-in-a-trailer.md)
put the full ticket key into a commit trailer and the pull request body precisely so that a
machine could read it. [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D5
says nothing leaves cowork; [ADR 0064](0064-one-direction-import-and-export-no-synchronisation.md)
says cowork touches no repository; [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)
D5 and D6 make `done` a person's or a capable agent's act with a verification note, and the
tickets page says "merged is not verification". What a ticket can gain without breaking any
of that is context: which pull requests belong to it, and the moment one is merged — as a
fact shown and notified, not as a state changed.

## Decision

**D1 — Optional, per tenant, off by default.** A tenant administrator creates a GitHub
webhook secret in the tenant's settings (shown once, rotatable, revocable); until one
exists the endpoint answers `404` for that tenant. The feature is **on trial**: if it does
not prove useful it is removed by amending this record, and nothing else depends on it.

**D2 — The endpoint is inbound only:** `POST /api/v1/tenants/{slug}/integrations/github/webhook`.
cowork holds no GitHub token and makes no call to GitHub; it is called.

**D3 — Signature first, then size, then dedup.** Every delivery is verified against the
tenant's secret with GitHub's `X-Hub-Signature-256` before the body is parsed; an invalid or
missing signature answers `401` without processing; the body is bounded by the JSON limit of
[ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2; the delivery id is kept for twenty-four hours and a repeat answers `200` without effect.

**D4 — Only `pull_request` (opened, edited, synchronize, closed *— and reopened, made concrete
2026-10-06*) and `push` to the default branch are processed;** every other event answers `202` and
is discarded. The repository in
the payload (`repository.clone_url`) is normalised and resolved to a project by
[ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D1–D2; an unbound repository answers `202` and is discarded — no error back to GitHub, no
leak of what is bound.

**D5 — Keys are read from the trailer first, the subject suffix as fallback** (ADR 0068 D1,
D2), from the pull request's title and body and from its commits' messages; a pull request
may reference several tickets, and a key of another tenant is ignored.

**D6 — What a ticket gains:** a `## Pull requests` section (number, title, state, URL,
author, merged at) and a timeline entry when a pull request is opened, merged or closed.
**No state changes.** A merge creates a notification to the assignee and the watchers
([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D2 gains the event) and a
hint on the ticket; a person or a capable agent moves the ticket.

**D7 — Setting the webhook up is the operator's step,** per repository in GitHub, with the
endpoint URL and the secret; the operations page shows it. No GitHub App, no organisation
installation.

**D8 — GitLab and others come through the same mechanism** with a payload adapter per
provider, each its own amendment, each optional.

## Consequences

- A ticket shows its pull requests and the moment of merge, with one small public endpoint
  per installation, HMAC-protected, idempotent, bounded — and no credential in cowork.
- Nothing automatic happens to a ticket; the verification rule holds, and "merged is not
  verification" stays true in cowork as it is in the tickets page.
- The trial framing is honest: the owner is not sure it is needed; D1 makes removal cheap.
- The pull-request table and the parser are the whole implementation; the notification
  event is one more row in ADR 0020 D2's table.

## Alternatives Considered

- **Nothing in the first release.** No surface, no setup; "merged, now move the ticket"
  stays manual and a ticket does not know its pull requests. Lost narrowly — the trial is the
  owner's compromise.
- **An automatic transition on merge** (`in-progress → done` with the pull request as the
  note). The tempting case; contradicts ADR 0009 D5 and the tickets page's rule, and a
  webhook would be the one actor without accountability. Lost.
- **An outbound integration** (comments on the pull request, a commit status when the
  trailer is missing). Visibility inside GitHub and enforcement of ADR 0068; a GitHub App or
  token inside cowork and outbound calls. Not now; its own record if the discipline of
  ADR 0068 proves insufficient.

## Residual risks

- A public endpoint, however small: D3's signature check is the whole defence, and the
  security page of integrations carries it with its `H-<n>` for what signature verification
  does not cover (a leaked secret). *(2026-10-06:)* the page is
  [docs/security/github-webhook.md](../security/github-webhook.md), its gaps H-64 to H-67: the
  answers tell which tenants take the webhook, nothing throttles it, a leaked secret writes until it
  is rotated, and one secret serves every repository of a tenant.
- Keys read from free text can be wrong (a key mentioned, not meant); the section shows the
  source, and a wrong link is removed by a person like any link.

## References

- [ADR 0068](0068-commits-carry-a-component-scope-the-short-key-in-the-subject-and-the-full-key-in-a-trailer.md) — the trailer and subject suffix the parser reads
- [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md) D1, D2 — the repository resolution
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D5, D6, [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D5 — why no state changes
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D2, D5, [ADR 0064](0064-one-direction-import-and-export-no-synchronisation.md) — inbound only
- [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md) D2 — the body limit
- [ADR 0035](0035-personal-access-tokens.md) D5, [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md) D3 — who makes the secret
- [`backend/api/integrations.yaml`](../../backend/api/integrations.yaml), [`internal/api/github.go`](../../backend/internal/api/github.go), [`internal/github`](../../backend/internal/github/) — the route, its handler and the parser
