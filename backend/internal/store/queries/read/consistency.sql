-- The consistency check of the attachments (docs/adr/0059 D4, migration 42):
-- the latest result of a tenant, read by its administrators, and every
-- tenant's counts and the time of the last run, read in a transaction that
-- names the job and no tenant — a scrape's and the schedule's. Beside them the
-- time of a tenant's last export, the second line of its backup (docs/adr/0059
-- D2, migration 46).

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

-- name: ListLastExports :many
-- When every tenant was last exported — a project of it or the whole tenant, as
-- the act exported records it —, or made, where it never was: what the seconds
-- since its last export count from, for a scrape (docs/adr/0060 D4). A
-- ticket's export is no copy of the tenant and does not count; the policy
-- audit_exports_read of migration 46 admits the job's read of those acts alone.
SELECT t.id AS tenant_id,
       coalesce((SELECT max(a.created_at) FROM audit_events a
                 WHERE a.tenant_id = t.id AND a.action = 'exported' AND a.entity_type IN ('project', 'tenant')),
                t.created_at)::timestamptz AS since
FROM tenants t
ORDER BY t.id;

-- name: LastTenantExport :one
-- When a project of the tenant or the whole tenant was last exported, for its
-- administrators beside the consistency check; no row where none was.
SELECT created_at FROM audit_events
WHERE tenant_id = sqlc.arg(tenant_id) AND action = 'exported' AND entity_type IN ('project', 'tenant')
ORDER BY created_at DESC
LIMIT 1;

-- name: LastConsistencyCheck :one
-- How many tenants have a result, and when the last run checked: the epoch
-- where none has.
SELECT count(*)::bigint AS tenants, coalesce(max(checked_at), 'epoch'::timestamptz)::timestamptz AS last_checked_at
FROM consistency_checks;
