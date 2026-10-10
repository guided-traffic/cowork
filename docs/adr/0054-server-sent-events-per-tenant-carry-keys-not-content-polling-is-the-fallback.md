# ADR 0054: Server-Sent Events per Tenant Carry Keys, Not Content — Published Through `LISTEN/NOTIFY` at Commit, Filtered by Visibility, With Polling as the Fallback

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "live
updates?": sub-second updates through Server-Sent Events, over adaptive polling with `304`s
(the recommendation), over WebSockets, and over polling now with SSE as a later amendment.
The rules of D4–D9 are this record's design for the owner's requirement and were not objected
to.

Amended 2026-10-09 with the removal of GitHub's webhook, which the owner dropped before its trial
(D2: `pull_request.changed` is gone; [ADR 0071](0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
Status).

Amended 2026-10-10 by the owner's rename of a tenant to a team
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1; D1: the stream is
`GET /api/v1/teams/{slug}/events`, its twin under `/api/v1/tenants/{slug}/events` answered as it for
one release ([ADR 0023](0023-the-tenant-is-in-the-path.md) D1), and a membership event names its
team as `team` and, beside it for one release, as `tenant`), built the same day
(`membershipData` and `eventsRoute` in [`api/events.go`](../../backend/internal/api/events.go); the
client reads `team`, or `tenant` from a server of the release before). The event's `tenant` goes in a
later release.

Amended 2026-10-10 for the relations between teams
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3; D2: `ticket.changed`
of the kind `derived`, made concrete by the implementer, open to the owner's objection), built the
same day ([migration 47](../../backend/internal/store/migrations/000047_relations_across_teams.up.sql), `refresh_derived`).

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
it; built the same day), and on 2026-10-04 for the events a stream dropped until its next heartbeat
(D3: the stream recomputes what it admits on every act that changes it, before the next event;
built the same day), and on 2026-10-05 by the owner's answer to how the person-level pages follow
the changes of every tenant of the person — the person-level stream carries every event of every
tenant the person belongs to that the tenant's filter admits, over one connection whatever the
number of tenants — chosen over a stream per tenant on the person-level pages, which a person with
more tenants than `COWORK_SSE_MAX_STREAMS_PER_PERSON` would close against themselves, and over a
reload on `inbox.changed` with a fifteen-second poll, the latency this record turned down (D1: the
person-level stream spans the person's tenants; D3: a filter per tenant, recomputed on every act
that changes it and at every heartbeat, which checks every membership; D5: a reconnect replays
across the tenants; built the same day), and on 2026-10-05 (D2: `project.changed`, for the sort of
a project's rank by the score of [ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) D3,
which is one act of the project, with no event of the tickets it moved; built the same day —
`ProjectChange` in [`store/notify.go`](../../backend/internal/store/notify.go), the client's reload
of the open lists in [`tickets.service.ts`](../../frontend/src/app/core/tickets.service.ts)), and
made concrete on 2026-10-05 for the dashboard of
[ADR 0018](0018-the-views-of-the-first-release.md) D6 (D4: a time booking stays unpublished, and the
dashboard's time tile follows it at the next reload; settled on the recommendation, the owner
reviewing the result).

