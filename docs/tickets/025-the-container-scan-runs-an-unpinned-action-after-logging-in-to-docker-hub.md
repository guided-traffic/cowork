---
id: T25
title: the container scan of every pull request logs in to Docker Hub with the publishing token
state: analysed
severity: high
security: hardening
threat: a read-only token would additionally cover a compromised step of the scan job — a new release of aquasecurity/trivy-action, which Renovate's own pull request runs, or an action named by a tag — that reads the DOCKERHUB_PAT the job's login leaves in the runner's Docker configuration and pushes images under guidedtraffic/
urgency: later        # rule 4: each answer of Q1 is a cheap known fix
effort: XS
filed-from: the phase-2 pipeline work, when DOCKERHUB_PAT became an organisation secret
opened: 2026-10-03
decided:
done:
---

## Current state

- The job "Container Malware Scan" ([`release.yml`](../../.github/workflows/release.yml), the
  `container-malware-scan` job) logs in with `docker/login-action@v4` and the organisation
  secret `DOCKERHUB_PAT` (ADR 0061 D1), builds the image with `docker/build-push-action@v7`, and
  runs `aquasecurity/trivy-action` twice. Both Trivy steps name the commit of the release
  `v0.36.0` (`ed142fd0673e97e23eac54620cfb913e5ce36c25`, the commit its signed tag points to)
  with the tag in a trailing comment; a `pinDigests` rule for that action in
  [`renovate.json`](../../renovate.json) keeps it on a SHA, and Renovate moves SHA and comment
  together. The two Docker actions name a tag.
- The job runs on every pull request from a branch of this repository, Renovate's branches
  included, and Renovate's updates of an action automerge after CI, majors included. The same
  secret is the token `build.yml` publishes the images with, so whatever runs in the scan job
  after the login can read a token that pushes under `guidedtraffic/` — a new release of the
  Trivy action too, which Renovate's pull request for it runs before anybody reviews it. The pin
  keeps a moved branch or tag out, not a compromised release.
- No attack path exists today: it needs the upstream of a step of the scan job to be
  compromised.
- The pinned release installs Trivy `v0.70.0`, the action's default there, because no step sets
  its `version` input; the head of the action's `master` installs `v0.75.0`. A local Renovate
  extract finds that input as the dependency `aquasecurity/trivy` and skips it for want of a
  value.

## Required changes

1. The token the scan job logs in with, per Q1.

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
secret, and it covers what the pin cannot: a compromised release that Renovate's own pull
request runs.

**Answer:** _open_
