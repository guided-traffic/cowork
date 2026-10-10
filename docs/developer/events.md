# The event stream

How a committed act reaches the clients that may see it: publication in the act's transaction,
one listener per replica, the hub that fans out, the filter per stream, the person-level stream,
the replay, the heartbeat, the limits, the shutdown, and what the Ingress must do for it. The decision is the one of
[ADR 0054] — events carry keys and versions, never content, and polling is the fallback. Read
against the tree on 2026-10-05.

```
Mutate ─► audit row ─► pg_notify('cowork_events') ─(at commit)─► DB.Listen (one per replica)
                                                                     │ hub.Publish
                                                                     ▼
                         events.Hub: ring per team ─► Filter per stream and team ─► buffered channel
                                                                     │
                                         serveEvents ◄───────────────┘ text/event-stream
```

## Publication

`Writer.publish` ([`notify.go`](../../backend/internal/store/notify.go)) runs inside `Mutate`
for every act of a team that names a ticket, except the actions `downloaded` and `exported`
and the entity `time_entry` — data leaving the system changes nothing a client shows, and time
follows its own visibility — and except an act marked `Event.Quiet`, which `writeEvents` in
[`tx.go`](../../backend/internal/store/tx.go) records and never publishes: another act of the
transaction announces it, as an import's one act announces the tickets it creates. It sends `pg_notify('cowork_events', <json>)` in the act's
transaction: PostgreSQL delivers it at commit and never after a rollback (D4). The payload,
`store.Notification`, is what the filter needs and what the event tells: the audit row's id, the
team, the project, the entity, the action, the ticket key, the ticket's version, and the
confidential rule's inputs — the flag, the assignee, the reporter.

**A deletion, a restoration and a purge** are ticket acts like any other — `ticket.changed` with the
kind `deleted`, `restored` or `purged` — and reach whoever could see the ticket by the facts it had,
on the team's streams and on the person-level stream of every person who sees it, whichever of
their teams that stream was opened in: a client that refetches the ticket gets `404` after a
deletion or a purge and drops it, the bin of an administrator loads again, and the person-level
pages and the inbox's count, which no `inbox.changed` tells of a deleted ticket, read again on a
deletion or a restoration (`changesExistence` in
[`event-stream.service.ts`](../../frontend/src/app/core/event-stream.service.ts)). A purge's act
carries the facts the ticket had (`Event.Published`), because the row is gone when the act is
written ([ADR 0024] D1, D2). Saved filters are not published; their list, like the bin, answers
`304` to the poll of the fallback when nothing changed.

