---
id: T50
title: cowork-mcp has not run in a live Claude Code session, and whether its agent mark follows a model switch is unanswered
state: done
severity: low
security: hardening
threat: the live check would additionally cover a hook or a skill of the plugin that acts otherwise than its tests say
urgency: later        # rule 4: a known piece of work
effort: S
blocked-by:
filed-from: the close of phase 5 (T48), 2026-10-04
opened: 2026-10-04
decided: 2026-10-04
done: 2026-10-09
shipped: 0.8.0, the hook of the model switch with it
---

## Current state

`cowork-mcp` (released in 0.3.0) with its tools, its hooks and the Claude Code plugin under
[`claude/cowork/`](../../claude/cowork/) is verified over stdio against a fresh backend
([mcp.md](../developer/mcp.md)): `session-context` bound this repository by its remote, and
`session_start`, `file_ticket`, three transitions, `record_state`, `open_question`, `finish_work`
and `get_ticket` left every act in the activity as the agent's. Nobody has installed the plugin
and run a `claude` session with it, so its SessionStart, Stop and PostModelSwitch hooks and its
skills have run only in their tests ([claude-code.md](../operations/claude-code.md)).

The model in the agent mark of the server's acts is recorded per project directory
(`CLAUDE_PROJECT_DIR`) by two hooks, and `serve` reads it at each request
([`mcpcli/cli.go`](../../backend/internal/mcpcli/cli.go) `sessionContext`, `modelSwitch`,
`client.header`; ADR 0067 D5). `session-context` records the `model` of the SessionStart input; a
start without one keeps the recorded one after `/clear` or a compaction and records none
otherwise. `model-switch`, which a `PostModelSwitch` hook runs, records the `to_model` of a switch
— `/model`, an automatic fallback, `opusplan` entering or leaving plan mode, the model restored on
a resume — unless the input names an `agent_id`, which makes it a subagent's switch; it prints
nothing, because Claude Code adds that hook's standard output to the model's context. The event
needs Claude Code 2.1.251 or later. The plugin carries the switch hook in
[`hooks/model-switch.json`](../../claude/cowork/hooks/model-switch.json), which its `plugin.json`
names, apart from `hooks/hooks.json`: Claude Code 2.1.218 loads none of the plugin's hooks from a
hooks file that names the event, and with the separate file it runs the SessionStart and Stop
hooks and lists the switch hook's file as failed to load (`claude plugin details`,
`claude plugin list --json`). `TestTheSessionStartHookNamesTheModelOfTheServer` and
`TestThePostModelSwitchHookNamesTheModelOfTheServer` run the hooks on the inputs Claude Code's
[hook reference](https://code.claude.com/docs/en/hooks) shows and describes, not on inputs recorded
from a live session.

## Required changes

None. The live check with Claude Code 2.1.251 or later is part of the owner starting to use cowork, T55.

## Open questions

### Q1: Should the agent mark follow a model switched inside a session?

The SessionStart hook records the model a session started with. A switch with `/model`, or one
Claude Code makes itself, fires no SessionStart, so without a hook on the switch every act after it
is recorded under a model that did not make it.

- **A `PostModelSwitch` hook in the plugin and the `settings.json` block** — recommended. It runs a
  third hook mode of `cowork-mcp`, beside `session-context` and `session-end`, that records
  `to_model` in the same file, not a subagent's switch, and prints nothing (its standard output
  would reach Claude's context). Cost: an amendment of ADR 0067, a few lines beside
  `session-context`, a hook run per switch, a hooks file of its own in the plugin, and Claude Code
  2.1.251 or later for that hook. The mark the owner reads in the UI stays true; a wrong model in
  the record misleads where `unknown` would only be poorer.
- **Leave it out**: the hook mode and both hook entries removed again, ADR 0067's amendment struck
  and its residual risk restored. Costs nothing to run; the model in a mark is then "the model the
  session started with", which the docs say again.

**Answer:** (a) — the owner, 2026-10-07. The `PostModelSwitch` hook stays; ADR 0067 D5 records it as answered.

The recommended option is built (ADR 0067 D5) and stays unless the owner answers otherwise.

## Not verified

Claude Code runs PostModelSwitch for the model it restores on a resume, and its hook reference does
not say whether before or after the SessionStart hook. After a resume whose SessionStart input names
no model — a session restored through conversation recovery — the mark is therefore the restored
model or `unknown`. A debug log of such a restore (`claude --debug`), showing the order of the two
hooks, would settle it.

## Related

- T48 — phase 5, closed with 0.3.0 (archived)
- T49 — the notarisation of the darwin binaries
