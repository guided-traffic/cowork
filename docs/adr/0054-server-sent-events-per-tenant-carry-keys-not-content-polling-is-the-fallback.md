# ADR 0054: Server-Sent Events per Tenant Carry Keys, Not Content — Published Through `LISTEN/NOTIFY` at Commit, Filtered by Visibility, With Polling as the Fallback

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "live
updates?": sub-second updates through Server-Sent Events, over adaptive polling with `304`s
(the recommendation), over WebSockets, and over polling now with SSE as a later amendment.
The rules of D4–D9 are this record's design for the owner's requirement and were not objected
to.

Amended 2026-10-02 (D3: the visible projects are recomputed at every heartbeat; D4: what a
payload carries and which acts are published; D5: the heartbeat checks the token and the
membership again, and a replica that lost its listener keeps no replay point from before). Revocation is immediate
([ADR 0035](0035-personal-access-tokens.md) D6), and an open stream is not a next request.
Amended 2026-10-04 (D2: `membership.changed`, its payload and its audiences; D3: a membership event
reaches its audience whatever project it names; D5: the heartbeat asks the identity provider's gate
as well), and again on 2026-10-04 after the security review (D3: a project-restricted token's
stream hears only the membership events of its project and its own person), and on 2026-10-04 for
the chat in the UI (D9: the shutdown ends the chat's turns as well; built the same day —
`ChatOptions.Shutdown` in [`api/chat.go`](../../backend/internal/api/chat.go),
`TestTheChatsTurnLimitAndShutdown`), and on 2026-10-04 by the owner's decision on the routing
recorded in [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3 (D6: the Ingress routes the stream to the backend and the frontend's nginx has no location for
it; built the same day).

**Partly built** (phase 2, 2026-10-02): D1 without `?me=true` (the person-level events arrive
with the inbox), D2 without `inbox.changed` and ~~`membership.changed` (no route changes a
membership yet)~~ — built 2026-10-04, below —, D3–D6, D8 and D9 — [`internal/events`](../../backend/internal/events/),
[`notify.go`](../../backend/internal/store/notify.go) and [`events.go`](../../backend/internal/api/events.go);
~~the nginx template has the events location~~ *(gone 2026-10-04 with the frontend's proxy, D6)*. D3's recomputation on `membership.changed`
arrives with that event. D7's client side and D8's hidden tab since phase 3 (2026-10-03):
[`event-stream.service.ts`](../../frontend/src/app/core/event-stream.service.ts) opens one
`EventSource` per tenant page, falls back after three failures or `unavailable`, ticks every
fifteen seconds, retries every minute and holds events while the tab is hidden; ~~a poll still
reloads the lists in full — the `If-None-Match` of D7 is outstanding~~ *(built 2026-10-04: every
list the client loads again answers a weak `ETag` and `304`, and the client sends the tag of each
page it holds — [`core/conditional.ts`](../../frontend/src/app/core/conditional.ts))*. Measured on 2026-10-03
through the Angular dev server's proxy: a comment's event reached an open stream 29 ms after the
write began.

**Built** (phase 4, 2026-10-04): `membership.changed` of D2, published by every act on who belongs
to a tenant or who sees a project — an administrator's and the identity provider's alike
([`store/notify.go`](../../backend/internal/store/notify.go) `MembershipChange`,
[`events/hub.go`](../../backend/internal/events/hub.go) `Filter.Admits`) — and the client's reloads
on it.

## Context

The owner wants a change to reach every open client in under a second. Two boards, a
dashboard, the inbox and the detail page show the same tickets ([ADR 0018](0018-the-views-of-the-first-release.md));
every mutation goes through one wrapper in one transaction ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D3); the backend may run as several replicas behind ~~nginx and~~ an Ingress
([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md);
*since 2026-10-04 the Ingress alone, D3 of that record*);
a restricted project must stay invisible to people outside it ([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D3, D4), and nothing crosses a tenant ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
D3). PostgreSQL's `NOTIFY` is delivered at commit, which is exactly the moment an event
is true; Server-Sent Events are one-directional, HTTP/1.1-plain and reconnect by themselves,
which is all a client that refetches needs.

## Decision

**D1 — One event stream per tenant and person:** `GET /api/v1/tenants/{slug}/events`,
`text/event-stream`, authenticated like any route (session cookie; a bearer token may
subscribe too). The stream delivers events of that tenant the person may see, plus the
person's own events (inbox, questions asked of them) across their tenants when opened with
`?me=true` from the person-level pages.

**D2 — An event carries a key and a version, never content.**
`event: ticket.changed`, `data: {"key":"acme/VKO-12","version":17,"kind":"transition"}`;
likewise `comment.changed`, `question.changed`, `link.changed`, `interest.changed`,
`inbox.changed {"unread": 3}`, `membership.changed`. The client refetches what it shows
through the ordinary API, which enforces authorization; the stream itself exposes nothing a
list would not. *(Made concrete 2026-10-04: `membership.changed` carries the ids of what changed,
each only where it applies — `{"person_id", "project_id", "mapping_id"}` — and no version and no
kind. Its audience is part of the act: a grant, a derived membership and a project's restriction
reach every member of the tenant, who read the member list anyway and saw the project at one of the
two moments; a group mapping reaches the tenant's administrators, who alone read the mappings; an
entry of a restricted project's access list reaches the administrators and the person it names, so
no member who does not see the project hears of it. The client reloads its members, mappings and
access lists, its projects when a `project_id` is there, and the person's own `GET /api/v1/me`.)*

**D3 — Visibility is enforced at the stream.** Each event carries the project; a
subscription knows the person's visible projects (computed at connect, recomputed on
`membership.changed`) *(amended 2026-10-02: and at every heartbeat, with the person's current
role, so a project gained or lost by a change no event announces counts within one
heartbeat)* and drops events of projects the person may not see, so not even the
existence of a key in a restricted project leaks. *(Amended 2026-10-04: the server recomputes at
the heartbeat, not on `membership.changed`, and a membership event is judged by its audience alone,
whatever project it names ~~— the stream of a project-restricted token included, which therefore
hears the tenant's membership changes~~.)* *(Amended after the security review, 2026-10-04: except
on the stream of a project-restricted token, which knows nothing of the tenant beyond its project and
hears a membership event only when it names that project, or names the token's own person and no
project ([docs/security/tenancy.md](../security/tenancy.md#the-event-stream-carries-what-its-subscriber-could-read)).)* Events never cross tenants: a subscription
is to one tenant, and the person-level `?me=true` events are addressed to the person.

**D4 — Publication is `NOTIFY` at commit.** The mutation wrapper of ADR 0027 D3 issues
`NOTIFY cowork_events, '<json>'` inside the transaction; PostgreSQL delivers it when the
transaction commits and not otherwise. Each backend replica holds **one dedicated listener
connection** outside the pool, fans each notification out to its subscribed streams after D3's
filter, and never blocks a mutation on a slow subscriber (bounded per-stream buffers; a
subscriber that falls behind is sent `event: resync` and dropped). *(Made concrete 2026-10-02: an act of a ticket is
published — its comment, question, link, interest and attachment acts included — and the
reads of [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D5 and
the time entries are not; the payload is the audit row's id, the tenant, the project, the
entity, the action, the key, the version and the confidential rule's inputs, a few hundred
bytes against PostgreSQL's limit of 8000; while the listener has lost its connection, new
streams are refused with `503` and the open ones are told `resync` when it is back.)*

**D5 — Reconnect and replay.** Every event has an `id` (the audit row's UUIDv7); each
replica keeps a ring buffer of the last five minutes per tenant; a reconnect with
`Last-Event-ID` inside the buffer replays the gap, outside it receives `event: resync` and the
client refetches every visible list. A heartbeat comment every twenty seconds keeps proxies
from closing idle streams. *(Added 2026-10-02: the heartbeat checks the token —
not revoked, not expired, its person active — and the person's membership of the tenant again,
and ends the stream when either fails. A replica whose listener comes back after a loss empties
its ring buffers: the acts of the loss never reached them, so an earlier id answers `resync`.)*
*(Amended 2026-10-04: the heartbeat checks a session as well — it exists, neither limit passed,
its person active — and the identity provider's gate: a provider session's groups refresh when it
is due, without moving the idle clock, and a provider person's token meets the gate when its check
is due ([ADR 0035](0035-personal-access-tokens.md) D8).)*

**D6 — The proxies are told not to buffer.** The backend sets `X-Accel-Buffering: no`,
`Cache-Control: no-cache` and `Content-Type: text/event-stream`; ~~the nginx template gets a
location for `…/events` with `proxy_buffering off`, `proxy_cache off`, `proxy_read_timeout`
of one hour and HTTP/1.1 keep-alive~~ *(amended 2026-10-04 by the owner, ADR 0001 D3: the Ingress
routes the stream to the backend, and the frontend's nginx proxies nothing; an nginx-based Ingress
controller obeys `X-Accel-Buffering: no` itself, and the heartbeat of D5 every twenty seconds keeps
the stream inside any read timeout above that)*; the operations page lists the Ingress annotations an
installation needs for the same (read timeout, no buffering).

**D7 — Polling is the fallback, not the default.** A client whose `EventSource` fails
three times in a row, or receives `event: unavailable`, polls as the earlier design had it —
`resource().reload()` on a 15-second timer with `If-None-Match` on list ETags — and retries
the stream every minute. Lists carry a weak `ETag` for that purpose.

**D8 — Limits.** `COWORK_SSE_MAX_STREAMS_PER_PERSON` (default 10; `0` disables the limit,
[ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2) — the eleventh stream of a person closes the oldest; `COWORK_SSE_REPLAY_WINDOW` (default
five minutes). A client in a hidden tab keeps its stream but defers refetches until the tab is
visible again.

**D9 — Shutdown closes streams at once.** On `SIGTERM` the backend ends every stream before
draining requests ([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D5's timeout is for requests, not streams); clients reconnect to another replica and replay
from `Last-Event-ID`. *(Amended 2026-10-04 for the chat in the UI,
[ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md): a
turn of the chat is a stream as well, and the signal ends every turn that runs — its `error` event
`503 not_ready`, "the server is shutting down: send the turn again", then `done` with the messages
it added — instead of holding the drain for up to `COWORK_CHAT_TURN_TIMEOUT`. The client does
not send a turn again by itself; the person does, on another replica.)*

## Consequences

- A change is on every open board within the commit-to-notify latency, well under a second.
- The backend gains long-lived connections: one listener connection per replica, one goroutine
  and a bounded buffer per stream; memory per stream is small and the per-person limit bounds
  it. These are the first metrics the metrics record will want.
- ~~The nginx template and the Ingress need~~ *(since 2026-10-04: the Ingress needs)* timeout and
  buffering settings; a wrong Ingress default shows as streams that die every minute, which the
  operations page names as the symptom.
- Browsers cap HTTP/1.1 connections per origin at six; HTTP/2 at the Ingress lifts that, and
  D8's per-person limit keeps a person with many tabs from starving their own API calls.
- D7 means the polling path is built and tested too — the owner accepted that the fallback
  exists, and the earlier records that assumed polling ([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md)
  D4, [ADR 0053](0053-signals-and-services-no-store-framework.md)) are amended to "pushed,
  polled as fallback".
- The `LISTEN/NOTIFY` payload is limited to 8000 bytes; D2's key-and-version events are far
  below it.

## Alternatives Considered

- **Adaptive polling with `304`s** — the recommendation. No long-lived state, no proxy
  cases; up to fifteen seconds of latency, which the owner does not want. Kept as the
  fallback.
- **WebSockets.** Bidirectional, which nothing needs; a protocol upgrade through nginx and
  the Ingress for no gain over SSE. Lost.
- **Polling now, SSE later.** Would have shipped the first release without the owner's
  requirement. Lost.
- **A message broker** (Redis pub/sub, NATS) for fan-out. A third stateful component where
  `NOTIFY` on the database every replica already connects to does the job at this scale.
  Lost.

## Residual risks

- D3's visibility filter at the stream is a second place, after the data layer's predicate,
  where project restriction is enforced; the integration tier subscribes a person outside a
  restricted project and asserts silence.
- A burst of mutations (an import, an agent loop) produces a burst of events; D4's bounded
  buffers and `resync` keep the backend safe, at the cost of a full refetch for the clients
  that fell behind. The inbox collapse of ADR 0039 D5 does not apply to the stream.
- `NOTIFY` is not durable: a replica that is down misses nothing it needs (its clients are
  gone too), but a client reconnecting after longer than the replay window refetches fully.

## References

- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D3 — the wrapper that publishes at commit
- [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D3, D4, [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3 — what the stream must not leak
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D4, [ADR 0053](0053-signals-and-services-no-store-framework.md) — amended by this record
- [ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md) D2 — the version an event names
- ~~`frontend/nginx/default.conf.template` — where D6's location lands~~ *(until 2026-10-04)*;
  [docs/operations/installation.md](../operations/installation.md#expose-it) — the Ingress
  settings D6 names
