# ADR 0026: One Append-Only Audit Table, Written by the Request Layer in the Mutation's Transaction, Append-Only by Role Grant

## Status

Accepted, amended 2026-10-02 (D1: system actors, the ticket's key, the capabilities, the
named other tickets, comment texts kept out; D2: an act is required and published; D4: an
installation-level row is read by the person it names; D6: the activity withholds what a
reader may not see), 2026-10-03 (D1: the actors and actions of the login) and 2026-10-04 (D1: the
action `login_refused`, the column `source_hash`, the system actor `system:identity-provider`; after
the security review, no e-mail address in `before` or `after`; by the owner's answer on the groups,
no group of a person in them either; by the owner's decision that every act made through a token is
shown as such, [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D6, the column `token_name`; D6: the tenant's view shows it beside the token's id) and 2026-10-05
(D3: the purge's function runs inside the transaction that records its act instead of writing the
act itself — the first implementation found that a function cannot know the token, the agent, the
request and the source hash the row must carry; not yet put to the owner, see the Status below).
Date: 2026-10-01. Decided by the owner as the answer to the catalog
question "audit log — which form?": one table for every mutation of every entity, over a
history table per entity and over trigger-written rows. The rules of D6–D7 were put to the
owner with the question and not objected to.

The amendments of 2026-10-02 record what the first implementation needed: background jobs
act as no person, so a row names a person or a system actor; a withdrawn comment's text must
disappear from every route, which an append-only row cannot do, so it never enters one; and
an act on one ticket can name another the reader may not see
([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md) D4).

D3 holds as written because the owner role it presumes is mandatory since the amendment of
[ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2 of
2026-10-02: "the migration role" below is that owner role, and "the application role" is the
runtime role, which owns nothing.

**Built** (phase 2, 2026-10-02): D1–D6 — migration 5, `store.Mutate` and the routes of the
tenant's audit view, a ticket's activity and the downloads and exports. D3's `SECURITY
DEFINER` functions arrive with the purge and the tenant deletion; D6's per-token view and the
global administrator's reading arrive with their routes; D7 needs nothing yet. Since phase 4
(2026-10-04) the rows carry `source_hash` and the identity provider's acts
([migration 20](../../backend/internal/store/migrations/000020_identity_provider.up.sql),
[`store/identity.go`](../../backend/internal/store/identity.go)); the installation-level rows of a
system actor name no person and are read by no route. Since 2026-10-04 a row carries its token's
name beside its id ([migration 27](../../backend/internal/store/migrations/000027_acts_through_a_token.up.sql),
`tokenName` in [`store/tx.go`](../../backend/internal/store/tx.go)), and the activity shows it.
Since 2026-10-04 D2's notifications reference their act's row
([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D3,
[migration 30](../../backend/internal/store/migrations/000030_notifications.up.sql)), and marking
one's notifications read is the act `read` (added to D1's list below), in the tenant of the
notifications.

**D3's purge built** (2026-10-05): `purge_ticket_audit`
([migration 35](../../backend/internal/store/migrations/000035_ticket_deletion.up.sql)), owned by
the owner role, executable by the runtime role alone, its `search_path` fixed with `pg_temp` last,
empties `before`, `after`, `reason` and `note` of the current tenant's rows of one deleted ticket in
a transaction that names the purge, and refuses everything else; the policy `audit_purge` admits
that update to the owner role. The purge's own act is written by D2's wrapper in the same transaction
(D3 as amended). The amendment is the implementer's and is open to the owner's objection: a function
that wrote the row itself would need the request's facts handed in, and would trust them no more than
the wrapper does. The tenant deletion's function is not built.

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
| `actor_user_id` | the person; ~~never null~~ *(amended 2026-10-02: null exactly when `actor_system` names a background job's system actor, `system:<name>`; a CHECK holds one of the two)* *(amended
2026-10-03: the login's own transaction, where no person is known yet, acts as `system:login`,
and the start-up synchronisation as `system:bootstrap`)* *(amended 2026-10-04: what the identity
provider decides — a login through it, a session's groups refresh, a token's gate check, the
memberships it derives — is `system:identity-provider`'s, also where an administrator's request
records it: the derivation that follows a change of a group mapping is the system actor's act in
the administrator's transaction, and carries the request, not the administrator)* |
| `agent` | null, or the agent mark from the request (name, model, session) when an agent acted in the person's name; *(added 2026-10-02)* `agent_capabilities` the capability set that applied ([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md) D5) |
| `token_id` | the personal access token used, or null for a browser session; *(added 2026-10-04, [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6)* `token_name` its name as the token has it, copied when the row is written — the activity shows it to readers who may not read the token's row, and a revoked token's acts keep it; null without a token, and on every row written before the column existed, which names the token by its id alone; a system actor's act in a request carries neither |
| `entity_type`, `entity_id` | what changed |
| `ticket_id` | the ticket the entity belongs to, denormalised, so a ticket's activity is one index scan; *(added 2026-10-02)* `ticket_key` its key, which survives the ticket's purge |
| `action` | an enum: `created`, `updated`, `transitioned`, `linked`, `unlinked`, `commented`, `edited`, `withdrawn`, `assigned`, `interest`, `ranked`, `overridden`, `asked`, `answered`, `booked`, `voided`, `locked`, `uploaded`, `downloaded`, `exported`, `deleted`, `restored`, `purged`, … *(added 2026-10-03: `logged_in`, `logged_out`, `login_failed`, `unlocked`, `password_changed`, `password_reset`, `deactivated`, `reactivated`)* *(added 2026-10-04: `login_refused`, a login through the identity provider whose ID token verified and which the gate, a deactivation or the init state refused — installation-level, with the person when one exists and the reason; a login that fails before that is in the log only)* *(added 2026-10-04: `read`, a person's own notifications marked read — one or every one up to the newest seen, per tenant, [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D6)* |
| `before`, `after` | JSONB of the changed fields only; *(amended 2026-10-02)* never a comment's text, which a withdrawal must be able to hide ([ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D3) — the comment's revisions keep it; *(amended after the security review, 2026-10-04)* never an e-mail address, which no append-only row could erase on request — a changed address is recorded as `email_changed: true`; ~~a person's group lists are recorded~~ *(amended 2026-10-04, the owner's answer recorded in [ADR 0030](0030-a-global-allow-list-gates-login-group-mappings-derive-membership-a-marked-grant-adds-to-it.md) D6)* never a person's groups either: a change of them is recorded as `groups_changed: true`, and the row of a person's creation says nothing of them; the memberships the groups cause are recorded tenant by tenant |
| `refs` *(added 2026-10-02)* | the other tickets the payload names — a link's other end, the ticket a block waits on, the prerequisites a close overrode, a parent; D6 withholds the payload from a reader who cannot see one of them |
| `reason`, `note` | the transition's reason or verification note, the override's reason |
| `explained_by_comment_id` | the comment written in the same request (ADR 0015 D2) |
| `idempotency_key`, `request_id` | the request's keys |
| `source_hash` *(added 2026-10-04)* | the keyed hash of the client address of the request the row was written for ([ADR 0035](0035-personal-access-tokens.md) D2); null for the rows of jobs and of the start-up, and for every row written before the column existed; shown by no route |
| `created_at` | the time |

**D2 — The request layer writes the row, in the same transaction as the mutation.** No
mutation commits without its row; a request that fails writes no row. Notifications
([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D3) reference the row.
*(Made concrete 2026-10-02:)* the mutation wrapper refuses to commit a write that recorded no
act, and a request that changes nothing records none and commits nothing; the same wrapper
publishes each row of a ticket's act with `NOTIFY` in that transaction
([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md) D4).

**D3 — Append-only is a grant, not a convention.** The application role has `INSERT` and
`SELECT` on `audit_events` and nothing else. The two writes that are not inserts — the
purge's emptying of content fields and the tenant deletion of ADR 0024 D6 — run through
`SECURITY DEFINER` functions owned by the migration role, ~~each of which writes its own audit
row first~~ *(amended 2026-10-05, built with the purge: each of which runs only inside the
transaction that records its act through D2's wrapper, so that the act and the change commit
together or not at all, and refuses outside its own case — the purge's function outside a
transaction that names the purge, and for any ticket that is not deleted; the function cannot write
the act itself, because the token, the agent mark, the request and the source hash the row carries
are the request layer's, D2)*.

**D4 — Row-level security applies as everywhere** ([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md));
rows with `tenant_id IS NULL` are readable by global administrators only, through a policy
on that condition. *(Amended 2026-10-02: an installation-level row is readable by the person
it names as actor — a refused or revoked token's row is its owner's to read — and the global
administrator's reading arrives with that role; a row is inserted only into the context it
belongs to.)*

**D5 — Reads are not audited, with two exceptions:** the download of an attachment
([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md))
and the Markdown export of a ticket by a token — both mean "data left the system".

**D6 — The record is readable through the API** per ticket (the activity list), per tenant
for its administrators (filterable by actor, token, action, entity and period, exportable as
CSV; *(amended 2026-10-04, [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D6)* naming the token by `token_name` beside `token_id` in JSON and as the last column of the CSV, after the
columns released before), and per token (what
this token did). A global administrator reads installation-level
rows the same way. *(Added 2026-10-02:)* the activity list shows an act whose `refs` include a
ticket the reader cannot see without its `before`, `after`, reason and note, marked as
withheld; it leaves out time entries ([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D9) and the reads of D5.

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
