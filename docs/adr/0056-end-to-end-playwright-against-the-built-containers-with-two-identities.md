# ADR 0056: End-to-End Is Playwright Against the Built Containers, With Two Identities and Both Colour Schemes, a Required Gate From the First Workflow — Beside Fast Unit Tiers With Measured Line Coverage

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"end-to-end tests?": Playwright against the real images from the first workflow, over
Cypress, over end-to-end only in the hardening phase, and over end-to-end against the dev
server. The owner's two conditions — functionality must be verifiable continuously, and
there must be faster tests as well with line coverage for the frontend — are D5 and D6; the
unit tiers and the frontend coverage they name exist already. The rules of D7–D8 were put to
the owner with the question and not objected to. Amended 2026-10-04 by the owner's decision on the
routing recorded in [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3 (D1: the two images run behind a stand-in for the Ingress, since the frontend alone serves no
API). Amended 2026-10-09 by [ADR 0058](0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D2 as the owner amended it: the
stack's S3 server is PGSTY Silo, the maintained MinIO fork, in place of Chainguard's MinIO build —
`MINIO_IMAGE`, the container `<stack>-minio`; where this record says MinIO for it, it is Silo from
then on; no rule changes, and the end-to-end tier against Silo is CI's first run.

**Partly built.** The unit tiers and the frontend coverage report exist
([ADR 0003](0003-test-and-ci-policy.md), `make test`, `make frontend-test-coverage`, the
`frontend` CI job's artefact and the PR comment's frontend line). ~~The end-to-end tier is not
built;~~ ADR 0003 D2's end-to-end row points here, in place since 2026-10-01. *(Amended 2026-10-04: the end-to-end tier is
built — `frontend/e2e/`, `make e2e` ([`hack/e2e.sh`](../../hack/e2e.sh)) and the `e2e` job after
`container-malware-scan` with its images, ten minutes, trace and video on failure, in
`semantic-release`'s `needs:` (D1, D4). D3 as far as the UI has its pages: the login through the
local form, a temporary password and Dex; the session cookie, a write past the CSRF check and the
sign-out; a ticket filed and moved; a project opening on its board and a transition by drag on it;
the backlog's drag within and between horizons — every one in Chromium and WebKit (D8) and in both
schemes, and a dark-mode screenshot of a seeded board at 2 % of the pixels, which a light surface
fails and text turned dark on dark passes, measured; `data-testid` and data seeded through the API
(D7), a PrimeNG menu item and Dex's form found otherwise. Verified by running it locally on
2026-10-04. ~~**Not built:** the path with two identities in one test (D2) — filing, assigning, the
second identity's "assigned to me" and inbox within the stream's latency, its move and close with a
verification note — which waits for those pages and stands as a pending test;~~ *(2026-10-05: the
path with two identities is written — [`assigned.spec.ts`](../../frontend/e2e/assigned.spec.ts),
each identity in a browser context of its own, the pending mark gone — and passes in Chromium and
WebKit in both schemes, on a runner (run 37285901009 of commit `65337eb`) and in three local runs with two workers;
so do the tenant board's path, a transition by drag and the refusal across swimlanes
([`tenant-board.spec.ts`](../../frontend/e2e/tenant-board.spec.ts)), and the tenant's ticket list's
([`ticket-list.spec.ts`](../../frontend/e2e/ticket-list.spec.ts)).)* **Not built:** the init state
and the first tenant, a restricted project's third identity, the import and the MCP server's
`session_start` of D3. The `e2e` job ran on a runner first on 2026-10-05 (run 37267795406
of commit `dbe64f0`): passed in 3 min 9 s against its ten minutes, the stack's ports on `127.0.0.1`
reachable, the browsers installed with `--with-deps`, the Linux pictures holding on amd64. **Not
verified:** the `main` ruleset does not require the job yet
— the owner adds it ([ADR 0073](0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md)
D6). D1 amended the same day with the build, for the reasons written there: `make e2e` runs a
stack of its own instead of `make dev-up`'s containers — settled 2026-10-05 on the recommendation,
the owner reviewing the result.)*

*(Amended 2026-10-06: the paths of the first release's other views are written, and the whole tier
passes in Chromium and WebKit, in both schemes, in three local runs with two workers:
[`assigned.spec.ts`](../../frontend/e2e/assigned.spec.ts) edits the title and the body on its way;
a comment written, edited and withdrawn, a question's text edited and a new blocker reach a second
identity's open page of the ticket and its prerequisite tree
([`conversation.spec.ts`](../../frontend/e2e/conversation.spec.ts)); a body's rendered Markdown with
its own image loaded and its hostile lines as text, the shell's content-security policy refusing
nothing ([`rendered.spec.ts`](../../frontend/e2e/rendered.spec.ts)); the score's marker and the sort
by score ([`backlog.spec.ts`](../../frontend/e2e/backlog.spec.ts)); the start page of a person of one
tenant ([`start.spec.ts`](../../frontend/e2e/start.spec.ts)); the search of a tenant and of the
person's two tenants, a hit opening its comment, the policy refusing nothing
([`search.spec.ts`](../../frontend/e2e/search.spec.ts)); a saved filter one member shares and a
second person applies ([`filters.spec.ts`](../../frontend/e2e/filters.spec.ts)); the deletion, the
restoration and the purge, to a member no such ticket meanwhile
([`deletion.spec.ts`](../../frontend/e2e/deletion.spec.ts)); the dashboard with two identities, a
member counting nothing of a project restricted away from them
([`dashboard.spec.ts`](../../frontend/e2e/dashboard.spec.ts)), and its picture in both schemes beside
the board's dark one ([`visual.spec.ts`](../../frontend/e2e/visual.spec.ts)). D3's path of a third
identity outside a restricted project, seeing nothing of it anywhere, is still not built: the
dashboard's path shows the dashboard's share of it only. D7 amended the same day by the owner's answer:
roles and accessible names first, over a test id on everything a test touches.)*

## Context

The parts of cowork that unit and integration tests cannot reach are exactly the ones that
have already bitten once: the nginx proxy (resolver, buffering, writable configuration), the
cookie and the CSRF rule across two containers, the event stream through the proxy, the
roles across two identities, drag on two boards, two colour schemes. [ADR 0003](0003-test-and-ci-policy.md)
D2 promised an end-to-end tier at the first workflow; [ADR 0004](0004-cowork-is-a-team-product.md)
asks every team feature to be tested with two identities; [ADR 0038](0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
gives the fixture identities and the services to run against. A test that proves the shipped
images work together must run the shipped images, not the dev server.

## Decision

**D1 — Playwright Test, in `frontend/e2e/`, against the built containers.** The target is
the frontend and backend images of the same commit, with PostgreSQL, MinIO and the minimal
Dex ~~from the development `compose.yaml` ([ADR 0038](0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D2) as services~~ *(amended 2026-10-04: of its own, from the Makefile's images and
[`hack/dex/config.yaml`](../../hack/dex/config.yaml); there is no `compose.yaml`, ADR 0038 D2)*. *(Amended 2026-10-04: the two images run behind the stand-in for the Ingress of
[`hack/ingress/default.conf`](../../hack/ingress/default.conf), which routes `/api/` and `/auth/`
to the backend and the rest to the frontend as the chart's Ingress does; the suite talks to the
stand-in.)* Never `ng serve`, never a mocked API, in CI. Locally, `make e2e` runs the
same ~~against `make dev-up` plus locally built images~~ *(amended 2026-10-04: with locally built
images; in CI and locally alike it starts a PostgreSQL, a MinIO and a Dex of its own on a Docker
network of its own and removes them afterwards ([`hack/e2e.sh`](../../hack/e2e.sh)), because the
backend runs in a container and must reach the issuer at the URL the browser uses — the backend
takes plain `http` only on a loopback host, so Dex's container holds the network namespace the
backend and the stand-in join — and because the run's tenants and people stay out of the
development database. The stand-in terminates TLS with a certificate made for the run: WebKit
stores no `Secure` cookie from plain-HTTP localhost ([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D2))*;
a developer may point the suite at `ng serve` for iteration, which is not a gate.

**D2 — Two identities in one test where a feature is about people.** Playwright's browser
contexts hold two sessions at once: the first identity acts, the second sees — assignment,
inbox, a restricted project's silence, a `412` on a concurrent body write.

**D3 — The first release's suite:** login through Dex and as the local administrator; the
init state and the first tenant; file a ticket, assign it, the assignee sees it in "assigned
to me" and in the inbox within the event stream's latency; a transition by drag on the
project board and on the tenant board; `done` with its verification note; a third identity
outside a restricted project sees nothing of it; the import of a small archive with its
report; the MCP server's `session_start` against the same environment (a Go test driving the
built binary, no browser). Every smoke path runs in both colour schemes; a dark-mode
screenshot comparison catches regressions at a coarse threshold, not pixel-perfect.

**D4 — A required gate** ([ADR 0003](0003-test-and-ci-policy.md) D4): the `e2e` CI job runs
after `container-malware-scan`, reuses its images as artefacts, has a ten-minute budget, and
uploads trace and video on failure. `semantic-release` lists it in `needs:`.

**D5 — Three tiers, three budgets, all continuous.** Unit (Go and vitest on jsdom, seconds,
on every save and every push), integration (PostgreSQL, Dex, MinIO as services, under a
minute, every push), end-to-end (minutes, every push, a gate). Nothing runs only nightly;
nothing is skipped ([ADR 0003](0003-test-and-ci-policy.md) D3).

**D6 — Frontend line coverage is measured in the unit tier and reported on every pull
request.** `make frontend-test-coverage` (`@vitest/coverage-v8`: `text-summary`, `lcov`,
`coverage-summary.json`); the `frontend` job uploads it and the coverage comment carries
"Frontend (lines)". End-to-end coverage is not merged into that number: the suite proves
paths, the unit tier measures lines, and the two are not the same claim.

**D7 — Test hygiene.** ~~`data-testid` on everything a test touches;~~ *(amended 2026-10-06 by the owner: a
test finds what it touches by its role and its accessible name first, by its label next, and by a
`data-testid` where the page names it in no accessible way; a component gets a test id or an
`aria-label` for a test only then, and the test ids it has stay)*; tests seed their data
through the API with an administrator token and assert through the UI; each test owns a
project inside a fixture tenant, so tests run in parallel without a database reset; Playwright
auto-waits, no sleeps; a flaky test is a defect ticket, not a retry setting.

**D8 — Browsers.** Chromium in every run; WebKit in the same job for the smoke paths, because
the owner's machines are Macs and Safari's cookie and `EventSource` behaviour differs.
Firefox is not run.

## Consequences

- The proxy, the cookie, CSRF, the event stream, the roles and the boards are proven together
  on every push, against what is shipped.
- The pipeline grows by one job that needs the images; the scan job already builds them,
  and passing them as artefacts avoids a second build.
- Two colour schemes double the smoke paths' runtime; the ten-minute budget is the bound,
  and D3's list is cut, not the budget raised, if it is exceeded.
- D6 states what already exists so that nobody looks for a coverage number in the
  end-to-end tier.

## Alternatives Considered

- **Cypress.** Comparable; weaker at two simultaneous sessions (two contexts are native in
  Playwright, awkward in Cypress), no WebKit. Lost.
- **End-to-end only in the hardening phase.** Phase 3 done sooner; boards, drag, the stream,
  CSRF and login checked by hand until then — the parts unit tests cannot reach. Lost.
- **End-to-end against `ng serve` with the proxy to the backend.** Faster locally; nginx —
  proxy, stream buffering, cache headers, `error_page` — untested, and that is where it
  broke before. Allowed locally, not a gate.

## Residual risks

- End-to-end suites rot into flakiness; D7's rules and the "flaky is a defect" stance are
  the discipline, and the trace artefact is what makes a flake diagnosable.
- Service containers for Dex and MinIO on the runners are a new requirement beyond
  PostgreSQL; the integration tier adopts them first ([ADR 0038](0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
  D5), so the end-to-end job inherits a working setup.

## References

- [ADR 0003](0003-test-and-ci-policy.md) D2–D4 — the tiers, "nothing is skipped", required gates
- [ADR 0004](0004-cowork-is-a-team-product.md), [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) — the two-identity and restricted-project cases
- [ADR 0038](0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md) — the fixtures and services
- [ADR 0052](0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) D3, [ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md) — the schemes and the stream the suite exercises
- [`Makefile`](../../Makefile) (`frontend-test-coverage`), [`.github/workflows/release.yml`](../../.github/workflows/release.yml) — the coverage path that exists
