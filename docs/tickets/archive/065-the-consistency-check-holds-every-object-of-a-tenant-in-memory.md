---
id: T65
title: the consistency check holds every object of a tenant in memory, and a tenant's member can make that exhaust the replica on every start
state: done
severity: medium
security: boundary
threat: a member of any tenant with write scope, or an agent with the upload capability, uploads several hundred thousand small files across many tickets of their tenant; the daily consistency check then exhausts the memory of the replica that runs it, and because the check stays due it runs again at every start, so every tenant of the installation loses the backend
urgency: now           # rule 1: fixed in the change that found it
effort: S
blocked-by:
filed-from: the security pages reviewed against the code (T58 required change 2), 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-07
shipped: the consistency check compares the listing and the rows in ordered batches of 1,000 and holds a bounded memory (145 MiB to 0.1 MiB per million objects, measured), and the chart sets GOMEMLIMIT from the container's memory limit (0.12.0)
---

## Current state

The daily consistency check of ADR 0059 D4 (built 2026-10-06, released in 0.10.0) reads a tenant's
whole object listing into one slice — [`storage.List`](../../../backend/internal/storage/storage.go)
collects every object under the prefix — and `judge` in
[`store/consistency.go`](../../../backend/internal/store/consistency.go) builds two maps over the
listing and the rows (`named`, `seen`), so the check's memory grows with the number of objects of
the largest tenant: roughly 250 bytes per object (an estimate from the types, not measured). The
chart's backend limit is 256Mi and no `GOMEMLIMIT` is set.

The principal: a member with `write` scope, or an agent with `upload`. An upload of zero bytes is
accepted; the limit is 100 files per ticket, nothing bounds the number of tickets, and the quota
(off by default) counts bytes, not files. Around a million objects in one tenant — 10,000 tickets
with 100 files each — the check is killed for memory. Its transaction never commits, so the check
stays due; `runJobs` runs it at every start (`ConsistencyCheckDue`), so the replica crash-loops, and
every other replica that takes the job's lock at its hourly tick dies the same way. Every tenant
loses the backend until an operator removes the objects or the storage configuration.

Not verified: the threshold — read from the types, not measured.

## Required changes

1. The check compares the listing and the rows as a merge of two streams in the same order — S3
   lists keys in byte order, and `<tenant-id>/<lowercase uuid>` sorts as `ORDER BY id` does —, keeping
   only the counts, the bytes, the first `ConsistencyListBound` orphans and the attachments the
   listing did not show; `storage.List` becomes an iteration that hands each object on. A unit test
   runs the comparison over a synthetic listing of a million objects and holds its heap growth to a
   bound; the existing tests of the check pass unchanged.
2. Recommended beside it, as hardening: the chart sets `GOMEMLIMIT` from the container's memory
   limit (the downward API), so the collector works before the kernel kills.

## Related

- T63 — the consistency check
- T58 — the security pages reviewed against the code, which found this
