-- The login's writes. They run in the login's transaction (app.job = 'login'),
-- as a system actor (docs/adr/0027 D5).

-- name: InsertLoginAttempt :exec
INSERT INTO login_attempts (username, address, failed, created_at)
VALUES (sqlc.arg(username), sqlc.arg(address), sqlc.arg(failed), sqlc.arg(created_at));

-- name: UpsertLoginLock :exec
INSERT INTO login_locks (username, locked_at, sticky)
VALUES (sqlc.arg(username), sqlc.arg(locked_at), sqlc.arg(sticky))
ON CONFLICT (username) DO UPDATE
SET locked_at = EXCLUDED.locked_at, sticky = EXCLUDED.sticky, noted_at = NULL;

-- name: NoteLoginLock :exec
UPDATE login_locks SET noted_at = sqlc.arg(noted_at) WHERE username = sqlc.arg(username);

-- name: DeleteFailedLoginAttempts :execrows
-- The failures of a username, which is what a lock is made of: an
-- administrator's unlock, the creation of an account of that name, a changed
-- password of the local administrator.
DELETE FROM login_attempts WHERE username = sqlc.arg(username) AND failed;

-- name: DeleteLoginLock :execrows
DELETE FROM login_locks WHERE username = sqlc.arg(username);

-- name: DeleteExpiredLoginAttempts :execrows
DELETE FROM login_attempts WHERE created_at <= sqlc.arg(before);

-- name: DeleteExpiredLoginLocks :execrows
-- A lock that ended with its window; a sticky one stays until an administrator
-- unlocks it.
DELETE FROM login_locks WHERE NOT sticky AND locked_at <= sqlc.arg(before);
