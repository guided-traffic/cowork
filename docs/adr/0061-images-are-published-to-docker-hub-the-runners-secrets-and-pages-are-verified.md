# ADR 0061: Images Are Published to Docker Hub Under `guidedtraffic/`; the Self-Hosted Runners, the Release App Secrets and GitHub Pages Are Verified for This Repository; Actions Are Named by a Version Tag, Never a Commit SHA

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog questions on
the container registry, the CI runners and the release secrets: Docker Hub, as the workflows
assume, over GitHub Container Registry (the recommendation) and over publishing to both. The
verifications of D2–D4 were made read-only against GitHub on 2026-10-01 and answer the
runner and secrets questions without a decision.

**Built.** The workflows push to `guidedtraffic/cowork-backend` and
`guidedtraffic/cowork-frontend` and log in with `DOCKERHUB_PAT`, an organisation secret since
2026-10-03 (D1). The first release, `0.1.0` on 2026-10-03, published both images to Docker Hub
and the chart to `gh-pages`, served as the Helm repository `https://guided-traffic.github.io/cowork/`
("Release Docker & Helm", run 37108086347). Amended 2026-10-02 (D5): the runner has a Docker socket after all,
verified from the logs of run 36909513652. Amended 2026-10-03 (D5): the owner chose install
steps in the jobs for the runner-image gaps, over a runner image and over another container
mode; the integration job passed with them on the first run of the phase-2 code. Verified on
2026-10-03 (run 37105034106): every job of "Test and Release" passes on the phase-2 branch,
the container-scan legs among them — they log in with the organisation secret of D1, build
each image on the runner's Docker daemon and scan it.

