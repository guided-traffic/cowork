---
id: T20
title: clients learn about a change only by asking again — there is no event stream and nothing is published at commit
state: done
severity: high
security: hardening
threat: filters every event by the subscriber's tenant, visible projects, token restriction and the confidential rule before it is sent, and re-checks the token at each heartbeat — additionally covering the existence, key or version of a hidden ticket reaching a subscriber and a revoked token keeping an open stream
urgency: later        # rule 4: decided fix (the ADRs)
effort: L
blocked-by: T12
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: the event stream — NOTIFY at commit, one listener per replica, filtered fan-out, replay, heartbeat re-checks, limits, shutdown first
---

## Current state

Decided by [ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D1–D6, D8, D9, [ADR 0027](../adr/0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D2, D3, [ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D5, [ADR 0035](../adr/0035-personal-access-tokens.md) D6 and
[ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2. On `main`:

- No stream, no `NOTIFY`, no listener. `Serve` calls `srv.Shutdown`, which waits for active
  connections ([`server.go`](../../backend/internal/httpserver/server.go)), so an open stream would
  hold a shutdown for the whole timeout.
- A `pg_notify` payload of 7999 bytes is accepted and one of 8000 refused (verified).
- Any role that can connect to the database can `LISTEN` on a channel, and a new database grants
  `CONNECT` to `PUBLIC`: the payloads, which carry keys and visibility inputs of every tenant,
  pass row-level security for anyone who can connect.
- ADR 0054 D1's `?me=true` and D2's `inbox.changed` carry the inbox, which is phase 3's. No
  phase-2 route changes a membership, so nothing emits `membership.changed` yet.
- ADR 0035 D6 makes revocation immediate; an open stream is not a next request.
- T6 lifts ADR 0039 D2's request timeout for this route; T12 builds the polling fallback — weak
  `ETag`s and `304` on the lists (ADR 0054 D7).

## Required changes

1. **Publication:** `Mutate` issues `pg_notify` for every act that has a tenant (ADR 0054 D4) with
   the tenant, the project, the event type, the key, the version, the act's kind, the audit row's
   id and the visibility inputs (confidential, assignee, reporter); one event per act and ticket;
   under 8000 bytes.
2. **Listener:** one dedicated connection per replica outside the pool, inside `internal/store`
   (ADR 0027 D2); a hub per tenant; each subscription filters by its visible projects (computed
   at connect) ∩ the token's project restriction and by the confidential rule (ADR 0054 D3,
   ADR 0065 D5).
3. `GET /api/v1/tenants/{slug}/events` — documented, served outside the strict interface: the
   tenant boundary before the first byte; `text/event-stream`, `X-Accel-Buffering: no`,
   `Cache-Control: no-cache` (D6); `ticket.changed` (an upload included), `comment.changed`,
   `question.changed`, `link.changed`, `interest.changed` (D2); `id:` the audit row's id; a
   per-tenant ring buffer of `COWORK_SSE_REPLAY_WINDOW` (default five minutes) and a
   `Last-Event-ID` replay with the filter applied again, `event: resync` beyond the buffer (D5); a
   heartbeat every 20 seconds that re-validates the token and the membership and closes the
   stream when either fails; a bounded buffer per stream, `resync` and the stream dropped when it
   overflows (D4); `COWORK_SSE_MAX_STREAMS_PER_PERSON` (default 10, `0` disables; counted per
   replica), the eleventh stream closing the oldest with `event: unavailable` (D8); `resync` to
   every stream after the listener reconnects, `unavailable` while it cannot.
4. **Shutdown** ends every stream before `srv.Shutdown` drains the requests (D9).
5. **nginx:** inside the `/api/` location a nested location for `…/events` with
   `proxy_buffering off`, `proxy_cache off`, a one-hour read timeout and HTTP/1.1 (D6).
6. **Tests:** a committed act reaches a subscriber in under a second, a rolled-back one never; a
   member of B hears nothing of A; a person outside a restricted project hears nothing (seeded);
   the confidential silence; a replay inside the window and `resync` beyond it; a slow subscriber
   gets `resync` and is dropped while acts proceed; the eleventh stream closes the oldest; the
   heartbeat; a revoked token's stream closes within one heartbeat; `SIGTERM` ends the streams
   first; through both images, the stream passes nginx unbuffered.
7. **Docs and records:** new docs/developer/events.md and its row in the developer README;
   installation.md: `REVOKE CONNECT ON DATABASE … FROM PUBLIC`, with `CONNECT` granted to the
   owner and the runtime role only; runtime.md (the Ingress annotations, "streams die every
   minute" as the symptom of a proxy that buffers or times out, the limits); tenancy.md — the
   stream's filter, and its gaps with the next free `H-<n>`: the channel is readable by any role
   that can connect to the database, past row-level security; events are not kept beyond the
   replay window; the README's configuration rows and API; ADR 0054 (D1 without `?me=true`; D7's
   client side phase 3) Status and index row.

## Related

- T6 — the request timeout this route is exempt from.
- T8 — `Mutate`, where publication joins the commit.
- T12 — the predicates the filter applies and the polling fallback.
