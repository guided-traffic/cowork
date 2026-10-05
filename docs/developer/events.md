# The event stream

How a committed act reaches the clients that may see it: publication in the act's transaction,
one listener per replica, the hub that fans out, the filter per stream, the person-level stream,
the replay, the heartbeat, the limits, the shutdown, and what the Ingress must do for it. The decision is the one of
[ADR 0054] — events carry keys and versions, never content, and polling is the fallback. Read
against the tree on 2026-10-04.

```
Mutate ─► audit row ─► pg_notify('cowork_events') ─(at commit)─► DB.Listen (one per replica)
                                                                     │ hub.Publish
                                                                     ▼
                         events.Hub: ring per tenant ─► Filter per stream ─► buffered channel
                                                                     │
                                         serveEvents ◄───────────────┘ text/event-stream
```

## Publication

`Writer.publish` ([`notify.go`](../../backend/internal/store/notify.go)) runs inside `Mutate`
for every act of a tenant that names a ticket, except the actions `downloaded` and `exported`
and the entity `time_entry` — data leaving the system changes nothing a client shows, and time
follows its own visibility. It sends `pg_notify('cowork_events', <json>)` in the act's
transaction: PostgreSQL delivers it at commit and never after a rollback (D4). The payload,
`store.Notification`, is what the filter needs and what the event tells: the audit row's id, the
tenant, the project, the entity, the action, the ticket key, the ticket's version, and the
confidential rule's inputs — the flag, the assignee, the reporter.

**A membership act** is published too — any act of a tenant whose `Event.Membership` is set, written
by `Mutate` or by the identity provider's transactions ([data-access.md](data-access.md#the-identity-providers-transactions)):
a grant made, changed or removed, a membership the identity provider derived, a group mapping, a
project's restriction, an entry of its access list ([`members.go`](../../backend/internal/api/members.go),
[`store/identity.go`](../../backend/internal/store/identity.go)). Its notification has the entity
`membership`, the action, the keys of what changed — `person`, `project` and `mapping`, each where
it applies — and an `audience` (`MembershipChange`; [ADR 0054] D2):

| Act | Keys | Audience |
|---|---|---|
| a grant; a derived membership | the person | `members`: every member of the tenant |
| a project's restriction | the project | `members` |
| an entry of a project's access list | the person and the project | `admins-and-person`: the tenant's administrators and the person it names |
| a group mapping | the mapping | `admins`: the tenant's administrators |

