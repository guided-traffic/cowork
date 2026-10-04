-- name: GetChatCapabilities :one
-- The capabilities the person gave the chat (docs/adr/0043 D5); no row is the
-- default. The policy admits the person's own row only.
SELECT capabilities, updated_at
FROM chat_capabilities
WHERE user_id = sqlc.arg(user_id);
