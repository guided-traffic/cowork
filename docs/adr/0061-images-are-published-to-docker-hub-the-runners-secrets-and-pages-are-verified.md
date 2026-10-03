# ADR 0061: Images Are Published to Docker Hub Under `guidedtraffic/`; the Self-Hosted Runners, the Release App Secrets and GitHub Pages Are Verified for This Repository

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog questions on
the container registry, the CI runners and the release secrets: Docker Hub, as the workflows
assume, over GitHub Container Registry (the recommendation) and over publishing to both. The
verifications of D2–D4 were made read-only against GitHub on 2026-10-01 and answer the
runner and secrets questions without a decision.

**Partly built.** The workflows push to `guidedtraffic/cowork-backend` and
`guidedtraffic/cowork-frontend` and log in with `DOCKERHUB_PAT`; the secret does not exist on
this repository yet (D1); the runner-image gaps of D5 are open. Amended 2026-10-02 (D5): the
runner has a Docker socket after all, verified from the logs of run 36909513652.

## Context

The pipeline was copied from the sibling project with its assumptions: `runs-on: self-hosted`,
Docker Hub under the `guidedtraffic` organisation with a `DOCKERHUB_PAT` repository secret, a
GitHub App token from `APP_CLIENT_ID` and `APP_PRIVATE_KEY` for semantic-release and
Renovate, and a `gh-pages` branch served by GitHub Pages for the chart. The catalog asked
whether each holds for this repository. The owner committed and pushed the skeleton
(`4216da8 feat: initial commit`), Renovate ran, and its branches ran the "Test and Release"
workflow, which produced the evidence below.

## Decision

**D1 — The images are published to Docker Hub** as `guidedtraffic/cowork-backend` and
`guidedtraffic/cowork-frontend`, as `build.yml` and the chart's default `image.repository`
values already say. The owner creates `DOCKERHUB_PAT` as a repository secret of
`guided-traffic/cowork`, as the sibling project has it; until then every job that logs in to
Docker Hub fails with "Password required" and the release cannot publish.

**D2 — Verified: the self-hosted runner pool serves this repository.** The Renovate workflow
(`runs-on: self-hosted`) completed successfully on `main` on 2026-10-01, and every Go job of
"Test and Release" — linting, gosec, govulncheck, cyclomatic complexity, unit tests,
integration tests with the PostgreSQL service container, the coverage report — succeeded on
the Renovate branches. The runners are Actions Runner Controller pods (job-completed hooks
under `/etc/arc/`). No decision is needed.

**D3 — Verified: the GitHub App secrets are available.** Renovate authenticated with
`APP_CLIENT_ID` and `APP_PRIVATE_KEY` and opened pull requests; the same secrets serve
semantic-release. No decision is needed.

**D4 — Verified: `gh-pages` exists and GitHub Pages serves it** at
`https://guided-traffic.github.io/cowork/` (source branch `gh-pages`, path `/`, HTTPS
enforced). The chart release of `build.yml` has its target.

**D5 — Recorded, not decided here: the ARC runner image lacks `make` and `libatomic`** *(amended
2026-10-02: ~~and a Docker socket~~ — the run's logs show a Docker Engine 29.5.3 at
`unix:///run/docker.sock`, and both container-scan legs fail at "Login to Docker Hub" with
"Password required", which is D1's missing secret)*. Observed on 2026-10-01 (run 36909513652):
the Helm and release-tooling jobs fail with `make: command not found`; the frontend job's
Node binary fails to load `libatomic.so.1`. The Go jobs pass because `setup-go` brings its
toolchain and `make` is only missing where no `apt-get` step installed `build-essential`.
This is pipeline work — a ticket, the first one when the catalog is done — and either a
runner image with those tools or install steps in the affected jobs; the choice is made in
that ticket, not in this record.

**D6 — The repository is public.** Verified; the embargo rules of [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
D7 apply from the first security ticket, and the README's coverage and Go Report Card badges
resolve.

## Consequences

- One secret to create and the publishing path works as written; Docker Scout and the SBOM
  upload stay in `build.yml`.
- The runner, the App token and Pages need no change.
- D5 and D1 block five gates today; the first pipeline ticket fixes them before any release.
- The planning catalog's questions on registry, runners and secrets are closed; the
  repository-visibility question of the process block is answered by D6.

## Alternatives Considered

- **GitHub Container Registry with `GITHUB_TOKEN`** — the recommendation. No secret to
  manage, a token per run, packages beside the repository; Docker Scout would have gone. The
  owner chose Docker Hub as the organisation's home for images. Lost.
- **Publishing to both registries.** Double maintenance for no consumer. Lost.

## Residual risks

- A repository secret is manual work the owner has to do once; until then the release path is
  red, visibly.
- Docker Hub rate limits apply to anonymous pulls of the published images; the operations
  page notes it for installations without a pull secret.

## References

- [`.github/workflows/build.yml`](../../.github/workflows/build.yml), [`release.yml`](../../.github/workflows/release.yml) — the login, the image names, the runner label
- [`deploy/helm/cowork/values.yaml`](../../deploy/helm/cowork/values.yaml) — the default repositories
- [ADR 0003](0003-test-and-ci-policy.md) D4 — every job a required gate, which is why D5 matters
- [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D7 — the embargo a public repository needs