Amended 2026-10-06 (the Consequences and the References: they say what
[ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D4 and
[ADR 0053](0053-signals-and-services-no-store-framework.md) hold in place instead of claiming to
amend them, by the owner's rule that every amendment is made in place in the record it changes; no rule
changes).

**Partly built** (phase 2, 2026-10-02): D1 without ~~`?me=true` (the person-level events arrive
with the inbox)~~ — built 2026-10-04, below —, D2 without ~~`inbox.changed`~~ — built 2026-10-04,
below — and ~~`membership.changed` (no route changes a
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

**Built** (phase 3, 2026-10-04): D3's recomputation on the acts that change what a stream admits —
[`events/hub.go`](../../backend/internal/events/hub.go) `Hub.Changes`, `Hub.Refilter`,
[`api/events.go`](../../backend/internal/api/events.go) `refilter`; a project's creation is
published as `store.EntityProject` and refused by every filter. A person-level stream refilters as
any stream of its tenant; ~~its person's own events of other tenants are judged as they are written~~
(superseded 2026-10-05, below).

**Built** (2026-10-05): the person-level stream across the person's tenants of D1, D3 and D5 as
amended that day — [`events/hub.go`](../../backend/internal/events/hub.go) `Subscription`, a filter
per tenant, `Hub.Refilter` per tenant, the replay merged by `Event.Seq`;
[`api/events.go`](../../backend/internal/api/events.go) `streamFilters`, `refilter`, `heartbeat`; the
browser's person-level pages reload on the events of every tenant through
[`person-list.ts`](../../frontend/src/app/features/me/person-list.ts) `reloadOn`, and its tenant
pages keep to their tenant.

**Built** (phase 4, 2026-10-04): `membership.changed` of D2, published by every act on who belongs
to a tenant or who sees a project — an administrator's and the identity provider's alike
([`store/notify.go`](../../backend/internal/store/notify.go) `MembershipChange`,
[`events/hub.go`](../../backend/internal/events/hub.go) `Filter.Admits`) — and the client's reloads
on it.

**Built** (phase 3, 2026-10-04): D1's `?me=true` and D2's `inbox.changed {"unread": n}` — the
person-level stream carries, besides its tenant's events, the person's own across their tenants as
D1 names them: their inbox changing, as the unread count when the stream opens and once a burst of
changes is over, and the acts of questions asked of them in their other tenants, each judged before
it is written against that tenant — still a member, the ticket still visible by D3's facts, a
token restricted to another tenant hearing none ([`api/events.go`](../../backend/internal/api/events.go)
`writeStreamed`, `Hub.toPerson`). *(Made concrete 2026-10-04:)* neither carries an `id:` nor enters
D5's ring — a count is a state, ~~and another tenant's event is no place in this tenant's replay~~ —
so a reconnect replays neither and the client reloads its person-level pages on the count the stream
sends when it opens. *(Superseded 2026-10-05 by the amendment of D1, D3 and D5: the questions of other
tenants are events of those tenants like any other, judged by their filters and replayed with their
ids; `writeStreamed` and the questions' routing are gone.)* The browser opens every stream as a person-level one, on the tenant its pages
show or, on the person-level pages, on the person's first tenant
([`event-stream.service.ts`](../../frontend/src/app/core/event-stream.service.ts)). The integration
tier asserts what the person-level stream never carries: another person's events, a tenant the person
left, a project restricted away from them, a confidential ticket they cannot see.

**Built** (phase 6, 2026-10-06): D2's `project.changed` for an import, as made concrete that day —
`Event.Quiet` in [`store/tx.go`](../../backend/internal/store/tx.go), the act `imported` in
[`importwrite.go`](../../backend/internal/api/importwrite.go) —; the integration tier asserts one
event and no more for an import's execution (`TestImportADryRunAndItsExecution`).

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

**D1 — One event stream per tenant and person:** ~~`GET /api/v1/tenants/{slug}/events`~~
`GET /api/v1/teams/{slug}/events` *(2026-10-10; the old path answered as it for one release)*,
`text/event-stream`, authenticated like any route (session cookie; a bearer token may
subscribe too). The stream delivers events of that tenant the person may see, plus the
person's own events (inbox, ~~questions asked of them~~) across their tenants when opened with
`?me=true` from the person-level pages. *(Amended 2026-10-05 by the owner: opened with `?me=true`,
the stream carries, besides the person's inbox, every event of every tenant the person belongs to
that D3's filter of that tenant admits — the person-level pages follow all of the person's tenants
over one connection. A token restricted to a tenant reaches no other
([ADR 0035](0035-personal-access-tokens.md) D3): its person-level stream carries its tenant
alone. A membership event names its tenant, ~~`{"tenant": "<slug>", …}`~~ `{"team": "<slug>", "tenant": "<slug>", …}`
*(2026-10-10: `team`, and `tenant` beside it, the same slug, for one release)*, since the stream no longer
says it by its address; a ticket's event names it in its key.)*

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
*(Added 2026-10-05:)* `project.changed`, `{"key": "<tenant>/<PROJECT>", "kind": "ranked"}` without a
version, says that a project's rank was set as a whole — the sort by the score — and reaches whoever
sees the project, as its tickets' events do (D3); the client loads its open lists again.
~~*(Added 2026-10-06, [ADR 0071](0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
D6:)* `pull_request.changed`, a ticket's event like the others — `{"key", "version", "kind"}`, the
kind `linked`, `merged`, `closed`, `reopened`, `updated` or `unlinked` — says that GitHub's webhook
linked a pull request or a commit to the ticket or reported its state, or that a person removed a
link; the ticket's version is the one it has, which the act did not move. The client loads the
ticket's list of pull requests and its activity again.~~ *(Removed 2026-10-09 with the webhook.)*
*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)* an import's
execution ([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D3) is announced the same way, with the kind
`imported`: its acts on the tickets, questions and links it creates are recorded and not
published, so an import of hundreds of tickets is one event, not a burst.
*(Made concrete 2026-10-10 by the implementer, open to the owner's objection:)* a parent whose
derived stages a change of a child of another team moved
([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D3) is announced on its own team's streams as `ticket.changed` of the kind `derived`, with its
version, which the change does not move; the change is no act, so the event's id is made for the
event and names no act, and D3 filters it like any ticket's event.

**D3 — Visibility is enforced at the stream.** Each event carries the project; a
subscription knows the person's visible projects (computed at connect, recomputed on
`membership.changed`) *(amended 2026-10-02: and at every heartbeat, with the person's current
role, so a project gained or lost by a change no event announces counts within one
heartbeat)* and drops events of projects the person may not see, so not even the
existence of a key in a restricted project leaks. *(Amended 2026-10-04: the server recomputes at
the heartbeat, ~~not on `membership.changed`~~ *(and on it since the amendment below)*, and a
membership event is judged by its audience alone,
whatever project it names ~~— the stream of a project-restricted token included, which therefore
hears the tenant's membership changes~~.)* *(Amended after the security review, 2026-10-04: except
on the stream of a project-restricted token, which knows nothing of the tenant beyond its project and
hears a membership event only when it names that project, or names the token's own person and no
project ([docs/security/tenancy.md](../security/tenancy.md#the-event-stream-carries-what-its-subscriber-could-read)).)*
*(Amended again 2026-10-04, the fix decided on 2026-10-03 for events dropped until the next
heartbeat: the server recomputes on every act that changes what a stream may admit as well — a
project created, and every membership act: a grant, a derived membership, a mapping, a project's
restriction, an access entry — before it filters the next event, so a ticket filed in a project
created, opened or let into a moment ago reaches the stream, and one restricted away a moment ago
does not; the heartbeat stays, for a change made past the API. A project's creation is published
for this and sent to no client.)* ~~Events never cross tenants: a subscription
is to one tenant, and the person-level `?me=true` events are addressed to the person.~~
*(Amended 2026-10-05 by the owner, with D1: a person-level stream subscribes to every tenant of its
person, and an event of each passes that tenant's filter alone — the projects the person sees there,
their role there, the confidential rule, a project-restricted token's project — so nothing of a
tenant reaches anybody it would not reach on that tenant's own stream. The filter of each tenant is
recomputed on every act that changes what the stream may admit of it, and the heartbeat checks every
membership: a tenant the person left is followed no more, one they joined is. A membership act that
names the person in a tenant the stream does not follow — a grant into a tenant they had none in —
makes it follow that tenant at once. The act that takes a tenant away from the person still reaches
them, as it names them; nothing of that tenant after it does.)*

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
*(Made concrete 2026-10-05, settled on the recommendation, the owner reviewing the result:)* the
time entries stay unpublished for the dashboard of [ADR 0018](0018-the-views-of-the-first-release.md)
D6 too. Its time tile follows a booking at the next reload — another act of the tenant, a `resync`,
the fallback's poll, the page opened again — over publishing a booking as an act of its ticket,
which would need the stream's filter to judge the visibility of time
([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D5) or tell a member who may not see another's time that an entry exists, and over a timer that
asks every open dashboard once a minute for a sum that changes a few times a day.

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
is due ([ADR 0035](0035-personal-access-tokens.md) D8).)* *(Amended 2026-10-05, with D1: a
person-level stream replays across its tenants — the hub numbers the events in the order it receives
them, and a reconnect whose id is in the ring of any tenant the stream follows gets every followed
tenant's events after it, merged in that order, each through its tenant's filter; an id in none of
them is `resync` as before. Every event carries its id.)*

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
  exists, and the earlier records that assumed polling say "pushed, polled as fallback" in
  place since 2026-10-01: [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D4
  and [ADR 0053](0053-signals-and-services-no-store-framework.md)'s line on live updates.
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
- A burst of mutations (~~an import,~~ an agent loop) produces a burst of events *(2026-10-06: an
  import is one event, D2)*; D4's bounded
  buffers and `resync` keep the backend safe, at the cost of a full refetch for the clients
  that fell behind. The inbox collapse of ADR 0039 D5 does not apply to the stream.
- `NOTIFY` is not durable: a replica that is down misses nothing it needs (its clients are
  gone too), but a client reconnecting after longer than the replay window refetches fully.

## References

- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D3 — the wrapper that publishes at commit
- [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D3, D4, [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3 — what the stream must not leak
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D4, [ADR 0053](0053-signals-and-services-no-store-framework.md) — the inbox and the client's services, pushed over this stream and polled as the fallback
- [ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md) D2 — the version an event names
- ~~`frontend/nginx/default.conf.template` — where D6's location lands~~ *(until 2026-10-04)*;
  [docs/operations/installation.md](../operations/installation.md#expose-it) — the Ingress
  settings D6 names
