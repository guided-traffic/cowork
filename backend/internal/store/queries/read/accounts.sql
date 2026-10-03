-- name: ListManagedAccounts :many
-- The local accounts a tenant manages (docs/adr/0033 D1, D5), by person id, a
-- page after the cursor's person: the person's highest role in the tenant — empty
-- when they no longer have one — and whether the account is locked at the moment.
SELECT u.id, u.username, u.display_name, u.deactivated_at, u.created_at, a.password_change_required,
       COALESCE(m.role::text, '')::text AS role,
       EXISTS (SELECT 1 FROM login_locks l
               WHERE l.username = u.username AND (l.sticky OR l.locked_at > sqlc.arg(lock_since)))::boolean AS locked
FROM local_accounts a
JOIN users u ON u.id = a.user_id
LEFT JOIN (SELECT user_id, max(role)::tenant_role AS role FROM memberships
           WHERE tenant_id = sqlc.arg(tenant_id) GROUP BY user_id) m ON m.user_id = u.id
WHERE a.managing_tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(after)::uuid IS NULL OR u.id > sqlc.narg(after)::uuid)
ORDER BY u.id
LIMIT sqlc.arg(page_size);

-- name: GetManagedAccount :one
-- One of the tenant's managed accounts by username; no row when the username
-- names none — also one another tenant manages, which this tenant cannot see.
SELECT u.id, u.username, u.display_name, u.deactivated_at, u.global_admin
FROM users u
JOIN local_accounts a ON a.user_id = u.id
WHERE u.username = sqlc.arg(username) AND a.managing_tenant_id = sqlc.arg(tenant_id);

-- name: GetUserByUsername :one
-- The start-up synchronisation's lookup of the account COWORK_LOCAL_ADMIN_USERNAME
-- names (app.job = 'bootstrap'): the person, and what a local account has, if
-- they have one.
SELECT u.id, u.username, u.deactivated_at, u.global_admin,
       a.password_hash AS account_password_hash, a.origin AS account_origin
FROM users u
LEFT JOIN local_accounts a ON a.user_id = u.id
WHERE u.username = sqlc.arg(username);

-- name: ListConfigAccounts :many
-- The accounts the configuration keeps (docs/adr/0032 D2, D4).
SELECT u.id, u.username, u.deactivated_at
FROM local_accounts a
JOIN users u ON u.id = a.user_id
WHERE a.origin = 'config'
ORDER BY u.username;

-- name: GetOwnLocalAccount :one
-- The person's own account row: the hash of the current password and where the
-- account comes from.
SELECT password_hash, origin
FROM local_accounts
WHERE user_id = sqlc.arg(user_id);
