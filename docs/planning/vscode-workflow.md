# Streamlining the VS Code work through cowork

How a working day across many repositories, with Claude Code in VS Code, is meant to run once
cowork exists — and how to get there from the Markdown tickets of today without a big-bang
switch. The decisions this plan needs are the I-questions of [questions.md](questions.md);
the phases it maps to are 5 and 6 of [project-plan.md](project-plan.md).

Written 2026-09-29.

## Today

- Every repository carries its own `docs/tickets/` in Markdown. The rules are good; the
  overview across twenty repositories is a `grep` per repository, and nothing shows "what is
  waiting for me" across all of them.
- Open questions for the owner live inside ticket files. Finding them means opening files.
- Claude starts every session cold: which ticket, what state, what was decided last time —
  all re-read from files, or lost.
- An embargoed security finding is a gitignored file on one laptop.
- Prioritisation is a frontmatter field edited by hand, per file, per repository.

## The target picture

```
   VS Code + Claude Code (any repository)
   ┌──────────────────────────────────────────┐
   │ .cowork.yaml  →  tenant, project         │
   │ SessionStart hook → "active ticket: …"   │      MCP (stdio, PAT)      ┌──────────────┐
   │ skills: /next /ticket /question /done    │ ─────────────────────────► │   cowork     │
   │ commits: scope or trailer = KEY-123      │                            │  API + UI    │
   └──────────────────────────────────────────┘                            │  PostgreSQL  │
                                                                           └──────┬───────┘
   Browser: backlog, board, "next for me", "open decisions", timeline  ◄──────────┘
   git: ADRs, developer/operations/security pages — unchanged
```

One backlog for every tenant and project; Claude reaches it through an MCP server with a
personal access token; the owner reaches it through the UI; git keeps what git is good at.

## The daily loop

1. **Morning.** Open `/me/next` in cowork: the top tickets across all tenants by the priority
   model, and `/me/decisions`: every open question waiting for an answer. Answer the questions
   that can be answered from the couch; each answer is attributed and timestamped. Pick a
   ticket; its project names the repository; open it in VS Code.
2. **Session start.** Claude Code runs the `SessionStart` hook, which calls the MCP server's
   `session_start`: it reads the git remotes, finds the project across the owner's tenants
   (ADR 0066) — or proposes tenant, key and name and creates the project once the owner says
   yes in chat — then asks cowork for the ticket assigned to the owner in that project with
   state `in-progress` (or the top candidate when there is none) and prints its key, title,
   state and open questions into the context. `.cowork.yaml` is only written for a
   remote-less repository or a fork. Claude knows what it is doing before the first prompt.
3. **Work.** Claude reads the full ticket once (`get_ticket` returns the Markdown document);
   records findings by rewriting the ticket's current state (`record_state`), not by appending
   history; opens a decision as an open question with options and a recommendation
   (`open_question`), one at a time, as the working rules demand; the owner answers in chat
   or in the UI — in chat, Claude writes the answer down with `record_answer`, and the record
   says "answered by Hans, recorded via Claude Code" (ADR 0066 D8); the decision is always
   the person's. What else the agent may do (`decided`, `done`, rank, …) is the capability
   set of its token (ADR 0043); the owner's own token runs "full".
4. **Commit.** `fix(controller): guard the failover gate (VKO-12)` with a
   `Cowork-Ticket: guided-traffic/VKO-12` trailer, on a branch `fix/VKO-12-failover-gate`
   unless the owner names another (ADR 0068); the tools hand Claude the strings. No
   apostrophes. The PR body names the full key; the optional inbound webhook of ADR 0071
   (phase 7, on trial) shows the pull request on the ticket and notifies on merge — the state
   is still moved by a person or a capable agent.
5. **Session end.** `finish_ticket` refuses without a verification note (what was run, against
   what, result); the state moves to `done` or to review; the ADR extraction that the ticket
   rules require still happens in the repository, by Claude, in the same session.
6. **Evening.** The activity of the day across all tenants, with every agent write marked
   "via Claude Code", is the status report. Nothing to write up. The next session starts by
   reading the inbox: nothing pushes (ADR 0020), the session pulls.

## Building blocks and where each lives

| Block | Lives in | Phase |
|---|---|---|
| The API, the token model, the audit log | this repository | 2 |
| The UI views `/me/next`, `/me/decisions`, backlog, board, ticket timeline | this repository, `frontend/` | 3 |
| `cowork-mcp`: stdio MCP server with the thirteen tools of ADR 0042, configured once in Claude Code with `COWORK_URL` and `COWORK_TOKEN` (ADR 0040, 0041) | this repository, `backend/cmd/cowork-mcp/` | 5 |
| The remote lookup and proposal (`/me/repositories/lookup`, `create_project`); `.cowork.yaml` only as the override for remote-less repositories and forks (ADR 0066) | this repository (API and MCP server); the optional file per repository | 5 |
| `SessionStart` hook running `cowork-mcp session-context` and `Stop` hook running `cowork-mcp session-end` (ADR 0067); one user-level block, silent in unbound repositories | `~/.claude/settings.json`; the block is in the operations page | 5 |
| Skills `/next`, `/ticket <key>`, `/question`, `/done`: thin wrappers around the MCP tools that also enforce the writing rules (one question at a time, verification note) | a Claude Code plugin in this repository, installed globally | 5 |
| A `CLAUDE.md` block for bound repositories — three lines: work in cowork under `<tenant>/<KEY>`, rules here under the five homes, commits per ADR 0068 (ADR 0069 D4) | this repository, `docs/operations/` as a template | 5 |
| VS Code tasks: `make run`, `make frontend-serve`, `make test` | this repository, `.vscode/tasks.json` (optional) | 3 |
| The importer and the Markdown round-trip | this repository | 6 |

## The migration path

**Stage A — side by side (phases 2–5).** This repository is the pilot: its own tickets go into
cowork as soon as the API exists, and the workflow is exercised on the tool that builds it.
Every other repository keeps its Markdown tickets and rules unchanged.

**Stage B — cut-over of the first sibling (phase 6).** The importer moves the sibling
project's open tickets; that repository decides for itself to remove `docs/tickets/` and to
point its `CLAUDE.md` at cowork (a cross-repository change, so it is a ticket there, not an
edit from here). Its ADRs, developer, operations and security pages stay.

**Stage C — everything else.** One repository per session: add `.cowork.yaml`, import,
remove the directory, update `CLAUDE.md`. The owner's global working rules about tickets are
rewritten once to point at cowork.

## What cowork will not do

- Replace git for decisions and documentation. An ADR is a file next to the code it governs.
- Replace GitHub for pull requests and CI. It links to them.
- Run the LLM. It is what the LLM talks to, with a token that says who is accountable.
- Sync in any direction with Markdown files (ADR 0064). After the import, cowork is the
  source; the export exists for backups and for reading offline; what a repository does with
  its files afterwards is that repository's decision.

## Success criteria

- "What should I do next?" is answered in under a minute, across all tenants, without opening
  a repository.
- Every open decision is listed in one place, and answering it is one click plus a sentence.
- After the cut-over, no bound repository carries a `docs/tickets/` directory.
- Every write by an agent is attributable to a person and a session in the timeline.
- A Claude Code session in a bound repository starts with the correct ticket context without
  the owner typing it.

## Open questions

Q-I1 to Q-I6 in [questions.md](questions.md), plus Q-D1 to Q-D7 for the interface itself.
