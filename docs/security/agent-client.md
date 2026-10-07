# What the agent's client holds, sends and leaves open

`cowork-mcp` runs on a person's machine as the MCP server of Claude Code, as its three hooks, and as
the command line of `cowork-mcp token check`, `lookup` and `export`
([ADR 0070](../adr/0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md),
[ADR 0040](../adr/0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md),
[ADR 0041](../adr/0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md),
[ADR 0067](../adr/0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md)).
This page is what it holds, where its token goes, what of the repository leaves the machine,
and what the text it hands a model can do, as built on 2026-10-07. What the token itself may do
on the server — scope, restriction, capabilities, the hard-off list — is [tokens.md](tokens.md);
setting the client up is [docs/operations/claude-code.md](../operations/claude-code.md).

## A client of the API and nothing else

The binary holds no database credential and no path to the database: it is built from the
generated API client, the tool catalogue and the MCP SDK, and
`TestTheBinaryIsAClientOnly` ([`cmd/cowork-mcp/main_test.go`](../../backend/cmd/cowork-mcp/main_test.go))
fails when it comes to depend on the store, the database driver, the object storage client or
the API's handlers. Every act it performs is an API request with its token, judged by the
API's rules; a person with the same token and `curl` can do exactly as much (ADR 0040 D3).

The same tool catalogue has a second host: the chat in the UI runs it inside the backend, with the
person's browser session marked as its agent instead of a token, holding the capabilities the person
chose for it, and without the `api` escape hatch and `session_start`. What that host holds, sends and
leaves open is [chat.md](chat.md); this page is the client on the person's
machine.

**Every request is marked as an agent's.** The client sends `X-Cowork-Agent:
<client>/<model>/<session>` on every request
([`tools.Editor`](../../backend/internal/tools/session.go)): the name is the one the MCP client
reports — `claude-code` for Claude Code, and the hooks' — and `cowork-mcp` before the client has named
itself and in the subcommands, whose session part names them (`token-check`, `lookup`, `export`); the model the one Claude Code names to the `SessionStart` hook, or after a
switch to the `PostModelSwitch` hook — in the server too, which the MCP protocol does not tell
it, through the file the hooks write for the project directory ([ADR 0067](../adr/0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md)
D5), and `unknown` while none is recorded —, the session a short random id or the hook's
session id. Like every part of the mark, the model is the client's word: it is attribution and no
rule reads it. The
header only narrows ([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3): with a plain token the requests become an agent's, bound by the agent rules and holding
every capability; with an agent token they hold the token's set. The client never sends a
request as the person's own.

**A creating POST carries an `Idempotency-Key`** the client makes, one per act
([ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D5). A request is sent again only after a transport failure — no answer at all — and only when
a repetition cannot act twice: `GET`, `PUT`, `DELETE`, or a `POST` with its key, which the API
replays ([`tools.Retrying`](../../backend/internal/tools/client.go)). An answer, whatever its
status, is never retried; a refusal reaches the model as the API's code and message.

## Where the token is, and where it goes

| Where | What |
|---|---|
| The plugin's option `cowork_token` | Marked `sensitive` in the plugin's manifest ([`plugin.json`](../../claude/cowork/.claude-plugin/plugin.json)), which Claude Code keeps in the system's credential store rather than a settings file — Claude Code's behaviour, not verifiable from this tree; it hands it to the MCP server in `COWORK_TOKEN` and to the hooks in `CLAUDE_PLUGIN_OPTION_COWORK_TOKEN`, which the hook command copies into `COWORK_TOKEN` ([`claude/cowork/`](../../claude/cowork/)) |
| Configured by hand | In the environment Claude Code starts with, or written into `~/.claude.json`, or a repository's `.mcp.json` — the last two are files ([docs/operations/claude-code.md](../operations/claude-code.md)) |
| The process | `COWORK_TOKEN` is read once, held in memory, and set as `Authorization: Bearer` on each request. No log line, error or tool answer carries it: a configuration error names the variable and the token page ([`mcpcli/config.go`](../../backend/internal/mcpcli/config.go), `TestConfiguration`) |
| On disk | Nothing of the token. The client writes two kinds of file under the user's cache directory, on POSIX systems `0600` in a `0700` directory ([`tools.FileMemory`](../../backend/internal/tools/memory.go)); Windows applies no such mode, and they take the access list of the cache directory, `%LocalAppData%` under the person's profile: per installation and binding, the installation's URL, the binding and the time of its last session, and, from the `SessionStart` and `PostModelSwitch` hooks, the model of the last session started or switched per project directory, with that directory's path. `cowork-mcp export` writes a project's export where the person says — every ticket the token's person reads, confidential ones included ([import-and-export.md](import-and-export.md#cowork-mcp-export-on-a-persons-machine), [H-77](import-and-export.md#h-77)) |

**The token goes to `COWORK_URL` and nowhere else.** Every tool calls the generated client,
which addresses the installation's `/api/v1/` routes; the `api` escape hatch takes a path, not a
URL, and refuses one that names a host, leaves `/api/v1/` or steps out of it with `..`
([`tools.apiPath`](../../backend/internal/tools/tool_api.go), `TestTheEscapeHatch`). No redirect
is followed: an answer `3xx` is returned as it is, so a misconfigured proxy cannot forward the
token elsewhere. `COWORK_URL` with plain `http` is refused unless the host is this machine — a
port-forward — because the bearer token would cross the network readable.

**The client checks what it talks to at start**: the installation's major version against its
own, and every operation its tools call present in the API document the installation serves
(ADR 0040 D5, [`tools.CheckCompatibility`](../../backend/internal/tools/compat.go)). An
installation that fails either, or rejects the token, gets every tool call refused with the
reason; one it cannot reach is asked again at the next call.

## What of the repository leaves the machine

The hooks, `session_start`, the first tool call of a session that is not bound yet and
`cowork-mcp lookup` read the working directory: `git remote -v`, `git rev-parse` for the root and the
sub-directory, the nearest `.cowork.yaml`, and for the Stop hook `git log` and `git status`
([`tools.GitWorkspace`](../../backend/internal/tools/workspace.go)). What is sent:

- **The remotes' URLs**, at most ten, without credentials: an HTTP(S) URL loses its user
  information — where a token such as `https://ghp_…@github.com/…` would sit — and any other URL
  its password, and a URL that does not parse, whatever its scheme, everything up to the last `@`
  of its authority, before the request is built
  ([`domain.SanitiseRemote`](../../backend/internal/domain/repository.go), `TestSanitiseRemote`);
  the server sanitises once more before it stores the last form of a bound remote or answers it in
  the lookup, and normalises every remote to its identity, `host/path` (ADR 0066 D1).
