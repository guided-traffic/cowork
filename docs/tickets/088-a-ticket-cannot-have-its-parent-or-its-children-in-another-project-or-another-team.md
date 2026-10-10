---
id: T88
title: a ticket cannot have its parent or its children in another project or another team
state: decided
severity: medium      # the owner's teams file work for each other; today that is two unlinked tickets
security: boundary
threat: once built, a person who sees a ticket of team B (any role, and their agents' tokens) reads the head of its parent, child or link end in team A, in which they hold no role, and the readers of A's parent read B's children's progress in its aggregate — the owner's accepted trade-off, bounded by who may set a relation (only a person who can read both ends) and by what a head holds; nothing crosses a tenant before it is built
urgency: next         # rule 3: severity medium, and the trigger is live — the owner starts using cowork with several teams
effort: L
filed-from: the owners walk through the UI under make dev, 2026-10-10
opened: 2026-10-10
decided: 2026-10-10
done:
---

## Current state

The owner decided on 2026-10-10 that an installation is an organisation's and a tenant is a team
inside it, and that a ticket's parent, children and links may cross projects and teams (Q1–Q6). The
ADRs state the rules as amended that day, each marked not built:
[ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1, D3,
[ADR 0008](../adr/0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D2,
[ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D2, D4, D6, D7,
[ADR 0017](../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D3, [ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D7 and
[ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D5; made concrete on the recommendation, open to the owner's objection:
[ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2, D6,
[ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D4 and [ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
D9. The code follows the rules before:

- **The parent is bound to the project in the schema:** `FOREIGN KEY (tenant_id, project_id,
  parent_id)` ([000008_tickets.up.sql:78](../../backend/internal/store/migrations/000008_tickets.up.sql#L78));
  the API names the parent by its number alone, `parent_number`
  ([tickets.sql:17](../../backend/internal/store/queries/read/tickets.sql#L17)); the parent cycle is
  refused by a walk under a per-project lock; the browser offers the project's open tickets as parents.
- **Links stay inside the tenant:** refused by the API and by the schema's composite keys; the
  `blocks` cycle is refused by a walk over the tenant's graph under a per-tenant lock; the
  prerequisite tree is a recursive query over one tenant's edges.
- **Every read and write runs under one tenant's `app.tenant_id`**, forced row-level security on
  every table (ADR 0021 D1, D3): a ticket of another tenant cannot be read in the child's
  transaction, nor its row written.
- **The derived progress** is maintained in the transaction of every change, up the ancestors, by
  `RefreshDerivedProgress`
  ([tickets.sql:139-168](../../backend/internal/store/queries/write/tickets.sql#L139-L168)).
- **A confidential ticket** is visible to the tenant's administrators, its assignee and its reporter
  only (`app_ticket_visible`,
  [000008_tickets.up.sql:106-115](../../backend/internal/store/migrations/000008_tickets.up.sql#L106-L115));
  every other surface behaves as if it did not exist, the prerequisite tree with no placeholder
  (ADR 0065 D5).
- **Export and import:** the export writes a parent as its canonical key already
  ([export.go:84-85](../../backend/internal/api/export.go#L84-L85), `domain.FullKey`); the importer
  resolves a parent within the project and reports an unresolved one as not set
  ([refs.go:194-205](../../backend/internal/importer/refs.go#L194-L205)), and makes an export's link
  where both ends resolve, reported and omitted where one does not
  ([refs.go:286-300](../../backend/internal/importer/refs.go#L286-L300)).
- **A purge** makes the purged ticket's children roots
  ([deletion.sql:114-119](../../backend/internal/store/queries/write/deletion.sql#L114-L119)).
- **The backlog** already names a parent that is not in the group beside the child
  ([backlog-model.ts:19](../../frontend/src/app/features/project/backlog-model.ts#L19)).

Impact: two teams of one installation — Sutorbank and Guided-Traffic — cannot say that a ticket of one
needs a change in the other; today that is two tickets nothing relates.

## Required changes

The target is the ADRs as amended; what the build has to do, in the order the work depends on:

### The data layer

1. A migration widens the parent's foreign key to the ticket alone, any tenant of the installation
   (a widening, which ADR 0028 D3 allows), and the links' keys likewise.
2. The two crossings of ADR 0021 D7, and nothing more: **the head read** — for a ticket a relation of
   the caller's ticket names, its team's name, key, title, type and state, or the confidential
   placeholder when the caller may not see it (ADR 0065 D1's rule, read for the caller in the other
   team; a project restricted from the caller counts as not a member, ADR 0034 D4; the placeholder
   inside the caller's own team likewise, ADR 0065 D5) — and **the
   derived-progress write** of ADR 0017 D3 into a parent of another team, those columns only, no
   version, no act. The build chooses the mechanism among ADR 0021 D7's and names it there.
3. The parent cycle walk and the `blocks` cycle walk cross teams under a lock that spans the teams
   involved, taken in a fixed order so that two writers never wait on each other.
4. A purge, and a deleted tenant, end every relation into another team, recorded in that team's
   audit record (ADR 0024 D2, D6).

### The API and `cowork-mcp`

1. A filing, the ticket's update and the links take the other end's canonical key (ADR 0007); a key
   the person cannot read answers exactly as a key that does not exist — the same status, code and
   body — so that trying keys tells nothing (ADR 0008 D2).
2. Setting a parent or a link takes a `member` or `admin` of the child's, or the source's, team who can
   read the other end; removing it, a write on the child or the source alone.
3. A ticket's body names its parent, children and link ends by key and, across a team the reader holds
   no role in, by the head; the prerequisite tree likewise (ADR 0012 D6); `done` is refused over an
   open prerequisite in another team whose state the closer reads (D7).
4. The tools of `cowork-mcp` take and show the same; an agent's token reads what its person reads.
5. Export and import as ADR 0051 D9 states it.

### The UI

1. The parent chooser, and the link dialog, offer the tickets the person can read across their teams.
2. The detail page, the backlog and the board show a parent, a child or a link end of another team by
   its team and head, unlinked where the person cannot open it, and `<team> [Confidential]` where they
   may not see it — in another team or in their own (Q6); in the prerequisite tree such a node is the
   placeholder and its subtree stays absent.
3. The parent's derived progress counts the children in other teams; nothing in the page says more
   about them than their heads.

### The documentation

- [docs/security/tenancy.md](../security/tenancy.md): the crossing as a mechanism, and in its
  `## What this does not cover` the gap with a stable `H-<n>` — team A's heads before team B's
  readers, A's aggregate progress revealing B's — named as the owner's accepted trade-off.
- [docs/developer/](../developer/README.md): the pages of the store and the API that describe the
  parent and the links; [README.md](../../README.md)'s API reference for the keys the routes take.
- The ADRs' Status sections move from "not built" to built in the change that builds each rule.

### The tests that prove it

The integration tier, with two teams and identities that hold a role in one, both or neither:

- a reader of team B's ticket reads team A's parent's head and nothing more — every other field
  absent from the body —, and `<team> [Confidential]` for a confidential one; a member of team A who
  may not see a confidential parent in A reads the same placeholder, and the lists, the board, the
  search and the counts still leave the confidential ticket out;
- a key of team A that the person cannot read is refused as a missing key, byte for byte;
- a viewer of team A who is a member of B sets A's ticket as the parent of B's; a member of B alone
  cannot;
- a parent's derived progress counts a child of another team, its version unchanged;
- a parent cycle and a `blocks` cycle across teams are refused; two writers that cross the same two
  teams in opposite directions both finish;
- `done` is refused over an open prerequisite in another team;
- a project restricted from a member shows its ticket's head at the end of a relation;
- the export writes the key of a parent in another team and no head; the import sets it for a person
  who can read it and reports it as not set for one who cannot;
- a purge and a deleted tenant leave the other team's children as roots, recorded in its audit record.

The end-to-end tier: the parent chooser across teams and the head on the detail page, with two
identities.

The word for a tenant — "team" in the UI and the API — is T89's.

## Open questions

### Q1: How far may a parent reach?

(a) across projects inside the tenant, ADR 0005 D3 untouched; (b) across tenants as well; (c) a
person-level grouping across tenants, visible to its owner only. Recommended: (a), and (c) for a
need across tenants — a tenant was a client, and (b) puts one client's titles before another.

**Answer:** (b), 2026-10-10 — an installation is rolled out per organisation, and a tenant is a team
listing the team's projects (Sutorbank, Guided-Traffic); a Sutorbank ticket can need a change in
Guided-Traffic. A person outside the other ticket's tenant sees the link and reads its head, and
cannot open the ticket until taken into that tenant, at least as a viewer. A confidential ticket
shows as `<tenant> [Confidential]` — the flag's own name, not "Embargo", which would need explaining —
its title unreadable without the permission.

### Q2: Who may make a ticket of another team a ticket's parent?

Ticket numbers are a sequence per project
([ADR 0022](../adr/0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md)), so keys are easy to
guess: if any key of the installation were accepted and its head shown back, a member of team B
could read team A's titles by trying `SUT-1`, `SUT-2`, … (a) a writer of the child's tenant who can
also read the parent; (b) a writer of the child's tenant alone, any key; (c) a writer of both tenants.
Recommended: (a) — nobody learns a title by making the link.

**Answer:** (a), 2026-10-10.

### Q3: What does a ticket's head hold for a person outside its tenant?

(a) the team's name, the key, the title, the type and the state; (b) the team's name, the key and the
title; (c) (a) and the assignee, the progress, the severity and the urgency. Recommended: (a) — what
the work is and whether it is done, which is what the other team waits on.

**Answer:** (a), 2026-10-10.

### Q4: Does a parent's derived progress count its children in another team?

(a) every child counts, through a narrow write of the parent's derived columns; (b) the children of
the parent's own tenant count, the others are listed by their heads and named as excluded; (c) only
the children of the parent's own project count. Recommended: (b) — the crossing stays read-only and
holds to Q3.

**Answer:** (a), 2026-10-10 — the other team's progress readable in the aggregate is accepted.

### Q5: Do the links cross teams as well?

(a) all four types of ADR 0012 D1, under the parent's rules; (b) none, only the parent crosses; (c)
`blocks` only. Recommended: (a) — "a Sutorbank requirement needs a change in Guided-Traffic" is what
`blocks` means, and one rule for every relation is the simpler one to hold.

**Answer:** (a), 2026-10-10.

### Q6: Does the confidential placeholder stand inside a team as well?

ADR 0065 D5 makes every surface behave as if a confidential ticket did not exist for a person who may
not see it — the prerequisite tree included, "with no placeholder" — so today a member of team A whose
ticket has a confidential parent in A sees no parent at all. Across teams the owner decided the
placeholder `<team> [Confidential]` (Q1). Left as it is, a member of team B learns that a confidential
ticket of A is the parent of B's ticket, while a member of A, who holds a role there, learns nothing
of the same relation inside A.

- **(a) Recommended:** the placeholder stands at the other end of every relation — parent, child,
  link end — that the reader may not see, inside a team as across teams, and nowhere else: lists,
  boards, search, counts, the inbox and the stream keep D5, and in the prerequisite tree the node
  shows as the placeholder, its subtree still absent. One rule, and a member is never shown less than
  an outsider; what it reveals inside the team is that a related ticket exists and is confidential,
  which the related ticket's own reader is the one to know.
- **(b)** the placeholder across teams only, D5 untouched inside a team: the least change to D5, at
  the price of the outsider knowing more than the member.
- **(c)** no placeholder anywhere, across teams included: D5 holds everywhere, and the owner's answer
  to Q1 is taken back — a child in team B looks parentless.

**Answer:** (a), 2026-10-10.
