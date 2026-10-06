---
id: T52
title: the horizon of a ticket is derived from its state, and an agent can neither file a ticket into one nor re-sort the backlog
state: in-progress
severity: low
security: none
threat:
urgency: later        # rule 4: a decided fix, unblocked since 0.6.0 shipped migration 38
effort: S
blocked-by:
filed-from: the owner's report of 2026-10-04
opened: 2026-10-04
decided: 2026-10-04
done:
shipped:
---

## Current state

The five values `now`, `release`, `next`, `later`, `icebox` are the ticket's **horizon**, a
planning category a person or an agent sets in whatever state the ticket is: nothing derives it, a
filing names its horizon and its place, `place_ticket` moves a ticket to a horizon and a place, the
UI says horizon, and a project opens on its board
([ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3,
[ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D2,
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1,
[ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D1, all as amended
2026-10-04).

The API names it by the word alone and the database keeps `urgency`
([ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D1 as amended
2026-10-06): a ticket answers `horizon` and `horizon_set`, `PUT …/horizon` is the one route, a
filing, both lists and the saved filters take `horizon` and refuse `urgency`, and the capability is
`set-horizon`, `override-urgency` refused with `400`
([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4 as amended 2026-10-06).
[Migration 38](../../backend/internal/store/migrations/000038_horizon_names_only.up.sql) rewrites
every `override-urgency` of `tokens.capabilities` and `chat_capabilities` to `set-horizon` and a saved
filter's `urgency` to `horizon`
([ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md) D1 as
amended 2026-10-06).

Both checks still take `override-urgency`: release 0.5 writes it beside `set-horizon`
(`auth.Stored`) into every set it stores, so an image rolled back to it over migration 38 keeps
writing ([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)
D3, D4). This release drops the old name wherever a set is read
([`auth.Canonical`](../../backend/internal/auth/principal.go)), which loses nothing. A saved filter
0.5 stores with the key `urgency` in such a window loses that condition under this release until a
later migration rewrites it.

## Required changes

1. **The narrowing**, now due: 0.6.0 shipped migration 38, and its code writes `override-urgency`
   nowhere (`auth.Stored` and `auth.CapOverrideUrgency` are gone at the tag `v0.6.0`), so the release
   before the one that carries the narrowing writes no old name: a migration that rewrites the three again — every
   `override-urgency` of `tokens.capabilities` and `chat_capabilities` to `set-horizon`, each name
   once, and the key `urgency` of `saved_filters.parameters` to `horizon`, for what a rollback to
   0.5 wrote in between — and then drops `override-urgency` from both checks; with its integration
   test from the version before, which proves the three rewrites and that both checks refuse the
   old name. The drop in `auth.Canonical` goes with it, and so does its unit test. Docs in the same
   change: the Status of
   [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
   D4 and its residual risk, [ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md)
   D1's build note and residual risk, the
   [operations page](../operations/installation.md#upgrade), [api.md](../developer/api.md#deprecated-names),
   [chat.md](../developer/chat.md), [testing.md](../developer/testing.md) and
   [docs/security/tokens.md](../security/tokens.md), each of which names the checks that still
   take the old name; the README reference names no stored name.

## Not verified

- An image rollback to 0.5 over migration 38 was not run; that it is safe is read from the code of
  0.5.1 (`auth.Stored`, `auth.Canonical`) and from the checks migration 38 leaves.
- The end-to-end tier did not run against this change; it needs both images built. Its TypeScript
  was checked against the generated client without Node's types.
