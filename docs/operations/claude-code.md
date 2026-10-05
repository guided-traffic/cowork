# Claude Code against an installation

How a person sets up Claude Code to work from a cowork installation: a session in a bound
repository starts with its ticket in context, Claude records its work through the cowork tools,
and the end of a session reminds of a ticket left standing. Four pieces make it: the
`cowork-mcp` binary, a personal access token, the Claude Code plugin of this repository (or
the same configuration by hand), and in each repository usually nothing — a repository is
found by its git remote. The decisions behind it are
[ADR 0040](../adr/0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
to [ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
and [ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
to [ADR 0070](../adr/0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md); the
subcommands and variables are [README.md, CLI (cowork-mcp)](../../README.md#cli-cowork-mcp);
what the client holds and leaves open is
[docs/security/agent-client.md](../security/agent-client.md). The other way to work with a model
is the assistant in the UI, which runs the same tools in the backend with the person's browser
session: [chat.md](chat.md).

```
 laptop                                                         cluster
┌──────────────────────────────────────────────┐             ┌──────────────┐
│ Claude Code                                  │             │ cowork       │
│  ├─ SessionStart hook → cowork-mcp session-context ──┐     │  frontend    │
│  ├─ MCP server        → cowork-mcp serve (stdio) ────┼────►│  /api/v1/…   │
│  └─ Stop hook         → cowork-mcp session-end ──────┘     │  backend     │
│ the repository: git remotes, maybe .cowork.yaml    HTTPS + │              │
│ the token: the system's credential store         token     └──────────────┘
└──────────────────────────────────────────────┘
```

## 1. The binary

Every release attaches `cowork-mcp` for Linux, macOS and Windows on amd64 and arm64, each with
a `.sha256` file, to its [GitHub release](https://github.com/guided-traffic/cowork/releases):
`cowork-mcp-<version>-<os>-<arch>` (`.exe` on Windows). Take the release the installation
runs — `cowork-mcp` compares its major version with the installation's at start and refuses
an API it does not know.

```bash
VERSION=0.3.0                      # example: the installation's release
OS=darwin ARCH=arm64               # example
curl -fsSLO https://github.com/guided-traffic/cowork/releases/download/v$VERSION/cowork-mcp-$VERSION-$OS-$ARCH
curl -fsSLO https://github.com/guided-traffic/cowork/releases/download/v$VERSION/cowork-mcp-$VERSION-$OS-$ARCH.sha256
shasum -a 256 -c cowork-mcp-$VERSION-$OS-$ARCH.sha256
gh attestation verify cowork-mcp-$VERSION-$OS-$ARCH --repo guided-traffic/cowork   # built by the release workflow of this repository
install -m 0755 cowork-mcp-$VERSION-$OS-$ARCH ~/.local/bin/cowork-mcp   # any directory on the PATH
cowork-mcp version
```

The checksum detects a damaged download; the attestation proves that the release workflow of this
repository built the file from the tagged commit, which a replaced release cannot fake. On macOS the
binary is not notarised, and the first start of a downloaded file is refused by Gatekeeper; after
the attestation is verified, `xattr -d com.apple.quarantine ~/.local/bin/cowork-mcp` lifts that once. Updates
are the same steps with the next release. `make build-mcp` builds the binary from a checkout
instead.

## 2. The token

Make the token in the UI, on **Your tokens** (`<installation>/me/tokens`): only a person in a
browser session makes a token, and the plaintext is shown once
([ADR 0035](../adr/0035-personal-access-tokens.md) D1, D5). Its name shows beside everything it does,
to everyone who reads the ticket
([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D6): name it for its use, `claude on my laptop`, not for what others must not read.

| Choice | Recommended | Why |
|---|---|---|
| Agent token | yes | Every request is an agent's whatever the client sends, and the record says so ([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)). `cowork-mcp` marks its requests as an agent's in any case, so a plain token works too — and then holds every capability |
| Scope | `write` | An agent token has at most `write`; `read` makes the session a reader that cannot record its work |
| Capabilities | **full** for your own work, **assisted** where a person keeps `decide`, `close`, `rank`, `create-project` and `record-answer` | They are fixed with the token ([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)); the tool descriptions tell Claude which ones this token holds, and `finish_work` stops at review without `close` |
| Restriction | a tenant, or a project where the work is one project | A leaked token then reaches no more than that. A project-restricted token cannot create a project, so a session in an unbound repository gets no proposal |
| Lifetime | the default, 90 days | `last used` on the token page shows a forgotten one |

Check it before anything else — the first step of every troubleshooting
([ADR 0070](../adr/0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md) D2):

```bash
COWORK_URL=https://cowork.example.com COWORK_TOKEN=cwk_… cowork-mcp token check   # example values
```

It says whose the token is, its scope, restriction, capabilities and expiry, and exits 1 with
the installation's answer when the token does not work. Typing the token on a command line
puts it in the shell's history; read it from a password manager or the clipboard instead, or
clear the line.

`COWORK_URL` is the URL the browser shows. `http://` is accepted only for this machine — a
port-forward, `http://localhost:8080` — because the token would otherwise cross the network in
plain text.

## 3. The plugin

The repository is a Claude Code plugin marketplace with one plugin, `cowork`
([`claude/cowork/`](../../claude/cowork/)): the MCP server, the two hooks and the four skills.

```bash
claude plugin marketplace add guided-traffic/cowork
claude plugin install cowork@cowork
```

Then, in a session, `/plugin configure cowork@cowork` opens the dialog for the two options:
`cowork_url`, the installation, and `cowork_token`, the token. The token is a sensitive option:
Claude Code keeps it in the system's credential store, not in a settings file. Restart Claude
Code afterwards.

| Part | File | Does |
|---|---|---|
| MCP server `cowork` | [`.mcp.json`](../../claude/cowork/.mcp.json) | Starts `cowork-mcp serve` with `COWORK_URL` and `COWORK_TOKEN` from the two options. In `/mcp` it shows as `plugin:cowork:cowork`, its tools as `mcp__plugin_cowork_cowork__<tool>` — the names permission rules use |
| Hooks | [`hooks/hooks.json`](../../claude/cowork/hooks/hooks.json) | `SessionStart` runs `cowork-mcp session-context`, `Stop` runs `cowork-mcp session-end`, each with a five-second timeout ([ADR 0067](../adr/0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md)). They take the options, or `COWORK_URL` and `COWORK_TOKEN` from the environment when the options are empty |
| Skills | [`skills/`](../../claude/cowork/skills/) | `/cowork:next` (the next ticket), `/cowork:ticket <key>` (load a ticket), `/cowork:question` (one decision for the person), `/cowork:done` (finish with a verification note); each also answers to its bare name while no other command has it |

The plugin carries no version, so `claude plugin update cowork@cowork` follows the repository's
`main`; the binary is updated by hand (above). Without the binary on the `PATH` the MCP server
fails to start and each hook reports `command not found` once per event.

## The same configuration by hand

Without the plugin, the MCP server is a user-scoped server — Claude Code keeps those in
`~/.claude.json`, which `claude mcp add` writes — and the hooks are a block of
`~/.claude/settings.json`. Both read the two variables from the environment Claude Code starts
with:

```bash
export COWORK_URL=https://cowork.example.com     # example, in the shell profile
export COWORK_TOKEN=cwk_…                        # example; see the note below
claude mcp add --scope user cowork --env 'COWORK_URL=${COWORK_URL}' --env 'COWORK_TOKEN=${COWORK_TOKEN}' -- cowork-mcp serve
```

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "cowork-mcp session-context", "timeout": 5 } ] }
    ],
    "Stop": [
      { "hooks": [ { "type": "command", "command": "cowork-mcp session-end", "timeout": 5 } ] }
    ]
  }
}
```

A token exported in the shell profile is in the environment of every process the shell starts;
one written into `~/.claude.json` is a plain-text file. The plugin's credential store is the
better place. A per-repository `.mcp.json` with the same entry works as well, but it is a file
in the repository: commit it only with `${COWORK_TOKEN}`, never with the token
([ADR 0041](../adr/0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md)
Residual risks). Use the plugin or this block, not both: the hooks would run twice.

## What a session does

| When | What happens |
|---|---|
| The session starts | `session-context` reads the git remotes of the working directory and a `.cowork.yaml`, asks the installation which project binds the repository, and prints the block Claude reads before the first prompt: the binding, the active ticket — assigned to you and `in-progress` — with its context, or the top of "next for me" in the bound project — your open tickets and the unassigned ones, by score, without those in progress, blocked or waiting on a prerequisite —, and what happened since the last session. In a directory without a remote and without a binding file it prints nothing. A failure is one line naming the cause and the token page; the session is never blocked |
| An unbound repository | The block carries a proposal — tenant, key, name — and Claude asks you; on your yes it calls `create_project`, which creates the project and binds the repository in one act |
| During the work | The 16 tools of [README.md, the tools](../../README.md#cli-cowork-mcp); `session_start` refreshes the block |
| Claude stops | `session-end` reminds you — a message in the transcript, never a block — when a ticket of yours is in progress, the repository shows work since the session started (a commit, or a file changed after it), and nothing was recorded on the ticket since |

The time of the last session is kept per installation and binding in one small file under the
user's cache directory (`~/Library/Caches/cowork-mcp/` on macOS, `~/.cache/cowork-mcp/` on
Linux); it holds timestamps only, and deleting it only makes the next block show no "since".

## Each repository

**Nothing, as a rule.** A repository is found by its remote, whatever form the clone uses —
`git@github.com:acme/app.git`, `https://github.com/acme/app` and `ssh://git@github.com:22/acme/app/`
are one repository ([ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D1). A project's repositories are listed and changed in the API
(`…/projects/{project}/repositories`), and a session proposes the binding when none exists.

**The `CLAUDE.md` block** ([ADR 0069](../adr/0069-rules-stay-in-git-work-moves-to-cowork.md)
D4): three lines that tell a session where the work lives, with the tenant and the key filled
in:

```markdown
## Work and rules

- The work of this repository — tickets, their open questions, the plan — lives in cowork under `acme/APP`.
- Decisions and documentation live here, beside the code: ADRs in `docs/adr/`, how it works in `docs/developer/`, running it in `docs/operations/`, the security architecture in `docs/security/`; nothing here cites a ticket.
- Commits end the subject with the short key, `fix(api): guard the gate (APP-12)`, carry the trailer `Cowork-Ticket: acme/APP-12`, and use the branch `<type>/APP-12-<slug>` unless the person names another.
```

**`.cowork.yaml`**, only where the remote cannot bind: a repository without a remote, or a fork
that works on the original's project. When it is present it wins, and a disagreement with the
server's binding is reported in the block ([ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D4). The nearest file at or above the working directory applies. Its schema is served at
`<installation>/api/v1/schemas/cowork-yaml.json`:

```yaml
# yaml-language-server: $schema=https://cowork.example.com/api/v1/schemas/cowork-yaml.json
tenant: acme                        # example
project: APP                        # example
# path: services/api                # example: the monorepo sub-directory it binds; default the file's own directory
# url: https://cowork.example.com   # example: the installation it belongs to, for people with several
```

A fork that has the original as a second remote (`upstream`) needs no file: the lookup tries
every remote, `origin` first, and binds by the first one with a binding.

## When something does not work

| Symptom | Check |
|---|---|
| No block at the start of a session | `cowork-mcp lookup` in the repository says what the binding is and why; `claude --debug` shows the hook's run |
| `cowork: the token in COWORK_TOKEN does not work …` | `cowork-mcp token check`; make a new token on the token page |
| `… runs cowork X, another major version …` | Install the `cowork-mcp` of the installation's release |
| The MCP server is `failed` in `/mcp` | The two variables reach it: `COWORK_URL` set, the token of the form `cwk_…`; its message is in Claude Code's MCP log |
| A tool answers `403 agent_forbidden` | The token lacks the capability, or the act is a person's; the answer names which. It is the API's no, not a failure |
| Several projects bind the repository | A data error the lookup reports: unbind all but one (`DELETE …/projects/{project}/repositories/{repository}`) |

Not verified, and this is the gap: the hooks on Windows, which run their command through Git
Bash; the binaries for Windows are built, but no session on Windows has used them.
