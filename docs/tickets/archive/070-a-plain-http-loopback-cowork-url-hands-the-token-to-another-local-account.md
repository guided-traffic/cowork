---
id: T70
title: a plain-http loopback COWORK_URL hands the token to another local account that listens on the port while the port-forward is down
state: done
severity: low
security: boundary
threat: another account of the same machine binds the loopback port of a plain-http COWORK_URL while the person's port-forward is down and receives the bearer token cowork-mcp sends (and, on Windows, could deliver the export of T66)
urgency: icebox        # accepted by the owner on 2026-10-07 within "the client trusts its machine"
effort: XS
blocked-by:
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-07
shipped:
publication-accepted: 2026-10-07
---

## Current state

mcpcli/config.go:54-55, 66-72 accept `http://` for a loopback host. Nothing checks who listens.

The owner accepted the risk and its publication (Q1, answer a): ADR 0040 D4 as amended keeps the
loopback exception without an opt-in, and docs/security/agent-client.md names it as H-108.

## Open questions

### Q1: Does the client accept a plain-http loopback URL without an opt-in?

- **(a) Document it** as within "the client trusts its machine" (agent-client.md), and accept publishing.
- **(b) Require an explicit opt-in** (`COWORK_ALLOW_HTTP=1`) for plain http, loopback included.

Recommended: **(b)** — one variable, and a port-forward over plain http stays possible on purpose.

**Answer:** (a) — the owner, 2026-10-07. Documented as H-108 within "the client trusts its machine"; publishing accepted.
