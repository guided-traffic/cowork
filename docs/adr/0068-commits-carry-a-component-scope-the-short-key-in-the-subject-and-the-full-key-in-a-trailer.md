# ADR 0068: Commits Carry a Component Scope, the Short Key in the Subject and the Full Key in a `Cowork-Ticket:` Trailer; Branches Default to `<type>/<KEY>-<n>-<slug>` and the Person May Name Another

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"commit and branch conventions?": component scope plus short key in the subject plus full
key in a trailer, over the full key as scope, over the trailer alone, and over the branch name
alone. The owner added that the branch pattern is the default and a person may name a
different branch; the rules of D4–D6 were put to the owner with the question and not objected
to. The convention binds every repository bound to cowork; cowork validates nothing of it in
the first release.

**Not built.** The conventions apply to repositories; the MCP tools that return the trailer
block do not exist.

## Context

[ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D2 made
the full key `tenant/KEY-n` the form cowork writes; where it sits in a commit was left to this
record. A commit has three readers: the person scanning `git log --oneline`, the changelog
semantic-release builds from scopes, and the machine — the coming GitHub integration — that
links commits to tickets. Each wants a different thing: the person a short mark, the
changelog the component, the machine the unambiguous full form. A branch lives in one
repository, and a repository is in one project of one tenant ([ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md)
D3), so a branch needs no tenant. The owner's global rule forbids apostrophes in commit
messages; it is referenced here, not restated.

## Decision

**D1 — The subject:** conventional commit type and **component** scope, the summary, and the
**short key** in parentheses at the end — `fix(controller): guard the failover gate (VKO-12)`.
Several tickets: several keys, comma-separated.

**D2 — The trailer:** one `Cowork-Ticket: <tenant>/<KEY>-<n>` line per ticket, in git trailer
syntax at the end of the body. The full form appears here and only here; the machine reads
the trailer, never the subject.

**D3 — Branches default to `<type>/<KEY>-<n>-<slug>`** — `feat/VKO-12-failover-gate` — with
`type` from the conventional commit set and no tenant. **The person may name any other
branch;** the default is what the agent proposes and uses when nobody says otherwise, and a
differently named branch carries the key in its commits all the same.

**D4 — The MCP tools hand the agent the block.** `finish_work`, `comment` and `get_ticket`
([ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md)) return the ready subject
suffix, the trailer line and the default branch name for the ticket, so the agent copies
rather than composes.

**D5 — Pull requests follow the commits:** the title is the squash commit's subject, the
body's first line is the full key, the trailer rides in the squash commit.

**D6 — cowork enforces none of this in the first release.** No commit hook, no API check;
the convention is the ground the GitHub integration (its own record) will stand on. The
CLAUDE.md template for bound repositories (the next record) states it.

## Consequences

- Every reader gets its form: the oneline log the short key, the changelog the component,
  the integration the full key.
- The key appears twice in a commit — short in the subject, full in the trailer — in two
  roles; that redundancy is the price of three readers.
- Branch names stay short and tenant-free; a person's own branch name is respected.
- The agent never composes the forms by hand; D4 makes the tools the source of the strings.

## Alternatives Considered

- **The full key as the scope.** Visible everywhere; the changelog groups by ticket instead
  of component, and thirty characters of prefix on every line. Lost.
- **The trailer alone.** Clean subjects and a machine-readable key; invisible in the oneline
  log. Lost.
- **The key in the branch name only.** No commit discipline; after a squash merge the branch
  and the key are gone. Lost.

## Residual risks

- A convention without enforcement drifts; the integration record decides whether a missing
  trailer becomes a check, and until then the tools' ready-made strings are the defence.
- Repositories with their own commit conventions (a client's) may conflict; the trailer is
  the part that must survive, the subject suffix can yield.

## References

- [ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D2, D3 — the full and the short form
- [ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md) D3 — one repository, one project, one tenant
- [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) — the tools that return the strings
- [`.releaserc.json`](../../.releaserc.json) — the changelog that reads the scope
