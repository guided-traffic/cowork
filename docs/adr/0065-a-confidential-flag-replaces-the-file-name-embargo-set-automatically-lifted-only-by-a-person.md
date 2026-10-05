# ADR 0065: A `confidential` Flag Replaces the File-Name Embargo — Set Automatically by the Security Class, Lifted Only by a Person's Recorded Act, Enforced Where the Project Restriction Is

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"embargoed tickets?": a per-ticket flag with a narrow circle, over a dedicated restricted
project, over a per-ticket person list, and over encryption of the body. The rules of D6–D9
were put to the owner with the question and not objected to.

**Partly built** (phase 2, 2026-10-02): D1–D6, D8 and D9 — the `confidential` column, the
predicate `app_ticket_visible` in every ticket query (a unit test holds the queries to it),
the flag set on creation and on a change to `live` or `boundary`, the administrator's act to
set or lift it (lifting with a reason, never an agent's), and the security page on tenancy.
D7 arrives with the importer. *(2026-10-04.)* The detail page offers a tenant administrator to
set the flag, with a reason they may give, and to lift it, only with one. *(2026-10-05.)* D5's
dashboard tiles exist ([ADR 0018](0018-the-views-of-the-first-release.md) D6): every tile counts,
names and measures only the tickets the reader sees, the open `live` count among them, and the
integration tier reads each tile with a confidential ticket the reader cannot see.

## Context

In the Markdown backlogs an open security finding is kept out of the repository by a
`local_` file-name prefix and an ignore rule, and the embargo ends at the fix, not at the
ticket's close; a dropped or risk-accepted finding is published only on the owner's explicit,
dated acceptance ([ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
D7, [docs/tickets/README.md](../tickets/README.md)). cowork has no "untracked"; the
security class is a column ([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md)),
so the condition is machine-readable; the project restriction of [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D3–D4 already enforces a visibility predicate in the data layer across every view, union,
search, event and notification. A finding belongs to the project it was found in; moving it
to a restricted side project would tear its context — `found-in`, rank, board — apart.

## Decision

**D1 — A ticket has a boolean `confidential`.** While it is set, the ticket is visible to the
tenant's administrators, its assignee and its reporter, and to nobody else — not to other
members, not to a global administrator without a role in the tenant ([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D2). Its comments, questions, attachments, links, interest rows and time entries inherit the
flag's visibility.

**D2 — The flag is set automatically when `security` becomes `live` or `boundary`,** at
creation or by a later change, and may be set by a tenant administrator by hand for any
ticket (the HR case). *(Made concrete 2026-10-02: "becomes" is a change of the class into one
of the two — a later edit of a ticket whose class stays `live` does not set again a flag an
administrator lifted; the setting is an act of its own, `confidential_set`, with the class as
its reason.)*

**D3 — The flag is never lifted automatically.** Changing `security` back to `hardening` or
`none`, reaching `done`, reaching `dropped` — none of these lifts it. Only a tenant
administrator's explicit act, with a reason, lifts it; that act is the successor of the
`publication-accepted:` date and is recorded ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)).

**D4 — Enforcement is the data layer's, in the same place as the project restriction.** The
wrapper of [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D2 carries the person; every generated query on tickets and their children carries
`not confidential OR person is admin OR person is assignee OR person is reporter` beside the
project predicate of ADR 0034 D4. There is no code path to a confidential ticket that does
not pass it.

**D5 — Every surface behaves as if the ticket did not exist for the unauthorised:** lists,
boards, dashboard tiles (the `security: live` count excludes what the viewer may not see),
search ([ADR 0025](0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md)),
the person-level unions, the prerequisite tree ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)
D6: a confidential node and its subtree are absent, with no placeholder), notifications
([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md): no inbox line reaches the
unauthorised), the event stream ([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D3: filtered like the project restriction), the export ([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
D4: omitted, with a count in the manifest "n confidential tickets not included"), the API
and the MCP tools (`404`, [ADR 0023](0023-the-tenant-is-in-the-path.md) D5's reading).

**D6 — No agent sets or lifts the flag directly.** Both are administration acts on the
hard-off list of [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D3; an agent that files a ticket with `security: live` sets it indirectly through D2, which
is intended.

**D7 — The importer applies the rule of the source.** A `local_` prefix, or `security:
live|boundary` without a `shipped:` line, sets the flag; a `publication-accepted:` date leaves
it unset and the report says so ([ADR 0063](0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md)
D3).

**D8 — The security page of tickets carries this chapter,** and names as an open gap with an
`H-<n>` identifier that a confidential body is readable by anyone with database or backup
access: encryption at rest of confidential bodies is a different threat model and is not
built.

**D9 — Assigning a confidential ticket to a person is what admits them to it.** The assignee
sees it from the moment of assignment, which is the way to bring in a reviewer; a per-ticket
person list beyond assignee and reporter is an amendment when a case needs more than one.
*(Made concrete 2026-10-02: an assignee must be a member who can see the ticket's project; a
reassignment drops the former assignee from the circle at once — the answer to that write
still shows its writer the ticket as written. An agent may reassign a confidential ticket
until the agent gates are reviewed after experience.)*

## Consequences

- The embargo becomes a database predicate instead of a file trick, with the same two rules
  the owner lives by: it closes by itself, it opens only by a person.
- Every list and tile the project restriction already guards gains a second predicate at
  the same place; the three-person test of ADR 0034 becomes a four-person test (administrator,
  assignee, member, outsider).
- Numbers that could leak existence — dashboard counts, prerequisite counts, export
  manifests — are computed per viewer, not once.
- A fixed finding stays confidential until someone says it may be read; that is the owner's
  rule and the cost is one click per finding.

## Alternatives Considered

- **A restricted project for security findings.** No new predicate; findings torn from the
  project they belong to, and an HR ticket in an ordinary project unprotected. Lost.
- **A per-ticket person list in addition to the flag.** An external reviewer could be named;
  assignment covers that case today. Deferred by D9.
- **Encryption of confidential bodies at rest.** Protects against database and backup
  readers; key management, no full-text search, unreadable audit diffs — a different threat
  model. Not built; named in D8.

## Residual risks

- D8 by name: database and backup access reads confidential bodies.
- D5's per-viewer counts cost a predicate on aggregates; at the expected sizes that is
  nothing, and it is the only honest way to count.

## References

- [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D7, [docs/tickets/README.md](../tickets/README.md) — the embargo this replaces
- [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D3, D4 — the predicate this joins
- [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) — the `security` column that sets it
- [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md) D3 — the hard-off list
- [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D6, [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md), [ADR 0025](0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md), [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4, [ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md) D3 — the surfaces D5 binds
