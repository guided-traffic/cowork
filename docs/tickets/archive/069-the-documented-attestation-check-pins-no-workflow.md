---
id: T69
title: the documented attestation check of the cowork-mcp binaries pins neither the signing workflow nor the tag
state: done
severity: medium
security: boundary
threat: whoever can write releases and push a branch with a workflow of their own (a repository writer, or a token with contents and workflow write) attests a swapped binary that passes the documented `gh attestation verify --repo`, for everyone who installs cowork-mcp — which agent-client.md and claude-code.md say the check refuses
urgency: now           # rule 1: fixed in the change that found it
effort: XS
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-07
shipped: the documented attestation check names --signer-workflow and --source-ref, verified against the v0.11.0 binary (0.12.0)
---

## Current state

docs/operations/claude-code.md:46 and README.md:197 document `gh attestation verify --repo …` alone;
gh's defaults then accept an attestation by any workflow of the repository on any ref (gh's
documentation, not run here). agent-client.md:150-152 and claude-code.md:51-52 say the check fails a
swap.

## Required changes

1. The documented command adds `--signer-workflow guided-traffic/cowork/.github/workflows/build.yml`
   and `--source-ref refs/tags/v$VERSION`; H-35 says what the check then proves and what not.
