-- User-entered chat secrets are retained encrypted at rest and referenced
-- from chat_message.content by an opaque UUID. Deleting the chat session
-- deletes its secret rows as well.
CREATE TABLE chat_secret (
    secret_ref UUID PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    chat_session_id UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX chat_secret_session_idx ON chat_secret (chat_session_id, created_at);
