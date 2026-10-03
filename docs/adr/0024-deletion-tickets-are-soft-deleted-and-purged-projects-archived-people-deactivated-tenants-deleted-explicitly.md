# ADR 0024: Deletion — Tickets Are Soft-Deleted and Purged After Thirty Days, Projects Archived, People Deactivated, Tenants Deleted Only Explicitly

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"deleting?": the full matrix — soft delete with purge for tickets, archive for projects,
deactivation for people, explicit deletion for tenants — over "dropped is the delete", over
soft delete for tickets alone, and over hard delete. The additional rules of D7 were put to
the owner with the question and confirmed.

**Partly built** (phase 2, 2026-10-02): D4 for projects (archived, never deleted) and people
(a `deactivated_at` column a token's person is refused by). Ticket deletion, the purge, its
filter on every list and the tenant deletion are not built; no route deletes anything.

**Partly built** (phase 3, 2026-10-03): D5 for local accounts — a tenant's administrator
deactivates an account their tenant manages (`PUT …/accounts/{username}/deactivation`), and the
start-up synchronisation deactivates the local administrator when its variables are emptied: the
person cannot log in, their sessions end, their tokens are revoked, and the person and every
act stay. Not built: marking the memberships inactive — they stay as they were, and a deactivated
person is refused at the resolver everywhere —, a route that reactivates (only the start-up
synchronisation does, for the configured account) and the deactivation of a person who has no
local account.

## Context

Earlier records already refuse to lose things: a project is archived
([ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md) D4),
a comment is withdrawn ([ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md)
D3), a time entry is voided ([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D7), a key is never reused ([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md)
D4), the audit record is append-only ([ADR 0004](0004-cowork-is-a-team-product.md) D3). What
none of them covers is the case where something must actually disappear: a ticket pasted
into the wrong tenant's tab with another client's data in it, a tenant whose contract ended,
an attachment that should not have been uploaded. `dropped` is a state, not an erasure; it
leaves the content readable.

## Decision

**D1 — A ticket is soft-deleted by a tenant administrator and purged after thirty days.**
Deletion sets `deleted_at` and records the act; the ticket leaves every list, board, search,
prerequisite tree, dashboard tile and person-level list at once; its key stays taken. Links
to it are hidden, not removed. Its attachments' objects stay in storage until the purge. A
tenant administrator sees deleted tickets in a bin and may restore one, which is a recorded
act and brings its links back.

**D2 — The purge is a job, or an administrator's explicit act.** Thirty days after deletion
a job hard-deletes the ticket row, its comments, questions, links, interest, time entries,
notifications and attachment metadata, and removes the attachment objects from storage. An
administrator may purge earlier with a second confirmation. **The audit rows survive the
purge**: they keep the ticket's key, the actor and the act, with the content fields emptied.

**D3 — Soft deletion is an application filter, not a policy.** Row-level security
([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md)) stays the
tenant predicate alone; `deleted_at IS NULL` is part of every list query in the data layer,
and the bin is the one query that inverts it.

**D4 — A project is archived (ADR 0006 D4); an archived project with no tickets at all —
deleted ones included — may be deleted.** Anything else about a project is archiving.

**D5 — A person is never deleted; a person is deactivated.** A deactivated person cannot
log in, their tokens are revoked, their memberships are kept as inactive, and every audit
row, comment, time entry and act keeps its actor. A person who returns is reactivated.

**D6 — A tenant is deleted only by a global administrator, explicitly, by typing its slug,
and the deletion is immediate and complete:** every row of the tenant, every attachment
object under its prefix ([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D2), every notification that referenced it. The audit rows of the tenant are deleted with
it, after one final audit row in the installation-level record names the tenant, the
administrator and the time. The slug is not reused.

**D7 — Who may, and what warns.** Delete, restore and purge are tenant-administrator acts;
tenant deletion is a global administrator's. An agent never deletes anything. Deleting a
ticket that other open tickets depend on ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)
D6) shows the dependents and asks for confirmation; it does not refuse. Restoring is a
recorded act.

## Consequences

- Every list query carries one more predicate; the data layer makes it part of the generated
  query, not of each call site.
- A purge job exists from the first release: a periodic task in the backend that is
  idempotent and logs what it removed.
- D2's survival of audit rows means the installation keeps that a ticket `acme/VKO-12`
  existed and was deleted by whom, without keeping what it said. That is the balance between
  "nothing is silently lost" (ADR 0004 D3) and "this must be gone".
- D5 means the people table only grows; a deactivated person is a row with a flag and no
  login. Personal data in that row is the privacy question of a later record.
- D6 is the one irreversible act in cowork. It is typed, not clicked.

## Alternatives Considered

- **`dropped` is the delete.** Nothing disappears; the wrong-tenant paste stays readable in
  the wrong tenant. Lost.
- **Soft delete for tickets only.** Correct for tickets, silent on projects, people and
  tenants; each would have come back as a question. Lost to the matrix.
- **Hard delete.** Audit rows pointing at nothing, prerequisite trees losing nodes silently,
  no protection against a slip. Lost.
- **Deleting people.** Every act they made would lose its actor. Lost to D5.

## Residual risks

- D2's thirty days are a guess; a tenant with a legal retention requirement wants it longer
  or shorter, and that is a per-tenant setting when asked for.
- D6 deletes audit rows with the tenant; an installation that must keep them for compliance
  needs an export before deletion, which the operations page will say.
- The purge job is the first background task in the backend; its scheduling in a multi-
  replica deployment (one runner, not two) is the data record's or an operations record's to
  settle.

## References

- [ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md) D4, [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D3, [ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md) D7 — what is never deleted
- [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md) D2 — attachment objects and the tenant prefix
- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) — why soft deletion is not a policy
- [ADR 0004](0004-cowork-is-a-team-product.md) D3, D4 — attribution and the administrator as an explicit grant
