# ADR 0002: Documentation Has Five Homes, Tickets Are Work Lists That Get Archived, an Open Security Finding Is Embargoed, and Planning Documents Are Transitional

## Status

Accepted. Date: 2026-09-29. The owner asked for the documentation of this repository to be
built like the `docs/` tree of a sibling project; this record adopts that project's rules,
which the owner decided there between 2026-09-26 and 2026-09-27, and adds D10 for the
bootstrap documents this repository starts with.

**Implemented** in the working tree on 2026-09-29: the five directories exist with their
rules pages, [docs/tickets/README.md](../tickets/README.md) carries the ticket rules,
[`.gitignore`](../../.gitignore) carries `docs/tickets/**/local_*`, and
[`.graphifyignore`](../../.graphifyignore) excludes `docs/tickets/archive`. There are no
tickets yet and no legacy citations.

## Context

A repository that is worked on by a person and an LLM accumulates text fast: analyses,
decisions, work lists, explanations for operators, threat models. When these share a file, the
file cannot be closed, cannot be trusted as current, and cannot be cited. The sibling project
learned this the expensive way: a single security document cited by section number, tickets
that were untracked yet cited from code, a board that had to be groomed beside the files it
summarised, and a backlog of fifty files that blocked feature work.

This repository starts empty, so the rules can be in force from the first file. It also starts
with three documents no steady-state rule covers: a question catalog, a project plan and a
workflow plan. They exist to be consumed.

## Decision

**D1 — A statement has exactly one home.**

| Kind | Home |
|---|---|
| A decision — what cowork does and why, what was rejected | an [ADR](README.md) |
| How the code works — a subsystem, an invariant, the contributor workflow | [docs/developer/](../developer/README.md) and [DEVELOPER.md](../../DEVELOPER.md) |
| What somebody running cowork needs — installation, configuration, upgrading, backups, monitoring | [docs/operations/](../operations/README.md) |
| The threat model, and the gap each mechanism leaves | [docs/security/](../security/README.md) — one page per perspective, each closing with `## What this does not cover`. Reporting a vulnerability is [SECURITY.md](../../SECURITY.md) |
| Work still outstanding | a [ticket](../tickets/README.md), archived when the work lands |

**D2 — `README.md` is the front page and carries the reference.** The configuration
variables, the Helm values, the API routes and the CLI commands are tables in the README and
nowhere else; a page under `docs/operations/` explains a setting without restating the table.
The README explains nothing an operations page should explain.

**D3 — A ticket is a work list and nothing else.** `docs/tickets/NNN-<slug>.md`, closed by
moving it to `docs/tickets/archive/` when the work lands. **The extraction is the close**: the
decision goes into an ADR, the operator-facing consequence into the README or
`docs/operations/`, the security-relevant one into `docs/security/`, the contributor knowledge
into `docs/developer/` — an archived ticket is history, never the source of a current rule.

**D4 — A number is never reused,** not an embargoed ticket's, not a merged ticket's. The
numbering command in the rules page reads deleted files from git history for that reason.

**D5 — A finding goes into an existing ticket first.** The open ticket of the same subject
collects it; a new ticket only when none fits. A collecting ticket is still one subject.

**D6 — A ticket carries no history.** Current state, required changes, open questions with an
answer line, nothing struck through, nothing dated. A changed fact is rewritten.

**D7 — An open security finding is embargoed.** A ticket with `security: live` or
`security: boundary` whose finding is not fixed keeps the `local_` prefix and stays untracked
through the repository's own `.gitignore` line; no tracked file, commit message or pull request
carries its details or its file name. The embargo ends at the fix, not at `state: done`; a
dropped or risk-accepted finding is published only on the owner's explicit, dated acceptance.

**D8 — Nothing outside `docs/tickets/` cites a ticket.** Not by number, label, file name or
path; cite the ADR instead. A ticket may cite an ADR; an ADR does not cite a ticket.

**D9 — A security page is one perspective, and it ends with its limits.** One page per
perspective under `docs/security/`, no single all-covering document, every page closing with
`## What this does not cover`; an open gap carries a stable `H-<n>` identifier in its heading.

**D10 — Planning documents are transitional.** `docs/planning/` holds the question catalog,
the project plan and the workflow plan. A question that is answered becomes an ADR in the same
session and is removed from the catalog; a plan phase that starts becomes tickets; a workflow
step that is built becomes a page under `docs/developer/` or `docs/operations/`. The directory
is emptied by that process, never maintained as a parallel truth, and it is deleted when it is
empty. Nothing in code cites a planning document.

**D11 — English, everywhere.** Code, comments, commit messages, documentation. Conversation
with the owner may be German; the repository is not.

## Consequences

- Five directories and three root documents to keep in step: whoever changes behaviour
  updates the page that describes it in the same change, or the page is wrong.
- The extraction discipline makes closing a ticket slower than deleting it. That is the cost
  of archived tickets never being cited.
- The embargo puts security tickets outside git until fixed; a lost laptop loses them. The
  owner accepts this; the alternative is publishing an attack path.
- D10 means this repository's first sessions produce ADRs faster than code.

## Alternatives Considered

- **The three-file layout `README.md` / `DEVELOPER.md` / `SECURITY_ARCHITECTURE.md` at the
  root**, the owner's general standard. The sibling project moved off the single security
  document on 2026-09-27 because a document nobody finishes is a document nobody checks, and
  the owner asked for this repository to follow that tree. `SECURITY_ARCHITECTURE.md` is
  therefore not created; whether the general standard should be updated to match is put to the
  owner as an open question, not decided here.
- **Tickets as GitHub issues** until cowork exists. Would split the interim backlog from the
  repository the rules live in, and the rules page would have to be rewritten twice. Lost.
- **A `docs/planning/` that stays.** A plan that is maintained beside the tickets is the board
  problem again. Lost; D10.

## Residual risks

- Not verified: that `git check-ignore` reports the `docs/tickets/**/local_*` rule for a
  `local_` path under `archive/` in a clone without the owner's global excludes; the sibling
  project verified the identical line, this repository has not repeated it.
- D8 has no automated check. A `git grep` for `T[0-9]` outside `docs/tickets/` is the manual one.

## References

- [docs/tickets/README.md](../tickets/README.md) — the ticket rules, frontmatter and body skeleton
- [docs/security/README.md](../security/README.md) — the form of a security page
- [docs/developer/README.md](../developer/README.md), [docs/operations/README.md](../operations/README.md)
- [`.gitignore`](../../.gitignore), [`.graphifyignore`](../../.graphifyignore)
- [docs/planning/](../planning/) — what D10 consumes
