---
id: T1
title: five pipeline jobs fail on main, the Docker Hub secret is missing, and main has no ruleset
state: analysed
severity: high
security: none
threat:
urgency: release      # rule 2: gates the release
effort: S
blocked-by: decision
filed-from: ADR 0061 D5 and ADR 0073 (the pipeline ticket of ADR 0074 D2)
opened: 2026-10-02
decided:
done:
---

## Current state

Decided by [ADR 0061](../adr/0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md)
D1, D5, [ADR 0073](../adr/0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md)
D1–D7 and [ADR 0003](../adr/0003-test-and-ci-policy.md) D1, D4.

- Five of the thirteen jobs of "Test and Release" ([`release.yml`](../../.github/workflows/release.yml))
  fail on `main` and on every Renovate branch; the eight Go and source jobs pass:
  - **Helm Chart** and **Release Tooling** fail with `make: command not found`. The runner image
    has no `make`; the Go jobs pass because their step "Install build tools" installs
    `build-essential` first.
  - **Frontend (lint, test, build)** fails in its step "Setup Node.js": the Node 26 that
    `actions/setup-node` installs cannot load `libatomic.so.1`. Its later steps are `make`
    targets as well.
  - **Container Malware Scan (backend)** and **(frontend)** fail in "Login to Docker Hub" with
    `Password required`: the repository has no `DOCKERHUB_PAT` (ADR 0061 D1). The build step is
    skipped, and Trivy then reports that it finds no image; its message lists the missing
    containerd and podman sockets.
- **ADR 0061 D5's "no Docker socket" does not hold.** In every scan run so far, the step
  `docker/setup-buildx-action` reaches a Docker Engine at `unix:///run/docker.sock`
  (`Server: Docker Engine - Community` in its "Docker info" output); the job fails before it
  builds. Whether the build itself works on that daemon is not known, because it has never run.
- "Release Docker & Helm" ([`build.yml`](../../.github/workflows/build.yml)) logs in to Docker
  Hub with the same secret, for the image push and the Docker Scout scan. It has not run: no
  release exists.
- `main` has no branch protection and no ruleset; merge commits, squash and rebase are all
  allowed; auto-merge and delete-on-merge are off; the squash title is `COMMIT_OR_PR_TITLE`
  (read with `gh api`). ADR 0073 D1–D5 are an administrator's act in GitHub, and ADR 0073's
  Status assigns them to this ticket.
- Statements that ADR 0061 D2–D4 made false: ADR 0003's Status ("Not verified: that the
  self-hosted runner pool … serves this repository, and that the repository secrets … exist …
  open questions in the planning catalog") and its residual risk ("`runs-on: self-hosted` is
  inherited, not verified"), and [ci-and-release.md:25-28](../developer/ci-and-release.md#L25-L28),
  which points at "Questions Q-G5 to Q-G7 in the catalog". ADR 0003 D2's unit row still proves
  "the SPA fallback", which the backend has not had since ADR 0001's two containers.
- Once the ruleset exists, a red required check freezes `main` for everyone but a bypassing
  administrator (ADR 0073 D7): every phase-2 change waits on this ticket.

## Required changes

### Independent of the open question

1. The owner creates the repository secret `DOCKERHUB_PAT` (ADR 0061 D1) — an act outside the
   repository. With it the scan legs reach their build step; their first green run is the
   proof that the daemon the pods reach builds and scans an image.
2. An organisation administrator applies ADR 0073: the `main` ruleset with the thirteen required
   checks by their exact job names (D1) and the administrators on the bypass list (D2); squash
   merges only, the squash title taken from the pull request title, auto-merge on, head
   branches deleted on merge (D3); the `v*` tag ruleset for the semantic-release App (D4);
   `gh-pages` left unprotected (D5).
3. Corrected in place: ADR 0061 D5 and its Status (the daemon exists; the gaps were `make`,
   `libatomic` and the secret) and its index row; ADR 0003's Status, its residual risk, D2's
   unit row and its index row; the last paragraph of ci-and-release.md (runners, App secrets and
   `gh-pages` verified by ADR 0061; `DOCKERHUB_PAT` the one manual secret).
4. Verification, recorded here when run: one green "Test and Release" run on `main` (the
   thirteen jobs and semantic-release); `gh api` reads of the rulesets and the repository
   settings; a pull request that cannot merge while a required check is red.

### Depends on the answer

5. `make` for Helm Chart, Release Tooling and Frontend, and `libatomic1` for Frontend before
   `setup-node` runs, by the route Q1 chooses.

## Open questions

### Q1: Do the missing `make` and `libatomic1` come from a runner image or from install steps in the jobs?

ADR 0061 D5 leaves the choice to this ticket. The runner pool is an Actions Runner Controller
scale set that the sibling project uses as well.

- **(a) A runner image** with `make`, `libatomic1` and what the jobs install today: one place,
  faster jobs; an image to build, publish and keep current outside this repository, and every
  change to it changes the sibling project's jobs too.
- **(b) Install steps in the affected jobs**, the pattern the Go jobs already use
  (`sudo apt-get update && sudo apt-get install -y …`), with `libatomic1` installed before
  `setup-node`: a few lines this repository owns and reviews, no image to maintain, nothing
  changes for the sibling project; each run pays the `apt-get` time, as the Go jobs already do.
- **(c) A different container mode or an in-job builder** — ARC's Docker-in-Docker mode, or a
  rootless buildkit or buildah step: only needed if the image build fails on the daemon the pods
  already reach. A mode switch on the shared scale set changes every job of the sibling project,
  or needs a second scale set with its own runner label.

Recommended: **(b)** — it closes both gaps with lines this repository owns, matches what the Go
jobs do, and touches nothing the sibling project depends on; (c) only if the first build after
item 1 fails for want of a daemon.

**Answer:** _open_

## Not verified

- Whether an image build and the Trivy scan work on the daemon at `/run/docker.sock`; item 1
  settles it.
- Whether that daemon is ARC's Docker-in-Docker sidecar or a host socket; it matters only for
  option (c).

## Related

- The integration job starts an S3 test server beside PostgreSQL with `make minio-up` (a service
  container takes no command); its first green run shows that the job's Docker daemon runs it.
