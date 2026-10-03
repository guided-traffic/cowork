-- name: ListAuditForTenant :many
-- The tenant's audit record, newest first (docs/adr/0026 D6). Each filter is
-- optional; actions combine with OR, the filters with AND.
SELECT a.id, a.created_at, a.actor_user_id, a.actor_system, a.agent, a.agent_capabilities,
       a.token_id, a.entity_type, a.entity_id, a.ticket_key, a.action::text AS action, a.before,
       a.after, a.reason, a.note, a.request_id, a.idempotency_key,
       u.username AS actor_username, u.display_name AS actor_display_name
FROM audit_events a
LEFT JOIN users u ON u.id = a.actor_user_id
WHERE a.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(actor)::uuid IS NULL OR a.actor_user_id = sqlc.narg(actor)::uuid)
  AND (sqlc.narg(token)::uuid IS NULL OR a.token_id = sqlc.narg(token)::uuid)
  AND (cardinality(sqlc.arg(actions)::text[]) = 0 OR a.action::text = ANY (sqlc.arg(actions)::text[]))
  AND (sqlc.narg(entity_type)::text IS NULL OR a.entity_type = sqlc.narg(entity_type)::text)
  AND (sqlc.narg(from_time)::timestamptz IS NULL OR a.created_at >= sqlc.narg(from_time)::timestamptz)
  AND (sqlc.narg(to_time)::timestamptz IS NULL OR a.created_at < sqlc.narg(to_time)::timestamptz)
  AND (sqlc.narg(before)::uuid IS NULL OR a.id < sqlc.narg(before)::uuid)
ORDER BY a.id DESC
LIMIT sqlc.arg(page_size);
