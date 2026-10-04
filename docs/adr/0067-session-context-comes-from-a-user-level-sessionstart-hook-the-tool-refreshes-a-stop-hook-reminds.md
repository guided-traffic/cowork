# ADR 0067: Session Context Comes From a User-Level `SessionStart` Hook Running `cowork-mcp session-context`; the `session_start` Tool Refreshes; a `Stop` Hook Reminds of Unfinished Work

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"session start?": a `SessionStart` hook plus the tool for refresh plus a `Stop` hook, over a
CLAUDE.md instruction, over the hook alone, and over hook and tool without the end-of-session
reminder. The additional rules of D5–D7 were put to the owner with the question and
explicitly confirmed.

**Built** (phase 5, 2026-10-04): `cowork-mcp session-context` and `session-end`, the plugin's
`hooks/hooks.json` and the `settings.json` block of
[docs/operations/claude-code.md](../operations/claude-code.md). D1 and D7 without the inbox,
which does not exist ([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md)), and
with the candidates in rank order while the score is not built; the block is held to about
9 000 bytes. A compaction (`source: compact`) shows the block again without moving the
session's start. D4 and D5 as amended.

## Context

`session_start` is an MCP tool ([ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md),
[ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D3); a tool is called by the model, not by the host at start, so without something else the
first step of every session is a prompt or a CLAUDE.md rule — the prompt choreography
[ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
set out to remove. Claude Code runs hook commands on `SessionStart` and injects their standard
output as context, and on `Stop`; a hook can therefore deliver the session block before the
first prompt and a reminder at the end, with the same code the tool uses. Nothing pushes into
a running session ([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
serves browsers), so a refresh is the tool's job.

## Decision

**D1 — `cowork-mcp session-context` is a one-shot mode of the MCP binary.** It reads
`COWORK_URL` and `COWORK_TOKEN` from the environment and the git remotes of the working
directory, runs the same procedure as the `session_start` tool — lookup or proposal
(ADR 0066 D3), the active ticket as `/context` with `comments=5&activity=10`
([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)),
up to five candidates by score, the unread inbox grouped by ticket (at most twenty lines),
the activity of the bound tickets since the last session — and prints one Markdown block to
standard output. One function, two callers: the hook and the tool.

**D2 — The hook is configured once, user-wide,** in `~/.claude/settings.json` *(amended
2026-10-04: or by the Claude Code plugin of this repository, whose hooks run in every session
while it is enabled)*, not per repository: `SessionStart` runs `cowork-mcp session-context` with a five-second timeout. In a
repository without a remote, a binding or a token the command prints nothing and exits 0; a
session is never blocked by it.

**D3 — The `session_start` tool stays for refresh.** "What is new?" an hour later is the
tool; the hook is the first call.

**D4 — A `Stop` hook runs `cowork-mcp session-end`.** It checks whether a ticket of the bound
project assigned to the person is `in-progress` without a comment, a body change or a
transition since the session started, and prints a one-line reminder ("`acme/VKO-12` is
still in progress — `finish_work`?"). It is a hint, never a veto; it blocks nothing.
*(Amended 2026-10-04: and only while the repository shows work since the session started — a
commit, or a file changed after the start —, with no act of the person on the ticket since, of
any kind. Claude Code runs `Stop` after every answer; without the first condition a session
that only reads would be reminded after each of them. The line is a `systemMessage` to the
person, which continues nothing.)*

**D5 — The hook commands read only the environment and the working directory,** and write
nothing but the session-time cache of [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md)
D5. An error — the installation unreachable, the token expired, the API incompatible
([ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
D5) — prints one line naming the cause and the installation's token page, and exits 0; it is
never a hook failure. *(Amended 2026-10-04: the `SessionStart` hook prints the line; the `Stop`
hook stays silent on an error, because it runs after every answer and the start has said what
is wrong.)*

**D6 — The operations page ships the ready `settings.json` block:** both hooks, the timeout,
the environment passthrough, beside the MCP server entry of [ADR 0041](0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md)
D3.

**D7 — The block is bounded.** The active ticket with five comments and ten activity lines,
five candidates, twenty inbox lines, one proposal; a session that wants more calls
`get_ticket` or `search`.

## Consequences

- A session in a bound repository begins with its ticket in context and no prompt; in an
  unbound one it begins with the proposal to create a project, and the owner says yes or no.
- The end of a session reminds of the one expensive mistake — a ticket left standing without
  its verification note — without forcing anything.
- The MCP binary gains two subcommands that share the tool's code; the tests of
  [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) D6 cover the one-shot mode
  by running it against the fixture environment and asserting the block.
- Per-repository configuration disappears from the workflow plan: one user-level block, one
  binary, the token in the environment.

## Alternatives Considered

- **A CLAUDE.md rule "call `session_start` first".** No hook to configure; prompt discipline
  and one turn per session. Lost.
- **The hook alone, without the tool.** No refresh inside a long session. Lost.
- **Hook and tool without the `Stop` reminder.** The session's end left to memory. Lost.

## Residual risks

- Hook semantics belong to Claude Code and may change; the subcommands are plain commands
  that print to standard output, which is the most portable contract a host can have.
- D4's heuristic ("no act since the session started") is approximate; a session that only
  read is rightly not reminded, a session that worked in the UI instead is wrongly reminded —
  a hint, not a veto, by design.

## References

- [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md), [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md) D3 — the procedure the hook runs
- [ADR 0041](0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md) D3, D4 — the binary, its configuration and working directory
- [ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md) — the `/context` document in the block
- [docs/operations/claude-code.md](../operations/claude-code.md) — the daily loop as built (the workflow plan once linked here is consumed, [ADR 0074](0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md) D3)