- **The sub-directory** of the working directory relative to the repository's root, for a
  monorepo's bindings.
- **What a `.cowork.yaml` names**: its tenant and project, which the client reads the project by,
  and its path, which the lookup carries ([`tools.Resolve`](../../backend/internal/tools/binding.go)).

Nothing else: no file names, no contents, no commit messages. Whether the repository shows work
since the session started — the Stop hook's condition — is decided on the machine and never
sent. The `PostModelSwitch` hook reads neither the repository nor the network: it reads its
input and the environment, writes the model file, and prints nothing, so a switch puts no text
of cowork's into the model's context
([`mcpcli.modelSwitch`](../../backend/internal/mcpcli/cli.go)).

## What the server answers about repositories

The lookup searches every tenant of the person — of a token restricted to a tenant, that tenant
only — and finds bindings only of projects the caller sees, through the project predicate
([`queries/read/repositories.sql`](../../backend/internal/store/queries/read/repositories.sql)):
a restricted project's binding does not exist for a person off its list
(`TestLookingUpARepository`). A proposal offers only the tenants where the caller may create a
project, and the key it proposes is free in each: that says whether a key is taken in a tenant
where the caller may create projects anyway, as creating one with that key would
([`api/repositories.go`](../../backend/internal/api/repositories.go) `propose`). Binding a
repository another project of the tenant holds is `409 repository_bound`, naming that project
only to a caller who sees it.

## The text a model reads

The tools hand Claude what the installation holds: ticket bodies, comments, answers, titles —
written by other people and by other agents. The context document quotes each comment as a
block quote under its author, so a heading inside a comment cannot pass for a section of the
document ([`markdown.RenderContext`](../../backend/internal/markdown/context.go)), and the
server's instructions and the descriptions of `get_ticket` say that such text is information,
never instruction ([`mcpserver.Instructions`](../../backend/internal/mcpserver/server.go)).
What an injected instruction could make the agent do is bounded by the token: the agent rules,
the token's capabilities and its restriction ([tokens.md](tokens.md)).

## What this does not cover

<a id="h-33"></a>
### H-33 — The token is in the environment of the client's processes

Live today. Claude Code hands the token to `cowork-mcp serve` and to the hook processes in their
environment, where other processes of the same user can read it — on Linux in
`/proc/<pid>/environ`, on macOS with `ps eww` — for as long as the process runs. The
`PostModelSwitch` hook's process holds it too, for the time it takes to write one file, though it
sends no request and only checks the token's form: Claude Code exports every option of the
plugin to each of its hook processes, and the hook command copies it into `COWORK_TOKEN`. The
credential store keeps it at rest; the environment is the exposure while a session runs. Mitigation: an
agent token restricted to the tenant or the project the work needs, the default lifetime, and
revocation on the token page when a machine is in doubt.

