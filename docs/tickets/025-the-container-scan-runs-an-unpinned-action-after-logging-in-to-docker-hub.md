---
id: T25
title: the Docker Hub publishing token is within reach of third-party actions named by a tag, in the scan of every pull request and in the release build
state: analysed
severity: high
security: hardening
threat: would additionally cover a compromised third-party action that captures DOCKERHUB_PAT — from the job's Docker configuration, from the login it intercepts, or as its own input — and pushes images under guidedtraffic/; a read-only token for the scan covers every such action of the scan job, a new commit that Renovate's own pull request runs included, and a commit SHA for every action that can reach the token covers a moved tag in the release build until Renovate's next run takes the new commit in
urgency: later        # rule 4: each answer of Q1 and of Q2 is a cheap known fix
effort: XS
filed-from: the phase-2 pipeline work, when DOCKERHUB_PAT became an organisation secret
opened: 2026-10-03
decided:
done:
---

## Current state

- **Every step of a job that logs in to Docker Hub can capture the token, not only the steps
  after the login.** The login leaves the organisation secret `DOCKERHUB_PAT` (ADR 0061 D1) in
  the runner's Docker configuration for the steps after it. A step before it can prepare what the
  login runs: `docker/login-action@v4` runs `docker login --password-stdin` through
  `@docker/actions-toolkit`, whose `Exec` resolves `docker` on the `PATH` (`@actions/exec`,
  `io.which`), and any step can put a directory of its own in front of that path through
  `$GITHUB_PATH`.
- **The scan job** "Container Malware Scan" ([`release.yml`](../../.github/workflows/release.yml),
  `container-malware-scan`) runs on every pull request from a branch of this repository,
  Renovate's branches included. Besides GitHub's own `actions/checkout` and
  `actions/upload-artifact`, it runs `docker/setup-buildx-action@v4` before the login,
  `docker/login-action@v4`, `docker/build-push-action@v7`, and `aquasecurity/trivy-action` twice.
  Both Trivy steps name the commit of the release `v0.36.0`
  (`ed142fd0673e97e23eac54620cfb913e5ce36c25`, the commit its signed tag points to) with the tag
  in a trailing comment, and a `pinDigests` rule for that action in
  [`renovate.json`](../../renovate.json) keeps it on a SHA. The three Docker actions name a tag.
- **The release build** ([`build.yml`](../../.github/workflows/build.yml), the `build` job)
  publishes the images with the same secret when a release is published; no pull request runs
  this workflow. Before its login it runs `docker/setup-qemu-action@v4` and
  `docker/setup-buildx-action@v4`; after it `docker/metadata-action@v6`,
  `docker/build-push-action@v7`, `anchore/sbom-action@v0`, `softprops/action-gh-release@v3` and
  `docker/scout-action@v1`, which also takes the token as its input `dockerhub-password`. All
  eight third-party actions of the job, the login included, name a tag.
- **A pin keeps a new commit out only until Renovate's next run.** Renovate's github-actions
  manager moves a pinned SHA when the tag in its comment moves, and the rules for actions in
  `renovate.json` automerge digest, patch, minor and major updates after CI (ADR 0003 D9). A moved
  tag or a new release of Trivy therefore arrives through Renovate's next pull request, whose CI
  runs the scan job with the token before anybody reads it. The pin buys the wait for that daily
  run and a pull request that records the change.
- No attack path exists today: it needs the upstream of one of these actions to be compromised.
- The pinned release installs Trivy `v0.70.0`, the action's default there, because no step sets
  its `version` input; the head of the action's `master` installs `v0.75.0`. A local Renovate
  extract finds that input as the dependency `aquasecurity/trivy` and skips it for want of a
  value.

## Required changes

1. The token the scan job logs in with, per Q1.
2. The refs of the third-party actions that can reach the token, per Q2: a `pinDigests` rule in
   `renovate.json` and the pinned lines, each SHA resolved with `gh api` to the commit of its
   tag; actionlint and Renovate's config validator pass, and a local Renovate extract reads every
   pinned line.

## Open questions

### Q1: Which Docker Hub token does the container scan log in with?

The scan only pulls base images; it logs in so that the runners, which share one address, stay
within Docker Hub's pull limit.

- **(a) Keep the publishing token,** pinned Trivy only: nothing to create; a compromised step
  of the scan job can still push.
- **(b) A read-only token for the scan job** (a second organisation secret, a Docker Hub access
  token with public read only), the publishing token only in `build.yml`, which runs on a
  release: a pull request's job can no longer push; one more secret to rotate. The release build
  keeps the publishing token within reach of its eight third-party actions under every answer,
  `docker/scout-action@v1` as an input among them; that is Q2's.
- **(c) No login in the scan job:** anonymous pulls under the shared address's limit; a burst
  of Renovate pull requests can fail on rate limits.

Recommended: **(b)** — it takes the push right out of every pull-request run at the cost of one
secret, which no pin can do: a new commit of a pinned action runs in Renovate's pull request
before it merges. It leaves the release build as it is; there Q2 is the only cover.

**Answer:** _open_

### Q2: Which actions that can reach the publishing token name a commit SHA?

A tag is followed wherever its repository points it, at the next run. A SHA makes a new commit
wait for Renovate's next run and leaves a pull request on record; it does not get it read, since
those pull requests automerge.

- **(a) Trivy only, as now:** nothing to do; a moved tag of any other of these actions reaches
  the token at the next pull request or release.
- **(b) Every third-party action of the two jobs:** the eight packages added to the `pinDigests`
  rule of `renovate.json`, which pins them wherever they appear; the list has to follow every
  action either job gains, and a step before the login is easily left out of it.
- **(c) Every action of every workflow:** Renovate's `helpers:pinGitHubActionDigests` preset in
  place of the Trivy rule, GitHub's own actions included; no list to keep, and it is the rule
  T54 asks for the other tokens the workflows hand out; the most Renovate pull requests, which
  merge themselves.

Recommended: **(c)** — the rule does not depend on knowing which step can reach which token,
the judgement that is easy to get wrong; its cost is pull requests nobody has to handle.

**Answer:** _open_

## Not verified

- That Renovate opens a digest pull request when the tag in a pin's comment moves: from the
  github-actions manager's documentation ("Renovate will update the commit SHA according to the
  GitHub tag you specified"), not from a run.
- The `PATH` lookup was read in `@docker/actions-toolkit` `v0.94.0` (the range `docker/login-action`
  `v4` names) and in `@actions/exec` at the head of `actions/toolkit`, not in the bundle the action
  ships.

## Related

- T54 — the GitHub App token and the job tokens that write, within reach of the same and of
  other third-party actions named by a tag; Q2 (c) is the rule its Q1 (b) asks for.
