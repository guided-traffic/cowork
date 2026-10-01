# ADR 0045: Idempotency — `PUT` Where It Is Free, a `from` Precondition on Transitions, and an `Idempotency-Key` Required on Every Agent `POST`, Stored in the Act's Transaction

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"idempotency for agent writes?": idempotency by semantics where it is free and by a stored
key where `POST` is unavoidable — over a stored key on every unsafe request, over natural
idempotency alone with client-generated ids, and over nothing. The rules of D5–D7 were put
to the owner with the question and not objected to.

**Not built.** No mutation route exists.

## Context

An agent retries after a timeout, and it cannot know whether the first attempt committed. A
retried `POST` without protection is a second ticket, a second comment, a second link.
[ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D1 already
reserved an `idempotency_key` column in the audit row; [ADR 0022](0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md)
D1 forbids the application — and therefore a client — to supply an id on insert, which rules
out the "client-generated UUID turns `POST` into `PUT`" trick; [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D3 gives every mutation one transaction to store a key in. Most of the API does not need a
key at all if its routes are shaped as `PUT` on a known address.

## Decision

**D1 — Routes that can be `PUT` are `PUT`, and need no key.** The body
(`PUT …/tickets/{n}/body`), the progress and the other scalar fields
(`PATCH …/tickets/{n}` with `If-Match`, the API record), interest
(`PUT …/tickets/{n}/interest`), a link (`PUT …/tickets/{n}/links/{type}/{other-key}`; an
existing link is success, not conflict), a watch. Repeating any of them is harmless by
construction.

**D2 — A transition carries its `from` state, and a stale one is a `409`.**
`POST …/tickets/{n}/transitions` with `{from, to, reason|note}`; when the ticket is no
longer in `from`, the answer is `409 state_conflict` with the current state, so a retry can
decide. The same transition repeated after it succeeded therefore does nothing twice.

**D3 — Every other `POST` accepts an `Idempotency-Key`, and an agent's `POST` requires
one.** Tickets, comments, questions, attachments, time entries, tokens. The key is scoped to
the caller — the token or the session — and is a UUID the client generates; a request from
an agent-marked caller ([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md))
without the header answers `400 idempotency_key_required`.

**D4 — The key is stored with the act, in the act's transaction.** The row holds the
caller, the key, a hash of the request body, and the response (status and body) for
twenty-four hours. The same key with the same body returns the stored response without
repeating the act; the same key with a different body answers `422 idempotency_mismatch`.
Because the row is written by the same `Mutate` as the act ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D3), "act committed, key lost" cannot happen. Expired rows are removed by a job.

**D5 — The MCP server generates and reuses keys itself;** one UUIDv7 per tool call, the
same one on every retry of that call. The model never sees or supplies a key.

**D6 — A stored response contains no attachment bytes** and is bounded by the JSON body
limit of [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2; an upload's stored response is its metadata.

**D7 — The audit row of the act carries the key** (ADR 0026 D1), so an attempt and its act
are one line of inquiry.

## Consequences

- A retry is safe whether or not the first response arrived: `PUT` and `from` by shape, the
  key by storage.
- The API's routes are shaped by this record: addresses for links and interest, a `from` on
  transitions, `PATCH` with `If-Match` for fields. The API record inherits that shape.
- One table for keys, one middleware step inside the mutation path, one cleanup job; nothing
  for `GET`.
- A person's `curl` without a key still works on `POST`; only agents are required to carry
  one, because that is where retries come from.

## Alternatives Considered

- **A stored key on every unsafe request.** Uniform; stores responses for routes that are
  idempotent by shape and would never need them. Lost.
- **Natural idempotency alone, with client-generated ids on `POST`.** No stored state; breaks
  ADR 0022 D1 and moves duplicate prevention into every client. Lost.
- **Nothing; people spot duplicates.** Every timeout in a session a possible duplicate ticket,
  and deletion is an administrator's act ([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)).
  Lost.

## Residual risks

- Twenty-four hours is a guess at the longest retry gap; a key reused after it is a new
  request. Configurable if it ever matters.
- D3's requirement applies to agent-marked callers only; a person's script that retries
  without a key can still duplicate. The operations page recommends the header for scripts.

## References

- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D1 — the key in the audit row
- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D3, D5 — the transaction and the cleanup job
- [ADR 0022](0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md) D1 — why clients do not supply ids
- [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) — who counts as an agent
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) — the transitions D2 shapes
