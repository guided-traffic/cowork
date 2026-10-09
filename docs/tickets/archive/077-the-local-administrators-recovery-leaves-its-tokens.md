---
id: T77
title: rotating the local administrator's Secret ends its sessions but leaves its personal access tokens
state: done
severity: medium
security: boundary
threat: whoever held the leaked local-administrator password (or a stolen session of it) makes a personal access token, optionally after granting itself admin in every tenant, and keeps that reach over the installation's tenants after the documented recovery, which rotates the Secret and restarts
urgency: next          # rule 3: severity medium, live; decided, waits for the build
effort: XS
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-09
shipped: 0.13.0: the synchronisation revokes the local administrator's tokens when it stores a changed password
---

## Current state

The synchronisation revokes tokens only on a take-over (bootstrap.go:166-170, 247-253;
bootstrap_test.go:76-91 checks sessions, not tokens); local-accounts.md:251-253, installation.md:305-309
and README:516 promise the recovery without naming tokens. A person's own password change leaves
their tokens too.

## Open questions

### Q1: Does a changed password end the account's tokens?

- **(a) The synchronisation revokes the local administrator's tokens when it stores a changed password**, and the recovery's docs say "review its grants"; a person's own change stays as it is (their tokens are theirs).
- **(b) Every password change revokes the account's tokens.**
- **(c) Docs only**: the recovery names "revoke its tokens and review its grants".

Recommended: **(a)** — the rotation is the incident's recovery, and a token made with the leaked password must not survive it; a person who changes their own password may keep the tokens they made.

**Answer:** (a) — the owner, 2026-10-07. The synchronisation revokes the local administrator's
tokens when it stores a changed password; a person's own change keeps their tokens. The amendment of
ADR 0033 and the recovery's pages land with the fix, in the same change.
