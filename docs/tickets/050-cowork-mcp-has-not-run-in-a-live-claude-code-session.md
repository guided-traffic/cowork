---
id: T50
title: cowork-mcp has not run in a live Claude Code session, and its agent mark keeps the model a session started with
state: in-progress
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

The model in the agent mark of the server's acts comes from the SessionStart hook:
`session-context` records the `model` of Claude Code's hook input per project directory
(`CLAUDE_PROJECT_DIR`), and `serve` reads it at each request
([`mcpcli/cli.go`](../../backend/internal/mcpcli/cli.go) `sessionContext`, `client.header`;
ADR 0067 D5). `TestTheSessionStartHookNamesTheModelOfTheServer` runs it on the input Claude Code's
[hook reference](https://code.claude.com/docs/en/hooks) shows, not on one recorded from a live
session. A model switched with `/model` inside a session fires no SessionStart, so the mark keeps
the model the session started with; Claude Code has a `PostModelSwitch` hook event, which does not
block and whose input carries `from_model` and `to_model`.

## Required changes

1. A live check, by the owner: install the plugin from this repository as
   [claude-code.md](../operations/claude-code.md) describes, start `claude` in a bound repository,
   and walk the phase's verification — the session names its ticket, Claude records its state,
   opens a question and finishes with a verification note, each act in the UI with the agent icon
   and the session's model in its mark, `claude-code/<model>/<id>`.

## Open questions

### Q1: Should the agent mark follow a model switched inside a session?

The server's mark names the model the last SessionStart hook in the project directory recorded; a
switch with `/model`, or one Claude Code makes itself, is not seen, so every act after it is
recorded under a model that did not make it.

- **A `PostModelSwitch` hook in the plugin and the `settings.json` block** — recommended. It runs a
  third hook mode of `cowork-mcp`, beside `session-context` and `session-end`, that records
  `to_model` in the same file and prints nothing (its standard output would reach Claude's
  context). Cost: an amendment of ADR 0067, a few lines beside `session-context`, and a hook run
  per switch. The mark the owner reads in the UI stays true;
  a wrong model in the record misleads where `unknown` would only be poorer.
- **Leave it**, the gap named in ADR 0067's residual risks as it is now. Costs nothing; the
  model in a mark is then "the model the session started with", which the docs say.

**Answer:** _open_

## Related

- T48 — phase 5, closed with 0.3.0 (archived)
- T49 — the notarisation of the darwin binaries
