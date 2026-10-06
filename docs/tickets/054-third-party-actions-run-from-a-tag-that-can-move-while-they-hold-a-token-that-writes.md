---
id: T54
title: third-party actions run from a tag that can move while they hold the GitHub App token or a job token that writes
state: analysed
severity: high
security: hardening
threat: a commit SHA for every action would additionally cover, until Renovate's next run takes the new commit in, a moved tag of a third-party action that uses the token within its reach — the GitHub App installation token in renovate.yml, or a job token that writes the repository and its releases in build.yml; a release build without the permissions it does not use would cover a compromised action of that job that requests an OIDC token of the release workflow and signs a provenance attestation for a binary it did not build
urgency: later        # rule 4: item 1 is a cheap known fix, and so is each answer of Q1
effort: S
filed-from: the review of the Trivy pin of the container scan, 2026-10-06
opened: 2026-10-06
decided:
done:
---

## Current state

- Every action of the three workflows names a tag, except the two Trivy steps of the container
  scan; a tag is followed wherever its repository points it, at the next run. A third-party
  action reaches the job's `GITHUB_TOKEN` two ways: `actions/checkout@v7` keeps it in the
  checkout's git configuration (`persist-credentials` defaults to `true`), and most of these
  actions take it as an input that defaults to `${{ github.token }}` — `docker/metadata-action`,
  `docker/build-push-action`, `anchore/sbom-action`, `softprops/action-gh-release`,
  `docker/scout-action`, `marocchino/sticky-pull-request-comment` and `azure/setup-helm`, read in
  each `action.yml` at the tag the workflows name.
- The tokens within reach of third-party actions named by a tag (`DOCKERHUB_PAT` in the same jobs
  is T25's):

  | Workflow, job | Token | Third-party actions named by a tag |
  |---|---|---|
  | [`renovate.yml`](../../.github/workflows/renovate.yml) `renovate` | the GitHub App installation token with every permission of the App, as the input `token` | `renovatebot/github-action@v46.3.7` |
  | [`build.yml`](../../.github/workflows/build.yml) `build` | job token: `contents`, `pages`, `attestations` and `id-token` write, `actions` read | `docker/setup-qemu-action@v4`, `docker/setup-buildx-action@v4`, `docker/login-action@v4`, `docker/metadata-action@v6`, `docker/build-push-action@v7`, `anchore/sbom-action@v0`, `softprops/action-gh-release@v3`, `docker/scout-action@v1` |
  | `build.yml` `release-mcp` | job token: `contents`, `attestations` and `id-token` write | `softprops/action-gh-release@v3` |
  | `build.yml` `release-helm-gh` | job token: `contents` and `pages` write | `azure/setup-helm@v5`, `softprops/action-gh-release@v3` |
  | [`release.yml`](../../.github/workflows/release.yml) `coverage-report` | job token: `contents` read, `pull-requests` write | `marocchino/sticky-pull-request-comment@v3` |
  | `release.yml` `helm`, `container-malware-scan` | job token: `contents` read, `pull-requests` and `issues` write | `azure/setup-helm@v5`; `docker/setup-buildx-action@v4`, `docker/login-action@v4`, `docker/build-push-action@v7` |

- The `build` job declares no `permissions:` and inherits the workflow's block, which serves no
  other job: `release-mcp` and `release-helm-gh` declare their own. With `id-token: write` any
  step of `build` can request an OIDC token of the release workflow — the identity under which
  `actions/attest-build-provenance` in `release-mcp` signs the provenance of the `cowork-mcp`
  binaries (ADR 0041 D2) — and with `attestations: write` store what it signs. With
  `contents: write` any step can replace an asset of the release.
- Under the automerge rules for actions (ADR 0003 D9, digest updates included in
  `renovate.json`), a pinned SHA would still take a new commit through Renovate's next pull
  request after CI, unread; a pin buys the wait for that daily run and the record.
- No attack path exists today: it needs the upstream of one of these actions to be compromised.

## Required changes

### Independent of the open question

1. The `build` job of `build.yml` declares its own `permissions:` with what its steps use —
   `contents: write` for the SBOM upload to the release, and any other only where a release run
   shows a step needs it — and the workflow's block drops to `contents: read`. Verified by the
   next release: both images, their SBOMs and the Scout report published, and
   `gh attestation verify` still passes for a `cowork-mcp` binary.

### Depends on the answer

2. The refs of the third-party actions in the table, per Q1; actionlint and Renovate's config
   validator pass, and a local Renovate extract reads every pinned line.

## Open questions

### Q1: Do the third-party actions that hold these tokens name a commit SHA?

A SHA makes a new commit of an action wait for Renovate's next run and leaves a pull request on
record; it does not get it read, since those pull requests automerge after CI.

- **(a) Tags, as now:** nothing to do; a moved tag runs with the token at the next run of its
  job — for `renovate.yml` the next night, with every permission of the App.
- **(b) Every action of every workflow names a SHA** with its tag in a trailing comment:
  Renovate's `helpers:pinGitHubActionDigests` preset, the rule T25's Q2 (c) asks for, one rule
  for both tickets; no list to keep, the most Renovate pull requests, which merge themselves.

Recommended: **(b)** — a token that rewrites the releases and an App token with every permission
are worth the wait and the record, and one rule for every action needs no judgement of which
step reaches which token.

**Answer:** _open_

## Not verified

- Which permissions the App behind `APP_CLIENT_ID` holds: the organisation's setting, not read
  from here.
- Whether a step of `build` uses `pages`, `id-token`, `attestations` or `actions`: not traced
  through the actions' code, and no release has run without them.
- That an OIDC token requested in `build` signs an attestation that `gh attestation verify --repo
  guided-traffic/cowork` accepts: inferred from the shared workflow and ref, not tried.

## Related

- T25 — `DOCKERHUB_PAT` within reach of the same actions in `build` and in the scan job; its Q2
  (c) is the rule this ticket's Q1 (b) asks for.
