---
id: T41
title: there is no project board
state: decided
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: M
blocked-by: T30
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

No board exists. [ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1: one column per
state in [ADR 0009](../adr/0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) order,
the leaves as cards with type, severity, security class, assignee, progress bar, block reason and
the count of open prerequisites; a drag between columns is a transition and asks for what it
requires; WIP limits per column ([ADR 0019](../adr/0019-no-sprints-and-no-milestones-continuous-flow-with-optional-wip-limits.md)).

## Required changes

1. `/t/{slug}/p/{KEY}/board` with CDK drag and drop
   ([ADR 0052](../adr/0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) D1)
   and PrimeNG cards; the transition dialogs of T30.
2. Live: a card another person moves moves here within the stream's latency; a card being
   dragged is not yanked away by an event.
3. Unit tests; the e2e path drags a card in both schemes
   ([ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md) D3).
