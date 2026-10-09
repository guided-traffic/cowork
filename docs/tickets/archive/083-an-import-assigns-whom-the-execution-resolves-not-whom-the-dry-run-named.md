---
id: T83
title: an import's execution resolves the assignees again, so a person added between the dry run and the execution is assigned — and admitted to a confidential ticket
state: done
severity: low
security: live
threat: no hostile principal needed — an administrator adds a member, or grants a restricted project, between a dry run and its execution; the execution assigns that person where the corrected report named nobody, which admits them to a confidential ticket the report never showed them on
urgency: later         # rule 4: a known fix; a narrow window
effort: XS
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-09
done: 2026-10-09
shipped: 0.13.0: the execution assigns only whom the dry run named
---

## Current state

imports.go:371, 284-304 and columns.go:251-264 resolve the assignee at the execution again.

## Required changes

1. The execution refuses, or reports and asks, when an assignee resolves otherwise than in the dry run
   unless a correction names it; an integration test that adds a member in between.
