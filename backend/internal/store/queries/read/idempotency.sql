-- name: GetIdempotencyKey :one
SELECT fingerprint, response_status, response_headers, response_body
FROM idempotency_keys
WHERE token_id = sqlc.arg(token_id) AND key = sqlc.arg(key) AND expires_at > now();
