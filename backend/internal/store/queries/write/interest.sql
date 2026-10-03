-- name: InsertInterest :exec
INSERT INTO ticket_interest (tenant_id, ticket_id, user_id, weight, note)
VALUES (sqlc.arg(tenant_id), sqlc.arg(ticket_id), sqlc.arg(user_id), sqlc.arg(weight), sqlc.arg(note));

-- name: UpdateInterest :exec
UPDATE ticket_interest
SET weight = sqlc.arg(weight), note = sqlc.arg(note), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id) AND user_id = sqlc.arg(user_id);

-- name: DeleteInterest :execrows
DELETE FROM ticket_interest
WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id) AND user_id = sqlc.arg(user_id);
