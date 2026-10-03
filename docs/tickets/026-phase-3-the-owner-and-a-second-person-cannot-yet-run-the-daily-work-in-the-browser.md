---
id: T26
title: phase 3 (UI v1) — the owner and a second person cannot yet run the daily work in the browser
state: in-progress
severity: high
security: none
threat:
urgency: next         # rule 3: severity high and the trigger is live (nobody can work in the browser)
effort: L
blocked-by:
filed-from: docs/planning/project-plan.md phase 3, converted by ADR 0074 D2
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The family ticket of phase 3 ([ADR 0074](../adr/0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D2). **Goal:** the owner and a second person run the daily work in the browser.

- The backend serves the API of phase 2; the frontend is the foundation of T28 — PrimeNG with
  cowork's preset, the shell, read-only pages and the event stream's client. Nobody can log in:
  until T27 lands, the Angular dev server's proxy presents a seeded token
  ([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
  D2), which no installation has.
- **Decisions the phase builds on**, beyond the records it implements — the owner's answers of
  the conversion, amended into the records they change:
  - **the local login moves from phase 4 into phase 3.** The plan's "temporary login with a
    token" predated [ADR 0035](../adr/0035-personal-access-tokens.md) D7 ("the browser never
    holds a token") and ADR 0038's amendment ("the UI needs the login first"); the owner chose
    sessions, the local administrator, local accounts, CSRF and the creation of tenants and
    tokens (T27) over moving all of phase 4 forward and over a token in the browser. OIDC, Dex,
    the group gate and the mappings stay in phase 4;
  - **PrimeNG 22 under the PrimeUI Community License.** PrimeNG left MIT with version 22
    (2026-07-15); the owner chose a free Community key, kept out of the repository, over Angular
    Material and over PrimeNG 21 on Angular 21
    ([ADR 0052](../adr/0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md)
    D9);
  - **the logo in the style of the owner's reference** — a gradient border from teal to gold
    around deep ink, white sparkles, a violet glow — and the primary colour taken from it
    (ADR 0052 D2, D8); dark is designed first, light works with the same tokens (D3);
  - **live updates in under a second without a page reload**, which
    [ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
    already decides; measured through the dev proxy: a comment's event reached an
    open stream 29 ms after the write started;
  - **the owner watches the UI while it is built**: `make dev` runs the whole stack with live
    reload and demo data; the dev proxy is the token's only holder until T27;
  - high line coverage in the frontend's unit tier, measured and reported as
    [ADR 0003](../adr/0003-test-and-ci-policy.md) D5 has it, not gated by a number.
- **Moved into phase 3:** the local login of phase 4 (above). **Carried over from phase 2:**
  rank (T33), deletion and purge (T38), numbered pages on the audit view, members, tokens and
  projects (T39), the person-level events with the inbox (T35), the attachment quota (T31), the
  server-side Markdown sanitiser (T32).
- **Not in phase 3:** OIDC, Dex, the group gate and mappings, membership administration beyond
  what a local account's creation grants (phase 4); the MCP server (phase 5); import (phase 6).

## Required changes

1. **The children, in this order.** Each lands with its tests, the pages that describe what it
   built, and the Status of every ADR it builds:
   - T27 — the local login: sessions, the local administrator, local accounts, CSRF, tenant
     and token creation
   - T28 — the frontend foundation: PrimeNG, the preset, the logo, the shell, the generated
     client, the services, the event stream's client, `make dev`
   - T29 — the end-to-end tier: Playwright against the built images, two identities, both schemes
   - T30 — tickets are filed, edited and moved through their states in the browser
   - T31 — the ticket's conversation: comments, questions, interest, links, the prerequisite tree
   - T32 — attachments, the attachment quota, time entries and the time report
   - T33 — the rendered Markdown body and the server-side sanitiser
   - T34 — rank, its moves and the ranked backlog with drag order and the score marker
   - T35 — the person-level lists: next for me, assigned to me, open decisions
   - T36 — the inbox and the person-level events
   - T37 — search
   - T38 — saved filters
   - T39 — ticket deletion and the purge
   - T40 — numbered pages on the administration lists and the tenant administration pages
   - T41 — the project board
   - T42 — the tenant board with swimlanes
   - T43 — the fixed dashboard
2. **The phase verification**, recorded here with what was run, against what, with what result:
   the owner files a ticket, assigns it to a second identity, that identity sees it in "assigned
   to me" and in its inbox, moves it and closes it — through the UI, without touching the API —
   and the Playwright suite of T29 covers that path with both identities in both schemes.
3. **Phase close:** every child extracted and archived; `docs/security/` has pages for the
   sessions, the local accounts and CSRF, each ending with `## What this does not cover`; the
   README reference covers every variable, value, route and problem code the phase added; the
   Status of every ADR the phase built says what is built; the phase-3 section is gone from
   [project-plan.md](../planning/project-plan.md) and phase 4 names only what is left of it.
