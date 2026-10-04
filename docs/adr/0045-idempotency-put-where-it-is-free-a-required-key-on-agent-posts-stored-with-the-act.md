# ADR 0045: Idempotency — `PUT` Where It Is Free, a `from` Precondition on Transitions, and an `Idempotency-Key` Required on Every Agent `POST`, Stored in the Act's Transaction

## Status

Accepted, amended 2026-10-03 (D3: a session's key is scoped to the person; D6: a stored
response never holds a secret; D4: the fingerprint is an HMAC under a key derived from the
server key) and 2026-10-04 (D5: the chat in the UI derives its keys from the conversation and the
call). Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"idempotency for agent writes?": idempotency by semantics where it is free and by a stored
key where `POST` is unavoidable — over a stored key on every unsafe request, over natural
idempotency alone with client-generated ids, and over nothing. The rules of D5–D7 were put
to the owner with the question and not objected to.

**Built** (phase 2, 2026-10-02) except D5: `PUT` for the body, the links, the interest and the
flags; the `from` precondition on transitions; keys on the other `POST`s, required from
agents; the stored response and the replay in [`store.Mutate`](../../backend/internal/store/tx.go)
with a `422` on a different request under the same key, expired by an hourly job; an upload's
fingerprint over the file's hash, name and comment instead of the raw multipart body; an
unsolicited key on a transition recorded on the act. D5 is built (2026-10-04) in `cowork-mcp`: every `POST` of a tool — the `api` tool's included —
carries a key of its own, and the integration tier holds that every ticket, comment and
question the working day creates carries one on its act. D3 holds
for a browser session since phase 3 (2026-10-03), and for the creation of a token, a tenant and
a local account.

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
without the header answers `400 idempotency_key_required`. *(Amended 2026-10-03: no row carries a
session's id ([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D7), so a key sent
with the session cookie is scoped to the person, and a key sent with a token to the token, as
before; the unique index is on the person, the token and the key, with an absent token one
value of its own.)*

**D4 — The key is stored with the act, in the act's transaction.** The row holds the
caller, the key, a hash of the request body *(amended 2026-10-03: an HMAC-SHA-256 under a key
derived from the server key, over the operation, its scope and the body — the body of a creation
can carry a local account's temporary password, and a plain hash of it in a backup could be
guessed far faster than the Argon2id hash the password is kept as; a key replayed across a
change of the server key meets `422 idempotency_mismatch` until its row expires)*, and the
response (status and body) for
twenty-four hours. The same key with the same body returns the stored response without
repeating the act; the same key with a different body answers `422 idempotency_mismatch`.
Because the row is written by the same `Mutate` as the act ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D3), "act committed, key lost" cannot happen. Expired rows are removed by a job.

**D5 — The MCP server generates and reuses keys itself;** one UUIDv7 per tool call, the
same one on every retry of that call. The model never sees or supplies a key. *(Made concrete
2026-10-04: one per `POST` a tool call sends — `finish_work` sends two, and one key on two
requests is D3's `422`; a retry is the transport's, of the same request. A model that calls a
tool again after a failure makes a new call with new keys.)* *(Amended 2026-10-04 for the chat in
the UI, [ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md):
the chat, the catalogue's second host, derives a tool call's keys instead of drawing them — the
n-th `POST` of a call gets a name-based UUID of the conversation's id, the call's id and n — so a
call the conversation holds, run again — ~~a decision the browser sends twice~~ *(amended again
2026-10-04: the decisions are gone with the proposals, ADR 0076; a turn the browser sends twice)* —
carries the same keys and replays instead of acting twice. A new answer of the model has new call ids and new keys.
The keys are scoped to the person, whose session the chat's requests carry (D3).)*

**D6 — A stored response contains no attachment bytes** and is bounded by the JSON body
limit of [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2; an upload's stored response is its metadata. *(Amended 2026-10-03: nor a secret. The response
stored for a token's creation omits the plaintext, which is shown once
([ADR 0035](0035-personal-access-tokens.md) D1), and a replay for the key answers the token's
metadata without it.)*

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
