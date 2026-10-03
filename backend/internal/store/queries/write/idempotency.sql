-- name: StoreIdempotencyKey :one
-- A key that exists and has expired is taken over; one that has not expired
-- belongs to a concurrent request with the same key, and no row comes back.
INSERT INTO idempotency_keys (
    token_id, user_id, tenant_id, key, fingerprint, response_status, response_headers,
    response_body, expires_at
) VALUES (
    sqlc.arg(token_id), sqlc.arg(user_id), sqlc.narg(tenant_id), sqlc.arg(key), sqlc.arg(fingerprint),
    sqlc.arg(response_status), sqlc.arg(response_headers), sqlc.arg(response_body),
    now() + interval '24 hours'
)
ON CONFLICT (token_id, key) DO UPDATE
SET user_id = EXCLUDED.user_id, tenant_id = EXCLUDED.tenant_id, fingerprint = EXCLUDED.fingerprint,
    response_status = EXCLUDED.response_status, response_headers = EXCLUDED.response_headers,
    response_body = EXCLUDED.response_body, created_at = now(), expires_at = EXCLUDED.expires_at
WHERE idempotency_keys.expires_at <= now()
RETURNING id;

-- name: DeleteExpiredIdempotencyKeys :execrows
DELETE FROM idempotency_keys WHERE expires_at <= now();
