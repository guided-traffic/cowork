# The MCP server and the tool catalogue

How `cowork-mcp` is built: a tool catalogue that knows no transport, the MCP layer that serves
it over stdio, the command line with the hooks and the workflow subcommands, and the Claude Code
plugin in `claude/cowork/`. The decisions are
[ADR 0040](../adr/0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
(a thin client of the API), [ADR 0041](../adr/0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md)
(stdio, a binary per platform), [ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md)
(the tools), [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D6 (the limits in the descriptions), [ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D5 (the keys), [ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
(the binding), [ADR 0067](../adr/0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md)
(the hooks), [ADR 0068](../adr/0068-commits-carry-a-component-scope-the-short-key-in-the-subject-and-the-full-key-in-a-trailer.md)
(the commit strings) and [ADR 0070](../adr/0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md)
(the subcommands). Setting it up is [docs/operations/claude-code.md](../operations/claude-code.md);
what it holds and leaves open, [docs/security/agent-client.md](../security/agent-client.md).
Read against the tree on 2026-10-05.

## Three layers

```
cmd/cowork-mcp ──► internal/mcpcli ──┬──► internal/mcpserver ──► MCP Go SDK (stdio)
  main, linker     Run: serve,       │      one mcp.Server, a handler per tool
  variables        session-context,  │
                   session-end,      │
                   model-switch,     └──► internal/tools ──► internal/api/apigen (the generated client)
                   token check,             the catalogue,        ──► HttpRequestDoer: the network,
                   lookup, export,          Session, Start,           or a handler in the same process
                   version
                                            Resolve, Remind
```

`internal/tools` is the catalogue and everything a tool needs; it imports no MCP package and
holds no global state, so another host runs the same tools with its own API client, memory and
working directory — the chat in the UI does, inside the backend
([another host](#another-host-for-the-catalogue)). `internal/mcpserver` binds the
catalogue to the MCP SDK; `internal/mcpcli` is the command line. The binary depends on nothing
of the server: `TestTheBinaryIsAClientOnly` fails when it comes to import the store, the
database driver, the object storage client or the API's handlers.

| Package, file | Responsibility |
|---|---|
| [`tools/tools.go`](../../backend/internal/tools/tools.go) | `Tool` (name, description, surface, operations, schema, run), `define[In]` — the schema inferred from the input type — and `Define`, the same for a host's own tools, `Catalogue(surfaces…)`, `Operations`, `Valid`, `Call` (validation, the binding resolved once, then the run, every failure a `Result` with `IsError`), `Usage` |
| [`tools/session.go`](../../backend/internal/tools/session.go) | `Session` — the API client, the installation, `Memory`, `Workspace`, the clock and the key maker, the `Person` and the `Tenants` a host may name —, `Binding` with `Bind`, `BindTenant` and `bindOnce`, `Token` with `Can`, `ReadToken`, `Assume`, `Me`, `AgentHeader`, `Editor` (bearer token, agent header, user agent) |
| [`tools/client.go`](../../backend/internal/tools/client.go) | `check` and `APIError` (the API's problem, rendered with its code and the refusal note), `Retrying` (a transport failure retried where a repetition cannot act twice), `HandlerDoer` (a request served by an `http.Handler` in the same process) |
| [`tools/binding.go`](../../backend/internal/tools/binding.go) | `Resolve`: the binding file, the remotes, the lookup, the drift, the session's binding |
| [`tools/start.go`](../../backend/internal/tools/start.go) | `Start`, the procedure of `session_start` and the SessionStart hook: the unbound block with the proposal, or the bound block — the active ticket's context or the candidates, what happened since, each act's line naming its person `via <agent>` or `through the token <name>` (`actLine`) — within `MaxBlock` |
| [`tools/remind.go`](../../backend/internal/tools/remind.go) | `Remind`, the Stop hook's check |
| [`tools/compat.go`](../../backend/internal/tools/compat.go) | `CheckVersion`, `CheckCompatibility` (the major version and the operations the served document has), `IncompatibleError` |
| [`tools/memory.go`](../../backend/internal/tools/memory.go) | `Memory`, `InMemory`, `FileMemory` under the user's cache directory: one file per installation and binding for the time of the last start, one per project directory for the model the SessionStart hook read or the PostModelSwitch hook named (`SetModel`, `Model`) |
| [`tools/workspace.go`](../../backend/internal/tools/workspace.go) | `Workspace`, `GitWorkspace` (git remote, rev-parse, log, status), `BindingFile` and its reading and checking |
| [`tools/keys.go`](../../backend/internal/tools/keys.go), [`query.go`](../../backend/internal/tools/query.go), [`limits.go`](../../backend/internal/tools/limits.go) | Keys resolved against the binding, the commit strings of ADR 0068; the list and read helpers; the capability line of a description |
| `tools/tool_*.go` | The tools: `tool_tickets.go` (get_ticket, search — over the ticket lists' `q` filter, not the ranked search routes ([search.md](search.md#the-q-filter-and-the-mcp-tool)) —, file_ticket, record_state, comment, link, watch, place_ticket), `tool_flow.go` (transition, set_progress, finish_work), `tool_questions.go` (open_question, record_answer, and `person`, which resolves a person named as `me`, a username, a display name or an id through the tenant's member list — `open_question`'s `asked_of` and `comment`'s `mentions`), `tool_project.go` (session_start, create_project), `tool_api.go` (api) |
| [`mcpserver/server.go`](../../backend/internal/mcpserver/server.go) | `New(Options)`: the server, its `Instructions`, each tool with its schema, its described limits and its annotations; a handler that learns the client's name, asks `Ready` and runs the tool |
| [`mcpcli/cli.go`](../../backend/internal/mcpcli/cli.go), [`config.go`](../../backend/internal/mcpcli/config.go), [`commands.go`](../../backend/internal/mcpcli/commands.go) | `Run` and the command table; the configuration from `COWORK_URL`, `COWORK_TOKEN`, `CLAUDE_PROJECT_DIR`; `serve` with its readiness; the hooks' input and output; `token check`, `lookup`; `export` in [`export.go`](../../backend/internal/mcpcli/export.go) |
| [`cmd/cowork-mcp/main.go`](../../backend/cmd/cowork-mcp/main.go) | The linker's variables, the signal context, standard input for a hook when it is not a terminal |

## A tool

A tool is defined once, with its input as a Go type:

```go
type watchInput struct {
	Key string `json:"key"`
}

func watchTool() Tool {
	return define(Tool{
		Name:        "watch",
		Description: "Register the person's watch on a ticket …",
		Operations:  []string{"setInterest"},
	}, nil, func(ctx context.Context, s *Session, in watchInput) (string, error) { … })
}
```

- **The schema** is inferred from the type by `github.com/google/jsonschema-go`: `json` names
  the property, a `jsonschema` tag is its description, `omitempty` makes it optional, a struct
  refuses properties it does not declare. The second argument of `define` shapes it further —
  `enum` and `bound` for vocabularies and ranges, a pattern, a minimum length. `Call` validates
  the arguments against it before anything is sent; a schema the inference cannot make panics
  at start, and `TestTheCatalogue` builds every one.
- **The run** works only through `s.API`, the generated client of
  [`internal/api/apigen`](../../backend/internal/api/apigen/) (ADR 0040 D3), and answers Markdown
  that names the canonical key of what it touched (ADR 0042 D4). It returns an error for a
  failure: `check` turns an answer other than the wanted status into an `APIError`, rendered
  with the API's code, its message, the fields it named and — for `agent_forbidden` — that a
  refusal is the API's no; `usage` is a call the tool refuses itself, a key it cannot resolve; a
  `textError` (the `api` tool) is the answer as it is. A deleted ticket is a `404` to every tool, as
  a missing one is, and no tool deletes: through `api`, the deletion, the restoration and the purge
  meet the hard-off rule `deleting, restoring or purging` with any token
  ([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
  D7; `TestTheToolsNeverDeleteAndMissADeletedTicket`).
- **A creating `POST`** sends `IdempotencyKey: s.key()`, from the session's `NewKey` — a fresh
  UUIDv7 per act in `cowork-mcp`, a key derived from the conversation and the call in the chat; a
  write that overwrites reads the ticket first and sends its `ETag` in `If-Match`; a transition sends
  the state it read as `from`. A caller may pin what it read before: `transition` and `finish_work`
  take `from`, `set_progress` takes `version`, and the call is refused — or does nothing — when the
  ticket has moved on since (`TestPreconditionsTheCallerRead`). Nothing is retried on an answer.
- **`Operations`** lists every `operationId` the run calls. The start-up check refuses an
  installation whose document lacks one, and `TestEveryOperationOfAToolIsInTheDocument` holds
  them to this repository's document (ADR 0042 D6).
- **`limits`** — `limitsOf(text, capabilities…)` — is the part of the description that names
  the agent rules the tool can run into; `Describe(token)` appends which of the capabilities the
  agent holds — "This agent holds …; lacks …" —, the token's, read once at start (ADR 0043 D6), or
  in the chat the ones the person gave it, read at each turn (D5). `ReadToken` holds the set as
  `/me/token` answers it in `request.capabilities`.
- **`Surface`**: `Anywhere` for a tool that takes everything as arguments, `Terminal` for one
  that reads the working directory — today `session_start` alone. A host without a working
  directory takes `Catalogue(tools.Anywhere)`.

Short keys, `COW-12`, resolve against the session's binding; without one the tool asks for the
full key. A host that knows the binding — a page that shows a project — sets it with
`Session.Bind`, or `Session.BindTenant` for a tenant without a project. A session that runs in a
working directory and has no binding resolves it once, before its first tool call (`bindOnce` in
`Call`), so a short key works without `session_start` once the SessionStart hook said the session is
bound (`TestAToolCallBindsTheSessionOnce`); a failed or empty resolution is not tried again.

## The session start, the binding and the reminder

`Start` is one function with two callers, the hook and the tool (ADR 0067 D1):

1. `Resolve` reads the nearest `.cowork.yaml` (`GitWorkspace.BindingFile`, from the working
   directory up to the repository's root; a file naming another installation is ignored), the
   remotes (`git remote -v`, the fetch URLs, `origin` first, credentials removed), and the
   working directory's sub-directory, and asks `GET /api/v1/me/repositories/lookup`. A file
   binds when its project is found, and a server binding of another project is reported as
   drift; otherwise the server's one binding binds. The binding found is the session's.
2. Unbound: the block says why — no remote, several bindings, none — and carries the proposal
   with the exact `create_project` call. No remote and no file is silence for the hook.
3. Bound: the person's tickets `in-progress` in the project, in rank order. The first is active:
   its `/context` with five comments and ten acts, cut to the budget, then the commit strings and
   its page. None: the top five of "next for me" in the bound project — `GET /api/v1/me/next` with
   the binding's `tenant` and `project`, a page of 25, the person's open tickets and the unassigned
   ones by score ([ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D5) — passing
   over those in progress, blocked or waiting on an open prerequisite, each with its score and its
   place in its horizon of the backlog (`candidatesSection`).
4. With a previous start in the memory: the acts since then on the person's other tickets in
   progress, the count of them on the active one, and the project's tickets that changed.
5. The start is recorded in the memory — by the tool and the hook, not by `lookup`, and not
   after a compaction, which continues the session it compacts.

The block stays under `MaxBlock`, 9,000 characters: Claude Code hands a hook's output to the
model in one piece up to 10,000 and cuts above. `Remind` resolves the binding the same way and
answers its line when a ticket of the person is in progress, the repository shows work since the
recorded start — a commit, or a changed file whose modification is later — and the person has no
act on the ticket since then.

## The command line and the hooks

`mcpcli.Run(ctx, Env)` takes everything from its `Env` — arguments, the environment's lookup,
the streams, the build — and, for tests, a memory, an HTTP doer, a workspace and an MCP
transport in place of the real ones.

| Command | Contract |
|---|---|
| `serve` | Exits 1 at once on a configuration error, naming the variable; otherwise runs the server until the host closes standard input. A start-up check — the compatibility, then the token — that fails for good refuses every tool call with the reason; a failure to reach the installation is tried again at the next call (`readiness`) |
| `session-context` | Reads the hook's JSON on standard input (`session_id`, `cwd`, `source`, `model`), records the `model` for the server of the project directory ([below](#the-agent-mark)), prints the block on standard output, which Claude Code adds to the context; an unconfigured client or an unbound directory prints nothing; a failure prints one line naming the cause and the token page. Always exits 0, within a budget of 4.5 s |
| `session-end` | Prints `{"systemMessage": "cowork: …"}` when `Remind` has a line — a message to the person, which neither blocks nor continues the turn — and nothing otherwise, also on every error. Always exits 0 |
| `model-switch` | Reads the hook's JSON on standard input (`to_model`, `agent_id`) and records `to_model` for the server of the project directory ([below](#the-agent-mark)), as `session-context` records the `model`; an input with an `agent_id` — a subagent's switch — or without `to_model` records nothing, nor does an unconfigured client or one without `CLAUDE_PROJECT_DIR`. Talks to no installation. Prints nothing on standard output, which Claude Code adds to the model's context after a switch; a malformed variable or a failed write is one line on standard error, which Claude Code keeps in its debug log. Always exits 0 |
| `token check`, `lookup` | Results on standard output, `--json` for the structured form, exit 1 on an error |
| `export <tenant>/<PROJECT> <dir>` | Exit 2 on a malformed argument; refuses a target that is a file or a non-empty directory before it asks (exit 1); fetches the project export and unpacks it through a root at the directory — regular files only, only the names an export of the project holds, `O_EXCL`; on POSIX systems directories `0700` and files `0600`, on Windows the directory's access list —, printing the count of documents and of the confidential tickets left out ([import-and-export.md](import-and-export.md#cowork-mcp-export)); its requests carry `cowork-mcp/unknown/export` |

### The agent mark

Every request carries `X-Cowork-Agent: <client>/<model>/<session>`
([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3), built by `tools.AgentHeader`, which keeps each part to at most 64 printable ASCII characters
without `/` or a space and puts `unknown` for an empty model. The requests of the hooks carry `claude-code/<model>/<session id>` from the hook's
input. The server's carry the MCP client's name, the model and a short id of its own; MCP does not
tell a server its model, so the SessionStart hook hands it over
([ADR 0067](../adr/0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md)
D5): `session-context` writes the input's `model` with `Memory.SetModel` under `CLAUDE_PROJECT_DIR`
— the one value Claude Code gives both the hook and the server, which gets no session id — and
`client.header` reads it with `Memory.Model` at each request of `serve`, so a session started after
the server, or started again, is named. A switch — `/model`, an automatic fallback, `opusplan`
entering or leaving plan mode, the model Claude Code restores on a resume — fires
`PostModelSwitch`, and `model-switch` writes the input's `to_model` the same way, so the server's
next act names the new model; the event needs Claude Code 2.1.251 or later. A switch whose input
names an `agent_id` happened inside a subagent and is not recorded: the record is the main
session's, and a subagent's acts through the cowork tools carry it, whichever model the subagent
runs on. A session started with
`--agent` names an `agent_type` but no `agent_id`, and its switches are recorded. An input
without a model leaves the recorded one after
`/clear` or a compaction (`source` `clear`, `compact`), where the running Claude Code goes on with
its model, and records none after any other start — a session restored through conversation
recovery is not marked with an older session's model, and is named again once a switch names
one; a server without `CLAUDE_PROJECT_DIR` or a recorded model sends `unknown`. The last session
started or switched in a directory names the model of every server there.
`TestTheSessionStartHookNamesTheModelOfTheServer` runs the hook on the input Claude Code's
[hook reference](https://code.claude.com/docs/en/hooks) shows, on starts without a model, and the
server around it; `TestThePostModelSwitchHookNamesTheModelOfTheServer` runs the switch on the
input the reference describes, a subagent's switch, one without `to_model` and one in another
directory, each silent on standard output, and `TestTheHooks` its standard output empty when the
configuration is missing or malformed or the record cannot be written.

## The plugin

[`claude/cowork/`](../../claude/cowork/) is the plugin, [`.claude-plugin/marketplace.json`](../../.claude-plugin/marketplace.json)
at the root the marketplace that lists it: `.claude-plugin/plugin.json` (the two options, the
token one sensitive, and `hooks` naming the switch hook's file), `.mcp.json` (the server),
`hooks/hooks.json` (the `SessionStart` and `Stop` hooks, five seconds each, the options copied
into `COWORK_URL` and `COWORK_TOKEN`), `hooks/model-switch.json` (the `PostModelSwitch` hook, the
same way), `skills/{next,ticket,question,done}/SKILL.md`.
The formats are Claude Code's ([code.claude.com/docs/en/plugins-reference](https://code.claude.com/docs/en/plugins-reference),
[hooks](https://code.claude.com/docs/en/hooks)); `claude plugin validate ./claude/cowork` and
`claude plugin validate .` check them — `hooks/model-switch.json` only with a Claude Code that
opens the file `plugin.json` names: 2.1.288 does, 2.1.218 does not. The switch hook has a file of
its own because a Claude Code that does not know an event can refuse the whole hooks file that
names it, and `PostModelSwitch` came with 2.1.251: 2.1.218 loads none of the plugin's hooks from
a `hooks/hooks.json` with a `PostModelSwitch` entry, and with the separate file it runs the other
two and lists the file as failed to load (`claude plugin details`, `claude plugin list --json`).
A hook on an event that older Claude Code releases do not know belongs in a file of its own, not
in `hooks/hooks.json`. The plugin carries no version on purpose: users follow
`main`. A skill names tools by their short names, which every host shows, and grants no tool
permission of its own.

## Another host for the catalogue

The chat in the UI runs the same tools inside the backend
([chat.md](chat.md), [ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)):
the generated client over its loopback, which sends each call to the server's own handler through
`tools.HandlerDoer`, so every call runs the whole pipeline — authentication, the CSRF check, the
boundary, validation, the agent rules, the audit — with a request editor that sets the person's
session cookie and the chat's agent header, `chat/<model>/<conversation>`. It takes
`tools.Catalogue(tools.Anywhere)` without `api`, adds three page tools of its own through
`tools.Define`, offers each only once its `offered` names it, and runs every call at once;
`Session.Bind` or `BindTenant` for the page; no memory; `Assume` instead of `ReadToken`, the mark and
the person's chat capabilities being known, which `Describe` puts into the descriptions; `Person` for
`open_question`'s `me`; `Tenants` to keep a search of every tenant in the turn's. A tool added to the
catalogue is not offered by the chat until `offered` names it
([chat.md](chat.md#adding-a-tool-to-the-chat)).

## Adding a tool

1. Ask first whether it should be a tool: a new tool is an amendment of ADR 0042 D1, and a
   procedure a session needs often; `api` reaches every route already.
2. The route it needs exists in the API document and the server ([adding-things.md](adding-things.md#an-api-operation));
   `make generate` gives the client its method.
3. A `tool_*.go` function with `define`: the input type with `json` and `jsonschema` tags, the
   shaping, `Operations`, `limits` naming the rules it can run into, the run answering Markdown
   with the canonical key. Add it to `Catalogue`.
4. Unit tests against the fake API ([testing.md](testing.md#backend-unit-tests)): what it sends —
   method, path, query, the key on a `POST`, `If-Match` on an overwrite — what it answers, and a
   refusal of the API surfacing as an error with its code.
5. A step in [`test/integration/mcp_test.go`](../../backend/test/integration/mcp_test.go) that
   runs it through the server against the real API.
6. The tool's entry in the chat — `offered` in
   [`chat/chat.go`](../../backend/internal/chat/chat.go), or `TestEveryToolIsNamed` fails — as
   [chat.md](chat.md#adding-a-tool-to-the-chat) says.
7. The tool's row in [README.md, CLI (cowork-mcp)](../../README.md#cli-cowork-mcp), and ADR 0042's
   status.
