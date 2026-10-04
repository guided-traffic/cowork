# ADR 0069: Rules Stay in Git, Work Moves to cowork — ADRs, Developer, Operations and Security Pages Beside the Code; Tickets, Questions and Plans in the Backlog

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "what
stays in git?": the hard separation by kind of statement, over moving ADRs into cowork, over
keeping plans in the repository, and over a per-repository `docs/COWORK.md` page (the
recommendation's addition, not taken). The rules of D4–D6 were put to the owner with the
question and not objected to.

**D4 and D5 built** (phase 5, 2026-10-04): the three-line template is in
[docs/operations/claude-code.md](../operations/claude-code.md), and `finish_work`'s answer
carries the extraction question. D6, this repository's own cut-over, is phase 6.

## Context

[ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
gives every statement one home: decisions in ADRs, how-it-works in developer pages, running
in operations pages, the threat model in security pages, work in tickets; D10 made the
planning documents transitional. [ADR 0064](0064-one-direction-import-and-export-no-synchronisation.md)
made cowork the source after an import and left the files' fate to each repository. The line
to draw for every bound repository is the one ADR 0002 already draws: what is a rule lives
beside the code it governs and is reviewed with it; what is work lives in the backlog. ADRs
in cowork would decouple a decision from the pull request that changes the behaviour it
describes and turn code-comment links into URLs of one installation; plans in the repository
would be the board beside the tickets again.

## Decision

**D1 — Rules stay in the repository:** `docs/adr/`, `docs/developer/`, `docs/operations/`,
`docs/security/`, `README.md`, `SECURITY.md`, `CLAUDE.md`. They are
versioned, reviewed and changed in the same pull request as the code they describe.

**D2 — Work moves to cowork:** tickets, their open questions, findings, plans and phases
(phases become parent tickets, [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md)
D3; questions become question entities, [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D2), and this repository's own question catalog once it is empty.

**D3 — The citation rule holds across the boundary.** Nothing in a repository cites a
ticket — not a file, not a key, not a URL into cowork ([ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
D8); a ticket cites ADRs by repository link. A security finding's extracted rule names the
gap in `docs/security/`, never the ticket.

**D4 — The bound repository's `CLAUDE.md` carries three lines,** from a template in this
repository's operations page: the work lives in cowork under `<tenant>/<KEY>`; decisions and
documentation live here under the five homes; commits follow [ADR 0068](0068-commits-carry-a-component-scope-the-short-key-in-the-subject-and-the-full-key-in-a-trailer.md).
No further page is added to a repository for cowork's sake.

**D5 — `finish_work` reminds of the extraction.** Its return text asks whether the ticket
holds a decision that must become an ADR, a behaviour that must reach an operations or
developer page, or a gap for a security page — the close of [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
D3, performed by the agent in the same session, in the repository. It is text, not a required
field.

**D6 — This repository's own cut-over** (phase 6, first of all): `docs/tickets/README.md` is
replaced by one line — "tickets live in cowork since <date>; `archive/` is history" — the
archive stays unless the owner imports it ([ADR 0063](0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md)
D1), and `docs/planning/` is deleted when its last question is an ADR and its plan is
tickets ([ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
D10).

## Consequences

- The contract between a repository and cowork is one sentence: rules here, work there.
- Code review keeps reviewing ADRs with the code; a decision changes where the behaviour
  changes.
- The extraction at a ticket's close spans two systems — the backlog and the repository —
  and D5 makes the agent carry it in one session.
- No `docs/COWORK.md`: a person reading a bound repository learns where the work went from
  `CLAUDE.md`'s three lines and from the absence of `docs/tickets/`.

## Alternatives Considered

- **ADRs into cowork as `decision` tickets.** One source; decisions decoupled from the pull
  request, code-comment links pointing at an installation, a repository without cowork access
  losing its reasons. Lost.
- **Plans stay in the repository.** The board beside the tickets, which ADR 0002 D10 retired.
  Lost.
- **A `docs/COWORK.md` page per bound repository** — the recommendation's addition. One page
  for people; the owner preferred the three lines in `CLAUDE.md` and no further file. Lost.

## Residual risks

- D3 across two systems has no automatic check; `git grep` for keys and installation URLs in
  a repository is the manual one, as it was for ticket labels.
- D4's three lines are a template; a repository that forgets them has an unbound session,
  which the hook of [ADR 0067](0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md)
  reports rather than hides.

## References

- [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D1, D3, D8, D10 — the homes, the extraction, the citation rule, the transitional plans
- [ADR 0064](0064-one-direction-import-and-export-no-synchronisation.md) — cowork as the source after import
- [ADR 0068](0068-commits-carry-a-component-scope-the-short-key-in-the-subject-and-the-full-key-in-a-trailer.md), [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) — the convention and the tool that reminds
- [docs/planning/project-plan.md](../planning/project-plan.md) — phase 6
