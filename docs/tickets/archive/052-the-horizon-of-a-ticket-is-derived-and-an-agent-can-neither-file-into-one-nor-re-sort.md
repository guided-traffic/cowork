---
id: T52
title: the horizon of a ticket is derived from its state, and an agent can neither file a ticket into one nor re-sort the backlog
state: done
severity: low
security: none
threat:
urgency: later        # rule 4: a decided fix, unblocked since 0.6.0 shipped migration 38
effort: S
blocked-by:
filed-from: the owner's report of 2026-10-04
opened: 2026-10-04
decided: 2026-10-04
done: 2026-10-06
shipped: migration 40 rewrites the stored capability sets and saved filters once more and narrows both capability checks to the nine names, so the database refuses override-urgency, and auth.Canonical drops nothing any more (ADR 0043 D4, ADR 0010 D1)
---

## Current state

The five values `now`, `release`, `next`, `later`, `icebox` are the ticket's **horizon**, a
planning category a person or an agent sets in whatever state the ticket is
([ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3,
[ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D2,
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1,
[ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D1, all as amended
2026-10-04). The API names it by the word alone and the database keeps `urgency`
([ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D1 as amended
2026-10-06); the capability is `set-horizon`, `override-urgency` refused with `400`
([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4 as amended 2026-10-06).

Built on this branch, the narrowing:

- **Checked before relying on it:** at the tags `v0.6.0` and `v0.7.0` no code writes
  `override-urgency` — `auth.Stored` and `auth.CapOverrideUrgency` are gone, and the two writers of
  a capability set (`InsertToken` in [`me.go`](../../backend/internal/api/me.go),
  `SetChatCapabilities` in [`chat_capabilities.go`](../../backend/internal/api/chat_capabilities.go))
  store the request's set, which the API's nine-value `Capability` holds; the only mention outside
  the tests and the migrations is the drop in `auth.Canonical`. So the release before the narrowing
  writes no old name.
- **[Migration 40](../../backend/internal/store/migrations/000040_capability_checks_set_horizon_only.up.sql)**
  rewrites again what an image rollback to 0.5 over migration 38 wrote — every `override-urgency`
  of `tokens.capabilities` and `chat_capabilities` to `set-horizon`, each name once in the order
  first named, and a saved filter's `urgency` to `horizon`, a horizon already there winning, through
  the moderation guard of migration 39 — and then both checks take the nine names and refuse
  `override-urgency`. It lifts the forced row-level security of the three tables for its statements
  and restores it, as migration 38 does.
- **`auth.Canonical`** ([`principal.go`](../../backend/internal/auth/principal.go)) reads a set each
  name once and drops nothing; the unit tests of the drop are gone (`principal_test.go`,
  `api/horizon_test.go`), and `TestTheCapabilityIsSetHorizonOnly` keeps the `400` for
  `override-urgency` on a token and on the chat and no longer stores the old name.
- **Docs:** ADR 0043 (the Status, D4's `set-horizon` row, the Residual risks — which keep that a
  rollback to 0.5 was never run), ADR 0010 (the built note, the Residual risks), the ADR index,
  [installation.md](../operations/installation.md#upgrade), [api.md](../developer/api.md#deprecated-names),
  [chat.md](../developer/chat.md), [testing.md](../developer/testing.md) and
  [tokens.md](../security/tokens.md) carry the narrowed state; the README reference names no stored
  old name, as before. [tenancy.md](../security/tenancy.md) said that one migration lifts the force
  of row-level security, stale since migration 19; it now says that a migration whose own statements
  rewrite rows does — 17, 19, 29, 34, 38 and 40, the last two on `tokens`, `chat_capabilities` and
  `saved_filters`. The lock the page rests on, `AccessExclusiveLock` for
  `NO FORCE ROW LEVEL SECURITY`, was checked on PostgreSQL 18.6.

## Required changes

None.

## Verified

- `TestTheNarrowingMigrationRewritesAgainAndRefusesTheOldName` (integration, from version 39): the
  three rewrites of the shapes 0.5 stores, a revoked token's and a shared filter's included, no
  version and no time moved; both checks refuse the old name with SQLSTATE `23514` naming the check,
  take every name of `auth.AllCapabilities` and refuse one outside the catalogue; the force
  restored. It fails without migration 40 (every rewrite and refusal assertion) and without the
  lift of the forced row-level security (the migration fails on the narrowed check, the rows hidden
  from its rewrite).
- `TestTheContractMigrationRewritesTheNamesBefore` stops at version 38 now, where the checks still
  take the old name; the tests of migrations 37 and 39 pass, their assertions unchanged.
- `make test-unit lint cyclo gosec`, `make test-integration` and `make generate-check` pass.
- The end-to-end tier ran against the contract this ticket followed up: the release run of 0.6.0
  (37422872359, commit `306e4d5`) passed its End-to-End Tests before Semantic Release.

## Not verified

- An image rollback to 0.5 over migration 38, or over migration 40, was not run; what each does is
  read from the code of 0.5.1, 0.6.0 and 0.7.0
  ([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md),
  its Residual risks).
- The end-to-end tier did not run against the narrowing; it needs both images built. The narrowing
  changes no route, no answer and no screen.
