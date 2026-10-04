---
id: T50
title: cowork-mcp has not run in a live Claude Code session, and the agent mark of its acts names no model
state: decided
severity: low
security: hardening
threat: the live check would additionally cover a hook or a skill of the plugin that acts otherwise than its tests say
urgency: later        # rule 4: a known piece of work
effort: S
blocked-by: human
filed-from: the close of phase 5 (T48), 2026-10-04
opened: 2026-10-04
decided: 2026-10-04
done:
---

## Current state

`cowork-mcp` (released in 0.3.0) with its tools, its hooks and the Claude Code plugin under
[`claude/cowork/`](../../claude/cowork/) is verified over stdio against a fresh backend
([mcp.md](../developer/mcp.md)): `session-context` bound this repository by its remote, and
`session_start`, `file_ticket`, three transitions, `record_state`, `open_question`, `finish_work`
and `get_ticket` left every act in the activity as the agent's. Nobody has installed the plugin
and run a `claude` session with it, so its SessionStart and Stop hooks and its skills have run
only in their tests ([claude-code.md](../operations/claude-code.md)).

The agent mark of `cowork-mcp` is `<client>/unknown/<session>`: MCP does not tell a server its
model, and the SessionStart hook does not pass the `model` that Claude Code's hook input carries
([`mcpcli/cli.go`](../../backend/internal/mcpcli/cli.go) `header`).

## Required changes

1. The SessionStart hook passes the model from Claude Code's hook input to `cowork-mcp`, which
   puts it into the agent mark; a unit test with a recorded hook input.
2. A live check, by the owner: install the plugin from this repository as
   [claude-code.md](../operations/claude-code.md) describes, start `claude` in a bound repository,
   and walk the phase's verification — the session names its ticket, Claude records its state,
   opens a question and finishes with a verification note, each act in the UI with the agent icon
   and the model in its mark.

## Related

- T48 — phase 5, closed with 0.3.0 (archived)
- T49 — the notarisation of the darwin binaries
