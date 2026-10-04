---
id: T29
title: there is no end-to-end tier — the proxy, the cookie, CSRF, the event stream and two identities are not tested together
state: decided
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: L
blocked-by: T27
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
`frontend/e2e/`, no `make e2e`, no `e2e` job. The login of T27, now built, is the precondition:
the suite logs in through the page.

## Required changes

1. `frontend/e2e/` with Playwright Test, `make e2e` against both images of the same commit,
   PostgreSQL and MinIO; the identities are the local administrator and local accounts it
   creates, and a person of the identity provider who signs in through the Dex fixture of phase 4
   (`make dex-up`, `hack/dex/config.yaml`; ADR 0056 D3) — the redirect URI the suite's origin
   uses must be registered in that file.
2. The first path, the phase's own: the owner files a ticket, assigns it to the second identity,
   which sees it in "assigned to me" and in its inbox within the stream's latency, moves it and
   closes it with a verification note. Each later child adds its view's path.
3. Every smoke path in both schemes; a coarse dark-mode screenshot comparison (D3); WebKit beside
   Chromium for the smoke paths (D8); `data-testid` and data seeded through the API (D7).
   **TLS in front of the images**: WebKit stores no `Secure` cookie from plain-HTTP localhost
   (measured, [ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md)
   D2), so the suite reaches the frontend over HTTPS — a TLS-terminating proxy or nginx with a
   test certificate — with `ignoreHTTPSErrors` and `COWORK_BASE_URL` set to that origin.
4. The `e2e` job after `container-malware-scan`, reusing its images, ten minutes budget, trace and
   video on failure, in `semantic-release`'s `needs:` and the ruleset's required checks
   ([ADR 0073](../adr/0073-main-is-protected-by-a-ruleset-every-job-required-admins-may-bypass.md) D6).
5. [testing.md](../developer/testing.md) and [ci-and-release.md](../developer/ci-and-release.md)
   describe the tier; ADR 0056's Status says what is built.
