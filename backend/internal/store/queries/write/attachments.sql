-- name: InsertAttachment :exec
INSERT INTO attachments (id, tenant_id, ticket_id, comment_id, file_name, size, sha256, content_type, uploaded_by, agent,
                         token_id, token_name)
VALUES (sqlc.arg(id), sqlc.arg(tenant_id), sqlc.arg(ticket_id), sqlc.narg(comment_id), sqlc.arg(file_name), sqlc.arg(size),
        sqlc.arg(sha256), sqlc.arg(content_type), sqlc.arg(uploaded_by), sqlc.narg(agent), sqlc.narg(token_id),
        sqlc.narg(token_name));
