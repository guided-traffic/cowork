---
id: T82
title: the project and the tenant export build the whole archive in memory, with no slot and no bound
state: done
severity: medium
security: live
threat: the guarantee that one tenant's work does not take the backend from the others weakens without any hostile principal — a large tenant's export (the nightly CronJob of docs/operations/backups.md), or a few at once, can exhaust the replica's memory, which every tenant shares; any reader of a tenant can also start exports at will
urgency: next          # rule 3: severity medium, live
effort: M
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-09
done: 2026-10-09
shipped: 0.13.0: the export streams a page of tickets at a time under one slot per replica
---

## Current state

exports.go loads every visible row with its body (:212), renders every document (:224-230) and builds the
whole tar.gz in a buffer (:268-299); no slot (api.go:231), no size bound, only the request timeout.

## Required changes

1. Stream the archive to the response, read the rows in pages, and hold exports to a slot per replica
   (as the import's); a test that exports a large synthetic project within a memory bound.
