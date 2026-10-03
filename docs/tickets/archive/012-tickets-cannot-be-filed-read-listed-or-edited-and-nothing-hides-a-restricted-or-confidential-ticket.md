---
id: T12
title: tickets cannot be filed, read, listed, filtered or edited, a ticket key cannot be resolved, and nothing hides a restricted or confidential ticket
state: done
severity: high
security: hardening
threat: carries the project-restriction and the confidential predicate in every ticket query from the first one, so a member outside a restricted project, or a member who is neither admin, assignee nor reporter of a confidential ticket, never reads it through any route of the phase — additionally covering the in-tenant leak ADR 0034 names as the risk of adding a predicate later
urgency: later        # rule 4: decided fix (the ADRs)
effort: L
blocked-by: T11
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: tickets filed, read, resolved, listed with filters and pages, edited, the urgency override, the confidential flag and the visibility predicate (rank and deletion moved to phase 3)
---

## Current state

Decided by [ADR 0007](../adr/0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md)
D1–D5, [ADR 0008](../adr/0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md)
D1, D2, [ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md) D1–D3,
[ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D1,
[ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D1, D2,
[ADR 0022](../adr/0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md) D2, D4,
[ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D1, D3,
[ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D1, D3, [ADR 0025](../adr/0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md)
D1–D3, D6, [ADR 0034](../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D3, D4, [ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md)
D1–D6, [ADR 0050](../adr/0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
D1–D5 and [ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D1–D6, D9. On `main`:

- No ticket table, no key parser, no resolver route.
- ADR 0023 D3's route `GET /api/v1/tickets/{slug}/{KEY}-{number}` cannot be registered on
  `http.ServeMux`, which takes a wildcard only as a whole path segment ("bad wildcard name",
  verified with Go 1.26.5); one `{key}` wildcard split at the last hyphen works.
- `to_tsvector('simple', unaccent(…))` is refused in a stored generated column ("generation
  expression is not immutable", verified on PostgreSQL 18.6); a text-search configuration with
  `unaccent` as a filter dictionary works, and `unaccent`, `pg_trgm` and `btree_gin` can be
  created by a non-superuser owner (verified).
- Fractional rank strings sort wrongly under the image's default collation; `COLLATE "C"` sorts
  them (verified).
- ADR 0007 D3: the short form `KEY-n` "is never stored", which rules out a stored short key as a
  trigram target; ADR 0025 D3's key half serves the search routes of phase 3.
- ADR 0009 D5: the `done` note "is a field of the transition, not of the ticket"; it lives on
  the transition's act (T14), not in a column.
- ADR 0050 D1 counts "fields, body, state, assignee, rank, progress" in the ticket's version
  and keeps comments, links, interest and attachments out, "otherwise every field change would
  fail on a concurrent comment"; a value derived from another entity — urgency re-derived from a
  link or another ticket, a parent's progress derived from its children — falls under the same
  reason.
- The README's naming table says the ticket key in cowork is "undecided" (question Q-A4);
  ADR 0007 decided it.

## Required changes

1. **The key grammar** (ADR 0007 D1–D3), pure Go: parse and render `<slug>/<KEY>-<number>` and
   the short form; the short form accepted only where the path fixes the tenant and echoed in
   full; a full key whose slug is not the path's refused before any lookup; non-canonical numbers
   refused.
2. **Migration:** the extensions `unaccent`, `pg_trgm`, `btree_gin` (ADR 0025's Consequences) and
   the configuration `cowork_simple`, `simple` with `unaccent` (ADR 0025 D2 amended in place);
   the enums of ADR 0008 D1, ADR 0009 D1, D2 and ADR 0010 D1 with every value from the start;
   `tickets` with the number (unique per project and not partial — a deleted ticket keeps its
   key, ADR 0024 D1), type, title, body, state with `blocked_from`, block kind, reason text and
   external reference (ADR 0009 D2), severity, security and threat (ADR 0010 D2 as a CHECK),
   urgency with its derived value, rule and the override with its reason, person and time
   (ADR 0010 D3), effort, `progress` (0–100 in fives) and the derived value a parent shows (T18),
   parent (a composite key in the same project, no self-parent), rank `COLLATE "C"` (NULL exactly
   when terminal, ADR 0014 D1), reporter, assignee, `confidential`, `opened_at`, `decided_at` and
   `done_at` (set by the acts, ADR 0009 D6), `deleted_at` (ADR 0024 D1; no phase-2 route sets
   it), the generated `search` column (title weighted above body), `version`, timestamps; indexes
   led by `tenant_id` for backlog order, state, assignee, reporter, parent, full text, and a
   trigram index on the title; the tenant policy; grants without `DELETE`. The columns that bump
   the version are listed in the migration's comment and on domain.md (ADR 0050's residual risk).
3. **Create** `POST …/projects/{KEY}/tickets` (member, `write`, agent baseline): the number from
   the counter row in the same `Mutate`; the rank at the end of its sibling group; the reporter
   is the caller's person; an assignee must be a member of the tenant who can see the project; a
   parent from the same project; urgency derived by rule set v1 (item 7); `confidential` set
   when `security` is `live` or `boundary` (ADR 0065 D2), in application code so the importer
   can apply ADR 0065 D7 later; an archived project refuses (`409`); an `Idempotency-Key`,
   required when agent-marked; the act `created`.
4. **Read:** `GET …/tickets/{number}` and the resolver `GET /api/v1/tickets/{slug}/{key}` (ADR
   0023 D3 amended in place: one `{key}` wildcard split at the last hyphen) answer the same body
   and the same `ETag`; an entity read carries the `ETag` for `If-Match` and never answers `304`
   (ADR 0050 D2 amended in place).
5. **Lists:** `GET …/projects/{KEY}/tickets` ordered by `(rank, id)`, terminal tickets after by
   `id`; `GET /api/v1/tenants/{slug}/tickets` by `id`, newest first; ADR 0049's filters with `q`
   (`plainto_tsquery('cowork_simple', …)`, capped by `COWORK_MAX_QUERY_LENGTH`) and
   `include_terminal`, `deleted_at IS NULL` in every list (ADR 0024 D3); the filters whose data
   arrives later join with it — `blocked` (T13), `has_open_questions` (T15), `interest` (T17);
   table and cursor modes; a weak `ETag` per viewer and `304` on `If-None-Match`, the polling
   fallback of [ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
   D7.
6. **Edit:** `PATCH …/tickets/{number}` (`If-Match`; title, type, severity, security, threat,
   effort, parent, assignee; a new parent moves the rank to the end of its new sibling group; a
   parent cycle is refused with `409` by a walk under a per-project transaction-level lock; T18
   adds `progress`) and `PUT …/tickets/{number}/body` (`If-Match`; the act `updated` with the
   previous and the new body, ADR 0011 D1). Agents edit fields and assign — the open variant of
   details ADR 0043 D2 does not list. Reassigning a confidential ticket admits the new assignee
   (ADR 0065 D9); an agent may do it too, the open gate T22 reviews, and tokens.md lists it.
7. **Urgency** (ADR 0010 D3): the derivation v1 as one function storing the value and its rule —
   blocked with kind `release` → `release` (`v1:release-block`); blocked with kind `decision`,
   `human` or `product` → `icebox` (`v1:icebox-block`); an open `decision` ticket blocks it →
   `icebox` (`v1:icebox-decision`, its input arrives with T13); otherwise `later`
   (`v1:default`); T13 and T14 re-run it when its inputs change. The override: `PUT` and
   `DELETE …/tickets/{number}/urgency-override` (`{value, reason}`; `If-Match`; member, `write`;
   an agent needs `override-urgency`,
   [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
   D4); the act `overridden`. When an input of the derivation changes, the override is dropped
   with a timeline row on that ticket, attributed to the act that changed the input.
8. **Version scope** (ADR 0050 D1 amended in place): a ticket's version bumps on a change to its
   own fields, body, state, assignee, rank, progress, urgency override and confidential flag;
   urgency re-derived from a link or another ticket's change and a parent's derived progress do
   not bump it. `updated_at` moves on every change of the ticket and of its comments, questions,
   links, interest rows and attachments (ADR 0049's residual risk).
9. **The confidential flag** (ADR 0065): `PUT …/tickets/{number}/confidential`
   (`{confidential, reason?}`; admin role, `admin` scope; refused to an agent-marked request,
   ADR 0065 D6; `If-Match`; a reason required to lift); the acts `confidential_set` and
   `confidential_lifted`; never lifted by a change of `security`, by `done` or by `dropped` (D3).
   **The predicates** — the project restriction (ADR 0034 D4) and `not confidential OR admin OR
   assignee OR reporter` (ADR 0065 D4) — sit in every generated ticket query and in the list
   builder, and every later child applies them to its rows. An integrity walk (the parent cycle
   here, the `blocks` cycle of T13) walks the tenant's whole graph as a named exception that
   returns no ticket, only cycle or no cycle.
10. **Tests:** numbers consecutive per project and independent across projects; concurrent
    creations get distinct numbers; a rolled-back creation consumes no number; the key grammar
    table; the resolver's body and `ETag` equal the canonical route's; the threat rule both
    ways; self, two-step and n-step parent cycles and a concurrent re-parenting refused; the
    filter semantics (OR, AND, `!`, `me`, `none`, an unknown parameter, an out-of-vocabulary
    value, dates, the `q` cap, `include_terminal`); a seeded deleted ticket absent from every list
    with its key still taken; the three-person restriction test (on the list, off it, admin) with
    seeded `project_access`; the confidential matrix (admin, assignee, reporter, another member,
    an outsider of tenant B) — set on creation and on a change to `live` or `boundary`, never
    lifted automatically, a reassignment admitting the new assignee and dropping the old one; an
    agent's reassignment of a confidential ticket succeeds (the open gate, asserted); a field
    change bumps the version and a comment does not; the v1 rules and the override's expiry; the
    extensions present; the agent rules per route; the cross-tenant rows.
11. **Docs and records:** domain.md (keys and the counter, types and parent, the vocabularies,
    urgency v1, the version list, the two predicates); docs/security/tenancy.md — the
    confidential chapter (ADR 0065 D8; the visibility of tickets inside a tenant is that page's
    perspective) with its gap that a confidential body is readable with database or backup
    access and nothing encrypts it at rest, and the integrity-walk gap: whether a parent change
    is refused can depend on a ticket the caller cannot see (dormant until a project is
    restricted or a ticket confidential), each with the next free `H-<n>`; tokens.md's gap of
    open agent gates gains the confidential reassignment; the README's naming
    (the ticket key grammar in place of the "undecided" row) and API section; installation.md
    (the extensions, ADR 0058 D5); ADR 0007, 0008, 0010, 0014 (the rank column and the append;
    moves and the score phase 3), 0022, 0023, 0024 (D3's filter; no deletion route), 0025 (the
    columns and the configuration; the search routes phase 3), 0034 D4, 0049, 0050, 0065 (D7
    phase 6) Status and index rows.

## Not verified

- Go 1.27.1's `ServeMux` on the resolver pattern; it was checked with Go 1.26.5.

## Related

- T11 — projects and the archive the creation route checks.
- T13 — links, the `blocked` filter and v1's prerequisite rule.
- T14 — transitions and the notes on their acts.
- T15 — questions under the predicates.
- T18 — progress on `PATCH` and the derived value.
- T22 — the confidential reassignment gate.
