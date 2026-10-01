# Developer documentation

The contributor entry point and the overviews for people changing this code. The detail is
in the code; what lives here is the shape of things — how the pieces fit, which invariants
hold, why a design that looks odd is the way it is, and the workflow around it.

**What belongs here:** anything a future developer needs before touching a subsystem, that the
code cannot state on its own, and everything about contributing: the layout, the build and
test matrix, continuous integration and the release, the checklists, the conventions.

**What does not:** decisions (those are [ADRs](../adr/README.md)), work lists (those are
[tickets](../tickets/README.md), archived when the work lands), what somebody running cowork
needs (that is [docs/operations/](../operations/README.md), with the reference tables in
[README.md](../../README.md)), and the security design (that is [docs/security/](../security/README.md)).
[ADR 0002](../adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
is the rule that separates those homes; [ADR 0075](../adr/0075-developer-documentation-lives-in-docs-developer-and-the-general-standard-says-so.md)
is why there is no `DEVELOPER.md` at the root.

A page here **may and should** point at files and functions. That is the point of it. It also
means it goes stale when the tree moves, so whoever moves the tree updates the page in the same
change.

## What has to be in your head first

- **Two containers, one origin.** The Go backend in `backend/` serves the API and migrates
  the schema on start; the nginx frontend in `frontend/` serves the Angular bundle and proxies
  `/api/` to the backend
  ([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)).
- **`make` is the entry point**, from the repository root. CI runs Makefile targets; so do
  you ([ADR 0003](../adr/0003-test-and-ci-policy.md) D1). Go targets `cd backend`, npm
  targets `cd frontend`. `make help` lists them.
- **Nothing is skipped.** No `-short`, no `testing.Short()`, no "skip when the database is
  missing". The integration tier fails and tells you how to start the database.
- **Newest toolchains.** Go 1.27 and Angular 22 today, moved by Renovate; a lagging version
  is a defect (ADR 0001 D9).
- **A statement has one home.** Decision → ADR; work → ticket; how → `docs/developer/`; run →
  `docs/operations/`; threat → `docs/security/`; reference tables → `README.md`
  ([ADR 0002](../adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)).
  Whoever changes behaviour updates the page that describes it in the same change.
- **English only**, in code, comments, commits and documentation.


| Page | Read it when |
|---|---|
| [repository-layout.md](repository-layout.md) | You are new and want the tree |
| [package-map.md](package-map.md) | You are looking for where something lives and what it is responsible for |
| [architecture.md](architecture.md) | You want the picture: what runs where, what a request goes through, what happens at start |
| [build-test-lint.md](build-test-lint.md) | You want to build, run or lint anything, locally or the images together |
| [testing.md](testing.md) | You are adding a test, choosing a tier, or a suite is failing and you need to know what it is for and what it needs |
| [ci-and-release.md](ci-and-release.md) | You touch a workflow, Renovate or the release |
| [adding-things.md](adding-things.md) | You add a configuration variable, a migration, an endpoint, a frontend feature, an nginx path, a chart value or a CI job |
| [conventions.md](conventions.md) | You write a commit, Go, Angular, documentation or anything security-relevant |

## Core flows, one fact each

| Flow | The fact | Where |
|---|---|---|
| Backend start | Configuration is validated completely before anything else runs; the migration runs before the listener opens | [architecture.md](architecture.md#backend-startup-sequence-cowork-serve) |
| Backend request | Method patterns on the mux; every known path is registered twice so the wrong method is a `405`, not the catch-all's `404` | [architecture.md](architecture.md#backend-request-path) |
| Frontend request | nginx: `/healthz` itself, `/api/` proxied to `BACKEND_URL`, hashed bundles immutable, everything else `index.html` with `no-store` | [architecture.md](architecture.md#frontend-container) |
| Migration | golang-migrate over embedded files, advisory lock across replicas, dirty version refuses to start | [runtime.md](../operations/runtime.md#the-migration-run) |


## What has no page here

There are no subsystems beyond the pages above yet: no domain, no authentication, no API
beyond health and version. The order in which they are to be built is
[docs/planning/project-plan.md](../planning/project-plan.md); each gets its page here when it
exists.
