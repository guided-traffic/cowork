# ADR 0041: The MCP Server Speaks stdio and Ships as a Release Binary per Platform; Streamable HTTP Is Its Own Record When a Session Needs It

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "MCP
transport?": `stdio` now, with streamable HTTP as a separate record when a session runs
where no local process can start — over `stdio` alone without that door, over streamable HTTP
now, and over a container image run as the stdio process. The rules of D4–D7 were put to the
owner with the question and not objected to.

**Built** (phase 5, 2026-10-04): D1 — stdio through the MCP Go SDK, standard error for
warnings, never the token; D2 as amended — the job `release-mcp` of the release workflow, and
`make build-mcp`; `cowork-mcp version` prints the version, the commit, the build time and the
number of API operations its tools call. D3 as amended, in
[docs/operations/claude-code.md](../operations/claude-code.md) with the plugin in `claude/cowork/`.
D4 as amended. D5 — the start-up message names the variable and the token page; a missing
variable ends `serve` with exit 1, a rejected token answers every tool call with it. D6 — the
tools in `internal/tools` know no transport and run as well over an in-process doer. D7 — no
image.

## Context

[ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
made the MCP server a thin, environment-configured client of the API that lives in this
repository. Claude Code starts an MCP server natively as a child process over `stdio` and
passes it an environment; that is the transport that needs nothing hosted, works against a
port-forward as well as against the public URL, and keeps the token on the laptop.
Streamable HTTP would make cowork an OAuth resource server, add a third container and an
Ingress path, and leave open how a session in the cloud reads a repository's binding — for a
case the owner does not have today. The MCP Go SDK separates transport from tool handlers,
so choosing `stdio` now closes nothing.

## Decision

**D1 — The transport is `stdio`.** Claude Code (or any MCP host) starts `cowork-mcp` as a
child process with `COWORK_URL` and `COWORK_TOKEN` in its environment; the process lives as
long as the session. Standard output is the MCP channel; logging goes to standard error and
never contains the token.

**D2 — The binary ships as a release asset per platform.** The release workflow cross-compiles
`cowork-mcp` for `darwin/arm64`, `darwin/amd64`, `linux/amd64` and `linux/arm64` *(amended
2026-10-04: and `windows/amd64` and `windows/arm64`, as `.exe`; no session on Windows has used
them)*, attaches each with a SHA-256 file to the GitHub release, and `cowork-mcp version` prints the version,
the commit and the API version it was built against ([ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
D5).

**D3 — Installation is one copy and one configuration entry.** The operations page gives the
~~`~/.claude/settings.json` (user-wide)~~ *(amended 2026-10-04: user-wide, which Claude Code
keeps in `~/.claude.json` and `claude mcp add --scope user` writes — `~/.claude/settings.json`
holds the hooks of [ADR 0067](0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md);
first of all the Claude Code plugin of this repository, which starts the server and keeps the
token in the system's credential store)* and `.mcp.json` (per repository) forms; both set the
command and pass the two variables. Updates are manual in the first release.

**D4 — The server reads the repository binding from its working directory and nowhere
else.** ~~The MCP host starts the process in the repository root; `.cowork.yaml` there names
the tenant and the project ([ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md)
D3).~~ *(amended 2026-10-04, after [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D3 and D4: the working directory — `CLAUDE_PROJECT_DIR` when the host sets it, a hook's own
`cwd` — gives the git remotes the server's lookup binds by, and an optional `.cowork.yaml`, the
nearest at or above it within the repository, which wins.)* A missing or malformed binding is
reported by the session-start tool, not guessed.

**D5 — A missing or rejected token is a clear start-up message,** naming the variable and
the token page of the installation, never the token value.

**D6 — The transport is a swappable layer.** Tool handlers take no dependency on `stdio`;
adding streamable HTTP is a new transport and a new record (OAuth, a container, an Ingress
path, the binding question), not a rewrite.

**D7 — No container image for the MCP server in the first release.** A `docker run` as the
stdio process would need Docker on every laptop and the repository mounted into it; a static
Go binary needs neither.

## Consequences

- Nothing to host; the token stays on the developer's machine, bounded by
  [ADR 0035](0035-personal-access-tokens.md) D3–D4.
- The release workflow gains a cross-compile matrix and asset uploads with checksums; the
  release page lists four binaries beside the two images and the chart.
- Updates are a manual download until a `self-update` or a package manager formula exists —
  an amendment when the owner tires of it.
- A session that runs in the cloud has no MCP access in the first release and falls back to
  the API through the OpenAPI document (ADR 0040 D6).

## Alternatives Considered

- **Streamable HTTP now.** Central updates, cloud sessions; OAuth in cowork, a third
  container, and an unanswered binding question for remote sessions. Deferred to its own
  record by D6.
- **A container image as the stdio process.** No per-platform binaries; Docker on every
  laptop and the repository mounted into the container. Lost to D7.
- **`stdio` with no door to HTTP.** Same as this record in practice; D6 names the door so the
  handlers are written transport-free from the start.

## Residual risks

- The token in a local configuration file is the credential most likely to leak; expiry,
  scope and the `cwk_` prefix are the mitigations, and the operations page says to prefer the
  per-repository `.mcp.json` only when the repository is private.
- Cross-compiled binaries without code signing produce a warning on macOS at first start;
  the operations page gives the one-time step. Signing is an amendment.

## References

- [ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md) D2, D4, D5 — the server this record transports
- [ADR 0035](0035-personal-access-tokens.md) — the token in the environment
- [ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md) D3 — the binding read from the working directory
- [`.github/workflows/build.yml`](../../.github/workflows/build.yml) — the release workflow the matrix joins
