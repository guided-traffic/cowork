-- name: TimeLockedUntil :one
-- The closed period, read FOR SHARE so a lock moved concurrently waits for
-- the write that checked it (docs/adr/0017 D8).
SELECT time_locked_until FROM tenants WHERE id = sqlc.arg(tenant_id) FOR SHARE;

-- name: InsertTimeEntry :one
INSERT INTO time_entries (tenant_id, ticket_id, person_id, author_id, minutes, day, note, token_id, token_name)
VALUES (sqlc.arg(tenant_id), sqlc.arg(ticket_id), sqlc.arg(person_id), sqlc.arg(author_id), sqlc.arg(minutes),
        sqlc.arg(day), sqlc.arg(note), sqlc.narg(token_id), sqlc.narg(token_name))
RETURNING id;

-- name: InsertTimeEntryRevision :exec
INSERT INTO time_entry_revisions (tenant_id, entry_id, minutes, day, note, edited_by, token_id, token_name)
VALUES (sqlc.arg(tenant_id), sqlc.arg(entry_id), sqlc.arg(minutes), sqlc.arg(day), sqlc.arg(note), sqlc.arg(edited_by),
        sqlc.narg(token_id), sqlc.narg(token_name));

-- name: UpdateTimeEntry :one
UPDATE time_entries
SET minutes = sqlc.arg(minutes), day = sqlc.arg(day), note = sqlc.arg(note), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version) AND voided_at IS NULL
RETURNING version;

-- name: VoidTimeEntry :exec
UPDATE time_entries
SET voided_by = sqlc.arg(voided_by), voided_at = now(), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND voided_at IS NULL;
