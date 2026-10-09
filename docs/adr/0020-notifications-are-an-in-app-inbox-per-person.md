# ADR 0020: Notifications Are an In-App Inbox per Person — No Webhooks, No E-Mail in the First Release

## Status

Accepted, amended 2026-10-01 (D4: the inbox is pushed over the event stream, polled as the
fallback — the owner's answer to the catalog question "live updates?", Server-Sent Events over
adaptive polling, whose stream
[ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
records). Date: 2026-09-30. Decided by the owner as the answer to the catalog
question "notifications — which channel first?": in-app only, over in-app plus signed webhooks (the
recommendation), over e-mail, and over both. The event list of D2 was proposed with the
question and not objected to.

Amended 2026-10-05 by the answer on how a comment names a person, recorded in
[ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D5 — a list of person ids
beside the text, over a username or a link in the text — built on the recommendation, the owner
reviewing the result (D2: the mention is built; one act tells a person once).

Amended 2026-10-06 by GitHub's webhook of
[ADR 0071](0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md),
whose D6 gives this record's D2 the merge as an event (D2: a pull request of a ticket merged tells
its assignee and its watchers, by the reason `merged`; built the same day,
`api/github_links.go` `mergeNotices`,
[migration 41](../../backend/internal/store/migrations/000041_github_webhook.up.sql)). D5 stands:
the webhook is inbound, and nothing leaves cowork.

Amended 2026-10-09 with the removal of GitHub's webhook, which the owner dropped before its trial
([ADR 0071](0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md) Status): D2 no longer holds the merge, and no
act makes a notification of the reason `merged`. The enum value stays in the database until a later
contract migration; the inbox leaves out a notification of that reason a release up to 0.12.0 made
([`inbox.sql`](../../backend/internal/store/queries/read/inbox.sql)).

**Built** (phase 3, 2026-10-04), ~~but for D2's mention~~ *(built 2026-10-05, below)*: D1 — the inbox `GET /api/v1/me/inbox`, a
union of per-tenant reads newest first, each entry with its tenant, its ticket as it is now and its
act, the unread count beside it, and in the UI the page `/me/inbox` grouped by ticket with the bell's
unread count in the top bar ([`api/inbox.go`](../../backend/internal/api/inbox.go),
[`features/me/inbox.ts`](../../frontend/src/app/features/me/inbox.ts)); D2's events but the mention,
each telling only an active member who sees the ticket, never the actor nor for the actor's agent
([`store/inbox.go`](../../backend/internal/store/inbox.go)) — "a question I asked is withdrawn"
tells nobody, because only the asker withdraws a question and their own act tells them nothing; D3 —
`notifications` rows written by `Mutate` with the act and referencing its audit row
([migration 30](../../backend/internal/store/migrations/000030_notifications.up.sql)), rendered from
it, so a withdrawn comment or question and a reversed transition show as they are now; D4 — the
count pushed as `inbox.changed` on the person-level stream of ADR 0054 and loaded again on its
fallback's poll; D6 — marking one read and every one up to the newest seen
(`PUT /api/v1/me/inbox/{notification}/read`, `PUT /api/v1/me/inbox/read`), each the person's act
`read`, and the job `notification-expiry` that deletes a notification ninety days after it was read
*(made concrete 2026-10-04: the ninety days count from the reading, so a notification read late is
kept as long as one read at once)*. ~~**Not built:** D2's mention — D5 of
[ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) names `@person` without
saying how a comment's text names a person, and that is open.~~ **Built** (2026-10-05): D2's mention,
the reason `mentioned` ([migration 36](../../backend/internal/store/migrations/000036_comment_mentions.up.sql)),
for each person a new comment's `mentions` names and each person an edit adds, and the inbox's
"mentioned you in a comment".

## Context

[ADR 0004](0004-cowork-is-a-team-product.md) D2 puts notifications into the first release
cycle. The recipients are fixed by [ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md)
D6 (the watcher set) and [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md)
D5 (mentions); the inbox is one of the person-level views of
[ADR 0018](0018-the-views-of-the-first-release.md) D3. What remained was whether anything
leaves cowork — a webhook, an e-mail — or whether the inbox is the whole mechanism. The owner
chose the inbox alone.

## Decision

**D1 — Every person has an inbox: a list of notifications, each read or unread, grouped by
ticket, with an unread count in the page header.** The inbox is a person-level view and spans
the person's tenants as a union ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
D3); every entry names its tenant beside the key.