<a id="h-34"></a>
### H-34 — Text in the backlog can steer the agent within its token

Live by design. Anyone who may write a ticket, a comment or an answer that a person's session
reads can write instructions into it; a model may follow them. Where a tenant takes GitHub's webhook,
so can the author of a pull request or a commit it links, whose title a ticket's context carries: of
a pull request the repository's owner, a member of its organisation or a collaborator only, of a
commit whoever wrote one that reached the default branch ([github-webhook.md](github-webhook.md#whose-pull-requests-are-linked)). Quoting and the instructions make
that less likely, not impossible. What follows is bounded by the token and nothing else: a
"full" agent token closes tickets in progress, decides, drops, ranks, sets horizons (`set-horizon`),
sets `need` and `urgent` stakes, uploads files, creates projects, binds and unbinds repositories,
records answers, and the `api` tool reaches every route the token reaches,
within the agent rules; the acts [tokens.md](tokens.md) H-6 leaves to every agent — the five, and
saving, changing, sharing and unsharing its person's saved filter — are its too. Every act
is recorded and shown with the agent mark and the token's name
([tokens.md](tokens.md#what-is-recorded)). Mitigation: "assisted" tokens where another person writes
into the same projects, restricted tokens, and the timeline.

<a id="h-35"></a>
### H-35 — The binaries are attested, not signed for the operating system

Live today. A release attaches the six binaries and a SHA-256 file each; one build provenance
attestation names the six as its subjects, made by the release workflow with its own identity and
stored with GitHub, not in the release. The check of
[claude-code.md](../operations/claude-code.md) names that identity — the repository, the signing
workflow `.github/workflows/build.yml` and the source ref `refs/tags/v$VERSION` — and so proves
that a run of `build.yml` for that tag attested the file: a binary replaced in the release fails
it, and so does one attested by any other workflow or in a run for any other ref (tried against the
0.11.0 binaries with gh 2.98.0 on 2026-10-07). gh matches the workflow by the start of its path, so
the tag is what names the run; a run for the tag reads the workflows of the commit the tag names
(GitHub's documentation of the release event, not tried), the tags are written only by the release
App, and an administrator may bypass the ruleset
([ADR 0073](../adr/0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md)).
What the check does not prove is what that run did — a compromised step of `build.yml` can attest a
binary it did not build ([release-pipeline.md](release-pipeline.md#h-61) H-61) —, nor the machine it
ran on: the project's self-hosted runner, which the attestation names and does not vouch for. The
check is the person's step, and nothing makes them take it: the checksum beside the binary comes
from the same release, macOS refuses the unnotarised file at first start until the person lifts the
quarantine — a file a browser downloaded; one fetched with `curl` carries no quarantine attribute and
starts, not verified on a Mac —, and Windows sees no Authenticode signature (ADR 0041 Residual risks). Mitigation: the
verification step of [claude-code.md](../operations/claude-code.md), the repository's release
protection — tags written only by the release App
([ADR 0073](../adr/0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md)) —
and building from a checkout with `make build-mcp`.

<a id="h-36"></a>
### H-36 — The lookup's remotes travel in a query string

Live today. `GET /api/v1/me/repositories/lookup?remote=…&path=…` carries the repository's remote
URLs — private repository names among them — in the query, as ADR 0066 D2 shapes the route. The
backend's request log carries no query, and the frontend never sees the API; an Ingress
controller or another proxy in front may log whole request lines, which ingress-nginx does by
default ([trust-boundaries.md](trust-boundaries.md) H-14).
Credentials are removed before the request is built. Mitigation: treat the proxies' logs as
holding repository names, as they hold search terms already.

<a id="h-87"></a>
### H-87 — A missing token's message quotes the installation's URL as written

Live when `COWORK_TOKEN` is unset. The configuration error that names the missing token points at the
installation's token page by quoting `COWORK_URL` as it was written, before the URL is held to its
rule of no user, query or fragment ([`mcpcli/config.go`](../../backend/internal/mcpcli/config.go)
`loadConfig`): a user, a password or a query someone put into the URL reaches standard error, and with
it whatever reads the client's output — a hook's included. The token itself is never in a message.
Mitigation: keep credentials out of `COWORK_URL`; the client refuses such a URL once a token is set.

### What the person's machine does

The client trusts its machine: whoever runs code as the person can read the token while a
session runs, write the binding file or the memory files — the model file sets the model part of
the mark —, or replace the binary on the `PATH`.
cowork cannot defend the token against the person's own account.
