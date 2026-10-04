# ADR 0003: Test, Verification and CI Policy

## Status

Accepted, amended 2026-10-01 (D2: the end-to-end row now points at
[ADR 0056](0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)).
Date: 2026-09-29. The owner asked for tests for the backend and the frontend and for a CI
pipeline built closely after the sibling project's; this record fixes what those tests have
to be and what the pipeline gates.

**Implemented** for the tiers that exist, verified on 2026-09-29 by running every target
locally (`make lint`, `make cyclo`, `make gosec`, `make vuln`, `make test-unit`,
`make test-integration` against `postgres:18`, `make frontend-lint`, `make frontend-test-coverage`,
`make frontend-build`, `make helm-lint`, `make helm-template`, `make test-release-tooling`,
`make docker-build`) and by running both built images together, read-only, against the same
database (the nginx proxy path included). **Not built:** the end-to-end tier (D2, row "E2E") — decided, no test exists.
Amended 2026-10-02 (D2's integration row and D3: the integration tier needs an S3-compatible
server since attachments exist). Re-verified on 2026-10-02 for the backend tiers, the chart and
the images after phase 2. Amended 2026-10-03 (this Status, D2's unit row and the residual
risks: the runners and secrets are verified, the backend has no SPA fallback). Amended 2026-10-04
(D2's integration row and D3: the integration tier needs an OpenID Connect issuer, the Dex of
`make dex-up`, since the login through the identity provider exists).
~~**Not verified:** that the `self-hosted` runner pool of the sibling project serves this
repository, and that the repository secrets the workflows name exist (D9); both are open
questions in the planning catalog.~~ *(Amended 2026-10-03: verified for this repository in
[ADR 0061](0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md)
D2–D4 — the runners serve it, the release App secrets work; `DOCKERHUB_PAT` is the one secret
still to be created, D1 there.)*

## Context

The sibling project's policy exists because tests that were skipped in CI had been failing
unnoticed, because a gate that was not required did not gate, and because a release broke on a
dependency set no PR had exercised. This repository inherits those rules on day one rather
than rediscovering them. It has two codebases (Go and Angular) and a database, so the tiers
are named here.

## Decision

**D1 — `make` is the entry point, in CI and locally.** Every CI job runs a Makefile target;
a developer runs the same target and gets the same result. The Makefile pins tool versions
with `# renovate:` comments and installs each tool under a versioned file name in `bin/`, so
a bump is a missing file and installs itself.

**D2 — The tiers, and what each needs.**

| Tier | Target | Build tag | Needs | Proves |
|---|---|---|---|---|
| Backend unit | `make test-unit` | none | nothing running | Configuration parsing, the handler, ~~the SPA fallback~~ *(amended 2026-10-03: the backend serves no UI since the two containers of [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md))*, the server lifecycle, the migration set's well-formedness — against `httptest`, `fstest.MapFS` and injected lookups |
| Backend integration | `make test-integration` | `integration` | PostgreSQL 18 at `COWORK_TEST_DATABASE_URL`; *(added 2026-10-02)* an S3-compatible server at `COWORK_TEST_S3_*` (`make minio-up`); *(added 2026-10-04)* an OpenID Connect issuer at `COWORK_TEST_OIDC_ISSUER` (`make dex-up`; `make dev-up` starts all three) | What only the database decides: the migrations apply and are idempotent, the schema relies on 18, the recorded version matches; *(added 2026-10-02)* the store and the whole API against both servers, every response checked against the document |
| Frontend unit | `make frontend-test` | — (vitest, jsdom) | Node.js | Components and services against `HttpTestingController`; no browser |
| Chart | `make helm-lint`, `make helm-template` | — | Helm | The chart lints strictly and renders with each `ci/*-values.yaml` |
| Container | the `container-malware-scan` job, one leg per image | — | Docker | Each `Containerfile` builds from its own directory on a clean checkout; each image passes Trivy at CRITICAL/HIGH |
| Release tooling | `make test-release-tooling` | — (node) | Node.js | The semantic-release dependency set still renders release notes |
| End-to-end | `make e2e` (planned) | — | the built images, PostgreSQL, MinIO, Dex, a browser | A person and a token can do a workflow through the real API and UI. Decided in [ADR 0056](0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md): Playwright against the built containers, two identities, both colour schemes, a required gate from the first workflow *(amended 2026-10-01)* |

