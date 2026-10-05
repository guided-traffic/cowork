---
id: T29
title: the end-to-end tier lacks the phase path with two identities, its job has not run on a runner, and the ruleset does not require it
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: S
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The tier of [ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)
is built ([testing.md](../developer/testing.md#end-to-end-tests)):
[`frontend/e2e/`](../../frontend/e2e/) with Playwright Test, `make e2e`, `make e2e-up`,
`make e2e-down`, `make e2e-browsers`, and [`hack/e2e.sh`](../../hack/e2e.sh), which runs both images
of one commit read-only behind the Ingress stand-in of
[`hack/ingress/default.conf`](../../hack/ingress/default.conf), listening with TLS on
`https://localhost:18443` with a certificate made for the run, against a PostgreSQL, a MinIO and a
Dex of its own; Dex's container holds the network namespace the backend and the stand-in join, so
that `http://localhost:5557/dex` is the issuer for the browser and the backend alike. The suite's
redirect URI is in [`hack/dex/config.yaml`](../../hack/dex/config.yaml).

The paths, each in Chromium and WebKit and in both colour schemes, data seeded through the API
with the administrator's token and `data-testid` selectors:
[`login.spec.ts`](../../frontend/e2e/login.spec.ts) (the local form; the session cookie
`Secure`, `HttpOnly`, `SameSite=Lax`, kept across a reload; a write through it and the same write
without `X-Requested-With` refused `403 csrf`; the sign-out; a temporary password changed at the
first sign-in; *Sign in with Dex* as `bob`), [`tickets.spec.ts`](../../frontend/e2e/tickets.spec.ts)
(filed and moved), [`board.spec.ts`](../../frontend/e2e/board.spec.ts),
[`backlog.spec.ts`](../../frontend/e2e/backlog.spec.ts), and
[`visual.spec.ts`](../../frontend/e2e/visual.spec.ts), the coarse dark-mode comparison of a seeded
board. The comparison holds one picture per browser for every platform at 2 % of the pixels;
measured on 2026-10-04, a light sidebar, light cards or a light top bar fail it, and card titles
turned dark on the dark cards pass.

The phase's own path is
[`assigned.spec.ts`](../../frontend/e2e/assigned.spec.ts), a `test.fixme` that Playwright lists as
skipped: "assigned to me" (T35) and the inbox (T36) do not exist.

CI: each `container-malware-scan` leg hands its scanned image to the job `e2e` (End-to-End Tests)
as an artefact; `e2e` installs the browsers, runs `make e2e` with ten minutes' budget, uploads
traces, videos and the containers' logs on failure, and is in `semantic-release`'s `needs:`
([ci-and-release.md](../developer/ci-and-release.md)).

Verified on 2026-10-04 on the owner's machine (macOS, Docker Desktop, images of `a721dc9` built
without the PrimeUI key): `make e2e` from nothing to removal in 24 s — 34 passed, 4 pending, the
suite 17.3 s with four workers, 29.5 s with two (`CI=true`); the suite three times over against one
stack, 102 passed; a failing run exits non-zero, keeps the containers' logs and removes the stack;
the dark pictures pass in the Linux image `mcr.microsoft.com/playwright:v1.63.0-noble` (arm64);
`make frontend-lint` and `make frontend-test` pass; `actionlint` reports nothing in the lines the
job added.

## Required changes

1. With T35 and T36: write the path of `assigned.spec.ts` — the administrator files a ticket and
   assigns it to a local account it made, which sees it in "assigned to me" and in its inbox within
   the stream's latency, in a browser context of its own, moves it and closes it with a
   verification note, while the administrator's page shows it done (ADR 0056 D2) — and remove the
   `fixme`.
2. The `main` ruleset requires `End-to-End Tests` — the owner's change
   ([ADR 0073](../adr/0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md)
   D6); then ADR 0073's Status and its index row say so.
3. The job's first run on a runner, recorded here and in ADR 0056's Status: that the stack's two
   ports on `127.0.0.1` reach the runner as the integration job's do, that the browsers install
   with `--with-deps`, its duration against the ten minutes, and that the Linux pictures hold on
   amd64.

## Open questions

### Q1: does `make e2e` run a stack of its own, or against the containers of `make dev-up`?

ADR 0056 D1 said locally `make e2e` runs "against `make dev-up` plus locally built images"; the
tier was built with a stack of its own instead, and D1 is amended so with the reasons, not yet put
to the owner. The options:

- **A stack of its own, as built** (recommended): the same script in CI and locally; the backend in
  a container reaches Dex at the browser's `http://localhost:5557/dex` only inside Dex's network
  namespace, which `make dex-up`'s container cannot lend because its ports are fixed when it is
  made; the run's tenants and people never enter the development database; it costs about five
  seconds of start per run and an image pull the first time.
- **`make dev-up`'s PostgreSQL and MinIO with a database and a bucket of the run's, and a Dex of
  its own:** reuses two containers, but needs their published ports from inside the backend's
  container (`host.docker.internal` locally, something else on a runner) and keeps CI and the
  local run different.

**Answer:** _open_

## Related

- T35, T36 — the pages the pending path walks
- T41 — the board's path, built here
- T34 — the backlog's drag path, built here
