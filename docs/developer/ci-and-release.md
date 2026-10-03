# Continuous integration and the release

The two workflows, Renovate, and where the state of the runners and secrets is recorded.

**"Test and Release"** ([`release.yml`](../../.github/workflows/release.yml)) runs on every push
and PR to `main`: `linter`, `gosec`, `govulncheck`, `cyclomatic-complexity`, `malware-scan`,
`unit-tests`, `integration-tests`, `frontend`, `helm`, `container-malware-scan` (one leg per
image: builds the `Containerfile` from its directory, Trivy at CRITICAL/HIGH), `coverage-report`
(merges the backend profiles, comments the PR with the per-package table, the difference to
`main` and the frontend lines percentage, writes the badge on `main`), `release-tooling`. On a
push to `main`, `semantic-release` — which `needs:` every one of them — cuts a release from the
conventional commits with a GitHub App token and commits the badge.

Two jobs need more explanation than their targets:

- **`linter`** (Code Linting) runs `make lint` and then `make generate-check`: the generated
  files — the bundled API document, the oapi-codegen and sqlc output, the problem-code enum and
  the README table — must be what `make generate` writes from the sources
  ([build-test-lint.md](build-test-lint.md#generated-code)).
- **`integration-tests`** has a `postgres:18` service container, whose `cowork` superuser is the
  administrative URL in `COWORK_TEST_DATABASE_URL`. The S3 server is started by `make minio-up`
  on the job's Docker daemon, because a service container takes no command and the Chainguard
  MinIO image needs `server /data`; `make test-integration-coverage` reads `COWORK_TEST_S3_*` from
  the Makefile's defaults, and `make minio-down` runs `if: always()`. Both variables are required
  by the tests, so a job that loses one fails instead of passing on zero tests.

**"Release Docker & Helm"** ([`build.yml`](../../.github/workflows/build.yml)) runs on the
published release: builds and pushes `guidedtraffic/cowork-backend:<version>` and
`guidedtraffic/cowork-frontend:<version>` with provenance and SBOM, scans them, packages the
chart with the release version and publishes it to the `gh-pages` branch and the release
assets.

**Renovate** ([`renovate.yml`](../../.github/workflows/renovate.yml), [`renovate.json`](../../renovate.json))
runs daily on a self-hosted runner: minor and patch updates automerge after CI, majors wait
for a review (except GitHub Actions).

Every job says `runs-on: self-hosted`. Which of the runners, the secrets (`DOCKERHUB_PAT`,
`APP_CLIENT_ID`, `APP_PRIVATE_KEY`) and the `gh-pages` branch were verified for this
repository, and which jobs fail on what is still missing, is recorded in
[ADR 0061](../adr/0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md).
A new job goes into the `needs:` list of `semantic-release` and into the required checks of the
`main` ruleset ([adding-things.md](adding-things.md#a-ci-job)).
