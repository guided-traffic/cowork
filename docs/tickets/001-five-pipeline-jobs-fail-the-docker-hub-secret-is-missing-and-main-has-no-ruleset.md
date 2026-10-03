---
id: T1
title: five pipeline jobs fail on main until the phase-2 branch lands, and main has no ruleset
state: in-progress
severity: high
security: none
threat:
urgency: release      # rule 2: gates the release
effort: S
blocked-by: human
filed-from: ADR 0061 D5 and ADR 0073 (the pipeline ticket of ADR 0074 D2)
opened: 2026-10-02
decided: 2026-10-03
done:
---

## Current state

Decided by [ADR 0061](../adr/0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md)
D1, D5, [ADR 0073](../adr/0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md)
D1–D7 and [ADR 0003](../adr/0003-test-and-ci-policy.md) D1, D4.

- Five of the thirteen jobs of "Test and Release" ([`release.yml`](../../.github/workflows/release.yml))
  fail on `main` and on every Renovate branch:
  - **Helm Chart** and **Release Tooling** fail with `make: command not found`, and
    **Frontend (lint, test, build)** fails in "Setup Node.js", whose Node 26 cannot load
    `libatomic.so.1`. The runner image has neither. On the phase-2 branch these jobs install
    `make`, and the frontend job `libatomic1` before `setup-node`, the way the Go jobs install
    `build-essential` (ADR 0061 D5), and they pass; so does the integration job, whose S3
    server starts after its build tools.
  - **Container Malware Scan (backend)** and **(frontend)** fail in "Login to Docker Hub" with
    `Password required`, skip their build, and Trivy reports that it finds no image. With the
    organisation secret `DOCKERHUB_PAT` (ADR 0061 D1) both legs pass on the phase-2 branch:
    they log in, build each image on the runner's Docker daemon and scan it.
  - On the phase-2 branch all thirteen jobs pass (run 37105034106).
- The scan jobs build on a Docker Engine at `unix:///run/docker.sock` (`Server: Docker Engine -
  Community` in the "Docker info" output of `docker/setup-buildx-action`; ADR 0061 D5).
- "Release Docker & Helm" ([`build.yml`](../../.github/workflows/build.yml)) logs in to Docker
  Hub with the same secret, for the image push and the Docker Scout scan. It has not run: no
  release exists.
- `main` has no branch protection and no ruleset; merge commits, squash and rebase are all
  allowed; auto-merge and delete-on-merge are off; the squash title is `COMMIT_OR_PR_TITLE`
  (read with `gh api`). ADR 0073 D1–D5 are an administrator's act in GitHub, and ADR 0073's
  Status assigns them to this ticket.
- Once the ruleset exists, a red required check freezes `main` for everyone but a bypassing
  administrator (ADR 0073 D7): every phase-2 change waits on this ticket.

## Required changes

1. An organisation administrator applies ADR 0073: the `main` ruleset with the thirteen required
   checks by their exact job names (D1) and the administrators on the bypass list (D2); squash
   merges only, the squash title taken from the pull request title, auto-merge on, head
   branches deleted on merge (D3); the `v*` tag ruleset for the semantic-release App (D4);
   `gh-pages` left unprotected (D5).
2. Verification, recorded here when run: one green "Test and Release" run on `main` (the
   thirteen jobs and semantic-release); `gh api` reads of the rulesets and the repository
   settings; a pull request that cannot merge while a required check is red.

## Related

- The integration job starts an S3 test server beside PostgreSQL with `make minio-up` (a service
  container takes no command); the job passes on the phase-2 branch, so the job's Docker daemon
  runs it.
