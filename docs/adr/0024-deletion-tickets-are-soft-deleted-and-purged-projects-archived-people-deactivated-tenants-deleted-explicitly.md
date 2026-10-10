# ADR 0024: Deletion — Tickets Are Soft-Deleted and Purged After Thirty Days, Projects Archived, People Deactivated, Tenants Deleted Only Explicitly

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"deleting?": the full matrix — soft delete with purge for tickets, archive for projects,
deactivation for people, explicit deletion for tenants — over "dropped is the delete", over
soft delete for tickets alone, and over hard delete. The additional rules of D7 were put to
the owner with the question and confirmed. Amended 2026-10-05, built on the recommendation, the
owner reviewing the result (D7: the purge takes a browser session; a token, an administrator's
`admin` token included, cannot make it). Amended 2026-10-06 (D2: a purged ticket's children become
roots, and a block that waited on it waits on its key as an external reference — two decisions of
the implementer in building, accepted by the owner 2026-10-06).

**Partly built** (phase 2, 2026-10-02): D4 for projects (archived, never deleted) and people
(a `deactivated_at` column a token's person is refused by). ~~Ticket deletion, the purge, its
filter on every list and the tenant deletion are not built; no route deletes anything.~~ *(Ticket
deletion and the purge built 2026-10-05, below; the tenant deletion is not.)*

**Built** (phase 3, 2026-10-05): D1–D3 and D7 for tickets.
[Migration 32](../../backend/internal/store/migrations/000032_ticket_deletion.up.sql) adds
`deleted_at` and `deleted_by`; `DELETE …/projects/{project}/tickets/{number}` deletes,
`GET …/deleted-tickets` is the bin, `PUT …/deleted-tickets/{key}/restore` restores and
`DELETE …/deleted-tickets/{key}` purges — a tenant administrator's acts with `admin` scope, never an
agent's, the purge in a browser session only (D7 as amended 2026-10-05)
([`api/deletion.go`](../../backend/internal/api/deletion.go)), each recorded and published.
D3 as built: every query of the data layer that reads a ticket carries `deleted_at IS NULL` beside
the visibility predicate, held to it by a unit test over the query files; the bin's two queries and
the purge's invert it, and the rank keys, a writer's reread, the publication of an act and the
integrity walks name their exemptions. D2 as built: the job `ticket-purge` runs every hour on every
replica under its advisory lock ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D5), up to 200 tickets a run, and the explicit purge asks twice in the browser and takes a browser
session in the API; deleting what belongs to the ticket is held to the purge of a deleted ticket by restrictive
policies, the audit rows' content is emptied by an owner's function
([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D3 as amended
2026-10-05), and the attachment objects go after the commit. Two consequences the record did not
name were decided in building: a purged ticket's children become roots, and a block that waited on
it waits on its key as an external reference, an act on that ticket *(accepted by the owner
2026-10-06, and stated in D2)*. D1's "search" is covered by
the same filter — the `q` filter of the lists and the search of
[ADR 0025](0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md) D5, every ticket
it reads —, and the person-level stream carries the deletion and the restoration across the
person's tenants, on which the person-level pages and the inbox's count read their lists again; the
dashboard of D1's "dashboard tile" is not built. D4's deletion of a project and D6 are not built
*(2026-10-10: but D6's end of the relations into other teams, a function of the data layer that no
route calls, D6)*. *(2026-10-06,
made concrete by the implementer with the import of
[ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md), open to the
owner's objection:)* the purge of a ticket an import created also takes the ticket's file out of the
report of its import job, which keeps every file's title, threat, note and questions for good and
would keep what the purge removes; the report's summary still counts it, and the purge's act counts
`import_report` (`forgetImportedFile` in [`store/deletion.go`](../../backend/internal/store/deletion.go)).

**Partly built** (phase 3, 2026-10-03): D5 for local accounts — a tenant's administrator
deactivates an account their tenant manages (`PUT …/accounts/{username}/deactivation`), and the
start-up synchronisation deactivates the local administrator when its variables are emptied: the
person cannot log in, their sessions end, their tokens are revoked, and the person and every
act stay. *(Built 2026-10-04:)* a tenant administrator's deactivation is refused with
`409 last_admin` when it would leave the managing tenant without an administrator who can log in,
under that tenant's lock ([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D1); the other tenants the person administers are not asked. Not built: marking the memberships
inactive — they stay as they were, and a deactivated person is refused at the resolver everywhere
—, a route that reactivates (only the start-up
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
*(Added 2026-10-06, decided by the implementer in building and accepted by the owner that day:)* a
purged ticket's children become roots, and a ticket blocked on it waits on its key as an external
reference from then on, an act recorded on that ticket. *(Made concrete 2026-10-10 on the
recommendation, open to the owner's objection, ~~not built~~ built the same day:)* the same holds across teams
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3): its children in another team become roots and its links into another team go, each
change recorded in the audit record of the team it changes. *(Built through
`end_relations_elsewhere`, a crossing of
[ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D7, inside the purge
of a deleted ticket only: a child of another team becomes a root, its version unchanged, with an
`updated` act whose reason is "the parent was purged" ~~and which names no ticket~~ *(after the
security review: which names the purged ticket in its refs alone, no id of it in its payload)*; a
link to or from the ticket goes, with an `unlinked` act on the other end, read from its side; the
actor is the purge's, the administrator or `system:ticket-purge` *(after the security review:
`system:ticket-purge` also for an administrator who holds no role in the other team, ADR 0026 D1)*. A block of another team never waits on the
ticket, since a block names a ticket of its own team.)* *(Made concrete 2026-10-10 by the
implementer after the security re-check, open to the owner's objection:)* a write of another team
that would record an act on a ticket while the ticket is being purged — the removal of a link to it,
a child leaving it, the close of a ticket it waits on — records none: the act waited on the purge's
lock of the ticket while the purge waited on the row the write held, a child or a link the purge
ends, and one of the two failed as a deadlock. The ticket is gone when the purge commits; a purge
that fails after it locked the ticket leaves it in the bin without the act
([docs/developer/data-access.md](../developer/data-access.md#crossings-between-teams)).

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
administrator and the time. The slug is not reused. *(2026-10-10, ~~not built~~ built the same day
as a function of the data layer that no route calls, while the deletion of a team is not built:)*
Every relation from another team into it ends as D2's purge ends it, recorded in that other team's
audit record. *(`DB.EndTeamRelations`, a job transaction named `team-deletion` over
`end_team_relations`: the children in other teams of the team's tickets become roots, the team's
tickets whose parent is elsewhere become roots, the links between the team and the others go in
both directions, each change in another team recorded there as `system:team-deletion`, and the
parents elsewhere that counted the team's tickets derive their progress again; the integration tier
calls it at the store.)*

**D7 — Who may, and what warns.** Delete, restore and purge are tenant-administrator acts;
tenant deletion is a global administrator's. An agent never deletes anything. Deleting a
ticket that other open tickets depend on ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)
D6) shows the dependents and asks for confirmation; it does not refuse. Restoring is a
recorded act. *(Amended 2026-10-05, built on the recommendation, the owner reviewing the result:
the purge takes a person in a browser session. A token — an administrator's `admin`-scope token
included — answers `403 session_required` on `DELETE …/deleted-tickets/{key}` before anything is
looked up, and a session the agent header marks is refused with `403 agent_forbidden`, as on every
operation that takes a session only. Deleting and restoring stay open to a tenant administrator's
`admin`-scope token: the bin undoes a deletion for thirty days, and a restoration brings back only
what was there. The purge is the one act on a ticket nothing undoes — the ticket, its texts and its
files gone, its audit rows emptied —, so what a leaked token did there would outlive the token's
revocation, which is the rule by which [ADR 0035](0035-personal-access-tokens.md) D5 keeps an act
to a session. The options weighed: (a) the purge in a session only — chosen; (b) all three acts
open to an `admin`-scope token, as first built, which leaves a leaked administrator's token able to
destroy a ticket for good; (c) deleting and purging both in a session only, which takes from an
administrator's script a deletion the bin can undo and closes nothing (a) leaves open.)*

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
