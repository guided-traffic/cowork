---
id: T42
title: there is no tenant board with one swimlane per project
state: decided
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: M
blocked-by: T41
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

No board exists. [ADR 0018](../adr/0018-the-views-of-the-first-release.md) D4: columns are the
states, rows the non-archived projects, cards as on the project board; a drag between columns is
a transition, a drag between rows is refused, rank is not edited here; lazy per swimlane, with a
project filter.

## Required changes

1. `/t/{slug}` board view (beside the overview of T28 until the dashboard of T43 takes the front
   page) with lazy swimlanes and the project filter; saved filters apply (T38).
2. The refusal of a drag across swimlanes is visible, not silent.
3. Unit tests; the e2e path drags a card across columns and is refused across rows.
