-- The consistency check of the attachments (docs/adr/0059 D4, migration 42):
-- the latest result of a tenant, read by its administrators, and every
-- tenant's counts and the time of the last run, read in a transaction that
-- names the job and no tenant — a scrape's and the schedule's.

-- name: GetAttachmentConsistency :one
-- The tenant's latest result, with the administrator who removed its orphans;
-- the restrictive policy holds it to the tenant's administrators.
SELECT c.id, c.checked_at, c.dangling, c.accepted, c.orphans, c.orphan_bytes, c.dangling_items, c.orphan_items,
       c.orphans_removed_at, c.orphans_removed_by, ru.username AS orphans_removed_by_username,
       ru.display_name AS orphans_removed_by_name, c.orphans_removed, c.orphans_kept
FROM consistency_checks c
LEFT JOIN users ru ON ru.id = c.orphans_removed_by
WHERE c.tenant_id = sqlc.arg(tenant_id);

-- name: ListConsistencyCounts :many
-- Every tenant's counts of its latest result, for a scrape (docs/adr/0060 D4).
SELECT tenant_id, dangling, orphans FROM consistency_checks ORDER BY tenant_id;

-- name: LastConsistencyCheck :one
-- How many tenants have a result, and when the last run checked: the epoch
-- where none has.
SELECT count(*)::bigint AS tenants, coalesce(max(checked_at), 'epoch'::timestamptz)::timestamptz AS last_checked_at
FROM consistency_checks;
