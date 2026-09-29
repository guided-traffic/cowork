# Architecture Decision Records

Every durable decision of this project lives here, one file per decision family. An ADR
records **what was decided, why, what was rejected, and what it costs** — so a later change
can argue with the decision instead of rediscovering it.

## Format

Filename: `NNNN-kebab-case-title.md`, numbered in the order they were written.

Sections, in this order:

| Section | Content |
|---|---|
| `# ADR NNNN: Title` | The decision as a title, not a topic |
| `## Status` | `Accepted` / `Superseded by ADR NNNN` / `Amended`, plus `Date:` and what is actually implemented versus open |
| `## Context` | The forces and the concrete failure that made the decision necessary |
| `## Decision` | `D1 … Dn`, each a rule that holds going forward, in present tense |
| `## Consequences` | What this costs, including the parts nobody likes |
| `## Alternatives Considered` | Each option and why it lost |
| `## Residual risks` | Accepted risks, open items, and what was **not** verified |
| `## References` | Relative links to the code and to sibling ADRs |

Ground rules: English only; every claim verified against the code, with unverified statements
marked as such; identifiers (`functions`, environment variables, constants) quoted exactly so
the ADR stays checkable against the tree. An ADR here may link into the code, and it cites no
ticket — the rule is [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md).

## Keeping them current

**An ADR is part of the code, not a historical note.** When a decision changes, the ADR is
updated in the same change — the `Decision` section states the new rule, the `Status` records
the amendment with its date, and the superseded rule is marked in place rather than deleted.
A reader must never find the old rule stated as current.

The record's row in the index below changes in the same change as its `Status`: a new record
gets its row, with its *State*, in the change that writes it, and an amendment that moves what is
built, or supersedes a rule, updates the row's *State* with it.

## How a decision gets here

The open decisions of the project are collected in
[docs/planning/questions.md](../planning/questions.md), one question at a time is put to the
owner, and **an answered question becomes an ADR in the same session** — the question is then
removed from the catalog. A decision taken in a ticket's `## Open questions` section takes the
same route.

## Index

Every record here is **Accepted**. The *State* column is the coarse build state as of
2026-09-29: **Implemented**, **Partly built** (some rules of the decision hold, the rest are
decided and outstanding) or **Not built** (decided, nothing of it exists yet). The record's own
`Status` section is the authority; this column is a reading aid.

### Stack and shape

| ADR | Decision | State |
|---|---|---|
| [0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) | Two containers — a Go backend that migrates PostgreSQL 18 on start and an nginx frontend that serves the Angular UI and proxies the API — installed by one Helm chart; both toolchains track the newest release | Partly built: both images, the migration run, the chart and the configuration surface exist; no domain, no authentication, no API beyond health and version |

### Process

| ADR | Decision | State |
|---|---|---|
| [0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) | Documentation has five homes, tickets are work lists that get archived, an open security finding is embargoed, planning documents are transitional | Implemented |
| [0003](0003-test-and-ci-policy.md) | Test, verification and CI policy | Implemented for the tiers that exist; the end-to-end tier is decided and not built |

## Related documents

- [docs/developer/](../developer/README.md) — how the code works, for somebody changing it
- [docs/operations/](../operations/README.md) — installing and running cowork
- [docs/security/](../security/README.md) — the security architecture, one page per perspective
- [docs/tickets/](../tickets/README.md) — the work lists
- [docs/planning/](../planning/) — the question catalog, the project plan and the workflow plan, until they are dissolved into ADRs and tickets
