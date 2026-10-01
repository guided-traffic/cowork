# ADR 0073: `main` Is Protected by a Ruleset — Pull Requests, Every Job Required, Linear History, No Force Push — With an Administrator Bypass; Squash-Only Merges, Auto-Merge On, Branches Deleted on Merge, Release Tags Protected

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"branch protection?": a ruleset with every job required and an administrator bypass, over the
same ruleset without any bypass (the recommendation), over classic branch protection, and
over leaving `main` unprotected. The rules of D4–D7 were put to the owner with the question
and not objected to.

**Not built.** Verified read-only on 2026-10-01: `main` has no protection and no ruleset;
merge commits, squash and rebase are all allowed; auto-merge and delete-on-merge are off.
The ruleset and the three repository switches are an administrator's act in GitHub; they are
part of the first pipeline ticket ([ADR 0061](0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md)
D5) unless the owner has them applied earlier.

## Context

[ADR 0003](0003-test-and-ci-policy.md) D4 says a gate that is not required is not a gate;
without required status checks the sentence is prose. Renovate's automerge
([ADR 0062](0062-toolchains-go-minor-after-seven-days-go-patch-at-once-angular-minor-at-once-typescript-inside-angulars-peer-range.md))
waits for green checks on its own, but a person's merge does not; semantic-release reads the
commits that reach `main` and builds releases from them. The owner works alone most of the
time and wants a way past the gate when it is needed; the recommendation argued that the one
mechanism that makes D4 true is the one that exempts nobody, and the owner chose the bypass.

## Decision

**D1 — A ruleset on `main`:** changes arrive by pull request; the required status checks are
every job of "Test and Release" by its exact name — Code Linting, GoSec Security Scan,
Vulnerability Check, Cyclomatic Complexity, Malware Scan (Source Code), Unit Tests,
Integration Tests (PostgreSQL), Frontend (lint, test, build), Helm Chart, Container Malware
Scan (backend), Container Malware Scan (frontend), Combined Coverage Report, Release Tooling,
and the end-to-end job once it exists ([ADR 0056](0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)
D4); the branch must be up to date; linear history; no force push; no deletion.

**D2 — The organisation's administrators are on the bypass list.** A bypass is for an
emergency; it is not the way work lands. Every bypass is recorded by GitHub on the pull
request and in the repository's audit log, and the owner reads those as exceptions to
explain, not as routine.

**D3 — Repository settings:** squash merge only (merge commits and rebase disabled), the
squash commit's title taken from the pull request title so semantic-release reads a
conventional commit; auto-merge enabled so Renovate's platform automerge works
([`renovate.json`](../../renovate.json) sets `platformAutomerge`); head branches deleted on
merge.

**D4 — A second ruleset protects release tags:** `v*` may be created only by the GitHub App
semantic-release runs as; nobody pushes a release tag by hand.

**D5 — `gh-pages` stays unprotected:** the release workflow force-pushes the chart index
there ([ADR 0061](0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md)
D4).

**D6 — A renamed or added job is a ruleset change in the same pull request.** The
[adding-things](../developer/adding-things.md) checklist "a CI job" gains the step; a required check whose job no longer exists
blocks every merge, which is how the omission would be noticed.

**D7 — While the pipeline is red, `main` is frozen for everyone but a bypassing
administrator.** That is the intended pressure behind the first pipeline ticket; the bypass
is what the owner keeps for the case where the freeze itself must be lifted.

## Consequences

- Nothing reaches `main` without a green pipeline — except by an administrator's recorded,
  deliberate bypass.
- Solo work means a pull request for a one-line change; the owner accepted that with the
  bypass as the valve.
- semantic-release sees squash commits with conventional titles; the changelog reads as the
  pull request titles.
- Renovate can automerge through the platform once checks are green; today's red jobs keep
  every Renovate pull request open, which is correct.
- The required-check names couple the ruleset to the workflow; D6 keeps them in step.

## Alternatives Considered

- **The same ruleset with no bypass, administrators included** — the recommendation. The
  one mechanism that exempts nobody; the owner wanted a valve for emergencies. Lost.
- **Classic branch protection.** The same rules in the older model; rulesets are the current
  one, with explicit bypass lists and an API. Lost.
- **No protection.** Renovate still waits for green on its own; a person's merge does not,
  and D4 of ADR 0003 stays a sentence. Lost.

## Residual risks

- D2 by name: a bypass that becomes a habit removes the gate; the audit log and the pull
  request note are the visibility, and nothing else.
- Required checks named by job title break on a rename; D6 is the discipline.

## References

- [ADR 0003](0003-test-and-ci-policy.md) D4 — every job a required gate
- [ADR 0062](0062-toolchains-go-minor-after-seven-days-go-patch-at-once-angular-minor-at-once-typescript-inside-angulars-peer-range.md), [`renovate.json`](../../renovate.json) — automerge that needs the platform switch
- [ADR 0061](0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md) D4, D5 — `gh-pages` and the pipeline ticket
- [`.github/workflows/release.yml`](../../.github/workflows/release.yml), [`.releaserc.json`](../../.releaserc.json) — the job names and the commit reader
