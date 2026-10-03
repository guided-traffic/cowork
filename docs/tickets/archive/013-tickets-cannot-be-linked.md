---
id: T13
title: tickets cannot be linked, so prerequisites, duplicates and provenance cannot be recorded
state: done
severity: high
security: none
threat:
urgency: later        # rule 4: decided fix (the ADRs)
effort: M
blocked-by: T12
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: typed links read both ways, the blocks cycle refusal, the blocked filter and the urgency re-derivation
---

## Current state

Decided by [ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D1–D5,
[ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D1, [ADR 0007](../adr/0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md)
D3, [ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3 and
[ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md) D1.

- No link table, no route.
- [ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D1 maps the MCP tool
  `link` to `POST …/links`; ADR 0045 D1, the API record, shapes the write as
  `PUT …/tickets/{n}/links/{type}/{other-key}`, an existing link being success. This ticket
  builds ADR 0045's form; ADR 0042's mapping is phase 5's.
- A full key holds a `/`; percent-encoded, it stays one path segment on the standard mux
  (verified with Go 1.26.5), not verified through the generated router and the validator. Links
  never leave the tenant (ADR 0012 D2), so the short key always suffices.
- ADR 0012 D4 checks `blocks` cycles "over the blocks graph of the tenant", while the visibility
  predicates sit on every ticket query; T12 settles it: the walk is a named exception that
  returns only cycle or no cycle.
- ADR 0049 D1's filter `blocked` sits beside `state=blocked`; the reading that is not redundant
  is "the ticket has an open direct `blocks` source the caller can see".
- No record lists the removal of a `blocks` link among an agent's acts. By the owner's choice it
  stays open, which leaves a two-call path around the hard-off prerequisite override
  ([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
  D3, ADR 0012 D7): remove the open prerequisites, then close (T22).

## Required changes

1. **Migration:** the link types of ADR 0012 D1; `ticket_links` (type, source, target, creator,
   time) with composite keys to `tickets`, no self link, `relates-to` stored once, at most one
   link of a type between two tickets in one direction (ADR 0012 D4); a target index; no version
   ([ADR 0050](../adr/0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
   D4); the tenant policy; grants with `DELETE`.
2. **Routes:** `PUT …/tickets/{number}/links/{type}/{other}` — `{other}` the short key, or a
   percent-encoded full key of the same tenant; another tenant's slug or a self link → `400`; an
   end the caller cannot see → `404`; `201` for a new link, `200` without an act for an existing
   one; `DELETE` (`204`, idempotent); `GET …/links` with both directions under ADR 0012 D1's
   reverse names, an end the caller cannot see — or a deleted one — absent. Member, `write`;
   agents create links (baseline, ADR 0043 D2) and remove them, `blocks` included.
3. **The `blocks` cycle check:** a walk over the tenant's whole `blocks` graph (T12's named
   exception) under a per-tenant transaction-level lock for `blocks` writes, so concurrent A→B and
   B→A cannot both pass; the other types may form cycles.
4. **Effects in the same `Mutate`:** one act per affected ticket (`linked`, `unlinked`; ADR 0012
   D3); the target's urgency re-derived by v1's rule "an open ticket of type `decision` blocks
   it" (ADR 0010 D3) without bumping its version, its override dropped with a timeline row when
   the derivation's input changed.
5. **Filter:** `blocked=true|false` on the ticket lists, in the reading above, recorded in ADR
   0049 D1 in place.
6. **Tests:** a self link refused; two-step and n-step `blocks` cycles refused, the other types
   allowed; concurrent A→B and B→A, one refused; a repeated `PUT` writes one row and no second
   act; a link into another tenant refused by the API and by the schema; a link across projects
   allowed; the act on both tickets; the urgency re-derivation and the override's expiry; an
   agent removes an open `blocks` link (the open gate, asserted); the `blocked` filter; the
   restriction and confidential rows (a hidden end absent from `GET …/links`); the cross-tenant
   rows.
7. **Docs and records:** domain.md (types, direction, reverse names, integrity rules, the
   `blocked` filter); tenancy.md (links never cross tenants, enforced by the API and the schema;
   the integrity-walk gap extended to `blocks`); tokens.md's gap of open agent gates gains the
   removal of `blocks` links; ADR 0012 (D1–D5 built; D6 phase 3; D7 with T14) and ADR 0049 Status
   and index rows.

## Related

- T12 — tickets, the predicates and the named walk exception.
- T14 — the `done` refusal reads these links.
- T22 — the removal of `blocks` links, reviewed after experience.
