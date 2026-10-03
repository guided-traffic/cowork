---
id: T25
title: the container scan runs an unpinned third-party action after logging in to Docker Hub with the publishing token
state: analysed
severity: high
security: hardening
threat: pinning and a read-only token would additionally cover a compromised aquasecurity/trivy-action master branch that reads the DOCKERHUB_PAT the job's login leaves in the runner's Docker configuration and pushes images under guidedtraffic/
urgency: later        # rule 4: a cheap known fix exists (the pin); the token split waits for Q1
effort: XS
filed-from: the phase-2 pipeline work, when DOCKERHUB_PAT became an organisation secret
opened: 2026-10-03
decided:
done:
---

## Current state

- The job "Container Malware Scan" ([`release.yml`](../../.github/workflows/release.yml), the
  `container-malware-scan` job) logs in with `docker/login-action@v4` and the organisation
  secret `DOCKERHUB_PAT` (ADR 0061 D1), builds the image, and then runs
  `aquasecurity/trivy-action@master` twice — a third-party action taken from the head of its
  default branch, not from a release or a commit. Every other action of the workflows names a
  version tag (`@v7`, a major tag such as `@v0`, `@v46.3.6`).
- The job runs on every pull request from a branch of this repository, Renovate's branches
  included, whose minor and patch updates automerge after CI. The same secret is the token
  `build.yml` publishes the images with, so whatever runs in the scan job after the login can
  read a token that pushes under `guidedtraffic/`.
- No attack path exists today: it needs the action's upstream branch to be compromised.

## Required changes

### Independent of the open question

1. Both `trivy-action` steps name a commit SHA with the release in a comment, and Renovate keeps
   the digest current (its github-actions manager with `pinDigests`).

### Depends on the answer

2. The token the scan job logs in with, per Q1.

## Open questions

### Q1: Which Docker Hub token does the container scan log in with?

The scan only pulls base images; it logs in so that the runners, which share one address, stay
within Docker Hub's pull limit.

- **(a) Keep the publishing token,** pinned actions only: nothing to create; a compromised step
  of the scan job can still push.
- **(b) A read-only token for the scan job** (a second organisation secret, a Docker Hub access
  token with public read only), the publishing token only in `build.yml`, which runs on a
  release: a pull request's job can no longer push; one more secret to rotate.
- **(c) No login in the scan job:** anonymous pulls under the shared address's limit; a burst
  of Renovate pull requests can fail on rate limits.

Recommended: **(b)** — it takes the push right out of every pull-request run at the cost of one
secret, and the pin of item 1 covers the rest.

**Answer:** _open_
