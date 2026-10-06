---
id: T25
title: the Docker Hub publishing token is within reach of third-party actions named by a tag, in the scan of every pull request and in the release build
state: done
severity: high
security: hardening
threat: would additionally cover a compromised third-party action that captures DOCKERHUB_PAT — from the job's Docker configuration, from the login it intercepts, or as its own input — and pushes images under guidedtraffic/; a read-only token for the scan covers every such action of the scan job, a new commit that Renovate's own pull request runs included, and a commit SHA for every action that can reach the token covers a moved tag in the release build until Renovate's next run takes the new commit in
urgency: later        # rule 4: each answer of Q1 and of Q2 is a cheap known fix
effort: XS
filed-from: the phase-2 pipeline work, when DOCKERHUB_PAT became an organisation secret
opened: 2026-10-03
decided: 2026-10-06
done: 2026-10-06
shipped: .github/workflows/release.yml — both Trivy steps name aquasecurity/trivy-action@v0.36.0 instead of master; renovate.json without a pinDigests rule; ADR 0061 D7 and its Residual risks record the publishing token in the scan and the version tags as the owner's accepted risk; docs/security/release-pipeline.md H-59 and H-60
---

## Current state

- **The container scan logs in with the publishing token**, by the owner's decision
  ([ADR 0061](../../adr/0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md)
  D7): `container-malware-scan` in [`release.yml`](../../../.github/workflows/release.yml) uses
  `DOCKERHUB_PAT`, as the release build in [`build.yml`](../../../.github/workflows/build.yml) does.
- **Every action is named by a version tag, never a commit SHA** (ADR 0061 D7). Both Trivy steps of
  the scan name `aquasecurity/trivy-action@v0.36.0`, the action's latest release on 2026-10-06
  (`gh api repos/aquasecurity/trivy-action/releases/latest`); the action publishes no major tag
  (`gh api repos/aquasecurity/trivy-action/git/matching-refs/tags/v0` lists only full releases).
  That release installs Trivy `v0.70.0`, the default of its `action.yaml`, since no step sets the
  `version` input. [`renovate.json`](../../../renovate.json) has no `pinDigests` rule; its
  github-actions rules move the tag.
- **The risks are the owner's**, each named in ADR 0061's Residual risks and in
  [docs/security/release-pipeline.md](../../security/release-pipeline.md): H-59 (the scan of every
  pull request, Renovate's among them, holds the publishing token) and H-60 (a moved tag of any
  action of the scan or of the release build runs with it).
- [docs/developer/ci-and-release.md](../../developer/ci-and-release.md) and
  [adding-things.md](../../developer/adding-things.md#a-ci-job) say how actions are named.

Verified on 2026-10-06: a YAML parse of the three workflows and a JSON parse of `renovate.json`
pass; actionlint `1.7.12` (its container image, shellcheck off) reports nothing on the Trivy steps,
only the `client-id` input of `actions/create-github-app-token@v3` in two untouched steps, which
the action's `v3` manifest has and actionlint's bundled copy does not; every relative link and
anchor of the changed Markdown files resolves. Not run: the scan itself — the first CI run of the
change is the first run of `v0.36.0` there.

## Required changes

None.

## Open questions

### Q1: Which Docker Hub token does the container scan log in with?

The scan only pulls base images; it logs in so that the runners, which share one address, stay
within Docker Hub's pull limit.

- **(a) Keep the publishing token,** pinned Trivy only: nothing to create; a compromised step
  of the scan job can still push.
- **(b) A read-only token for the scan job** (a second organisation secret, a Docker Hub access
  token with public read only), the publishing token only in `build.yml`, which runs on a
  release: a pull request's job can no longer push; one more secret to rotate.
- **(c) No login in the scan job:** anonymous pulls under the shared address's limit; a burst
  of Renovate pull requests can fail on rate limits.

Recommended: **(b)** — it takes the push right out of every pull-request run at the cost of one
secret, which no pin can do.

**Answer:** (a) — the scan keeps logging in with `DOCKERHUB_PAT`, without the Trivy pin (Q2); the
owner's accepted risk, ADR 0061 D7.

### Q2: Which actions that can reach the publishing token name a commit SHA?

A tag is followed wherever its repository points it, at the next run. A SHA makes a new commit
wait for Renovate's next run and leaves a pull request on record; it does not get it read, since
those pull requests automerge.

- **(a) Trivy only:** a moved tag of any other of these actions reaches the token at the next pull
  request or release.
- **(b) Every third-party action of the two jobs:** the eight packages in a `pinDigests` rule of
  `renovate.json`; the list has to follow every action either job gains.
- **(c) Every action of every workflow:** Renovate's `helpers:pinGitHubActionDigests` preset,
  GitHub's own actions included; no list to keep; the most Renovate pull requests, which merge
  themselves.

Recommended: **(c)** — the rule does not depend on knowing which step can reach which token.

**Answer:** none — no commit SHA anywhere: every action is named by a version tag, which may float.
The Trivy steps name the release `v0.36.0`, and `renovate.json` has no `pinDigests` rule. ADR 0061
D7.

## Not verified

- The `PATH` lookup through which a step before the login reaches the token was read in
  `@docker/actions-toolkit` `v0.94.0` (the range `docker/login-action` `v4` names) and in
  `@actions/exec` at the head of `actions/toolkit`, not in the bundle the action ships.

## Related

- T54 — the GitHub App token and the job tokens that write, within reach of actions named by a
  tag; closed by the same decision.
