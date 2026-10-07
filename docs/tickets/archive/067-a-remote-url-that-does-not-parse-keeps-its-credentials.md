---
id: T67
title: a git remote URL that Go's url.Parse refuses keeps its credentials and leaves the machine in the lookup
state: done
severity: medium
security: live
threat: the guarantee that cowork-mcp removes credentials from a remote before any request (agent-client.md) fails for a remote whose password holds a character url.Parse refuses; the password then travels in the lookup's query string, lands in the Ingress log and comes back in the answer
urgency: now           # rule 1: fixed in the change that found it
effort: XS
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-07
shipped: SanitiseRemote strips the userinfo of a remote that url.Parse refuses (0.12.0)
---

## Current state

`SanitiseRemote` (backend/internal/domain/repository.go:146-149) returns the remote raw when
`url.Parse` fails — a `^`, `|`, `{`, `"`, a space or a non-ASCII character in the password, or a
malformed `%` escape. The remote goes out in the lookup's query (tools/workspace.go:116,
tools/binding.go:97-108), into the Ingress log (trust-boundaries.md H-14), and back in
`remotes[].remote` (api/repositories.go:325). `NormaliseRemote` refuses it as well, so it never binds.

## Required changes

1. A `://` remote that does not parse is stripped of its authority's userinfo by hand (everything up to
   the last `@` of the authority) or dropped; table cases in `TestSanitiseRemote` for each character
   class above.
