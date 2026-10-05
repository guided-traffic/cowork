---
id: T52
title: the horizon of a ticket is derived from its state, and an agent can neither file a ticket into one nor re-sort the backlog
state: in-progress
severity: low
security: none
threat:
urgency: release      # rule 2: gated on the release — the contract waits for a release after the one that ships the expand
effort: S
blocked-by: release
filed-from: the owner's report of 2026-10-04
opened: 2026-10-04
decided: 2026-10-04
done:
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

The identifiers follow the word: the owner chose on 2026-10-05 to rename the API and keep the
database, recorded in ADR 0010 D1 as amended that day. Its expand half is built: a ticket answers
`horizon` and `horizon_set`, `PUT …/horizon` sets it, a filing, both ticket lists and the saved
filters take `horizon`, the capability is `set-horizon`
([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4), the export and the context write `horizon:`
([ADR 0044](../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D1), and the tools, the chat and the UI use the new names. The database keeps the enum `urgency`
and its columns, and migration 37 lets the stored capability sets take both names. The names before
stay in `/api/v1`, deprecated, behaving as they did:

| Deprecated | Replaced by |
|---|---|
| `Ticket.urgency`, `urgency_derived`, `urgency_rule`, `urgency_override`; the schemas `Urgency`, `UrgencyOverride`, `UrgencyOverrideSet` | `horizon`, `horizon_set`; `Horizon`, `HorizonSet`, `HorizonUpdate` |
| `PUT` and `DELETE …/urgency-override` (`overrideUrgency`, `withdrawUrgencyOverride`) | `PUT …/horizon` (`setHorizon`) |
| `TicketCreate.urgency`, the lists' filter `urgency`, `SavedFilterParameters.urgency` | `horizon` |
| the capability `override-urgency`, stored in `tokens.capabilities` and `chat_capabilities` and answered after `set-horizon` in `request.capabilities` of `GET /api/v1/me/token` | `set-horizon` |

The `cowork-mcp` of 0.4.x reads `urgency`, `urgency_derived`, the two urgency-override routes and
`override-urgency` from `/me/token`. `TicketCreate.urgency` and `SavedFilterParameters` were never
in a release.

## Required changes

The contract, in a release after the one that ships the expand, once no supported `cowork-mcp`
reads the names before ([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)
D3):

1. **A migration** that rewrites every `override-urgency` in `tokens.capabilities` and
   `chat_capabilities` to `set-horizon`, each name once, drops `override-urgency` from both checks,
   and rewrites the key `urgency` of `saved_filters.parameters` to `horizon` — a filter stored with
   `urgency` would otherwise lose that condition without a word, since the API decodes the stored
   object into a type that no longer has the field. Its integration test starts from the version
   before and proves the three rewrites and the refusal of the old capability name.
2. **The document:** the deprecated properties, schemas, operations, the `urgency` parameter and
   the enum value `override-urgency` out of [`backend/api/`](../../backend/api/); `make generate`
   and `make frontend-generate`.
3. **The code:** `urgencyFields`, the old name in `filedHorizon` and `ticketQuery.horizons`,
   `horizonNamed`, `horizonOfStored`, `auth.CapOverrideUrgency` and its mapping in `auth.Canonical`,
   the alias of `requestCapabilities`, the deprecated handlers; the tenant list's `urgency` kind in
   `parameterKinds` and the exclusion in `SelectableCapability`; the tests of the old surface
   (`TestTheRoutesAndFieldsUnderTheNamesBeforeKeepWorking`, the old names in
   `TestAFilingTakesTheHorizonUnderEitherName`, `TestAFilterSavedWithTheNameBeforeReadsAsHorizon`,
   `TestTheCapabilityIsSetHorizonUnderEitherName`, `TestCanonical`, the unit tests of
   `horizon_test.go`).
4. **Docs in the same change:** the deprecated rows of the README reference,
   [api.md](../developer/api.md#deprecated-names), the Status and the amended rows of ADR 0010
   D1, ADR 0043 D4 and [ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md)
   D1; ADR 0044 D3 keeps the importer reading `urgency`, which the ticket files of a repository
   carry.

## Open questions

### Q1: Does a capability set this release writes carry `override-urgency` beside `set-horizon`?

A token made, or a chat set chosen, under this release stores `set-horizon` alone. The release
before knows only `override-urgency`: after an image rollback, the only rollback
([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D4),
such an agent is refused the horizon (`403 agent_forbidden`, `missing capability:
override-urgency`) until a person makes a new token or chooses again; the sets stored before keep
the old name and hold on either release.

- (a) **Keep it as built:** `set-horizon` alone. The stored sets read as the brief named them; the
  rollback fails closed for the sets written after the upgrade.
- (b) **Write both names into a new set** until the contract — `set-horizon` and
  `override-urgency` — in the token's creation and the chat's choice. The reads are unchanged:
  `auth.Canonical` takes both as one, every answer names `set-horizon`. The rollback stays whole;
  the cost is the redundant name in the stored sets, which the contract's rewrite removes anyway,
  and a few lines with their tests.

Recommended: **(b)** — the expand exists so that the release before keeps working on what this
release writes, and (b) gives the capability that at little cost; (a) leaves the one hole in it.

**Answer:** _open_

## Not verified

- The end-to-end tier with its seeds and reads renamed did not run: it needs both images built.
  Its TypeScript was checked against the generated client without Node's types.
- An image rollback to 0.4.x over migration 37 was not run; what the release before does with
  `set-horizon` is read from its code (`Principal.Can` compares names), not observed.
