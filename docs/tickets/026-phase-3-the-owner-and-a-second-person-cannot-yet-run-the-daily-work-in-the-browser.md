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
- **What the goal still lacks:** "assigned to me" and the inbox, through which the phase's
  verification goes, exist; what is left of them is "next for me" (T35) and the mention (T36). The
  end-to-end tier (T29) is built with the login's paths, filing and moving, the board and the
  backlog; its path with two identities through "assigned to me" and the inbox, and its required
  check, are left. And what is left of each child below.
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
- **Carried over from phase 2:** rank (built; its score and the rebalancing of its keys are T34),
  deletion and purge (T39), numbered pages on the audit view, members, tokens and projects (T40),
  the attachment quota (T32); the server-side Markdown sanitiser is built, its end-to-end check is
  T33.
- **Not in phase 3:** import and the cut-over (phase 6).

## Required changes

1. **The children, in this order.** Each lands with its tests, the pages that describe what it
   built, and the Status of every ADR it builds:
   - T52 — the horizon (`now`, `release`, `next`, `later`, `icebox`) as a planning category set
     by a person or an agent in any state, filing into a horizon at a place, an agent tool that
     re-sorts the backlog, and the project opening on its board
   - T27 — the login's remainder: a token's project by key and the token form's longest lifetime
   - T28 — the foundation's remainder: `304` polls, one idempotency key per form content, a check
     by hand, and its open question on the bundle budget
   - T29 — the end-to-end tier's remainder: the path with two identities through "assigned to
     me" and the inbox, the ruleset's required check, the job's first run on a runner
   - T30 — the ticket page's editing on the end-to-end path of T29 (its editors are built)
   - T31 — the conversation's writes and the prerequisite tree seen by a second browser on the
     end-to-end path of T29 (the editors, the withdrawal and the tree are built)
   - T32 — the attachment quota (the upload to a comment, the raster preview and the correction
     of a time entry are built)
   - T33 — the rendered Markdown's end-to-end check under the shell's content-security policy (the
     renderer, the sanitiser and the rendered texts of the ticket page are built)
   - T34 — the score beside the rank, and the rebalancing of the rank keys (the backlog's
     end-to-end path is built)
   - T35 — next for me, the lists in the score's order
   - T36 — the mention in a comment, and the inbox's end-to-end path
   - T37 — the search's end-to-end check, and where "that tenant first" puts the other tenants (the
     search routes, the box and the results page are built)
   - T38 — saved filters
   - T39 — ticket deletion and the purge
   - T40 — numbered pages on the administration lists, the audit page and the tenant's tokens
   - T41 — done (the project board's end-to-end path); its move to the archive is left
   - T42 — the tenant board's end-to-end run (the board is built, its path written)
   - T43 — the fixed dashboard's end-to-end path and the owner's look at it (the route and the
     front page are built)
2. **The phase verification**, recorded here with what was run, against what, with what result:
   the owner files a ticket, assigns it to a second identity, that identity sees it in "assigned
   to me" and in its inbox, moves it and closes it — through the UI, without touching the API —
   and the Playwright suite of T29 covers that path with both identities in both schemes.
3. **Phase close:** every child extracted and archived; the README reference covers every
   variable, value, route and problem code the phase added; the Status of every ADR the phase
   built says what is built; the phase-3 section is gone from
   [project-plan.md](../planning/project-plan.md).