Amended 2026-10-06 by the owner (D7, the Consequences, the Alternatives and the Residual
risks): every action is named by a version tag, which may float, never by a commit SHA; the
container scan keeps logging in with the publishing token; the release build keeps the workflow's
permissions — each risk the owner's, accepted under *Residual risks*. Built the same day: both
Trivy steps of the container scan name `aquasecurity/trivy-action@v0.36.0`, the action's latest
release that day, instead of its branch `master`, and [`renovate.json`](../../renovate.json)
carries no rule that pins a digest.

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
Docker Hub fails with "Password required" and the release cannot publish. *(Amended
2026-10-03: the owner created it as an organisation secret of `guided-traffic` instead, beside
`APP_CLIENT_ID` and `APP_PRIVATE_KEY`; the repository reads it. Which repositories of the
organisation may read it is the organisation's setting, not verified from here.)*

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
that ticket, not in this record. *(Amended 2026-10-03: chosen — install steps. A job that runs
a `make` target installs `make` first with `sudo apt-get`, as the Go jobs install
`build-essential`, and the frontend job installs `libatomic1` before `setup-node`. The
repository owns those lines; the shared runner scale set and the sibling project's jobs stay
as they are, and each run pays the `apt-get` time.)*

**D6 — The repository is public.** Verified; the embargo rules of [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
D7 apply from the first security ticket, and the README's coverage and Go Report Card badges
resolve.

**D7 — Every action is named by a version tag; the container scan logs in with the publishing
token; the release build runs with the workflow's permissions** *(added 2026-10-06 by the owner)*.
Every `uses:` of the three workflows names a version tag of the action — a major such as
`actions/checkout@v7`, or a full release such as `aquasecurity/trivy-action@v0.36.0` where the
action publishes no major tag — and never a branch or a commit SHA; a tag may float, and the
workflows follow it. Renovate's github-actions manager keeps the tags current under the automerge
rules of [ADR 0003](0003-test-and-ci-policy.md) D9, and `renovate.json` has no `pinDigests` rule.
The `container-malware-scan` job of `release.yml` logs in to Docker Hub with `DOCKERHUB_PAT`, the
token the release build publishes the images with; there is no read-only token for the scan. The
`build` job of `build.yml` declares no `permissions:` of its own and runs with the workflow's
block: `contents`, `pages`, `attestations` and `id-token` write, `actions` read.

## Consequences

- One secret to create and the publishing path works as written; Docker Scout and the SBOM
  upload stay in `build.yml`.
- The runner, the App token and Pages need no change.
- D5 and D1 block five gates today; the first pipeline ticket fixes them before any release.
- The planning catalog's questions on registry, runners and secrets are closed; the
  repository-visibility question of the process block is answered by D6.
- *(Added 2026-10-06.)* D7 keeps one Docker Hub secret, one permissions block in `build.yml` and
  one form of reference in every workflow, and Renovate opens no digest pull requests for actions.

## Alternatives Considered

- **GitHub Container Registry with `GITHUB_TOKEN`** — the recommendation. No secret to
  manage, a token per run, packages beside the repository; Docker Scout would have gone. The
  owner chose Docker Hub as the organisation's home for images. Lost.
- **Publishing to both registries.** Double maintenance for no consumer. Lost.
- *(Added 2026-10-06.)* **A commit SHA for every action**, its tag in a trailing comment, through
  Renovate's `helpers:pinGitHubActionDigests` preset — the recommendation — or for the actions that
  can reach the publishing token only. A new commit of an action would have waited for Renovate's
  next run and left a pull request on record, which merges itself after CI all the same. The owner
  names actions by versions that may float. Lost.
- *(Added 2026-10-06.)* **A read-only Docker Hub token for the container scan**, the publishing
  token only in `build.yml` — the recommendation: it takes the push right out of every pull
  request's run, at the cost of a second secret. The owner keeps the publishing token. Lost.
- *(Added 2026-10-06.)* **The `build` job with a `permissions:` block of its own**, the workflow's
  dropped to `contents: read`. The owner keeps the workflow's block. Not built.

## Residual risks

- A repository secret is manual work the owner has to do once; until then the release path is
  red, visibly.
- Docker Hub rate limits apply to anonymous pulls of the published images; the operations
  page notes it for installations without a pull secret.
- *(Added 2026-10-06, the owner's accepted risks of D7; none has an attack path without a
  compromised upstream of an action.)*
  - **A moved tag runs with the publishing token.** A tag of `aquasecurity/trivy-action` or of any
    other action of `container-malware-scan` or of `build` that its repository points at another
    commit runs that commit with `DOCKERHUB_PAT` — push rights to the `guidedtraffic/cowork-*`
    images — at the next pull request, push to `main` or release, and the workflow file shows no
    change. A Renovate pull request is such a pull request: one for a new release of an action of
    the scan runs it there with the token and merges itself once CI is green, before anyone reads
    it ([release-pipeline.md](../security/release-pipeline.md#h-59) H-59,
    [H-60](../security/release-pipeline.md#h-60)).
  - **The release build runs with the workflow's permissions.** Any of the eight third-party steps
    of `build` holds a job token that writes the repository's contents and releases and Pages, and
    may ask for an OIDC token of the release workflow and store an attestation — the identity under
    which `release-mcp` attests the `cowork-mcp` binaries
    ([release-pipeline.md](../security/release-pipeline.md#h-61) H-61).
  - **The Renovate workflow's actions run with the GitHub App.** `renovatebot/github-action@v46.3.7`
    takes the App installation token with every permission of the App as its input, and
    `actions/create-github-app-token@v3` takes `APP_PRIVATE_KEY`; a moved tag of either runs with
    what it takes at the next night ([release-pipeline.md](../security/release-pipeline.md#h-60) H-60).
  - **The other jobs' tokens.** A moved tag in `release-mcp` or `release-helm-gh` runs with a job
    token that writes the repository's contents and releases at the next release, one in a job of
    `release.yml` with a job token that writes comments on pull requests and issues at the next pull
    request ([release-pipeline.md](../security/release-pipeline.md#h-60) H-60).

## References

- [`.github/workflows/build.yml`](../../.github/workflows/build.yml), [`release.yml`](../../.github/workflows/release.yml) — the login, the image names, the runner label
- [`deploy/helm/cowork/values.yaml`](../../deploy/helm/cowork/values.yaml) — the default repositories
- [`renovate.json`](../../renovate.json), [`renovate.yml`](../../.github/workflows/renovate.yml) — the rules that move the action tags, and the App token Renovate runs with (D7)
- [docs/security/release-pipeline.md](../security/release-pipeline.md) — what each job holds, and H-59 to H-61
- [ADR 0003](0003-test-and-ci-policy.md) D4 — every job a required gate, which is why D5 matters
- [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D7 — the embargo a public repository needs