**An act in another team** — a link across teams on its other end, the end of a relation at a
purge, a settled prerequisite's `prerequisite_settled` on a ticket it blocks — is written by
`Writer.RecordElsewhere` in that team's record and published like any act there, on that team's
streams, by that ticket's facts ([data-access.md](data-access.md#crossings-between-teams)).
**A parent's derived stages** that a change of a child — of its own team or another — moved are no act: the
crossing `refresh_derived` sends the notification itself, entity `ticket`, action `derived`, with
an id `uuidv7()` makes for it — not an audit row's —, the parent's team, project, key and version,
which the change does not move, and the confidential rule's inputs; the stream sends it as
`ticket.changed` of the kind `derived`. Its version is the one the client holds, so a client that
refetches only a newer version must refetch on this kind, as the UI's `TicketsService` does
([frontend.md](frontend.md#how-a-change-reaches-the-screen)). A parent of the writer's own team gets
it as well since migration 51 — the child's act, published as before, names the child alone
([ADR 0054] D2 as made concrete 2026-10-10).

**A project's creation** is published as a notification of the entity `project`, with the
team and the project and nothing else (`Event.NewProject`, set by `insertProject` in
[`projects.go`](../../backend/internal/api/projects.go)): it changes what a stream may admit, and no
client is told of it — `Filter.Admits` refuses it, so it is neither sent nor replayed. The UI's
sidebar reads the projects of the person's other teams again when the person may look at them anew
— the tab or the window back, a team entered or left —, not on an event
([frontend.md](frontend.md#the-shell-and-its-navigation)).

**The sort of a project's rank by the score** is published as a notification of the entity
`project-rank` (`store.EntityProjectRank`), with the team, the project and the project's key,
`<team>/<PROJECT>` (`Event.ProjectRank`, set by `SortProjectRank` in
[`score.go`](../../backend/internal/api/score.go)): the filter admits it as it admits the project's
tickets, and the stream sends it as `project.changed` ([the handler](#the-handler)). **An import's
execution** is published the same way, by the act `imported` on its job (`finish` in
[`importwrite.go`](../../backend/internal/api/importwrite.go)): the acts on the tickets, questions
and links it creates are `Quiet`, and a link to a ticket the project held before is published on
that ticket as any link is.

**A membership act** is published too — any act of a team whose `Event.Membership` is set, written
by `Mutate` or by the identity provider's transactions ([data-access.md](data-access.md#the-identity-providers-transactions)):
a grant made, changed or removed, a membership the identity provider derived, a group mapping, a
project's restriction, an entry of its access list ([`members.go`](../../backend/internal/api/members.go),
[`store/identity.go`](../../backend/internal/store/identity.go)). Its notification has the entity
`membership`, the action, the keys of what changed — `person`, `project` and `mapping`, each where
it applies — and an `audience` (`MembershipChange`; [ADR 0054] D2):

| Act | Keys | Audience |
|---|---|---|
| a grant; a derived membership | the person | `members`: every member of the team |
| a project's restriction | the project | `members` |
| an entry of a project's access list | the person and the project | `admins-and-person`: the team's administrators and the person it names |
| a group mapping | the mapping | `admins`: the team's administrators |

**A change of a person's inbox** is published as well: for every person an act notified
([data-access.md](data-access.md#notifications)), and for the person whose notifications an act
marked read (`Event.InboxOf`), `Writer.deliver` sends a notification with the entity `inbox`, the
team, the audit row's id and the person — nothing about what changed.

## The person-level stream

`GET …/events?me=true` is the person-level stream ([ADR 0054] D1 as amended on 2026-10-05): it
carries every event of every team the person belongs to that the filter of that team admits,
and the person's inbox. The browser always opens it — on the team the pages show, or on the
person's first team on the person-level pages — so one connection follows all of the person's
teams, whatever their number.

- **It spans the person's teams.** `serveEvents` computes a filter for the team it is opened on
  (`streamFilter`, through the boundary's role and a project-restricted token's project) and, for a
  spanning stream, one for every other team of the person (`tenantFilter`, through their
  membership's role there and `ListVisibleProjectIDs` in that team's transaction —
  `streamFilters`), and subscribes the stream to all of them (`events.Subscription`). A token
  restricted to a team does not span: its person-level stream follows its team alone
  ([ADR 0035](../adr/0035-personal-access-tokens.md) D3, `streamReq.span`).
- **Every event carries its id and its team.** A ticket's event names the team in its key, a
  membership event in `data.team` — and in `data.tenant` beside it, the same slug, for one release
  ([api.md](api.md#deprecated-names)) —, so a client can tell the team pages' own events from those
  of the person's other teams.
- **`inbox.changed`**, `data: {"unread": n}`: the person's unread notifications in every team the
  request reaches (`personUnread`, one read per team, as `GET /api/v1/me/inbox` counts them). The
  hub hands an `inbox` notification to every person-level stream of its person and keeps it in no
  ring. The stream writes the count when it opens and once a burst is over — the first change starts
  a wait of `inboxDebounce`, 100 ms, and the count is read when it ends — so an agent that comments in
  a loop costs one count, not one per comment. The count carries no id: it is a state, and the count
  the stream writes when it opens covers a reconnect.
- **It follows the person's memberships.** An act that changes what the stream may admit of one
  team makes it compute that team's filter again before the team's next event (*Filter*,
  below); a membership act that names the person in a team the stream does not follow — a grant
  into a team they had none in — makes the hub add that team to the stream, pending its filter;
  and the heartbeat reads every membership again (*Heartbeat*, below). A team the person left is
  followed no more: the act that took it away, which names them, still reaches them, nothing of the
  team after it does.

A stream without `me` hears its team alone, and no inbox.

## Hub

[`internal/events`](../../backend/internal/events/hub.go) is per replica and in memory:

- **A ring per team** keeps the events of the last `COWORK_SSE_REPLAY_WINDOW`; `Publish`
  appends and cuts what fell out of the window, and numbers every event in the order it received
  it, across its teams (`Event.Seq`).
- **Fan-out.** `Publish` hands an event to every stream that follows its team and whose filter of
  that team admits it (`streams`, by team), through a channel of 256; an inbox change to the
  person's person-level streams (`persons`, by person). A stream whose channel is full is ended with
  `resync`, never waited for.
- **Down.** While the listener is down, `Subscribe` refuses new streams. When it is back, every
  open stream is ended with `resync` and the rings start afresh: what was missed in between is
  unknown, so an id from before the loss is no replay point and a reconnect with it gets
  `resync` (`TestRecoveryDropsTheBuffer`).
- **Close.** `Close` ends every stream with `unavailable` and refuses new ones.
- **Metrics.** The hub records the streams it holds, every notification it receives, each stream it
  ends — `behind` when its channel was full, `limit` beyond the person's limit, `resync` at the
  listener's recovery, never at `Close` — and a `Last-Event-ID` as a replay's hit or miss, in the
  registry `New` was given, nil for none ([metrics.md](metrics.md)).

## Filter

A stream holds one `Filter` per team it follows; an event passes the filter of its own team.
`Filter.Admits` (D3, [ADR 0065] D5): the event's project is one the person can see —
`ListVisibleProjectIDs`, through `app_project_visible`, so a project-restricted token's stream
holds its one project — and a confidential ticket's event goes only to a team administrator,
its assignee and its reporter. A membership event is judged by its audience, not by the visible
projects (`admitsMembership`): `members` passes, `admins` passes a team administrator's stream,
`admins-and-person` an administrator's and the named person's (`TestMembershipAudiences`). A
project-restricted token's stream (`Filter.RestrictedProject`) first drops every membership event
that names another project, or no project and another person — a mapping, another member's grant —
so it hears its project's restriction and access entries and its own person's memberships only
(`TestMembershipEventsOfAProjectRestrictedStream`, `TestARestrictedStreamHearsOnlyItsProject`;
[tenancy.md](../security/tenancy.md#the-event-stream-carries-what-its-subscriber-could-read)). The filter, the administrator flag included, is
computed at connect, again on every act that changes what a stream may admit, and at every
heartbeat:

- **An admission change** (`Event.ChangesAdmission`) is a project's creation or any membership
  act. `Hub.Publish` counts it per stream and team (`changes`, kept when the stream stops following
  the team, so a later act counts on) and hands it to every stream that follows the team marked
  `Refilter` — judged by the stream's filter as any event, `Withheld` when the filter refuses it, sent
  on for the mark alone. Until the stream's filter of that team knows every change
  (`refiltered < changes`), the hub hands it each later event of the team `Unjudged`. One that names
  a person also reaches that person's spanning streams that do not follow its team yet, which then
  follow it with an empty filter, its events unjudged until the stream has computed the real one.
- **The stream refilters** the event's team when the event's `Refilter` is beyond what that
  team's filter knows: it reads `Hub.Changes`, computes the filter (`refilter` in
  [`events.go`](../../backend/internal/api/events.go)) — for the team it is opened on through the
  boundary, a person it no longer admits ending the stream; for another team through the person's
  membership there, a team they left followed no more — and hands it to `Hub.Refilter` with the
  count it read, then judges the `Unjudged` events itself. A change that comes while it computes
  keeps the hub's judgement off until the stream has refiltered for it as well, so no event is
  judged by a filter that does not know a change committed before it. A burst of changes costs a
  stream one or two recomputations, not one per act: the count read before the first covers the
  ones already published.
- **The heartbeat** recomputes every team's filter the same way, and for a spanning stream the
  set of teams from the person's memberships, which catches a change made in the database past the
  API: a team left, a team joined.
- The pump takes one event at a time (`handOn`): the refilter its mark asks for first, then the
  inbox's wait, the drop of what the hub withheld, of what the filter refuses of an unjudged event and
  of an event of a team the stream no longer follows — but the membership act that names the person,
  the one that took the team away —, or the write.

A project the person gains reaches the stream from the next event on, and one they lose stops
reaching it as soon (`TestTheStreamAdmitsWhatAnActOpensAtOnce`, `TestStreamFollowsAccess`).

## The handler

`serveEvents` in [`internal/api/events.go`](../../backend/internal/api/events.go) is served
outside the generated server, for `GET /api/v1/teams/{team}/events` (`eventsRoute`) — and for its
deprecated twin under `/api/v1/tenants`, whose path the pipeline read as the team path first, so the
twin's stream is the team path's in every respect, its heartbeat's boundary included
([api.md](api.md#deprecated-names)); the pipeline has authenticated the caller, admitted them to the
team (a project-restricted token included) and validated the request, and applies neither the
request timeout nor a body limit ([api.md](api.md#the-pipeline)). Then:

1. `read` authorization; no hub configured, or a writer that cannot flush: `503 not_ready`.
2. `Subscribe` with the filters and the `Last-Event-ID` header. A hub that is down or closed:
   `503 not_ready` — the client polls.
3. `200` with `Content-Type: text/event-stream`, `Cache-Control: no-cache` and
   `X-Accel-Buffering: no`; then, if the id is in the ring of no team the stream follows,
   `event: resync`; then the replayed events after the id: those of every followed team, merged in
   the order the hub received them (`Event.Seq`), each through its team's filter; then, on a
   person-level stream, the unread count; then the live ones.

An event is written as

```
id: 0199a3c2-1d2e-7f00-8000-0000000000aa
event: ticket.changed
data: {"key":"acme/VKO-12","version":4,"kind":"transitioned"}
```

with the audit row's id as `id` — the id made for it for the kind `derived`, which no act
records —, `kind` the act's action, and the name by entity:
`comment.changed`, `question.changed`, `link.changed`, `interest.changed`, `membership.changed`,
`project.changed`, everything else `ticket.changed` — an upload included (`# example` values above). A membership
event's `data` is its team's slug — as `team`, and as `tenant` beside it for one release, which a
client of the release before reads (`membershipData` in
[`events.go`](../../backend/internal/api/events.go)) — and the keys of what changed instead, each key
only where it applies, and no `kind`:

```
id: 0199a3c2-1d2e-7f00-8000-0000000000ab
event: membership.changed
data: {"team":"acme","tenant":"acme","person_id":"0199a3c2-1d2e-7f00-8000-000000000002","project_id":"0199a3c2-1d2e-7f00-8000-0000000000c1"}
```

(`# example`, an access entry). The client reloads what it shows of members, mappings and access
lists of the team it shows, its projects when `project_id` is there, and `GET /api/v1/me` for
that team's acts and for any act that names the person
([frontend.md](frontend.md#how-a-change-reaches-the-screen)). An act on a project's tickets as a whole
— the sort by the score ([domain.md](domain.md#rank)), kind `ranked`, and an import's execution,
kind `imported`, whose acts on the tickets it creates are `Quiet` and publish nothing
([import-and-export.md](import-and-export.md#the-execution)) — is `project.changed` with the
project's key and the kind and no version, since it is no ticket's; the filter admits it as it
admits the project's tickets, and the client loads the project's open lists and the person's lists
of tickets again — and, for an import, the dashboard and the open decisions, whose questions publish
nothing either:

```
id: 0199a3c2-1d2e-7f00-8000-0000000000ac
event: project.changed
data: {"key":"acme/VKO","kind":"ranked"}
```

(`# example`). A control message is
`event: resync` or `event: unavailable` with `data: {}`. At connect, `resync` only says that the
gap cannot be replayed, and the stream goes on; any later control message ends the stream.

## Heartbeat and the end of a stream

Every twenty seconds (`Options.Heartbeat`, which the tests shorten) the handler checks what a new
request would (`stillAdmitted`): the token is still usable — not revoked, not expired, its person
not deactivated (`TokenStillUsable`) — or the session is — it exists, neither limit has passed, its
person is not deactivated (`SessionStillUsable`); the identity provider still admits the person —
a provider session's groups refresh when it is due, without moving the idle clock, and a provider
person's token meets the gate when its check is due (`streamStillAdmitted` in
[`identity.go`](../../backend/internal/api/identity.go)); and the person still passes the team
boundary of the team the stream is opened on. If all hold, it recomputes the filter of every
team the stream follows as the person holds it now — a spanning stream reads the person's
memberships again, follows a team they joined and drops one they left — and writes the comment
`: heartbeat`; if not, it ends the stream without a message.

| The stream ends because | The client reads |
|---|---|
| it fell 256 events behind, or the listener came back after a loss | `event: resync` |
| the person opened one stream more than `COWORK_SSE_MAX_STREAMS_PER_PERSON` on this replica (0: no limit) — the oldest is closed | `event: unavailable` |
| the server shuts down | `event: unavailable` |
| the token, the session or the membership is gone, or the identity provider no longer admits the person, at a heartbeat | the connection closes |
| the client left | — |
| a stream could not compute a team's filter, or a person-level stream the unread count — the log says `event stream ended` | the connection closes |

The limit counts a person's streams on one replica across teams. Replay works from the ring of
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
membership event, the replay and the window, the dropped slow stream, the limit, down, up and
close, and the admission changes that hold the hub's judgement until the stream has refiltered. [`api_events_test.go`](../../backend/test/integration/api_events_test.go) reads real streams
through the whole handler: a committed act arrives with key and version, a rolled-back one never,
nothing crosses a team, a restriction or the confidential rule; the replay and `resync`; the
heartbeat, the limit, a revoked token's stream closing and the shutdown; and, with an hour's
heartbeat, that a ticket filed in a project created, opened or let into after the stream opened
arrives within a second and that the stream of a person whose grant is removed ends;
`TestAPersonLevelStreamRefiltersAndKeepsItsPersonsEvents` in `api_inbox_test.go` the same for a
person-level stream, which goes on telling its count and the events of its person's other team.
`TestMembershipEventsReachTheirAudience` in
[`api_members_test.go`](../../backend/test/integration/api_members_test.go) opens an
administrator's, a member's and a viewer's stream and checks who hears a grant, a mapping and an
access entry, with keys only. `TestPersonLevelEvents` in `hub_test.go` routes an inbox change to the
person-level streams; `TestAStreamAcrossTheTenantsOfItsPerson` there follows a spanning stream
through its teams' filters, into a team a grant names its person in, out of one they left, and
through a replay merged across its teams. In
[`api_inbox_test.go`](../../backend/test/integration/api_inbox_test.go),
`TestThePersonLevelStream` reads the count when the stream opens and as the inbox changes and the
questions of the person's other team with their ids, and asserts what never arrives: a question on
a project restricted away from the person, one on a confidential ticket they cannot see, and anything
of a team after the act that took it from them; `TestThePersonLevelStreamSpansThePersonsTenants`,
with an hour's heartbeat, that a ticket assigned to the person in their other team arrives within a
second on the stream opened on the first, that nothing of a project there hidden from them or a
confidential ticket there does, that a reconnect replays the other team, and that a grant into a
third team is followed at once; `TestTheHeartbeatChecksEveryMembershipOfThePersonLevelStream` that
the heartbeat drops a team left and follows a team joined in the database past the API; and
`TestARestrictedTokensPersonLevelStreamStaysInItsTenant` that a token restricted to a team hears
nothing of another.

[ADR 0024]: ../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md
[ADR 0054]: ../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md
[ADR 0065]: ../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md
