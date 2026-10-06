---
id: T56
title: the browser asks for a click to sign in again while the identity provider's session lives
state: in-progress
severity: medium
security: hardening
threat: the owner's check would additionally cover an issuer that answers prompt=none from a session of its own, which no test has, and Q2 the idle limit of a tab whose event stream reconnects or fell back to polling (H-63)
urgency: next         # rule 3: severity medium, and the owner meets the click every day
effort: S
blocked-by: human
filed-from: the owner's report of 2026-10-06
opened: 2026-10-06
decided: 2026-10-06
done:
---

## Current state

The owner's report of 2026-10-06: cowork often lost his login and showed the login page, where one
click on *Sign in with single sign-on* signed him in at once; cowork shall extend the session by
itself, and ask for no click while the session at the identity provider runs anyway.

**Built on this branch, as the coordinator designed it** — the decisions are
[ADR 0029](../adr/0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D6 and [ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D3 as amended
2026-10-06, the threat side [sessions.md](../security/sessions.md#what-keeps-a-session-and-what-brings-a-person-back)
and [identity-provider.md](../security/identity-provider.md#signing-in-again-without-a-click) with
H-62:

- **The silent start.** `GET /auth/oidc/login?silent=true` adds `prompt=none` to the authorization
  request and seals `Silent` into the state cookie; the callback turns the issuer's error to such a
  login into `/login?error=login_required&return=…`, and the same error to a login the person started
  stays `oidc_failed` ([`api/oidc.go`](../../backend/internal/api/oidc.go) `LoginOidc`,
  `callbackRefusal`; [`oidc/oidc.go`](../../backend/internal/oidc/oidc.go) `AuthCodeURL`;
  [`auth.yaml`](../../backend/api/auth.yaml)).
- **The login page signs in by itself** where the provider is offered, there is no `?error=`, the
  browser remembers the provider (`cowork.sign-in`, written by the button, removed by a local
  sign-in and by a sign-out before the backend is asked) and the tab has not tried since its last
  session; it says so, waits for a sign of a person, asks `GET /api/v1/me`, and goes back where
  another tab signed in, else to the silent start
  ([`login.ts`](../../frontend/src/app/features/auth/login.ts),
  [`presence.ts`](../../frontend/src/app/features/auth/presence.ts),
  [`sign-in-memory.ts`](../../frontend/src/app/core/sign-in-memory.ts),
  [`auth.service.ts`](../../frontend/src/app/core/auth.service.ts)). `login_required` shows a calm
  note, and the button signs in as ever.
- **The keep-alive.** While a page of the shell is shown, the time of the last pointer press, key,
  wheel or touch is noted, and every five minutes, while the document is visible and there was input
  since the last ask, one `GET /api/v1/me` moves the idle clock
  ([`keep-alive.service.ts`](../../frontend/src/app/core/keep-alive.service.ts), started and stopped
  by [`shell.ts`](../../frontend/src/app/layout/shell.ts)).

**Verified** on 2026-10-06 in the worktree of this branch: `make test-unit lint cyclo gosec`
(18 packages, no lint or gosec issue, no function above 15), `make test-integration` (passing in
153 seconds against the PostgreSQL, MinIO and Dex of `make dev-up`), `make generate-check`,
`make frontend-generate-check frontend-test frontend-lint frontend-build` (120 files and 4,275 tests
passing) — every one passing; the build's initial total is 1,004,813 bytes, below the budget's
warning at 1,048,576. The new tests: `TestASilentLoginAsksForNoPage`,
`TestASilentLoginTheIssuerCannotCompleteAsksForASignIn` and the silent cases of
`TestOIDCRoutesWithoutAProvider` and `TestAFailedCallbackKeepsThePathThePersonWanted` (unit);
`TestASilentSignIn` against the fake issuer — a code that signs in, `login_required` with the path
and no session from an issuer without a session (`NoSession`), also stale, `oidc_failed` for the
error to a login the person started — and `TestASilentStartThroughDex` (integration); the specs of
the login page, `whenPresent`, `SignInMemory`, `KeepAliveService` with fake timers, `AuthService`,
`SessionService` and the shell (frontend unit). Taking out the silent flag, the code
`login_required`, the `leaving` guard, the `error` condition, the forgetting at the sign-in and at
the sign-out, and the keep-alive's visibility check each makes them fail. Measured: Dex v2.45.1 answers a `prompt=none` request with its login form; the
coordinator measured on the same day that it keeps no session of its own.

The end-to-end test — bob through Dex, the session cookie removed, the login page's note, a pointer
move leading to Dex's form with `silent=true` and no click
([`e2e/login.spec.ts`](../../frontend/e2e/login.spec.ts)) — type-checks and is listed in all four
projects; it was not run here.

## Required changes

1. The end-to-end tier runs the new test in Chromium and WebKit (`make docker-build e2e`).
2. The owner checks it on his own installation, whose issuer is not Dex: sign in with the button,
   let the session end (or remove the `__Host-cowork-session` cookie), come back to a page, move the
   pointer — signed in again without a click while the issuer's session lives; with the issuer's
   session ended as well, the login page with the calm note. In Safari, a tab left alone on the login
   page must not sign itself in (the presence rule, H-62's not verified part).
3. The answers to Q1 and Q2 below, each an amendment of the record it changes in the same session.

## Open questions

### Q1: Does a tab try to sign in by itself once between two sessions, or every time its login page comes?

The design said nothing about it. An issuer that ignores `prompt=none` shows its own form instead
of an error — Dex does — so a person who comes back from that form to cowork's login page, for the
local form or after changing their mind, would be sent to the issuer again at their first input,
and again, until they sign out of cowork or clear the site's data.

- **Once per tab between two sessions** (`cowork.sign-in.attempt` in `sessionStorage`, noted
  before the page leaves, cleared by `SessionService` once the tab has a session) — recommended and
  built. The trap is gone for every issuer; an attempt that succeeds clears the note, so the next
  ended session tries again. Cost: a second key and a line in `SessionService`; a session that ends
  before the tab ever had one — a sign-in refused at the gate, say — leaves the tab waiting for the
  button until it has a session.
- **Every time the login page comes without `?error=`** — the design as written. No second key;
  with an issuer that ignores `prompt=none` the local form cannot be reached by a person whom the
  browser remembers as the provider's, and every return to the login page goes to the issuer.
- **Mark the login page's own history entry instead** (`history.replaceState` before it leaves):
  the back button stops the trap, a typed address or a bookmark does not.

**Answer:** _open_

### Q2: May a tab whose event stream reconnects or fell back to polling keep its session without a person?

Found while amending ADR 0031 D3, older than this work: the stream moves no idle clock at its
heartbeats, but its request is authenticated like any (`authenticateSession` in
[`api/session.go`](../../backend/internal/api/session.go)), so each new connection of it — the
browser's own reconnect after a proxy ended one, the polling fallback's attempt every minute — moves
the clock, and the fallback
([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D7) emits a `poll` every fifteen seconds while the tab is visible, on which the services load again
what the page shows, `GET /api/v1/me` among it. Such a tab keeps its session up to the absolute
limit with nobody at it. Read from the code (`event-stream.service.ts` `fallBack`, the reloads on
`poll`, `api.go` `ServeHTTP`), not run in a browser; named as
[sessions.md](../security/sessions.md#h-63) H-63.

- **Hold both to the idle limit** — recommended: the backend does not move the idle clock for the
  stream's own request (`streamEvents`), which is what ADR 0031 D3 says of an open stream already;
  and the fallback emits its `poll` and tries the stream only while the keep-alive saw input in the
  last few minutes, the first input after a pause bringing a `resync`. An unattended tab then
  reaches the idle limit in both modes. Cost: an amendment of ADR 0031 D3 and ADR 0054 D7, a
  condition in `authenticateSession` with its integration test, a few lines in `EventStreamService`
  and its spec; a page nobody touches in polling mode stops refreshing, as it would end at the idle
  limit with a live stream anyway. **Not built here**, unlike Q1: it changes the event stream's path
  in the backend and ADR 0054, which this work does not touch, and is no part of the sign-in.
- **Leave it, as H-63 says.** No code; the idle limit does not hold for such a tab, which is the
  exception where an Ingress holds the stream open.

**Answer:** _open_

## Not verified

- An issuer that answers `prompt=none` with a code from a session of its own, beyond the fake issuer
  of the tests: the owner's installation is that check (required change 2).
- Whether a browser sends pointer moves of its own under a resting pointer, or gives a window the
  focus without a person: the presence rule asks for a move to another place than the move before,
  which such a move is not, and takes the focus for a sign. Settled by required change 2 in Safari.
