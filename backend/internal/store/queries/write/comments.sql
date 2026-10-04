-- name: InsertComment :one
INSERT INTO comments (tenant_id, ticket_id, author_id, agent, token_id, token_name, body)
VALUES (sqlc.arg(tenant_id), sqlc.arg(ticket_id), sqlc.arg(author_id), sqlc.narg(agent), sqlc.narg(token_id),
        sqlc.narg(token_name), sqlc.arg(body))
RETURNING id;

-- name: InsertCommentRevision :exec
INSERT INTO comment_revisions (tenant_id, comment_id, body, edited_by, agent, token_id, token_name)
VALUES (sqlc.arg(tenant_id), sqlc.arg(comment_id), sqlc.arg(body), sqlc.arg(edited_by), sqlc.narg(agent),
        sqlc.narg(token_id), sqlc.narg(token_name));

-- name: UpdateCommentBody :one
UPDATE comments
SET body = sqlc.arg(body), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version) AND withdrawn_at IS NULL
RETURNING version;

-- name: WithdrawComment :exec
UPDATE comments
SET withdrawn_by = sqlc.arg(withdrawn_by), withdrawn_at = now(), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND withdrawn_at IS NULL;
