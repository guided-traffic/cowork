-- name: GetIdempotencyKey :one
-- A key is scoped to its caller: the token that sent it, or — for a browser
-- session, which has no token — the person (docs/adr/0045 D3).
SELECT fingerprint, response_status, response_headers, response_body
FROM idempotency_keys
WHERE user_id = sqlc.arg(user_id) AND token_id IS NOT DISTINCT FROM sqlc.narg(token_id)::uuid
  AND key = sqlc.arg(key) AND expires_at > now();
