---
id: T38
title: there are no saved filters
state: done
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-05
done: 2026-10-06
shipped: saved filters on the backlog, the tenant's ticket list and the tenant board, and a tenant administrator's unshare and deletion of another person's shared filter (migration 39 with its trigger, the filter bar's Stop sharing and Delete, a withheld filter included)
---

## Current state

Built: the table `saved_filters` (migration 33, tenant-bound, restrictive policies that hold a
person to their own filters and the shared ones), the routes `…/filters` and `…/filters/{filter}` —
list with a weak `ETag`, create with a key, read, edit with `If-Match`, share and unshare by the
edit, delete —, every change a recorded act ([`api/filters.go`](../../backend/internal/api/filters.go));
the parameters checked as the lists check them and checked again when read, a value that no longer
holds a warning ([ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md) D7),
another member's filter that names what the reader cannot see withheld; the backlog's filter bar
applies, saves — one key per content of the dialog — shares and deletes filters, a shared one
naming its owner ([`saved-filters.ts`](../../frontend/src/app/features/project/saved-filters.ts)),
and the tenant's ticket list (T53) applies them through the same bar, `project` included, a
condition a list leaves out named under the bar with why (`leftOut`); the tenant board applies,
saves and shares them through the same bar
([`tenant-board.ts`](../../frontend/src/app/features/tenant/tenant-board.ts)) — `toBoard` puts a
filter's projects into the address, `!OPS` leaving a swimlane out, and hands every other condition
to each swimlane's list, its `horizon` narrowed to the board's three (`horizonsOf`), and
`boardLeftOut` names `include_terminal`, which a board leaves out
([`saved-filter-model.ts`](../../frontend/src/app/features/project/saved-filter-model.ts)); unit
tests for the bar on all three pages; integration tests across two persons and two tenants
([`api_filters_test.go`](../../backend/test/integration/api_filters_test.go)).

A tenant administrator unshares or deletes another person's shared filter — one whose owner left
the tenant among them — and changes nothing else of it
([ADR 0018](../adr/0018-the-views-of-the-first-release.md) D5 as amended 2026-10-06):
`PATCH …/filters/{filter}` with `{"shared": false}` and nothing else, and `DELETE`, both with
`admin` scope and refused to every agent (`mayChangeFilter`), each recorded under the
administrator's name; a filter that is not shared stays out of reach (`404`). Migration 39 widens
the restrictive policies of `saved_filters` for exactly these two acts, and the read policy for the
one filter an unshare names in `app.saved_filter_id`, which the statement needs to read back the
row it leaves unshared ([ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D3 as amended 2026-10-06; [`store/filters.go`](../../backend/internal/store/filters.go)); its
trigger `saved_filters_moderation_guard` refuses any other change of another person's filter — its
name, its parameters —, which a policy cannot see. The filter bar offers an administrator *Stop
sharing* and *Delete* on another person's filter it applies, and on a shared filter the server
answers `redacted` — one that names a deleted ticket, or a key that names nothing —, which an
administrator may choose to withdraw: the bar holds it and the list applies none. Both act at once
as the owner's own acts do, and clear the filter from the list if it applied it. An owner who left
the tenant is named "a former member", since the API names such a person by the id alone.

Verified on 2026-10-06: `make test-unit` (`TestWhoChangesASavedFilter`), `make test-integration`
(`TestAnAdministratorUnsharesOrDeletesAnotherPersonsSharedFilter`,
`TestTheSavedFilterPoliciesAdmitAnAdministratorToASharedFilter` — the trigger's refusal of a rename
or a change of the conditions beside an unshare included —,
`TestTheModerationMigrationWidensTheFilterPoliciesAndChangesNoRow`, from version 38),
`make frontend-test` (the bar's administrator cases in `saved-filters.spec.ts`, a filter the server
withholds and an owner who left among them), `make lint cyclo gosec`, `make frontend-lint`,
`npx ng build` within its budgets.

In the browser, [`filters.spec.ts`](../../frontend/e2e/filters.spec.ts) walks the backlog's bar
with two identities: a member saves the backlog's filter shared, and the administrator applies it
from the saved filters and sees its owner — in Chromium and WebKit, each in both schemes, in three
local runs of the whole tier with two workers on 2026-10-06. Its assertions that the administrator
is offered *Stop sharing* and *Delete* of that filter and no share toggle were written with the
moderation and have not run.

## Required changes

None.

## Open questions

### Q1: What becomes of a shared filter whose owner left the tenant?

It stayed shared under the name of a person who is no member any more, and only that person could
unshare or delete it.

**Answer:** a tenant administrator may unshare or delete any shared saved filter of their tenant —
one whose owner left the tenant among them —, each a recorded act; an administrator may not
otherwise edit another person's filter (its name, its parameters) and may not touch another
person's filter that is not shared. Recorded in ADR 0018 D5.

### Q2: Which acts on its person saved filters are an agent's?

An agent saves, changes, shares, unshares and deletes its person's saved filter as built, since no
record lists these acts; ADR 0043 D3 keeps "deleting, restoring or purging anything" from every
agent, and a shared filter reaches every member of the tenant, whatever projects each of them sees,
its name and its `q` condition free text. A tenant administrator's unshare or deletion of another
person's shared filter is an administration act and hard-off already.

- **(a) Keep all five allowed**: nothing changes; a steered agent can delete a filter its person
  relies on, which reads as a deletion that D3 keeps from every agent.
- **(b) Deleting is D3's hard-off, the other four the baseline**: the deletion is refused with
  `403 agent_forbidden` naming the rule a ticket's deletion meets; saving, changing, sharing and
  unsharing join D2's baseline, the reach of sharing an accepted risk.
- **(c) As (b), and sharing hard-off too**: no agent widens a filter's readers, but sharing and
  unsharing its person's filter is one act of the person in the browser anyway, and an agent loses
  the share its person asks it for.
- **(d) Every write hard-off**: an agent keeps no filter of its person at all, for a risk that
  sharing alone carries.

Recommended: **(b)** — the deletion is what D3 already names, and the one risk of the other four,
the reach of sharing, is recorded on every share and undone by one unshare.

**Answer:** (b) — by the owner 2026-10-06, built the same day. Recorded in
[ADR 0043](../../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D2 and D3 as amended 2026-10-06; the reach of sharing is
[tokens.md](../../security/tokens.md#h-6) H-6.

## Not verified

The end-to-end assertions of the administrator's controls in `filters.spec.ts` have not run; the
next run of the end-to-end tier settles them. The administrator's hold of a filter the server
withholds and the name "a former member" are covered by unit tests only; no end-to-end path and no
look in a browser reaches them.
