---
id: T54
title: third-party actions run from a tag that can move while they hold the GitHub App token or a job token that writes
state: dropped
severity: high
security: hardening
threat: a commit SHA for every action would additionally cover, until Renovate's next run takes the new commit in, a moved tag of a third-party action that uses the token within its reach — the GitHub App installation token in renovate.yml, or a job token that writes the repository and its releases in build.yml; a release build without the permissions it does not use would cover a compromised action of that job that requests an OIDC token of the release workflow and signs a provenance attestation for a binary it did not build
urgency: later        # rule 4: item 1 is a cheap known fix, and so is each answer of Q1
effort: S
filed-from: the review of the Trivy pin of the container scan, 2026-10-06
opened: 2026-10-06
decided: 2026-10-06
done:
dropped-reason: the owner keeps version tags and the build job's workflow-wide permissions; both risks are accepted in ADR 0061 D7 and docs/security/release-pipeline.md H-60 and H-61
---

## Current state

- Every action of the three workflows names a version tag, which its repository can move, by the
  owner's decision
  ([ADR 0061](../../adr/0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md)
  D7). A third-party action reaches the job's `GITHUB_TOKEN` two ways: `actions/checkout@v7` keeps
  it in the checkout's git configuration (`persist-credentials` defaults to `true`), and
  `docker/metadata-action`, `docker/build-push-action`, `anchore/sbom-action`,
  `softprops/action-gh-release`, `docker/scout-action`, `marocchino/sticky-pull-request-comment`
  and `azure/setup-helm` take it as an input that defaults to `${{ github.token }}`, read in each
  `action.yml` at the tag the workflows name.
- The `build` job of [`build.yml`](../../../.github/workflows/build.yml) declares no `permissions:`
  and runs with the workflow's block — `contents`, `pages`, `attestations` and `id-token` write,
  `actions` read — by the owner's decision (ADR 0061 D7).
- Which token each job holds, and which third-party actions run beside it, is the table of
  [docs/security/release-pipeline.md](../../security/release-pipeline.md); the risks are the owner's,
  named in ADR 0061's Residual risks and in that page as H-60 (a moved tag runs with the App
  token, the App's private key or a job token that writes) and H-61 (the release build's
  permissions, the OIDC identity of the `cowork-mcp` attestations among them).

## Required changes

None. The owner decided not to build either change: no `permissions:` block of the `build` job's
own, no commit SHA for any action (Q1).

## Open questions

### Q1: Do the third-party actions that hold these tokens name a commit SHA?

A SHA makes a new commit of an action wait for Renovate's next run and leaves a pull request on
record; it does not get it read, since those pull requests automerge after CI.

- **(a) Tags, as now:** nothing to do; a moved tag runs with the token at the next run of its
  job — for `renovate.yml` the next night, with every permission of the App.
- **(b) Every action of every workflow names a SHA** with its tag in a trailing comment:
  Renovate's `helpers:pinGitHubActionDigests` preset, one rule for both tickets; no list to keep,
  the most Renovate pull requests, which merge themselves.

Recommended: **(b)** — a token that rewrites the releases and an App token with every permission
are worth the wait and the record, and one rule for every action needs no judgement of which
step reaches which token.

**Answer:** (a) — tags, never a SHA; and the `build` job keeps the workflow's permissions. Both the
owner's accepted risk, ADR 0061 D7.

## Not verified

- Which permissions the App behind `APP_CLIENT_ID` holds: the organisation's setting, not read
  from here.
- Whether a step of `build` uses `pages`, `id-token`, `attestations` or `actions`: not traced
  through the actions' code.
- That an OIDC token requested in `build` signs an attestation that `gh attestation verify --repo
  guided-traffic/cowork` accepts: inferred from the shared workflow and ref, not tried.

## Related

- T25 — `DOCKERHUB_PAT` within reach of the same actions in `build` and in the scan job; closed by
  the same decision.
