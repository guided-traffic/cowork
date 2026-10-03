---
id: T43
title: there is no dashboard — the tenant's front page shows open tickets per project only
state: decided
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix
effort: L
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The tenant's front page of T28 shows each project's open tickets by state and the tickets
updated last, from one list request. [ADR 0018](../adr/0018-the-views-of-the-first-release.md)
D6 decides a fixed dashboard of nine tiles, filterable by project and period: open by state, open
by severity, open `live` and `boundary` with the oldest named, `blocked` with the oldest block
and its reason kind, the age distribution, throughput (`done` per week, eight weeks), lead time
(median `filed`→`done`, thirty days), open decisions with the oldest named, time booked per
project.

## Required changes

1. A route per tile (or one dashboard route) with its definition in the API document, each a
   tested query under the visibility predicate.
2. The dashboard as the tenant's front page, live, with the project and period filters; charts
   follow the preset's tokens in both schemes.
3. Integration tests per tile's definition; unit tests per tile.
