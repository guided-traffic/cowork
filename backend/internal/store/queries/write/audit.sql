-- name: InsertAuditEvent :exec
INSERT INTO audit_events (
    id, tenant_id, actor_user_id, actor_system, agent, agent_capabilities, token_id,
    entity_type, entity_id, ticket_id, ticket_key, action, before, after,
    reason, note, explained_by_comment_id, refs, idempotency_key, request_id, source_hash
) VALUES (
    sqlc.arg(id), sqlc.narg(tenant_id), sqlc.narg(actor_user_id), sqlc.narg(actor_system), sqlc.narg(agent),
    sqlc.narg(agent_capabilities), sqlc.narg(token_id), sqlc.arg(entity_type), sqlc.narg(entity_id),
    sqlc.narg(ticket_id), sqlc.narg(ticket_key), sqlc.arg(action), sqlc.narg(before), sqlc.narg(after),
    sqlc.narg(reason), sqlc.narg(note), sqlc.narg(explained_by_comment_id), sqlc.arg(refs),
    sqlc.narg(idempotency_key), sqlc.narg(request_id), sqlc.narg(source_hash)
);