**D2 — The events that create a notification:**

| Event | Recipient |
|---|---|
| a ticket is assigned to me | the assignee |
| I am mentioned in a comment | the mentioned person |
| a question is asked of me | the person asked |
| a question I asked is answered or withdrawn | the asker |
| a ticket I watch changes state, or its `blocked` reason changes | the watchers |
| a ticket that blocks a ticket I watch reaches `done` or `dropped` | the watchers of the blocked ticket |
| a comment is written on a ticket I watch | the watchers |
| an `urgent` interest is registered on a ticket assigned to me | the assignee |
| ~~*(added 2026-10-06, ADR 0071 D6)* a pull request of a ticket I watch, or that is assigned to me, is merged at GitHub~~ *(removed 2026-10-09 with the webhook, ADR 0071 Status)* | ~~the assignee and the watchers~~ |

The watcher set is ADR 0013 D6. A person's own acts create no notification for that person;
an agent's act in a person's name creates none for that person either. *(Added 2026-10-05: one act
tells a person once about a ticket, by the first reason that names them — a comment that mentions a
watcher tells them `mentioned`, not also `commented`.)* ~~*(Added 2026-10-06:)* a merge is told by GitHub's
webhook, the system actor `system:github`, so nobody is left out as its actor; a person who cannot see
the ticket is told nothing of it, as of any act, and the ticket's state stays as it was — the merge is
a fact told, not a move (ADR 0071 D6). A commit that reaches the default branch tells nobody.~~
*(Removed 2026-10-09 with the webhook, ADR 0071 Status.)*

**D3 — Notifications are created by the same transaction as the act** and are rows, not
messages: a notification references the act in the audit record and renders from it, so a
withdrawn comment or a reversed transition shows its current state in the inbox.

**D4 — The inbox is ~~polled by the UI~~** *(amended 2026-10-01: the unread count and new
entries are pushed as `inbox.changed` events over the per-person stream of ADR 0054, and
polled with the interval of that record only when the stream is unavailable)*. Nothing
leaves cowork (D5 stands).

**D5 — Nothing leaves cowork.** No webhook, no e-mail, no chat integration in the first
release. A person who does not open cowork is not told anything. An agent that wants to know
what changed reads its person's inbox through the API.

**D6 — Retention.** Read notifications are kept ninety days, unread ones indefinitely; a
person may mark all read.

## Consequences

- One table, one poll, no delivery queue, no secret management, no SSRF surface, no SMTP.
- A client's member who rarely opens cowork learns nothing in between; the owner accepts
  that for the first release.
- The MCP server has no event source; a Claude session that wants "what changed since my
  last session" queries the inbox and the activity of its tickets when it starts. The
  workflow plan's expectation of a webhook feed is removed in the same change.
- Adding a webhook or e-mail channel later is an amendment that adds a sink to D3's rows; D2
  and D3 are designed so that any later channel reads the same events.

## Alternatives Considered

- **In-app plus signed webhooks per tenant** — the recommendation: one generic outgoing
  channel for chat, automation and agent sessions, with HMAC signatures, retries and SSRF
  guards. Lost by the owner's decision.
- **In-app plus e-mail** through an installation's SMTP relay. Reaches everyone, useless to
  agents, and brings templates, digests and bounces. Lost.
- **Both.** Lost with its parts.

## Residual risks

- D5 is the risk: silence for people outside the tool. The amendment path is cheap by D3.
- D2's watcher fan-out on a busy ticket fills many inboxes; digesting and muting are not
  decided and are the first amendment if the inbox becomes noise.

## References

- [ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D6 — the watcher set
- [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D5 — mentions
- [ADR 0018](0018-the-views-of-the-first-release.md) D3 — the inbox as a person-level view
- [ADR 0004](0004-cowork-is-a-team-product.md) D2 — notifications in the first release cycle
