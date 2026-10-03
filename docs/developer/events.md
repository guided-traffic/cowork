# The event stream

How a committed act reaches the clients that may see it: publication in the act's transaction,
one listener per replica, the hub that fans out, the filter per stream, the replay, the
heartbeat, the limits, the shutdown, and what nginx must do for it. The decision is the one of
[ADR 0054] — events carry keys and versions, never content, and polling is the fallback. Read
against the tree on 2026-10-02.

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
its assignee and its reporter. The filter, the administrator flag included, is computed at
connect and again at every heartbeat (`Hub.Refilter`): a project the person gains reaches the
stream, and one they lose stops reaching it, within one heartbeat (`TestStreamFollowsAccess`).

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
`comment.changed`, `question.changed`, `link.changed`, `interest.changed`, everything else
`ticket.changed` — an upload included (`# example` values above). A control message is
`event: resync` or `event: unavailable` with `data: {}`. At connect, `resync` only says that the
gap cannot be replayed, and the stream goes on; any later control message ends the stream.

## Heartbeat and the end of a stream

Every twenty seconds (`Options.Heartbeat`, which the tests shorten) the handler checks what a new
request would: the token is still usable — not revoked, not expired, its person not deactivated
(`TokenStillUsable`) — and the person still passes the tenant boundary. If both hold, it
recomputes the filter from the tenant as the person holds it now and writes the comment
`: heartbeat`; if not, it ends the stream without a message.

| The stream ends because | The client reads |
|---|---|
| it fell 256 events behind, or the listener came back after a loss | `event: resync` |
| the person opened one stream more than `COWORK_SSE_MAX_STREAMS_PER_PERSON` on this replica (0: no limit) — the oldest is closed | `event: unavailable` |
| the server shuts down | `event: unavailable` |
| the token or the membership is gone, at a heartbeat | the connection closes |
| the client left | — |

The limit counts a person's streams on one replica across tenants. Replay works from the ring of
the replica the client reconnects to; a replica that did not hear the event answers `resync`.

## Shutdown

`httpserver.ListenAndServe` registers `hub.Close` with `http.Server.RegisterOnShutdown`, so a
`SIGTERM` ends every stream with `unavailable` as the shutdown begins, and the drain bounded by
`COWORK_SHUTDOWN_TIMEOUT` does not wait for open streams (D9).

## nginx

[`default.conf.template`](../../frontend/nginx/default.conf.template) nests a location for
`^/api/v1/tenants/[^/]+/events$` inside `location ^~ /api/`: `proxy_buffering off`,
`proxy_cache off`, `proxy_read_timeout 1h`, an empty `Connection` header to the upstream (D6). The
backend sends `X-Accel-Buffering: no` as well. Running both images together is how a change here
is verified ([build-test-lint.md](build-test-lint.md#run-the-images-together)).

## Tests

[`hub_test.go`](../../backend/internal/events/hub_test.go) covers the filter, the replay and the
window, the dropped slow stream, the limit, and down, up and close.
[`api_events_test.go`](../../backend/test/integration/api_events_test.go) reads real streams
through the whole handler: a committed act arrives with key and version, a rolled-back one never,
nothing crosses a tenant, a restriction or the confidential rule; the replay and `resync`; the
heartbeat, the limit, a revoked token's stream closing and the shutdown.

[ADR 0054]: ../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md
[ADR 0065]: ../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md
