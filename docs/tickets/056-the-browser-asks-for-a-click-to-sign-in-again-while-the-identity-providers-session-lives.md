---
id: T56
title: the browser asks for a click to sign in again while the identity provider's session lives
state: in-progress
severity: medium
security: hardening
threat: the owner's check would additionally cover an issuer that answers prompt=none from a session of its own, which no test has
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
- **Only the person's activity moves the idle clock** (Q2, as built): a write that passes the CSRF
  check, or a read with `X-Cowork-Activity: input`; no other read, the event stream's connections and
  its polling fallback's reloads included ([`api/session.go`](../../backend/internal/api/session.go)
  `movesIdleClock`).
- **The keep-alive.** While a page of the shell is shown, the time of the last pointer press, key,
  wheel or touch is noted, and every five minutes, while the document is visible and there was input
  since the last ask, one `GET /api/v1/me` marked `PERSON_ACTIVITY` — the interceptor
  `personActivity` turns the mark into the header — moves the idle clock
  ([`keep-alive.service.ts`](../../frontend/src/app/core/keep-alive.service.ts), started and stopped
  by [`shell.ts`](../../frontend/src/app/layout/shell.ts); [`http.ts`](../../frontend/src/app/core/http.ts)).

**Verified** on 2026-10-07 in the worktree of this branch, on the final tree: `make test-unit lint
cyclo gosec` (18 packages, no lint or gosec issue, no function above 15), `make test-integration`
(passing in 162 seconds against the PostgreSQL, MinIO and Dex of `make dev-up`),
`make generate-check`, `make frontend-generate-check frontend-test frontend-lint frontend-build`
(120 files and 4,285 tests passing) — every one passing; the build's initial total is 1,005,045
bytes, below the budget's warning at 1,048,576. The new tests: `TestASilentLoginAsksForNoPage`,
`TestASilentLoginTheIssuerCannotCompleteAsksForASignIn`, `TestWhatMovesTheIdleClock` and the silent
cases of `TestOIDCRoutesWithoutAProvider` and `TestAFailedCallbackKeepsThePathThePersonWanted`
(unit); `TestASilentSignIn` against the fake issuer — a code that signs in, `login_required` with the
path and no session from an issuer without a session (`NoSession`), also stale, `oidc_failed` for the
error to a login the person started —, `TestASilentStartThroughDex`, and
`TestOnlyThePersonsActivityMovesTheIdleClock` — a read without the header, with another value, the
event stream's connection and a refused write leave `last_seen_at` alone, the marked read and a
write move it at most once a minute, a session that is only read ends at its limit — (integration);
the specs of the login page, `whenPresent`, `SignInMemory`, `KeepAliveService` with fake timers and
its mark, the interceptor `personActivity`, `AuthService`, `SessionService` and the shell (frontend
unit). `TestSessionLifetimes` relied on a read extending a session and now sends the keep-alive's
header. Taking out the silent flag, the code `login_required`, the `leaving` guard, the `error`
condition, the forgetting at the sign-in and at the sign-out, the keep-alive's visibility check, its
mark, the interceptor's condition, or the idle clock rule — every request moving the clock again —
each makes them fail. Measured: Dex v2.45.1 answers a `prompt=none` request with its login form; the
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
3. **Q2 answered (b), to build:** every request of a session moves its idle clock again — reads, the
   event stream's connections and reconnects and the polling fallback's reloads included —, as before
   0.8.0; a write the CSRF check refuses still does not (made concrete when the answer was recorded:
   a forged request from another site extends no session). The `X-Cowork-Activity` header and the
   keep-alive service are removed with their tests. ADR 0031 D3 is amended back in the same change,
   and docs/security/sessions.md names what it leaves open: a tab open with nobody at it keeps its
   session up to the absolute limit.

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

**Answer:** (a) — the owner, 2026-10-07. Once per tab between two sessions; ADR 0029 D6 records it as confirmed.

### Q2: Which requests of a session keep it alive?

Before this work every request of a session moved its idle clock, reads included: the event
stream's connections and reconnects and its polling fallback's reloads, requests every fifteen
seconds to every minute, kept a session that nobody used up to the absolute limit, and so did a
write the CSRF check then refused. The coordinator decided on the night of 2026-10-06 to build the
safer option; [ADR 0031](../adr/0031-server-side-sessions-in-an-httponly-cookie.md) D3 says it, as
amended that day.

- **(a) Only the person's input and writes keep a session** — built, recommended: a write that
  passes the CSRF check, or a read with `X-Cowork-Activity: input`, which only the keep-alive sends,
  after the person's input; no other read moves the idle clock. It is the guarantee the owner's idle
  limit promises — a tab that is only open signs out —, and the keep-alive with the sign-in without
  a click keeps it convenient for a person at the page. Cost: a person who only reads is held by
  reads five minutes apart, so an idle limit of about six minutes or less signs them out; a proxy in
  front has to pass the header through.
- **(b) Every request keeps a session, as before.** No header and no rule; an open tab whose stream
  reconnects or polls stays signed in up to the absolute limit with nobody at it, and the idle limit
  holds only for a tab whose stream stays connected.

(a) is built and stays unless the owner answers otherwise.

**Answer:** (b) — the owner, 2026-10-07. Every request of a session keeps it alive, as before
0.8.0; the cost — an unattended open tab stays signed in up to the absolute limit — was put to him
with the question and is his accepted risk.

## Not verified

- An issuer that answers `prompt=none` with a code from a session of its own, beyond the fake issuer
  of the tests: the owner's installation is that check (required change 2).
- Whether a browser sends pointer moves of its own under a resting pointer, or gives a window the
  focus without a person: the presence rule asks for a move to another place than the move before,
  which such a move is not, and takes the focus for a sign. Settled by required change 2 in Safari.
