# ADR 0050: Optimistic Concurrency — a Version per Entity, `If-Match` Required Where a Write Overwrites, and a `412` That Carries the Current Value

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"concurrency control?": optimistic locking with a version and `If-Match` on overwriting
writes, over last-write-wins, over pessimistic leases, and over per-field version counters.
The rules of D5–D7 were put to the owner with the question and not objected to.

Amended 2026-10-02 (D1: the urgency override and the confidential flag count, derived values
do not; D2: an entity read never answers `304`; D3: the overwriting writes the first
implementation added; D5: an empty field's current value is `null`) and 2026-10-03 (D1: a
move in the rank counts, the first key given to a ticket a release before the rank left
without one does not; D4: a move in the rank takes no `If-Match`, written when the rank was
built) and 2026-10-04 (D4: a person's choice of the chat's capabilities takes no `If-Match`, written
when it was built for the owner's answers recorded in
[ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)), and
made concrete 2026-10-05 (D1: the score of
[ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) D4 does not count, a sort of the
project's rank by the score counts for every ticket it moves, a rebalancing of the rank's keys does
not). A value cowork derives from another entity — the urgency re-derived when a link or
another ticket changes, a parent's progress derived from its children — falls under D1's own
reason: counting it would fail an edit on a concurrent change elsewhere. *(Made concrete 2026-10-10
by the implementer, open to the owner's objection:)* D1 — a child's parent cleared from the parent's
side does not count either.

**Built** (phase 2, 2026-10-02): D1–D5 for tickets, comments, questions, projects, tenants and
time entries; the rank's moves since 2026-10-03. D6's clients and D7's UI arrive with them;
memberships and saved filters have no write route yet. *(2026-10-04.)* The UI sends `If-Match` on
every overwriting write it makes — the ticket's fields and body, the horizon, the flag, a comment,
a question's text and its answer, a time entry. An editor that stays open while the event stream
brings newer versions writes over the version it began with, never over a newer one it did not
show, and a `412` is written over once without asking only where the field the write changes is
unchanged; otherwise the person decides. D7's recent writers are not shown.

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
*(Amended 2026-10-02: the urgency override and the confidential flag are the ticket's own and
count; the urgency re-derived from a link or another ticket's change and a parent's derived
progress do not.)* *(Amended 2026-10-03: a move in the rank counts; the first key the rank
gives a ticket a release before the rank left without one does not — it is no move, and it
comes with another ticket's write ([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)
D2).)* *(Made concrete 2026-10-05: the score of ADR 0014 D4 does not count — it is derived, and a
stake, an entity of its own, moves it —; a sort of the project's rank by the score counts for
every ticket it moves, as a move does; a rebalancing of the rank's keys, which keeps every
ticket's place, does not.)* *(Made concrete 2026-10-10 by the implementer, open to the owner's
objection:)* a child's parent cleared from the parent's side — the removal of a relation by a
writer of the parent ([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md)
D2 as amended again 2026-10-10), the purge of the parent
([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2) — does not count: it is a write on the parent, and in another team the crossing that performs
it may write the parent column alone. So the version alone no longer tells whether the parent a
patch read still stands, and the patch's compare-and-set covers the parent as it read it beside the
version: a patch that raced such a removal — read before it, written after it — is refused with
`412` instead of writing the parent back, which a patch that does not touch the parent would have
done, and instead of recording a removal that had happened already. The `412` of a lost
compare-and-set carries the version and the sent fields as they stand after the write that won
(D5), read again through the visibility predicate as the patch read the ticket first — a ticket that
write took out of the caller's sight answers `404` —, where it carried the values the patch had
read.

**D2 — The version is the strong `ETag`.** Every `GET` of an entity, including
`…/markdown` and `…/context` ([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)),
and every write's response carry `ETag: "<version>"`. *(Amended 2026-10-02: an entity's
`ETag` serves `If-Match`, not a cache — an entity read never answers `304`. The weak `ETag`
of a list is the polling fallback's ([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D7) and does answer `304`.)*

**D3 — `If-Match` is required where a write overwrites:** `PATCH …/tickets/{n}` and
`PUT …/tickets/{n}/body`, and the equivalent routes of the other entities (editing a
comment, a question's text, a project's settings). A missing header answers
`428 precondition_required`; a stale one answers `412 precondition_failed`. *(Added
2026-10-02:)* the urgency override's set and withdrawal, the confidential flag, a tenant's
settings, a time entry's correction, and the change of an answer once a question is answered.

**D4 — No `If-Match` on writes that do not overwrite:** `PUT` on addressed relations (links,
interest, watch), `POST` with an idempotency key, transitions with their `from`
([ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D1, D2, D3). *(Amended 2026-10-03:)* A move in the rank
([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) D2) takes none either: it names
where the ticket goes, not the key it had, so the last move wins and a drag keeps the person's
drop; the move still raises the version (D1). *(Amended 2026-10-04:)* The person's choice of the
chat's capabilities, `PUT /api/v1/me/chat`
([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D5), takes none either and has no version: it is the person's own setting with the person as its
only writer, the whole set is sent each time, and the same set sent again changes nothing; between
two tabs of the same person the last choice wins, and each tab reads it again when its panel opens.

**D5 — A `412` carries what the client needs to merge:** the current `version` and, in
`errors[]`, each field the request tried to change with its `pointer` and the `current`
value on the server, `null` for an empty field ([ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md)
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
