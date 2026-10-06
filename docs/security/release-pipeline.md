# The pipeline: what its jobs hold and the third-party code that runs beside it

Which credentials the three workflows hand their jobs, which third-party actions run in the same
jobs, how those actions are named, and what that leaves open, as built on 2026-10-06. How a person
checks a `cowork-mcp` binary of a release is [agent-client.md](agent-client.md#h-35); what the
running installation trusts is [trust-boundaries.md](trust-boundaries.md). The decisions behind
this page are [ADR 0061](../adr/0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md)
(the secrets, D7 the action references and the permissions) and
[ADR 0003](../adr/0003-test-and-ci-policy.md) D9 (Renovate's automerge).

## The jobs and what they hold

Every job runs on the self-hosted runners (ADR 0061 D2). A job not in the table runs GitHub's own
actions (`actions/*`) only, with the workflow's job token.

| Workflow, job | Runs at | Holds | Third-party actions, as named |
|---|---|---|---|
| [`release.yml`](../../.github/workflows/release.yml) `container-malware-scan`, one leg per image | every push to `main`, every pull request for `main` — Renovate's among them —, a manual start | `DOCKERHUB_PAT`, the token the release build publishes `guidedtraffic/cowork-backend` and `guidedtraffic/cowork-frontend` with; the workflow's job token: `contents` read, `pull-requests` and `issues` write | `docker/setup-buildx-action@v4` (before the login), `docker/login-action@v4`, `docker/build-push-action@v7`, `aquasecurity/trivy-action@v0.36.0` twice |
| `release.yml` `helm` | the same | the workflow's job token | `azure/setup-helm@v5` |
| `release.yml` `coverage-report` | the same | job token: `contents` read, `pull-requests` write | `marocchino/sticky-pull-request-comment@v3`, on a pull request |
| `release.yml` `semantic-release` | a push to `main` | the GitHub App installation token with `contents` write; job token: `contents`, `issues`, `pull-requests` and `id-token` write | none; the release tooling's npm packages from [`package-lock.json`](../../package-lock.json), their signatures checked by `npm audit signatures` before it runs |
| [`build.yml`](../../.github/workflows/build.yml) `build`, one leg per image | a published release | `DOCKERHUB_PAT`, `PRIMEUI_LICENSE`; the workflow's job token: `contents`, `pages`, `attestations` and `id-token` write, `actions` read — the job declares no `permissions:` of its own | `docker/setup-qemu-action@v4` and `docker/setup-buildx-action@v4` (before the login), `docker/login-action@v4`, `docker/metadata-action@v6`, `docker/build-push-action@v7`, `anchore/sbom-action@v0`, `softprops/action-gh-release@v3`, `docker/scout-action@v1`, which takes `DOCKERHUB_PAT` as its input `dockerhub-password` |
| `build.yml` `release-mcp` | a published release | job token: `contents`, `id-token` and `attestations` write | `softprops/action-gh-release@v3` |
| `build.yml` `release-helm-gh` | a published release | job token: `contents` and `pages` write | `azure/setup-helm@v5`, `softprops/action-gh-release@v3` |
| [`renovate.yml`](../../.github/workflows/renovate.yml) `renovate` | every night at 02:00 Berlin time, a manual start | the GitHub App installation token with every permission of the App — the step that makes it passes no `permission-*` input | `renovatebot/github-action@v46.3.7`, which takes that token as its input `token` |

A third-party action reaches more than its inputs:

- **`DOCKERHUB_PAT` is within reach of every step of a job that logs in**, not only of the login.
  `docker/login-action` leaves the token in the runner's Docker configuration for the steps after
  it, and a step before it can, through `$GITHUB_PATH`, put a `docker` of its own in front of the
  one the login runs, which it looks up on the `PATH`. The lookup was read in `@docker/actions-toolkit` `v0.94.0` (the range
  `docker/login-action@v4` names) and in `@actions/exec`, not in the bundle the action ships.
- **The job token is within reach of most of them.** `actions/checkout@v7` keeps it in the
  checkout's git configuration (`persist-credentials` defaults to `true`), and
  `docker/metadata-action@v6`, `docker/build-push-action@v7`, `anchore/sbom-action@v0`,
  `softprops/action-gh-release@v3`, `docker/scout-action@v1`,
  `marocchino/sticky-pull-request-comment@v3` and `azure/setup-helm@v5` take it as an input that
  defaults to `${{ github.token }}` — read in each action's manifest at the tag the workflows name,
  on 2026-10-06.
- **A job with `id-token: write` lets any of its steps ask for an OIDC token of its workflow.** In
  `release-mcp`, `actions/attest-build-provenance@v4` signs the provenance of the `cowork-mcp`
  binaries under that identity and stores it with `attestations: write`
  ([ADR 0041](../adr/0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md)
  D2); `build` holds both permissions too.

## How an action is named

Every `uses:` of the three workflows names a version tag — a major (`actions/checkout@v7`) or a
full release (`aquasecurity/trivy-action@v0.36.0`, `renovatebot/github-action@v46.3.7`) — never a
branch and never a commit SHA (ADR 0061 D7). A tag is followed wherever its repository points it,
at the job's next run, and the workflow file names the same tag before and after. Renovate's
github-actions manager moves the tags to new releases, and its pull requests for actions merge
themselves once the required checks pass — majors included
([`renovate.json`](../../renovate.json), ADR 0003 D9). `aquasecurity/trivy-action@v0.36.0` installs
Trivy `v0.70.0`, the action's default there: no step sets its `version` input.

## What this does not cover

<a id="h-59"></a>
### H-59 — The container scan of every pull request holds the publishing token

Accepted by the owner (ADR 0061 D7). `container-malware-scan` logs in to Docker Hub with
`DOCKERHUB_PAT`, the token that publishes the images, though it only pulls base images. Every step
of the job can reach it (above), and the job runs for every pull request from a branch of this
repository — a Renovate pull request among them. When Renovate proposes a new release of an action of
the job, its pull request runs that release in the scan with the token and merges itself once the
checks are green, before anyone has read the new code; a commit SHA in the workflow would not have kept that
run out. What a compromised step could do with the token: push an image under `guidedtraffic/` —
`cowork-backend` and `cowork-frontend` among it — that installations then pull. No attack path
exists without a compromised upstream of one of these actions. A read-only Docker Hub token for the
scan would close it for pull requests; the owner kept the publishing token. Mitigation: none in this
repository.

<a id="h-60"></a>
### H-60 — An action runs from a tag its repository can move

Accepted by the owner (ADR 0061 D7). When the repository of an action points a tag the workflows
name at another commit, that commit runs at the job's next run with what the job holds, and this
repository's history shows no change:

- a tag of `aquasecurity/trivy-action` or of any other action of `container-malware-scan`, at the
  next pull request or push to `main`, or of `build`, at the next release: with `DOCKERHUB_PAT` —
  push rights to the `guidedtraffic/cowork-*` images;
- a tag of `renovatebot/github-action`, at the next night: with the App installation token and
  every permission of the App, which is the organisation's setting and not read from here;
- a tag of `actions/create-github-app-token`, at the next night or push to `main`: with the App's
  private key `APP_PRIVATE_KEY`, from which it makes the token;
- a tag of an action of `build`, `release-mcp` or `release-helm-gh`, at the next release: with a
  job token that writes the repository's contents and its releases ([H-61](#h-61));
- a tag of an action of any other job of `release.yml`, at the next pull request or push to
  `main`: with a job token that writes comments on pull requests and issues — every job there
  without a block of its own inherits `pull-requests` and `issues` write.

A commit SHA would make a new commit wait for Renovate's next run and leave a pull request on
record; that pull request merges itself all the same. The owner names actions by version tags.
Mitigation: none in this repository beyond the choice of actions — GitHub's, Docker's, Anchore's,
Aqua Security's, Azure's, Renovate's, and two published from personal GitHub
accounts (`softprops/action-gh-release`, `marocchino/sticky-pull-request-comment`).

<a id="h-61"></a>
### H-61 — The release build runs with the whole workflow's permissions

Accepted by the owner (ADR 0061 D7). The `build` job declares no `permissions:` and inherits
`build.yml`'s block — `contents`, `pages`, `attestations` and `id-token` write, `actions` read —
which no other job uses: `release-mcp` and `release-helm-gh` declare their own. Any of its eight
third-party steps can therefore replace an asset of the release with `contents: write`, and can ask
for an OIDC token of the release workflow — the identity `release-mcp` attests the `cowork-mcp`
binaries under — and store an attestation with `attestations: write`: a compromised step could
attest a binary it did not build, and `gh attestation verify --repo guided-traffic/cowork` would
then not tell it apart ([agent-client.md](agent-client.md#h-35) H-35). That such an attestation
passes the check is inferred from the shared workflow and ref, not tried. A step of `build` uses
`contents` for the SBOM's upload to the release; whether one uses `pages`, `attestations`,
`id-token` or `actions` is not traced through the actions' code. Narrowing the job's block would
close this; the owner keeps it.

### The organisation's side

Which repositories of `guided-traffic` may read the organisation secrets, which permissions the
App behind `APP_CLIENT_ID` holds, what `DOCKERHUB_PAT` may push beyond the two images, and whether
a runner pod serves more than one job, are the organisation's and Docker Hub's settings, read from
none of the files here. GitHub hands no secret to the run of a pull request from a fork and gives
its job token read access only — GitHub's documented rule, not tried here.
