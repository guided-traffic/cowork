---
id: T85
title: an import's zip is parsed whole before its bounds apply, and its memory grows with the number of entries
state: done
severity: medium
security: boundary
threat: a tenant's administrator uploads a zip of a million and more empty entries within COWORK_MAX_IMPORT_BYTES; zip.NewReader parses its whole central directory before the 10,000-file and byte bounds apply, so the replica's memory grows with the entries (about 300 MiB of heap for 1.6 million entries in 125 MiB, measured in a scratch program; roughly 120 MiB at the 50 MiB default, beside the raw upload, against the chart's 256 MiB limit) and can kill the replica every tenant shares (the kill not measured)
urgency: next          # rule 3: severity medium, live
effort: XS
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-09
done: 2026-10-09
shipped: 0.13.0: the importer counts a zip's entries before it parses the archive
---

## Current state

backend/internal/importer/upload.go, function `zip`: `zip.NewReader` reads the central directory first;
the bounds of the file count and the bytes apply to the entries afterwards. H-76 names the import's
memory in general.

## Required changes

1. Read the end-of-central-directory record first and refuse a zip that declares more than 10,000
   entries, or a directory larger than its share of the bound, before `zip.NewReader`; a test with a zip
   of many empty entries that stays within a memory bound.
