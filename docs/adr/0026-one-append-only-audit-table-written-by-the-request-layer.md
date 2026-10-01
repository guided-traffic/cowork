# ADR 0026: One Append-Only Audit Table, Written by the Request Layer in the Mutation's Transaction, Append-Only by Role Grant

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "audit
log — which form?": one table for every mutation of every entity, over a history table per
entity and over trigger-written rows. The rules of D6–D7 were put to the owner with the
question and not objected to.

**Not built.** No `audit_events` table exists.

## Context

[ADR 0004](0004-cowork-is-a-team-product.md) D3 requires every change attributable and
shown; [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D6 makes the
activity list a projection of the record; [ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2 empties content fields on purge but keeps the rows; [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md)
D3 creates notifications in the act's transaction and lets them reference the act. Every
record so far writes "a recorded act"; this one says what a recorded act is. What only the
request layer knows — the person, the agent mark, the token, the reason, the idempotency key
— is what makes a row an audit row rather than a change log, which is why a trigger cannot
write it.

## Decision

**D1 — One table, `audit_events`, for every mutation of every entity.** Columns:

| Column | Content |
|---|---|
| `id` | UUIDv7; the order of events ([ADR 0022](0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md) D5) |
| `tenant_id` | the tenant, or null for installation-level acts (a tenant created or deleted, a global administrator granted) |
| `actor_user_id` | the person; never null |
| `agent` | null, or the agent mark from the request (name, model, session) when an agent acted in the person's name |
| `token_id` | the personal access token used, or null for a browser session |
| `entity_type`, `entity_id` | what changed |
| `ticket_id` | the ticket the entity belongs to, denormalised, so a ticket's activity is one index scan |
| `action` | an enum: `created`, `updated`, `transitioned`, `linked`, `unlinked`, `commented`, `edited`, `withdrawn`, `assigned`, `interest`, `ranked`, `overridden`, `asked`, `answered`, `booked`, `voided`, `locked`, `uploaded`, `downloaded`, `exported`, `deleted`, `restored`, `purged`, … |
| `before`, `after` | JSONB of the changed fields only |
| `reason`, `note` | the transition's reason or verification note, the override's reason |
| `explained_by_comment_id` | the comment written in the same request (ADR 0015 D2) |
| `idempotency_key`, `request_id` | the request's keys |
| `created_at` | the time |

**D2 — The request layer writes the row, in the same transaction as the mutation.** No
mutation commits without its row; a request that fails writes no row. Notifications
([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D3) reference the row.

**D3 — Append-only is a grant, not a convention.** The application role has `INSERT` and
`SELECT` on `audit_events` and nothing else. The two writes that are not inserts — the
purge's emptying of content fields and the tenant deletion of ADR 0024 D6 — run through
`SECURITY DEFINER` functions owned by the migration role, each of which writes its own audit
row first.

**D4 — Row-level security applies as everywhere** ([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md));
rows with `tenant_id IS NULL` are readable by global administrators only, through a policy
on that condition.

**D5 — Reads are not audited, with two exceptions:** the download of an attachment
([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md))
and the Markdown export of a ticket by a token — both mean "data left the system".

**D6 — The record is readable through the API** per ticket (the activity list), per tenant
for its administrators (filterable by actor, token, action, entity and period, exportable as
CSV), and per token (what this token did). A global administrator reads installation-level
rows the same way.

**D7 — Retention is indefinite, partitioned when it has to be.** Nothing is deleted from the
table except by ADR 0024's purge and tenant deletion. Partitioning by month is an amendment
when the table's size asks for it, not a design change.

## Consequences

- One shape to write, one to query; "everything token X did yesterday" and "every deletion in
  this tenant" are one query each.
- JSONB diffs instead of typed history columns: a question about one field goes through
  `->>`; the activity list renders from `action` and the diff.
- D3 makes the audit table the one table the application cannot rewrite; a defect in the
  application cannot erase its own trail.
- D5 adds a row per download and per token export; at the expected sizes that is noise the
  partitioning of D7 absorbs later.
- The data layer must offer "write this mutation and its audit row" as one operation, or
  D2 is a convention again; the data-access record takes that up.

## Alternatives Considered

- **A history table per entity with typed columns.** Typed queries per entity; n tables, n
  write paths, cross-entity questions as unions, retention n-fold. Lost.
- **Trigger-written rows.** Nothing forgotten; the trigger knows neither the person, the
  agent, the token nor the reason, and acts that change no row (a refused override, an
  export) leave no trace. Lost.
- **Auditing every read.** The table would be dominated by page views. Lost to D5.

## Residual risks

- D3's `SECURITY DEFINER` functions are the one privileged code path in the schema; they are
  small, reviewed, and tested in the integration tier for what they may and may not touch.
- The `action` enum grows with features; adding a value is a migration, which is the
  intended cost.

## References

- [ADR 0004](0004-cowork-is-a-team-product.md) D3 — attribution
- [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D2, D6 — the activity list and the explaining comment
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D3 — notifications in the act's transaction
- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md), [ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md) D2, D6 — policies, purge and tenant deletion
