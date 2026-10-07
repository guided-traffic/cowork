---
id: T55
title: phase 6 (import and cut-over) — the tickets of the owner's repositories do not live in cowork yet
state: in-progress
severity: high
security: none
threat:
urgency: next         # rule 3: severity high, live — every repository still keeps its backlog in Markdown files
effort: M
blocked-by:
filed-from: phase 6 of the project plan, converted by ADR 0074 D2
opened: 2026-10-06
decided: 2026-10-06
done:
---

## Current state

The family ticket of phase 6
([ADR 0074](../adr/0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D2). **Goal:** the sibling project's open tickets live in cowork, and its `docs/tickets/` is
retired. The phase started on 2026-10-06, on the owner's word to build every remaining phase of the
plan in one night, with every question that comes up filed as a ticket for the owner to answer the
next day; this ticket consumed the phase's section of the project plan.

The decisions the phase builds were all taken before it started:
[ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) (the
import job, its dry run and correctable report, the export as its mirror),
[ADR 0063](../adr/0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md)
(what the importer handles and how a file maps),
[ADR 0064](../adr/0064-one-direction-import-and-export-no-synchronisation.md) (one direction, a
repeated import is a conflict),
[ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D7 (the importer applies the rule of the source),
[ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
(a repository's binding), [ADR 0069](../adr/0069-rules-stay-in-git-work-moves-to-cowork.md) D4–D6
(what a bound repository keeps, and this repository's own cut-over) and
[ADR 0070](../adr/0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md) D2 and D5
(`cowork-mcp export`). The confidential flag the plan named is built since phase 2; what the import
adds to it is ADR 0065 D7.

Children:

- T57 — the importer, the dry-run report, the project and the tenant export, `cowork-mcp export`,
  and the UI's import page and export button.

## Required changes

1. **T57.**
2. **The owner's cut-over**, which only the owner can run, against his installation, in the order
   the plan had it — none of it is code of this repository, and nothing here touches another
   repository:
   - the sibling project's open tickets imported into its project: a dry run, the report read and
     corrected, the execution, the count verified; that repository's own decision on its
     `docs/tickets/` is taken there
     ([ADR 0064](../adr/0064-one-direction-import-and-export-no-synchronisation.md) D4);
   - this repository's own tickets imported, then its cut-over of
     [ADR 0069](../adr/0069-rules-stay-in-git-work-moves-to-cowork.md) D6: `docs/tickets/README.md`
     becomes the one line "tickets live in cowork since <date>; `archive/` is history", and the
     archive stays unless the owner imports it;
   - every other repository, one per session: its binding (its remote; `.cowork.yaml` only for a
     fork or a repository without a remote, ADR 0066), the import, that repository's decision on
     its `docs/tickets/`, and the three lines of ADR 0069 D4 in its `CLAUDE.md`;
   - the owner's global working rules about tickets rewritten once to point at cowork.
3. **The phase verification:** every imported ticket round-trips through `GET …/markdown` to a
   document equal to the source up to the mapping of
   [ADR 0063](../adr/0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md)
   D3, and the count of open tickets in the UI equals the `grep -rH '^state:'` count of the source
   repository at import time. The integration tier proves both on this repository's own
   `docs/tickets/` (T57); the owner's import proves them on his installation.
4. **Phase close:** T57 extracted and archived; the README reference covers every variable, value,
   route and problem code the phase added; the Status of every record the phase built says what is
   built.

## Related

- T26 — phase 3, still open for the owner's reviews
- T58 — phase 7, which started the same night
