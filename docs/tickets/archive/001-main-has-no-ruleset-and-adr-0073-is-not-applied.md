---
id: T1
title: main has no ruleset, so ADR 0073 is not applied and a red check does not stop a merge
state: done
severity: high
security: none
threat:
urgency: release      # rule 2: gated the release
effort: S
filed-from: ADR 0061 D5 and ADR 0073 (the pipeline ticket of ADR 0074 D2)
opened: 2026-10-02
decided: 2026-10-03
done: 2026-10-03
shipped: the rulesets main and release tags and the squash, auto-merge and delete-on-merge switches of ADR 0073; a pull request is blocked while its required checks run
---

## Current state

Decided by [ADR 0073](../adr/0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md)
D1–D7 and [ADR 0003](../adr/0003-test-and-ci-policy.md) D1, D4.

- The pipeline is green on `main`: every job of "Test and Release"
  ([`release.yml`](../../.github/workflows/release.yml)) and semantic-release passed after the
  phase-2 merge (run 37107802447), which cut the release `0.1.0`, and "Release Docker & Helm"
  ([`build.yml`](../../.github/workflows/build.yml)) published both images and the chart (run
  37108086347; ADR 0061).
- `main` has no ruleset and no branch protection (`gh api …/rulesets` lists none; the protection
  endpoint answers "Branch not protected"). Merge commits, squash and rebase are all allowed,
  auto-merge and delete-on-merge are off, and the squash title is `COMMIT_OR_PR_TITLE`. The
  phase-2 pull request was squashed under the subject "Merge pull request #14 from …", with its
  title in the body; semantic-release read the `feat:` from there, but the subject on `main` is
  not a conventional commit — ADR 0073 D3's squash title taken from the pull request title
  prevents that. ADR 0073 D1–D5 are an administrator's act in GitHub, and ADR 0073's Status
  assigns them to this ticket.

## Verification

- `gh api repos/guided-traffic/cowork/rulesets` lists the ruleset `main` (branch, active:
  deletion, non-fast-forward, linear history, pull request with squash only, the thirteen
  required checks bound to the GitHub Actions app with the branch kept up to date; bypass: the
  organisation's administrators and the release App) and the ruleset `release tags` (`v*`:
  creation, update and deletion; bypass: the release App). `…/rules/branches/main` returns the
  five rules.
- `gh api repos/guided-traffic/cowork` reads squash only, the squash title from the pull
  request title, auto-merge on, delete-on-merge on.
- Pull request #22 read `mergeStateStatus: BLOCKED` while its required checks ran.
- Release `0.1.1` created its tag through the release App under the tag ruleset. No release has
  pushed a release commit to `main` since the rulesets exist — the badge did not change —, so
  the App's bypass of `main` has not been exercised yet.