**A question's act** carries one key more, `asked_of`, the person the question is asked of as it
stands after the act (`QuestionAskedOf`), which the hub reads to reach that person's person-level
streams in their other tenants ([below](#the-person-level-stream)).

**A change of a person's inbox** is published as well: for every person an act notified
([data-access.md](data-access.md#notifications)), and for the person whose notifications an act
marked read (`Event.InboxOf`), `Writer.deliver` sends a notification with the entity `inbox`, the
tenant, the audit row's id and the person — nothing about what changed.

## The person-level stream

`GET …/events?me=true` is the person-level stream ([ADR 0054] D1): the tenant's stream with
`Filter.Me` set, which the browser always opens — on the tenant the pages show, or on the person's
first tenant on the person-level pages. It carries, besides the tenant's events, the person's own:

- **`inbox.changed`**, `data: {"unread": n}`: the person's unread notifications in every tenant the
  request reaches (`personUnread`, one read per tenant, as `GET /api/v1/me/inbox` counts them). The
  hub hands an `inbox` notification to every person-level stream of its person on any tenant
  (`Hub.toPerson`) and keeps it in no ring. The stream writes the count when it opens and once a burst
  is over — the first change starts a wait of `inboxDebounce`, 100 ms, and the count is read when it
  ends — so an agent that comments in a loop costs one count, not one per comment.
- **A question asked of the person in another of their tenants**: the hub hands a question's event
  to the person-level streams of its `asked_of` on every other tenant, and the stream judges it
  before it writes it (`writeStreamed`): a token restricted to another tenant hears nothing of it,
  and otherwise `SeesPublishedTicket` asks, in the event's tenant as the caller, whether the person
  still belongs to that tenant and sees the ticket by the facts the event carries — the project and
  the confidential rule, a project-restricted token's restriction included. A question of the
  stream's own tenant comes through the ordinary filter, once.

Neither carries an `id:` — the count is a state, not an act, and another tenant's event is no place
in this tenant's ring — so a reconnect replays neither, and the `Last-Event-ID` the browser keeps
stays the tenant's. The count the stream writes when it opens covers the gap; the browser reloads
its person-level pages on it. A stream without `me` hears none of this.

What the person-level stream does not carry: another tenant's events beyond the questions asked of
the person — a ticket assigned to them there reaches it as `inbox.changed`, a change that tells them
nothing does not reach it at all ([tenancy.md](../security/tenancy.md#the-person-level-stream)).

## Listener

`DB.Listen(ctx, hub.Publish, hub.SetUp)`, started by `cowork serve`, holds one connection outside
the pool with `LISTEN cowork_events` and hands every notification to the hub. It tells the hub
`SetUp(true)` once listening and `SetUp(false)` when the connection is lost, and reconnects after
a pause that starts at one second and doubles up to thirty.

## Hub

[`internal/events`](../../backend/internal/events/hub.go) is per replica and in memory:

- **A ring per tenant** keeps the events of the last `COWORK_SSE_REPLAY_WINDOW`; `Publish`
  appends and cuts what fell out of the window.
- **Fan-out.** `Publish` hands an event to every stream of its tenant whose filter admits it,
  through a channel of 256. A stream whose channel is full is ended with `resync`, never waited
  for.
- **Down.** While the listener is down, `Subscribe` refuses new streams. When it is back, every
  open stream is ended with `resync` and the rings start afresh: what was missed in between is
  unknown, so an id from before the loss is no replay point and a reconnect with it gets
  `resync` (`TestRecoveryDropsTheBuffer`).
- **Close.** `Close` ends every stream with `unavailable` and refuses new ones.

## Filter

`Filter.Admits` (D3, [ADR 0065] D5): the event's project is one the person can see —
`ListVisibleProjectIDs`, through `app_project_visible`, so a project-restricted token's stream
holds its one project — and a confidential ticket's event goes only to a tenant administrator,
its assignee and its reporter. A membership event is judged by its audience, not by the visible
projects (`admitsMembership`): `members` passes, `admins` passes a tenant administrator's stream,
`admins-and-person` an administrator's and the named person's (`TestMembershipAudiences`). A
project-restricted token's stream (`Filter.RestrictedProject`) first drops every membership event
that names another project, or no project and another person — a mapping, another member's grant —
so it hears its project's restriction and access entries and its own person's memberships only
(`TestMembershipEventsOfAProjectRestrictedStream`, `TestARestrictedStreamHearsOnlyItsProject`;
[tenancy.md](../security/tenancy.md#the-event-stream-carries-what-its-subscriber-could-read)). The filter, the administrator flag included, is
computed at connect and again at every heartbeat (`Hub.Refilter`): a project the person gains
reaches the stream, and one they lose stops reaching it, within one heartbeat
(`TestStreamFollowsAccess`).

## The handler

`serveEvents` in [`internal/api/events.go`](../../backend/internal/api/events.go) is served
outside the generated server; the pipeline has authenticated the caller, admitted them to the
tenant (a project-restricted token included) and validated the request, and applies neither the
request timeout nor a body limit ([api.md](api.md#the-pipeline)). Then:

1. `read` authorization; no hub configured, or a writer that cannot flush: `503 not_ready`.
2. `Subscribe` with the filter and the `Last-Event-ID` header. A hub that is down or closed:
   `503 not_ready` — the client polls.
3. `200` with `Content-Type: text/event-stream`, `Cache-Control: no-cache` and
   `X-Accel-Buffering: no`; then, if the id is no longer in the ring, `event: resync`; then the
   replayed events after the id that the filter admits; then the live ones.

An event is written as

```
id: 0199a3c2-1d2e-7f00-8000-0000000000aa
event: ticket.changed
data: {"key":"acme/VKO-12","version":4,"kind":"transitioned"}
```

with the audit row's id as `id`, `kind` the act's action, and the name by entity:
`comment.changed`, `question.changed`, `link.changed`, `interest.changed`, `membership.changed`,
everything else `ticket.changed` — an upload included (`# example` values above). A membership
event's `data` is the keys of what changed instead, each only where it applies, and no `kind`:

```
id: 0199a3c2-1d2e-7f00-8000-0000000000ab
event: membership.changed
data: {"person_id":"0199a3c2-1d2e-7f00-8000-000000000002","project_id":"0199a3c2-1d2e-7f00-8000-0000000000c1"}
```

(`# example`, an access entry). The client reloads what it shows of members, mappings and access
lists, its projects when `project_id` is there, and `GET /api/v1/me`
([frontend.md](frontend.md#how-a-change-reaches-the-screen)). A control message is
`event: resync` or `event: unavailable` with `data: {}`. At connect, `resync` only says that the
gap cannot be replayed, and the stream goes on; any later control message ends the stream.

## Heartbeat and the end of a stream

Every twenty seconds (`Options.Heartbeat`, which the tests shorten) the handler checks what a new
request would (`stillAdmitted`): the token is still usable — not revoked, not expired, its person
not deactivated (`TokenStillUsable`) — or the session is — it exists, neither limit has passed, its
person is not deactivated (`SessionStillUsable`); the identity provider still admits the person —
a provider session's groups refresh when it is due, without moving the idle clock, and a provider
person's token meets the gate when its check is due (`streamStillAdmitted` in
[`identity.go`](../../backend/internal/api/identity.go)); and the person still passes the tenant
boundary. If all hold, it recomputes the filter from the tenant as the person holds it now and
writes the comment `: heartbeat`; if not, it ends the stream without a message.

| The stream ends because | The client reads |
|---|---|
| it fell 256 events behind, or the listener came back after a loss | `event: resync` |
| the person opened one stream more than `COWORK_SSE_MAX_STREAMS_PER_PERSON` on this replica (0: no limit) — the oldest is closed | `event: unavailable` |
| the server shuts down | `event: unavailable` |
| the token, the session or the membership is gone, or the identity provider no longer admits the person, at a heartbeat | the connection closes |
| the client left | — |
| a person-level stream could not read the unread count or judge another tenant's event — the log says `event stream ended` | the connection closes |

The limit counts a person's streams on one replica across tenants. Replay works from the ring of
the replica the client reconnects to; a replica that did not hear the event answers `resync`.

## Shutdown

`httpserver.ListenAndServe` registers `hub.Close` with `http.Server.RegisterOnShutdown`, so a
`SIGTERM` ends every stream with `unavailable` as the shutdown begins, and the drain bounded by
`COWORK_SHUTDOWN_TIMEOUT` does not wait for open streams (D9).

## The Ingress

The Ingress routes the stream to the backend like any `/api/` path; the frontend's nginx never sees
it ([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3). The backend answers `X-Accel-Buffering: no`, which an nginx-based controller obeys — the
Ingress stand-in leaves its response buffering on to prove the header alone does it — and the
heartbeat every twenty seconds keeps the stream inside any read timeout above that (D6). What an
installation sets on its controller is
[installation.md, expose it](../operations/installation.md#expose-it). Running both images behind
the stand-in is how a change here is verified
([build-test-lint.md](build-test-lint.md#run-the-images-together)).

## Tests

[`hub_test.go`](../../backend/internal/events/hub_test.go) covers the filter, the audiences of a
membership event, the replay and the window, the dropped slow stream, the limit, and down, up and
close. [`api_events_test.go`](../../backend/test/integration/api_events_test.go) reads real streams
through the whole handler: a committed act arrives with key and version, a rolled-back one never,
nothing crosses a tenant, a restriction or the confidential rule; the replay and `resync`; the
heartbeat, the limit, a revoked token's stream closing and the shutdown.
`TestMembershipEventsReachTheirAudience` in
[`api_members_test.go`](../../backend/test/integration/api_members_test.go) opens an
administrator's, a member's and a viewer's stream and checks who hears a grant, a mapping and an
access entry, with keys only. `TestPersonLevelEvents` in `hub_test.go` routes an inbox change and a
question's act to the person-level streams; `TestThePersonLevelStream` and
`TestARestrictedTokensPersonLevelStreamStaysInItsTenant` in
[`api_inbox_test.go`](../../backend/test/integration/api_inbox_test.go) read the count when the
stream opens and as the inbox changes, a question asked of the person in their other tenant without
an id, and assert what never arrives: another person's question, a question on a project restricted
away from the person, one on a confidential ticket they cannot see, one of a tenant they left, and
anything of another tenant on a token restricted to one.

[ADR 0054]: ../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md
[ADR 0065]: ../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md
