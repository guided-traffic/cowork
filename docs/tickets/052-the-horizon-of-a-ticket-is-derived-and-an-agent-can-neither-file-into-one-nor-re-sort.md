---
id: T52
title: the horizon of a ticket is derived from its state, and an agent can neither file a ticket into one nor re-sort the backlog
state: in-progress
severity: high
security: none
threat:
urgency: next         # rule 3: severity high, trigger live — an agent asked to file into next cannot, and sends the person to a state instead
effort: M
blocked-by:
filed-from: the owner's report of 2026-10-04
opened: 2026-10-04
decided: 2026-10-04
done:
---

## Current state

The five values of a ticket's `urgency` — `now`, `release`, `next`, `later`, `icebox` — are, by
the owner's answer of 2026-10-04, the ticket's **horizon**: a planning category a person or an
agent sets, in whatever state the ticket is, and nothing else changes
([ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3 as amended
2026-10-04). The owner defined `now`, `next` and `later`, chose the same model for `release` and
`icebox`, and asked for a word for the five; *horizon* is the proposal.

What is built does not match:

- **Rule set v1 still derives.** A blocked ticket without a set horizon derives `release` or
  `icebox` ([`DeriveUrgency`](../../backend/internal/domain/ticket.go)), so it leaves the group it
  stood in; what a person sets is an "override" beside a derived value.
- **An agent cannot file into a horizon.** `file_ticket` takes none and files into `later`; asked
  to file a ticket into `next`, an agent told the owner to move the ticket to the state `analysed`.
- **An agent cannot re-sort the backlog.** No tool moves a ticket in the rank; only the `api`
  hatch reaches `PUT …/tickets/{number}/rank`.
- **The UI names the field and its values wrongly.** The tooltips of the five values are the
  urgency rule table of this repository's ticket page, not cowork's meanings; the ticket page
  shows the rule and "overridden".
- **A project opens on its backlog**, and the Board tab stands right of the Backlog tab; the
  owner wants the board first ([ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1 as
  amended 2026-10-04).

The backlog's drag within and between the groups, which sets the horizon and the place in one
gesture, is built and stays.

## Required changes

1. **Rule set v2** (ADR 0010 D3): `DeriveUrgency` answers `later` (`v2:default`) for every
   ticket. Migration 29 keeps what every ticket shows: a ticket that derived `release` or
   `icebox` under v1 and has no set horizon holds that value as its set horizon, set by nobody,
   with a reason that names the migration; every ticket's derived value becomes `later`.
2. **Filing into a horizon, at a place** ([ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md)
   D2 as amended 2026-10-04): `TicketCreate` takes an optional `urgency` and an optional `after`
   or `before`, the number of an open ticket of that horizon in the project. Without a horizon
   the ticket is `later`; without a place it joins at the bottom of the rank, the end of its
   horizon. An agent needs `override-urgency` for a horizon other than `later` and `rank` for a
   place ([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
   D4). The filing act records both.
3. **The tools** ([ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D1):
   `file_ticket(…, horizon?, after?, before?)`; `place_ticket(key, horizon?, after?, before?,
   reason?)` replaces `set_urgency` — a horizon, a place in it, or both, in one call; every
   description says what a horizon means and that it is independent of the state. The chat's
   instructions, if they name the urgency.
4. **The UI:** a project opens on its board, the Board tab left of the Backlog tab, every link
   that opens a project goes to the board; the word *horizon* and the five meanings of ADR 0010
   D3; no rule, no "derived", no "overridden".
5. **Docs in the same change:** the README reference (`TicketCreate`, the tools, the urgency
   routes' descriptions), [docs/developer/mcp.md](../developer/mcp.md),
   [docs/developer/chat.md](../developer/chat.md), [docs/developer/frontend.md](../developer/frontend.md),
   the Status of ADR 0010, 0014, 0018, 0042 and 0043.
6. **Tests:** the domain's rule set; filing with a horizon and a place, the neighbour in another
   horizon refused, the agent without `override-urgency` or `rank` refused, the migration's
   carry-over (integration tier); the two tools against the fake API and through the MCP server;
   the frontend's specs for the route, the tabs and the words.

## Open questions

**Q1 — Do the identifiers follow the word?** The API says `urgency`, `urgency_derived`,
`urgency_rule`, `urgency_override` and `…/urgency-override`; the capability is `override-urgency`
(stored on every agent token and in `chat_capabilities`); the database has the enum `urgency`
and the columns `urgency_*`. Options:

- (a) **Leave them.** *Horizon* in the UI, the documentation and the tools' arguments; the API
  and the database keep their names, the README maps them. No contract change; an agent that
  reads the API document meets "urgency override" for what the UI calls the horizon.
- (b) **Rename the API, keep the database** — recommended. `horizon` and `PUT …/horizon` beside
  the old names for one release (expand), the old names removed in the next (contract), the
  capability renamed with a migration that rewrites the stored sets; the columns stay, mapped in
  the store. Every surface an agent reads says horizon; a `cowork-mcp` of the previous release
  keeps working through the release that carries both.
- (c) **Rename everything**, the columns and the enum type included, over two releases
  ([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)). The
  cleanest tree, two releases of dual writes for names nobody outside the code reads.