**D3 — No `-short`, no `testing.Short()`, no skip on a missing dependency.** The integration
tier fails when `COWORK_TEST_DATABASE_URL` is unset and says how to set it *(amended
2026-10-02: and when the `COWORK_TEST_S3_*` variables of the attachment tests are)* *(amended
2026-10-04: and when `COWORK_TEST_OIDC_ISSUER` is unset or its discovery does not answer)*. A test
CI never runs is not a test.

**D4 — A gate that is not required is not a gate.** The `semantic-release` job lists every
verification job in `needs:`; a new job is added to that list in the change that adds it.

**D5 — Coverage is measured and shown, not enforced by a number.** Backend unit and
integration profiles are merged per PR into a comment with the per-package table and the
difference to `main`; the frontend lines percentage is in the same comment; the backend
number becomes the badge semantic-release commits on `main`. No threshold fails a build;
the reader does.

**D6 — Static analysis is part of the build.** `golangci-lint` with the enabled set in
[`backend/.golangci.yml`](../../backend/.golangci.yml), `gosec`, `govulncheck`, `gocyclo` at
threshold 15 (tests excluded), ESLint through `ng lint`. Every Go tool runs inside `backend/`,
so the frontend tree is never in its path.

**D7 — Supply chain: the source and the image are scanned on every PR.** ClamAV over the
tree, Trivy over the image (vulnerabilities, secrets, misconfiguration), `npm audit signatures`
before the release tooling runs. A finding at CRITICAL or HIGH with a fix available fails.

**D8 — The frontend is linted, tested and built on every PR, and its image is built and
scanned like the backend's.** `frontend/dist/` is never committed; `make frontend-build` and
`frontend/Containerfile` produce it.

**D9 — Releases are cut by semantic-release from `main` with conventional commits,** with a
GitHub App token so the release event starts the image and chart workflow. Renovate keeps the
dependencies moving with the automerge rules in [`renovate.json`](../../renovate.json): minor
and patch after CI, major by hand except GitHub Actions.

**D10 — A fix comes with the test that failed without it.** Where a defect could not be
reproduced in a tier, the ticket says so and the ADR or page that describes the behaviour
names the gap.

## Consequences

- CI installs the Go tools on every run (a few minutes); the sibling project accepted the same.
- The integration tier needs Docker locally (`make postgres-up`) or a PostgreSQL 18 at hand.
- Two lockfiles (`package-lock.json` for the release tooling, `frontend/package-lock.json`
  for the UI) and two `npm ci` steps.
- No coverage threshold means coverage can drift down without a red check; the comment makes
  it visible on every PR.

## Alternatives Considered

- **testcontainers for the integration tier** instead of a service container and a Make
  target. Self-contained, but heavier, and the same Docker dependency; the environment-variable
  contract works for both a local container and a CI service. Lost for now.
- **Karma/Chrome for the frontend tier.** The Angular CLI default is vitest on jsdom, which
  needs no browser on the runner. Kept the default.
- **A coverage threshold.** Fails builds on refactors that remove code and invites tests
  written for the number. Lost.

## Residual risks

- The end-to-end tier does not exist; nothing today proves that the UI and the API work
  together beyond the version footer test in the frontend tier and the manual run recorded in
  the Status.
- ~~`runs-on: self-hosted` is inherited, not verified for this repository (Status).~~
  *(Amended 2026-10-03: the runners serve this repository; their image lacks `make` and
  `libatomic1`, which the jobs install themselves — ADR 0061 D2, D5.)*
- The `malware-scan` job installs ClamAV on every run and depends on the runner allowing
  `sudo apt-get`; same as the sibling project.

## References

- [`Makefile`](../../Makefile) — every target named above
- [`.github/workflows/release.yml`](../../.github/workflows/release.yml) — "Test and Release"
- [`.github/workflows/build.yml`](../../.github/workflows/build.yml) — "Release Docker & Helm"
- [`backend/.golangci.yml`](../../backend/.golangci.yml), [`renovate.json`](../../renovate.json), [`.releaserc.json`](../../.releaserc.json)
- [`backend/test/integration/migrate_test.go`](../../backend/test/integration/migrate_test.go) — the integration tier
- [docs/developer/testing.md](../developer/testing.md) — the fixtures and the environment variables
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
