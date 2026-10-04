---
id: T29
title: there is no end-to-end tier — the Ingress routing, the cookie, CSRF, the event stream and two identities are not tested together
state: decided
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: L
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

[ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)
decides Playwright against the built containers, two identities, both colour schemes and a
required CI job. `@playwright/test` is a development dependency of the frontend (used by hand for
screenshots and for the login, tokens and accounts flows against `make dev`); there is no
`frontend/e2e/`, no `make e2e`, no `e2e` job. The login is built and released (T27): the suite
logs in through the page. [`hack/dex/config.yaml`](../../hack/dex/config.yaml#L36-L41) registers
the redirect URIs of `make dev` (`https://localhost:4200/auth/callback`) and of the integration
tier (`http://cowork.test/auth/callback`), none of the suite's. The frontend image serves the UI
only, and the Ingress routes `/api/` and `/auth/` to the backend
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3), so the two images need a stand-in for the Ingress in front: [`hack/ingress/default.conf`](../../hack/ingress/default.conf)
is one, for plain HTTP on the loopback address, and `make verify-phase-2` runs the images behind
it ([build-test-lint.md](../developer/build-test-lint.md#run-the-images-together)); it terminates
no TLS.

## Required changes

1. `frontend/e2e/` with Playwright Test, `make e2e` against both images of the same commit behind
   the Ingress stand-in (ADR 0056 D1), PostgreSQL and MinIO; the identities are the local
   administrator and local accounts it creates, and a person of the identity provider who signs in
   through the Dex fixture (`make dex-up`, `hack/dex/config.yaml`; ADR 0056 D3) — the redirect URI
   of the suite's origin joins that file.
2. The first paths. The login's, which T27 checked by hand against `make dev`: the local form and
   *Sign in with Dex*, the temporary password changed at the first login, the session cookie kept
   and a write through it past the CSRF check, the sign-out back to the login page. Then the
   phase's own: the owner files a ticket, assigns it to the second identity, which sees it in
   "assigned to me" and in its inbox within the stream's latency, moves it and closes it with a
   verification note. Each later child adds its view's path.
3. Every smoke path in both schemes; a coarse dark-mode screenshot comparison (D3); WebKit beside
   Chromium for the smoke paths (D8); `data-testid` and data seeded through the API (D7).
   **TLS at the Ingress stand-in**: WebKit stores no `Secure` cookie from plain-HTTP localhost
   (measured, [ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md)
   D2), so the suite reaches the stand-in over HTTPS — the stand-in terminating TLS with a test
   certificate, as an Ingress does — with `ignoreHTTPSErrors` and `COWORK_BASE_URL` set to that
   origin.
4. The `e2e` job after `container-malware-scan`, reusing its images, ten minutes budget, trace and
   video on failure, in `semantic-release`'s `needs:` and the ruleset's required checks
   ([ADR 0073](../adr/0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md) D6).
5. [testing.md](../developer/testing.md) and [ci-and-release.md](../developer/ci-and-release.md)
   describe the tier; ADR 0056's Status says what is built.
