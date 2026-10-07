---
id: T66
title: cowork-mcp export on Windows writes outside its target for names with backslashes, and its files take the directory's access list instead of owner-only modes
state: done
severity: medium
security: boundary
threat: a hostile or compromised installation (or a responder on a plain-http loopback COWORK_URL) answers `cowork-mcp export` with an archive whose entry names use backslashes and writes files of its choosing outside the target directory of a person on Windows — the Startup folder among them; and on Windows the exported files, confidential tickets included, are readable by whoever the target directory's inherited access list admits
urgency: now           # rule 1: fixed in the change that found it
effort: S
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-07
shipped: cowork-mcp export writes only the names an export holds, after fs.ValidPath, through os.OpenRoot with O_EXCL, and the docs say the modes are POSIX-only (0.12.0)
---

## Current state

`inside` in backend/internal/mcpcli/export.go:134-143 cleans an entry name with `path` and refuses
only `/`-separated `..`; on Windows `filepath.Join` reads `..\..\x` as a parent walk (inferred from Go's
documented `filepath` behaviour, not run on Windows). Missing directories are created; existing files
are not overwritten (`O_EXCL`). Windows binaries ship (build.yml:158). Separately, the 0600/0700 modes
(export.go:104, 148, 155) mean nothing on Windows: Go applies only the write bit there.

## Required changes

1. Accept only the names an export writes (`manifest.json`, `links.json`, `attachments.json`,
   `<slug>/<KEY>-<n>.md`), each after `fs.ValidPath`, and write through `os.OpenRoot`
   (`Root.MkdirAll`, `Root.OpenFile` with `O_EXCL`), which also closes import-and-export.md H-77; tests
   with backslash names and a planted directory link.
2. The docs stop claiming owner-only modes on Windows: the modes are POSIX-only, export under the user
   profile on Windows (README, docs/developer/mcp.md, docs/developer/import-and-export.md,
   docs/security/import-and-export.md, docs/security/agent-client.md) — a hardening gap of its own once
   item 1 ships.
