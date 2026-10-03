# ADR 0006: A Project Is the Backlog Unit of a Tenant and Owns Its Repositories

## Status

Accepted, amended 2026-10-01 (D3: the repository's identity is its normalised remote and the
server's binding is primary, the file the optional override —
[ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)).
Date: 2026-09-29. Decided by the owner as the answer to the catalog question "what is a
project?": a project is a piece of work with its own backlog inside a tenant, not a git
repository, and it owns zero or more repositories.

**Partly built** (phase 2, 2026-10-02): D1, D2 and D4 — `projects` (migration 3) with key,
name, description and WIP limits, created, listed, edited and archived through the API; the
repositories of D3 arrive with the repository binding of
[ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md).

## Context

The owner's working tree holds some two hundred repositories. Many of them are not an
undertaking of their own (a module, an image, a fork); several undertakings span more than
one repository (an operator with its chart and its documentation, a home lab across
manifests and tooling). The overview the owner loses is the overview of *undertakings*, and
a backlog tool that mirrors the repository structure reproduces exactly the fragmentation
it should remove.

A project is also the level three other decisions attach to: the ticket key's namespace, the
visibility rules inside a tenant, and the file in a repository that tells an LLM session
which backlog it is working from.

## Decision

**D1 — A project is the backlog unit of a tenant.** It has a key, a name, a description, a
ranked backlog of tickets and a board; it belongs to exactly one tenant
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1) and never
moves.

**D2 — The key is unique within the tenant and is the namespace of the ticket keys.** Its
format, and the ticket key format built on it, are decided in the record on ticket identity.

**D3 — A project owns zero or more repositories, and a repository belongs to at most one
project.** A repository is an attribute of the project: ~~its URL~~ *(amended 2026-10-01: its
normalised remote identity, ADR 0066 D1)*, and optionally a path within it for a monorepo.
~~The binding file in the repository (`.cowork.yaml`, named in the workflow plan) states the
tenant slug and the project key and is the reverse pointer; both sides are kept~~ *(amended:
the server's binding is primary and found by remote at session start; `.cowork.yaml` is the
optional override for remote-less repositories and forks, ADR 0066 D4)*, and a disagreement
is reported, not resolved silently. The "at most one" rule is what makes "the active ticket
of this repository" a single answer for a session that starts in it.

**D4 — A project is archived, never deleted.** An archived project keeps its tickets, its
keys and its history, disappears from the default lists and refuses new tickets. The general
delete rule is decided in the data record; this record only says that a project's tickets
do not vanish with it.

## Consequences

- The backlog follows the undertaking, not the repository layout: a repository split or
  merge changes the binding list, nothing else.
- Choosing a project for a repository is a decision per repository, made once, in one file.
  A repository nobody has bound has no backlog in cowork — that is the honest state, not a
  fallback project.
- Project-level visibility (the roles question) has a level to attach to.
- An installation with one tenant and one undertaking still has one project; there is no
  "tenant without projects" mode, in line with ADR 0005 D6's "no special modes".

## Alternatives Considered

- **A project is one repository.** Trivial binding, a key per repository name. Lost: dozens
  of backlogs, an undertaking across three repositories split into three priority lists, and
  repositories without an undertaking forced to have or lack a project.
- **No projects; tickets in the tenant, labels for structure.** The least model. Lost: no key
  namespace (keys would be tenant-wide), no ranked list per undertaking, no level for
  visibility rules, and no board "per project", which the founding brief asks for.
- **A repository in several projects.** Would let a shared library appear in every backlog
  that touches it. Lost to D3: it makes the session-start question ambiguous, and a shared
  library is one project with tickets linked from the others.

## Residual risks

- The binding is two-sided (project attribute and repository file) and can drift; D3 says the
  drift is reported. How and where is the workflow plan's to build.
- Nothing is built; every rule above describes what will be built.

## References

- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) — the tenant a project belongs to
- [ADR 0004](0004-cowork-is-a-team-product.md) — the people who work in a project
- [docs/planning/vscode-workflow.md](../planning/vscode-workflow.md) — the repository binding
