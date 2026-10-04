# ADR 0074: The Question Catalog Is Consumed; a Plan Phase Becomes Tickets When It Starts, in a Session of Its Own; the Plan and the Workflow Document Stay Until Each Part Is Built

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the last question of the
catalog, "how does it go on?": the catalog is closed now, the conversion of the starting
phase into tickets happens in a new session, the remaining plan phases and the workflow plan
stay until they start or are built. The owner set the session boundary; the rest follows
[ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
D10.

**Implemented** in the change that wrote this record: `docs/planning/questions.md` is a
tombstone that says where its content went; `project-plan.md` marks phase 1 as done and
phase 2 as the next, to be ticketed in a dedicated session; ~~`vscode-workflow.md` stays~~.

**D3 fulfilled 2026-10-04.** Phase 5 built the workflow, and every step and building block of
`vscode-workflow.md` was checked against [docs/operations/claude-code.md](../operations/claude-code.md),
[docs/developer/mcp.md](../developer/mcp.md) and the records: the session start, the binding by
remote and its override, the tools with their capabilities, the commit strings, the hooks, the
skills, the `CLAUDE.md` block and the finish with its extraction reminder are built and described
there ([ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)–[ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md),
[ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)–[ADR 0069](0069-rules-stay-in-git-work-moves-to-cowork.md));
the person-level views of its morning are [ADR 0018](0018-the-views-of-the-first-release.md) D3,
not built; its cut-over of the other repositories moved into phase 6 of
[the project plan](../planning/project-plan.md); its evening report across all tenants was never
decided — [ADR 0018](0018-the-views-of-the-first-release.md) D8 leaves a view across tenants
beyond D3 out of the first release — and its optional VS Code tasks file was never built. The file
is deleted. **D4 not yet:** phases 6 and 7 have not started, so `docs/planning/` stays with the
plan and the tombstone.

## Context

Between 2026-09-29 and 2026-10-01 the owner answered every question of the planning
catalog, one per turn, and each answer became a record: ADR 0004 to ADR 0073 (with
amendments in place where a later answer moved an earlier one). ADR 0002 D10 says a planning
document is consumed, never maintained: a question becomes an ADR and leaves, a phase that
starts becomes tickets, a workflow step that is built becomes a page. Seventy records refer
to "the catalog" in their Status sections; deleting the file would leave those sentences
pointing at nothing, while keeping it as a working document would be the second truth D10
forbids.

## Decision

**D1 — `docs/planning/questions.md` is a tombstone, not a document.** It states that every
question became a record, points at the ADR index, and says it is deleted with the directory.
No question is added to it; a new open decision is a ticket's `## Open questions` section
(the tickets page) and, when a backlog exists, a question entity in cowork.

**D2 — A phase of the plan becomes tickets when it starts, in a session dedicated to that
conversion.** Phase 2, "Core domain and API", is next; its conversion into a family ticket
with children per aggregate, in the order the records imply, is the first thing the next
session does — after the pipeline ticket of [ADR 0061](0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md)
D5, which is ticket one. Phases 3 to 7 stay in `project-plan.md` until each starts.

**D3 — `vscode-workflow.md` stays until phase 5 writes its steps into operations and
developer pages;** every mechanism it describes is now decided (ADR 0040–0043, 0066–0071),
and the page is the plan of record for building them. *(Fulfilled 2026-10-04: the steps are
in the operations and developer pages, and the file is deleted — Status above.)*

**D4 — `docs/planning/` is deleted when the plan's last phase has started and the workflow
document has been written into its pages,** as ADR 0002 D10 says.

**D5 — The decisions of this round are committed as one change** on a branch, with a
conventional commit (`docs:` scope), on the owner's word; this record does not commit.

## Consequences

- The catalog's seventy-odd cross-references keep a target that explains itself.
- The next session starts with two concrete deliverables and no decision to take: the
  pipeline ticket and the phase-2 family ticket.
- The plan shrinks phase by phase instead of being rewritten; the workflow document is
  consumed by the phase that builds it.

## Alternatives Considered

- **Convert the whole plan into tickets now and delete `docs/planning/`.** A clean cut;
  tickets for phases whose content will change before they start, which the ticket rules
  ("a ticket shows the current state") do not fit. Lost.
- **Convert phase 2 in this session.** The owner drew the session boundary. Lost.
- **Delete `questions.md` outright.** Seventy dangling sentences. Lost to the tombstone.

## Residual risks

- A tombstone is a file in a directory that D10 wants gone; D4 names the moment it goes.

## References

- [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D10 — planning documents are transitional
- [docs/adr/README.md](README.md) — the index every question became part of
- [docs/planning/project-plan.md](../planning/project-plan.md) — what stays, and until when;
  the workflow document it named is consumed into
  [docs/operations/claude-code.md](../operations/claude-code.md) and [docs/developer/mcp.md](../developer/mcp.md)
- [docs/tickets/README.md](../tickets/README.md) — where a new open question lives
