# ADR 0072: The Security Architecture Lives in `docs/security/`, One Page per Perspective, and the Owner's General Documentation Standard Says So

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"security documentation layout?": update the general standard to the per-perspective layout
and apply it directly, over keeping the general standard unchanged with a per-repository
exception and over moving this repository back to a root `SECURITY_ARCHITECTURE.md`. The
owner's words: from now on, always `docs/security/`.

**Implemented.** The owner's general standard (`~/.claude/CLAUDE.md`, "Documentation
standard") was changed in the same session, on the owner's explicit instruction: the
three-file wording became "README.md at the root, the security architecture under
`docs/security/`, `SECURITY.md` only for reporting" (the developer half of that sentence was
changed again the same day by [ADR 0075](0075-developer-documentation-lives-in-docs-developer-and-the-general-standard-says-so.md)); the `SECURITY_ARCHITECTURE.md`
paragraph became the `docs/security/` paragraph with the per-page rules; the README's
documentation table links `docs/security/` and `SECURITY.md`. A backup of the previous text
was kept in the session's scratch directory. This repository already follows the layout
([ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
D9, [docs/security/README.md](../security/README.md)).

## Context

Two standards of the same author disagreed: the general one asked for a single
`SECURITY_ARCHITECTURE.md` at the root; the sibling project re-decided on 2026-09-27 that
one all-covering security document is one nobody finishes and nobody checks, and moved to one
page per perspective, each ending with its own limits and carrying its open gaps as stable
`H-<n>` identifiers. This repository followed the sibling, and ADR 0002 recorded the
divergence as an open question rather than resolving it. Every new repository would have
tripped over the same disagreement, and every session reads the general text first.

## Decision

**D1 — The security architecture of a project lives under `docs/security/`, one page per
perspective.** No single document covers all perspectives. `docs/security/README.md` states
the form; the file names are the index.

**D2 — Every page ends with `## What this does not cover`.** An open gap lives in that
section of the page whose mechanism has it, carries a stable `H-<n>` in its heading with an
explicit anchor, and is never collected in a central list. The pages carry no tickets, no
checkbox lists, no "planned for".

**D3 — `SECURITY.md` at the root is for reporting only:** how to report a vulnerability, the
supported versions, a pointer to `docs/security/` for what is already known.

**D4 — The general standard says D1–D3,** so that every repository created or restructured
from now on follows them without a per-repository exception. Repositories that still carry
a root `SECURITY_ARCHITECTURE.md` move when that file is next restructured, not before.

**D5 — The README's documentation table links `docs/security/` and `SECURITY.md`,** as this
repository's does.

## Consequences

- One standard; the sibling project, this repository and every future one agree.
- A change to the general standard is a change outside this repository; it was made on the
  owner's explicit "mach es direkt", which the owner's cross-project rule requires, and this
  record is where that is written down.
- The three-file name in the general standard's heading is gone; the layout is "two root
  documents, a security directory, a reporting file".
- Older repositories with the single document are not touched by this record.

## Alternatives Considered

- **Keep the general standard, document the exception per repository.** Nothing to touch;
  every new repository decides again, every session finds the general text first and the
  exception second. Lost.
- **Move this repository back to `SECURITY_ARCHITECTURE.md`.** Standard-conformant on
  paper; the sibling's reasons apply here with seventy records even more, and the sibling
  would stay the exception. Lost.

## Residual risks

- The general standard is a file outside any repository's review; a later edit there can
  reintroduce the disagreement. This record is the reference a future session can cite.

## References

- [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D9 — the per-perspective rule this repository already follows
- [docs/security/README.md](../security/README.md) — the form of a page
- [SECURITY.md](../../SECURITY.md) — the reporting file
- `~/.claude/CLAUDE.md`, "Documentation standard" — the general standard, changed in this session (outside the repository, on the owner's instruction)
