---
id: T26
title: phase 3 (UI v1) — the owner and a second person cannot yet run the daily work in the browser
state: in-progress
severity: high
security: none
threat:
urgency: next         # rule 3: severity high, live — the daily work's path through "assigned to me" and the inbox has no end-to-end verification
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

- **On `main` and released** (0.2.0 and 0.3.0): the local login with its pages (T27); the
  frontend foundation with the event stream's client (T28); the backlog as a ranked table grouped
  by urgency (T34) and the project board (T41); the ticket page's writes — filing, fields, stages,
  moves, comments, questions, interest, links, attachments and time (T30, T31, T32); the time
  report; and the tenant's settings, projects, members, accounts and group mappings. Phases 4 (the
  identity provider) and 5 (`cowork-mcp` and the chat) were built ahead of this phase and released
  in 0.3.0; phase 3 is the open phase before phase 6
  ([project-plan.md](../planning/project-plan.md)).
- **Released in 0.5.0** (`9c6948b`, 2026-10-05): everything the children below
  built — the horizon, the person-level pages with the inbox and "next for me" across every tenant,
  the ticket page's editors, the prerequisite tree, rendered and sanitised Markdown, search, the
  score with the rank's rebalancing, deletion with the bin, saved filters, the tenant board, the
  dashboard as the tenant's front page, the tenant's ticket list, the tenant's tokens, the
  attachment quota, mentions, numbered administration pages with the audit page, and the
  end-to-end tier. What is left is listed per child below: the owner's reviews of what was
  built on the recommendation, one open question, and the second half of the horizon's contract.
  Every end-to-end path of the children is built and passes (2026-10-06).
- `make dev` runs the whole stack with demo data, and the browser logs in through the real login,
  as the local administrator or through Dex; the dev server's proxy holds no credential
  ([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
  D2).
- **Decisions the phase builds on**, beyond the records it implements — the owner's answers,
  amended into the records they change:
  - **the local login is part of this phase:** the browser never holds a token
    ([ADR 0035](../adr/0035-personal-access-tokens.md) D7), so the UI needed the login first; the
    owner chose sessions, the local administrator, local accounts, CSRF and the creation of
    tenants and tokens (T27) over moving the identity provider forward and over a token in the
    browser;
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
    reload and demo data;
  - high line coverage in the frontend's unit tier, measured and reported per pull request as
    [ADR 0003](../adr/0003-test-and-ci-policy.md) D5 has it, not gated by a number.
- **Carried over from phase 2:** rank (built, with its score and the rebalancing of its keys; their
  end-to-end path is T34),
  deletion and purge (T39), numbered pages on the audit view, members, tokens and projects (T40),
  the attachment quota (T32); the server-side Markdown sanitiser is built, its end-to-end check is
  T33.
- **Not in phase 3:** import and the cut-over (phase 6).

## Required changes

1. **What is left of the children.** Each lands with its tests, the pages that describe what it
   built, and the Status of every ADR it builds:
   - T52 — the narrowing of the two capability checks, in a release after the one that ships
     migration 38 (the API's old names are removed)
   - T32 — the owner's review of the attachment quota
   - T34 — the owner's look at the score's marker and the sort by score
   - T36 — the owner's review of the mention
   - T38 — the owner's answer on a shared filter whose owner left the tenant
   - T39 — the owner's word on its three decisions
   - T40 — the owner's review of the tenant's tokens page
   - T42 — the owner's look at the tenant board and its saved-filter bar
   - T43 — the owner's review of the dashboard
   - T53 — the owner's look at the tenant's ticket list
2. **The phase verification.** The Playwright path of T29,
   [`assigned.spec.ts`](../../frontend/e2e/assigned.spec.ts), passes with both identities in both
   schemes, Chromium and WebKit, on a runner (run 37285901009 of commit `65337eb`, 2026-10-05) and in
   three local runs with two workers: the administrator files a ticket in the backlog's dialog,
   assigned to a local account, which sees it in "assigned to me" and in its inbox without a reload,
   moves it and closes it with a verification note, while the administrator's page shows it done.
   Left: the owner walks the same path by hand through the UI, two identities, without touching the
   API, and records the result here.
3. **Phase close:** every child extracted and archived; the README reference covers every
   variable, value, route and problem code the phase added; the Status of every ADR the phase
   built says what is built; the phase-3 section is gone from
   [project-plan.md](../planning/project-plan.md).
