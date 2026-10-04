-- name: GetChatCapabilitiesForUpdate :one
-- The person's chat capabilities as they stand, locked for the change.
SELECT capabilities
FROM chat_capabilities
WHERE user_id = sqlc.arg(user_id)
FOR UPDATE;

-- name: SetChatCapabilities :exec
-- The person's choice of the chat's capabilities (docs/adr/0043 D5): written
-- the first time, replaced afterwards.
INSERT INTO chat_capabilities (user_id, capabilities)
VALUES (sqlc.arg(user_id), sqlc.arg(capabilities)::text[])
ON CONFLICT (user_id) DO UPDATE
SET capabilities = EXCLUDED.capabilities,
    updated_at = now();
