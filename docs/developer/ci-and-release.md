# Continuous integration and the release

The two workflows, Renovate, and what is not verified yet.

**"Test and Release"** ([`release.yml`](../../.github/workflows/release.yml)) runs on every push
and PR to `main`: `linter`, `gosec`, `govulncheck`, `cyclomatic-complexity`, `malware-scan`,
`unit-tests`, `integration-tests` (a `postgres:18` service container), `frontend`, `helm`,
`container-malware-scan` (one leg per image: builds the `Containerfile` from its directory,
Trivy at CRITICAL/HIGH), `coverage-report` (merges the backend profiles, comments the PR with
the per-package table, the difference to `main` and the frontend lines percentage, writes the
badge on `main`), `release-tooling`. On a push to `main`, `semantic-release` — which `needs:`
every one of them — cuts a release from the conventional commits with a GitHub App token and
commits the badge.

**"Release Docker & Helm"** ([`build.yml`](../../.github/workflows/build.yml)) runs on the
published release: builds and pushes `guidedtraffic/cowork-backend:<version>` and
`guidedtraffic/cowork-frontend:<version>` with provenance and SBOM, scans them, packages the
chart with the release version and publishes it to the `gh-pages` branch and the release
assets.

**Renovate** ([`renovate.yml`](../../.github/workflows/renovate.yml), [`renovate.json`](../../renovate.json))
runs daily on a self-hosted runner: minor and patch updates automerge after CI, majors wait
for a review (except GitHub Actions).

Every job says `runs-on: self-hosted`, inherited from the sibling project and **not yet
verified** for this repository; neither are the secrets `DOCKERHUB_PAT`, `APP_CLIENT_ID`,
`APP_PRIVATE_KEY` or the `gh-pages` branch. Questions Q-G5 to Q-G7 in
[the catalog](../planning/questions.md).
