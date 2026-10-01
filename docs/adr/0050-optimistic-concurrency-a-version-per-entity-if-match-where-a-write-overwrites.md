# ADR 0050: Optimistic Concurrency — a Version per Entity, `If-Match` Required Where a Write Overwrites, and a `412` That Carries the Current Value

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"concurrency control?": optimistic locking with a version and `If-Match` on overwriting
writes, over last-write-wins, over pessimistic leases, and over per-field version counters.
The rules of D5–D7 were put to the owner with the question and not objected to.

**Not built.** No mutable entity exists.

## Context

Two sessions on one ticket — the owner and an agent, or two agents — are the normal case.
The body is replaced, not appended ([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D1), so a second writer who did not see the first silently destroys an analysis; the audit
diff shows that afterwards, which is too late. [ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
already shaped the routes: `PATCH` with `If-Match` for fields, `PUT` on addresses for
relations, a `from` precondition on transitions, a key on `POST`. Those shapes leave exactly
two kinds of write that overwrite — the body and the scalar fields — and that is where a
precondition must be mandatory. A pessimistic lease would only move the conflict to the
lease's expiry, and per-field counters add little once body, fields, questions and relations
are separate routes anyway.

## Decision

**D1 — Every mutable entity carries a `version`,** an integer incremented on every write to
the entity itself: ticket, comment, question, project, tenant, membership, saved filter,
time entry. The ticket's version counts changes to the ticket — fields, body, state,
assignee, rank, progress — and not comments, links, interest or attachments, which are
entities of their own; otherwise every field change would fail on a concurrent comment.

**D2 — The version is the strong `ETag`.** Every `GET` of an entity, including
`…/markdown` and `…/context` ([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)),
and every write's response carry `ETag: "<version>"`.

**D3 — `If-Match` is required where a write overwrites:** `PATCH …/tickets/{n}` and
`PUT …/tickets/{n}/body`, and the equivalent routes of the other entities (editing a
comment, a question's text, a project's settings). A missing header answers
`428 precondition_required`; a stale one answers `412 precondition_failed`.

**D4 — No `If-Match` on writes that do not overwrite:** `PUT` on addressed relations (links,
interest, watch), `POST` with an idempotency key, transitions with their `from`
([ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D1, D2, D3).

**D5 — A `412` carries what the client needs to merge:** the current `version` and, in
`errors[]`, each field the request tried to change with its `pointer` and the `current`
value on the server ([ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md)
D2). The server never merges; the client decides.

**D6 — The generated clients carry the `ETag` automatically** ([ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md)):
a read keeps it, the next overwriting write sends it. The MCP server keeps the `ETag` of the
last `get_ticket` per key and sends it with `record_state` and `set_progress`; on `412` the
tool returns the current body to the model and stops ([ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md)).

**D7 — The UI shows recent writers as information, not as a lock:** "Hans (via Claude Code)
changed the body two minutes ago" on the detail page, from the activity list; it never
prevents an edit, and the `412` is the guard.

## Consequences

- A lost update on a body or a field is impossible; the conflict surfaces at the writer who
  would have destroyed something, with the material to merge.
- `curl` users must send `If-Match` on `PATCH` and the body `PUT`; `428` tells them so.
- One integer column per mutable entity and one comparison in the mutation wrapper
  ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
  D3), which also bumps the version.
- D1's scoping of the ticket version is what keeps the rule livable; an amendment that
  widens it must say which concurrent writes it is willing to fail.

## Alternatives Considered

- **Last-write-wins.** No friction; a second session replacing the body deletes the first
  one's analysis and the audit shows it afterwards. Lost.
- **Pessimistic leases** ("being edited by …" with a timeout). The UI can warn; leases
  expire wrongly for agents, the conflict moves to the expiry, and a ticket gains a state.
  Lost; D7 keeps the warning as information.
- **Per-field versions.** A body write would not collide with a field change; several
  counters and ETags per ticket, for separation the routes already provide. Lost.

## Residual risks

- D1's choice of what bumps the ticket version is a line a future field may land on the
  wrong side of; the data record keeps the list beside the schema.
- A client that reads, waits an hour and writes gets a `412` it has to handle; the generated
  clients and the MCP server do, a hand-written script may not.

## References

- [ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md) — the route shapes this record completes
- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D1 — the body replacement that needs the guard
- [ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D2 — `errors[]` on the `412`
- [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md), [ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) — the clients that carry the `ETag`
- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D3 — where the version is compared and bumped
