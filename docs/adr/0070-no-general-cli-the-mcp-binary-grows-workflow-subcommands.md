# ADR 0070: No General CLI — the MCP Binary Grows Workflow Subcommands (`export`, `token check`, `lookup`) and Nothing Generic

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "a CLI
beside the MCP server?": workflow subcommands on the MCP binary, case by case, over no CLI at
all, over a full command-line client, and over a third-party OpenAPI CLI. The rules of D4–D6
were put to the owner with the question and not objected to.

**Not built.** No `cowork-mcp`.

## Context

The MCP binary already has one-shot modes for the hooks and its version
([ADR 0067](0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md),
[ADR 0041](0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md)
D2); the API is callable with `curl` from its served OpenAPI document ([ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md)
D5); import and export are API routes ([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)).
The owner's terminal operator is Claude, not `curl`; what a person still does without a
model is operating and troubleshooting: fetching an export for a backup or a move, finding
out why a token does not work, checking which project a repository is bound to. A full
command-line client would be a third product beside the UI and the MCP server for a way of
working the owner does not have.

## Decision

**D1 — There is no general command-line client.** No `cowork tickets list`, no generated
command tree over the API. People use the UI; agents use the MCP tools; scripts use `curl`
with the OpenAPI document.

**D2 — The MCP binary grows subcommands case by case, each justified by a workflow step a
person or an operator performs without a model.** The first release ships three beside the
hook modes:

| Subcommand | Does |
|---|---|
| `cowork-mcp export <tenant>/<KEY> <dir>` | fetches the project export ([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4) and unpacks it into an empty or non-existent directory — for backups and moves without `curl` and `tar` |
| `cowork-mcp token check` | reports whether `COWORK_TOKEN` is valid against `COWORK_URL`, whose it is, its scope, tenant and project restriction, capabilities and expiry — the first step of every troubleshooting |
| `cowork-mcp lookup` | prints the binding of the working directory's remotes ([ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md) D2) or the proposal, without starting a session |

**D3 — The rule for a new subcommand:** it exists only if a session or an operator needs it
without a model, it is named in an amendment of this record, and it is implemented on the
same generated client and the same environment (`COWORK_URL`, `COWORK_TOKEN`) as the server
mode. "A model could do it through `api`" is a reason against, not for.

**D4 — Output discipline.** Results to standard output, diagnostics to standard error, exit
codes `0` success, `1` error, `2` usage — as the backend binary has them; `--json` where a
result is structured.

**D5 — `export` never overwrites.** It refuses a non-empty target directory; the archive is
written as files, named by key, plus the manifest.

**D6 — The subcommands are tested in the integration tier** like the tools
([ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) D6), against the fixture
environment, by running the binary.

## Consequences

- One binary, one distribution, one client library; the subcommands cost a `switch` on the
  first argument and the shared code.
- An operator has the three things a person reaches for at a terminal, and `curl` for the
  rest.
- The boundary question — "workflow or CLI?" — is answered by D3 per proposal, in writing.

## Alternatives Considered

- **No CLI beyond the hook modes.** Nothing to maintain; an operator left with `curl` and
  `tar` for an export and no quick token check. Lost.
- **A full command-line client** over the API. Scriptable and readable; help texts, output
  formats, pagination, error rendering — a product nobody here uses. Lost.
- **A third-party OpenAPI CLI** (`restish`, `httpie`) documented against the served
  document. A generic client without own code; another tool to install and configure auth
  for. Lost.

## Residual risks

- D3 is a rule for future conversations; the record of amendments is where it is kept
  honest.
- `export` through the binary carries a whole project's text through the token's reach; it
  is recorded like every export ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)
  D5).

## References

- [ADR 0041](0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md), [ADR 0067](0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md) — the binary and its existing one-shot modes
- [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4, [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md) D2, [ADR 0035](0035-personal-access-tokens.md) — what the three subcommands reach
- [ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md) D1, D6 — the API as the one contract, `curl` for the rest
