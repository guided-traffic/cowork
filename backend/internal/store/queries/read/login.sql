-- The login's reads. Each runs in a transaction that names the login as its
-- job (app.job = 'login'), which the policies of these tables admit
-- (docs/adr/0021 D3, D6): the login looks an account up by the username it was
-- given, before it knows a person.

-- name: GetLoginAccount :one
-- The local account a username names, with its password hash; no row when the
-- username names no local account.
SELECT u.id, u.username, u.deactivated_at, u.global_admin, a.password_hash, a.password_change_required
FROM users u
JOIN local_accounts a ON a.user_id = u.id
WHERE u.username = sqlc.arg(username);

-- name: TenantsExist :one
-- docs/adr/0032 D5: the installation is initialised once a tenant exists.
SELECT EXISTS (SELECT 1 FROM tenants) AS exist;

-- name: LocalLoginAvailable :one
-- docs/adr/0033 D8: the login page offers the local form when at least one
-- active local account exists.
SELECT EXISTS (
    SELECT 1
    FROM local_accounts a
    JOIN users u ON u.id = a.user_id
    WHERE u.deactivated_at IS NULL
) AS available;

-- name: GetLoginLock :one
SELECT locked_at, sticky, noted_at
FROM login_locks
WHERE username = sqlc.arg(username);

-- name: CountLoginFailures :one
-- The failed attempts of a username since a time; attempts refused because of
-- a lock count as failed.
SELECT count(*)
FROM login_attempts
WHERE username = sqlc.arg(username) AND failed AND created_at > sqlc.arg(since);

-- name: CountAddressAttempts :one
-- The attempts of an address since a time, whatever their outcome
-- (docs/adr/0033 D6).
SELECT count(*)
FROM login_attempts
WHERE address = sqlc.arg(address) AND created_at > sqlc.arg(since);
