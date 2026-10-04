-- name: InsertInterest :exec
INSERT INTO ticket_interest (tenant_id, ticket_id, user_id, weight, note, agent, token_id, token_name)
VALUES (sqlc.arg(tenant_id), sqlc.arg(ticket_id), sqlc.arg(user_id), sqlc.arg(weight), sqlc.arg(note),
        sqlc.narg(agent), sqlc.narg(token_id), sqlc.narg(token_name));

-- name: UpdateInterest :exec
-- A changed stake carries the mark of the write that changed it, none for a
-- person's own session (docs/adr/0036 D6).
UPDATE ticket_interest
SET weight = sqlc.arg(weight), note = sqlc.arg(note), agent = sqlc.narg(agent), token_id = sqlc.narg(token_id),
    token_name = sqlc.narg(token_name), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id) AND user_id = sqlc.arg(user_id);

-- name: DeleteInterest :execrows
DELETE FROM ticket_interest
WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id) AND user_id = sqlc.arg(user_id);
