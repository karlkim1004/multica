-- name: CreateChatSecret :exec
INSERT INTO chat_secret (secret_ref, workspace_id, chat_session_id, ciphertext)
VALUES ($1, $2, $3, $4);

-- name: ListChatSecretsForSession :many
SELECT secret_ref, ciphertext FROM chat_secret
WHERE workspace_id = $1 AND chat_session_id = $2
ORDER BY created_at, secret_ref;
