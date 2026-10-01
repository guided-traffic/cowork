# ADR 0075: Developer Documentation Lives in `docs/developer/`, One Page per Topic With a README as the Entry Point; There Is No `DEVELOPER.md`; the General Standard Says So

## Status

Accepted. Date: 2026-10-01. Decided by the owner, in the closing turn of the catalog: the
content of `DEVELOPER.md` goes into `docs/developer/` from now on, here and in the general
documentation standard.

**Implemented** in the change that wrote this record: `DEVELOPER.md` was split into
[docs/developer/README.md](../developer/README.md) (the entry point: what has to be in your
head first, the page table, the core flows), [repository-layout.md](../developer/repository-layout.md),
[build-test-lint.md](../developer/build-test-lint.md), [ci-and-release.md](../developer/ci-and-release.md),
[adding-things.md](../developer/adding-things.md) and [conventions.md](../developer/conventions.md),
beside the existing [package-map.md](../developer/package-map.md), [architecture.md](../developer/architecture.md)
and [testing.md](../developer/testing.md); the root file was deleted; every reference in the
repository was rewritten; the owner's general standard (`~/.claude/CLAUDE.md`, "Documentation
standard") was changed on the owner's instruction to name `docs/developer/` and to say that a
repository with a root `DEVELOPER.md` moves when that file is next restructured.

## Context

The general standard asked for a root `DEVELOPER.md` beside `README.md`; this repository had
one, and a `docs/developer/` directory beside it for the per-subsystem pages, with a rule that
the two must not repeat each other. Two homes for contributor knowledge is one too many: a
layout tree in one file and a package map in another, a build matrix at the root and the test
tiers under `docs/`. [ADR 0072](0072-the-security-architecture-lives-in-docs-security-and-the-general-standard-says-so.md)
had just moved the security architecture out of a single root file for the same reason. The
owner closed the gap: one directory, one page per topic, an entry page.

## Decision

**D1 — Contributor documentation lives in `docs/developer/`, one page per topic.** The topics:
the entry page (what has to be in your head first, the page table, the core flows one fact
each), the repository layout, the package map, the architecture, build/test/lint, the test
tiers and fixtures, continuous integration and the release, the extension checklists, the
conventions, and a page per subsystem once it exists.

**D2 — There is no `DEVELOPER.md` at the root.** The README's development section links the
directory; so do `CLAUDE.md` and the other homes.

**D3 — A page may and should point at files and functions,** and is updated in the same
change that moves the tree ([ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)).

**D4 — The general standard says D1–D3,** and tells a repository that still carries a root
`DEVELOPER.md` to move when that file is next restructured, not before.

## Consequences

- One home for contributor knowledge; no rule needed about two files not repeating each
  other.
- Links into the developer pages are deeper (`docs/developer/<page>.md#…`) and were rewritten
  in this change; a future rename of a page is a link change across the repository, as for
  every page under `docs/`.
- The general standard changed twice in one day (ADR 0072, this record); both changes are
  recorded, and the standard now reads "README.md at the root, `docs/developer/`,
  `docs/security/`, `SECURITY.md`".

## Alternatives Considered

- **Keep `DEVELOPER.md` as the entry point and `docs/developer/` for subsystems** — the
  state before this record. Two homes with a no-repeat rule. Lost.

## Residual risks

- Older repositories with a root `DEVELOPER.md` diverge from the standard until restructured;
  D4 says so on purpose.

## References

- [docs/developer/README.md](../developer/README.md) — the entry page
- [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D1 — the homes table, amended
- [ADR 0072](0072-the-security-architecture-lives-in-docs-security-and-the-general-standard-says-so.md) — the sibling change to the general standard
- `~/.claude/CLAUDE.md`, "Documentation standard" — changed in this session on the owner's instruction
