---
id: T8
title: there is no token table, no audit record and no idempotency store, so no write can be committed the way ADR 0027 requires
state: done
severity: high
security: hardening
threat: makes every committed write carry its audit row — person or system actor, token, agent mark and capabilities — in a table the runtime role can only insert into and read, and stores an agent POST's key with its act — additionally covering an unattributed change, a retried agent POST that acts twice, and a defect in the serving process that rewrites or deletes the trail
urgency: later        # rule 4: decided fix (the ADRs)
effort: L
blocked-by: T7
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: tokens, the append-only audit record, the idempotency store and its expiry job
---

## Current state

Decided by [ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md)
D1–D7, [ADR 0027](../adr/0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D3, D5, [ADR 0035](../adr/0035-personal-access-tokens.md) D1–D4, D6,
[ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D2, D5, [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D1, D4, D5, [ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D3, D4, D6, D7 and [ADR 0021](../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D2. On `main` none of it exists.

- ADR 0026 D1's column list predates later records: the ticket key that survives the purge
  ([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
  D2) and the capability set of an agent act (ADR 0043 D5). Its "`actor_user_id` … never null"
  meets the system actors of ADR 0027 D5 — this phase's first job, the idempotency expiry (ADR
  0045 D4), is one — while [ADR 0004](../adr/0004-cowork-is-a-team-product.md) D1 and ADR 0036 D8
  keep non-persons out of `users`.
- ADR 0026 D2 ("a request that fails writes no row") meets ADR 0035 D9 (a refused use of a dead
  token is a recorded act). The later, specific record governs: the refusal is an act of its
  own (T9 writes it), and D2 reads "a failed mutation writes no row".
- ADR 0026 D4 lets only global administrators read installation-level rows (`tenant_id` NULL),
  and phase 2 has none — so nobody could read the installation-level rows that name a person,
  among them the refused uses of that person's unrestricted token that T9's hourly bound must
  read. No record decides a person's read of their own rows.
- [ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1 says
  every audit record has a `tenant_id` that is never null; ADR 0026 D1, later and specific,
  allows NULL for installation-level acts.
- Verified on PostgreSQL 18.6: an `ON DELETE CASCADE` into the audit table erased a tenant's audit
  rows past both the grant and the policy; an `INSERT … RETURNING` of an installation-level row
  fails for a role whose read policy does not admit it.

## Required changes

1. **`tokens`** (ADR 0035, 0036, 0043): person, name, the SHA-256 of the token (32 bytes,
   unique), scope `read|write|admin`, optional tenant and project restriction — named
   `restricted_tenant_id` and `restricted_project_id`, so ADR 0021 D1's tenant policy does not
   read onto a person's table — with a composite key to `projects`; the agent flag; the capability
   set, a subset of ADR 0043 D4's nine (a plain token carries none; a request it marks with the
   header gets the default, all on, T9); `created_at`, `expires_at` after it (ADR 0035 D4),
   `last_used_on` (a UTC date); `revoked_at` with `revoked_by_user_id` or `revoked_by_system` —
   exactly one when revoked, the rule `audit_events` uses, so a system act (a deactivation, ADR
   0024 D5) can revoke. CHECKs: a project restriction needs a tenant restriction; no `admin`
   scope on a flagged token (ADR 0036 D5). Policies: readable by its person, or by the presented
   hash through `app.token_hash` (admits exactly that row — verified); updatable by its person.
   The runtime role's `UPDATE` grant names only `last_used_on` and the revocation columns: with
   the owner split a column grant binds it (ADR 0021 D2), so flag, scope, restrictions,
   capabilities and expiry stay immutable (ADR 0036 D2, ADR 0043 D1) without a trigger.
2. **`audit_events`** (ADR 0026 D1 with the resolved columns): `tenant_id` (NULL for an
   installation-level act; no cascade from anything), `actor_user_id` or `actor_system`
   (`system:<name>`) with an exactly-one CHECK, `agent` (the validated header or
   `unknown-agent`), `agent_capabilities` (on agent acts), `token_id`, `entity_type`,
   `entity_id`, `ticket_id` and `ticket_key` without foreign keys, `action` (ADR 0026 D1's values
   and the ones this phase acts with; a later value by a migration of its own), `before` and
   `after` (changed fields only), `reason`, `note`, `explained_by_comment_id`, `idempotency_key`,
   `request_id`, `created_at`; indexes led by `tenant_id` (by ticket, by token, by actor) and one
   for a person's installation-level rows. Policies: a tenant's rows readable inside the tenant,
   an installation-level row by the person it names; a row inserted only for the transaction's
   own context (`tenant_id IS NOT DISTINCT FROM` the tenant setting), so a tenant act is never
   filed as installation-level. Grant: `INSERT` and `SELECT`, nothing else (ADR 0026 D3). An
   installation-level act is inserted without `RETURNING`.
3. **`idempotency_keys`** (ADR 0045 D3, D4): the caller — in phase 2 always a token — by
   `token_id` and its person, `tenant_id`, `key`, a fingerprint (SHA-256 over method, operation,
   path parameters and body; T19 defines an upload's), the response's status, its `ETag` and
   `Location` headers and its body, `expires_at` (24 hours on); unique `(token_id, key)`.
   Policy: the caller's person inside the request's tenant, plus the expiry job's clause
   (`app.job`, T4). A later session caller is an added nullable column with its own unique index
   (ADR 0028 D3).
4. **`Mutate` completed:** one audit row per recorded act before commit, and any failure rolls
   back the act, its rows and the key (ADR 0026 D2); idempotency — the key row inserted first
   with `ON CONFLICT DO NOTHING`, so a concurrent duplicate waits on the unique index; on a
   conflict, the same fingerprint replays the stored response without acting, a different one —
   or a row of another tenant the policy hides — answers `422 idempotency_mismatch`; a stored
   response is bounded by `COWORK_MAX_JSON_BODY` and holds neither attachment bytes (ADR 0045 D6)
   nor a secret; an
   audited read (a download, a token's export; ADR 0026 D5) writes its audit row and nothing else.
5. **The expiry job** (daily, on T4's runner) deletes expired keys and writes one
   installation-level act per run that removed rows, as `system:idempotency-expiry`.
6. **Fixture:** the fixture package mints tokens in ADR 0035 D1's format — `cwk_` and 32 random
   bytes in base62, padded to a fixed 43 characters so a scanner can match the whole token — and
   stores only the hash.
7. **Tests:** an `fn` failure leaves no write, no audit row, no key; a stale version answers `412`
   with the current version; the idempotency matrix (same key and body → one act, identical
   responses; different body → `422`; concurrent duplicates → one act; a failed act stores no
   key; an expired key is a new request; the job removes only expired rows and records its act);
   the runtime role can neither `UPDATE`, `DELETE` nor `TRUNCATE` `audit_events`, nor change a
   token's flag, scope, restrictions, capabilities or expiry; a tenant act cannot be filed as
   installation-level; no foreign key with `ON DELETE CASCADE` reaches `audit_events` (a catalog
   walk); the token CHECKs.
8. **Docs and records:** data-access.md (`Mutate`'s audit and idempotency steps, audited reads,
   the job); adding-things.md ("An audit action"); in place: ADR 0026 D1 (the columns and the
   actor pair), D2 ("a failed mutation"; a refused use is an act of its own) and D4 (a person
   reads the installation-level rows that name them — the open variant of a read no record
   decides for persons), and ADR 0005 D1's note that installation-level audit rows carry no tenant
   (ADR 0026 D1); ADR 0026 and ADR 0045 Status and index rows.

## Related

- T4 — `Mutate`, the job runner and the settings this ticket's policies read.
- T7 — persons, tenants and projects the new tables reference.
- T9 — the resolver that fills the act's token, agent and capabilities.
