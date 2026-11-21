-- The key a client sent with a new message (the Idempotency-Key header), so a repeated submit
-- returns the message the first one created instead of storing it twice. One key names one message
-- per workspace; the row goes with its message. Older images ignore this table.
CREATE TABLE message_idempotency_keys (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    key text NOT NULL CHECK (char_length(key) BETWEEN 1 AND 200),
    message_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, key),
    FOREIGN KEY (workspace_id, message_id)
        REFERENCES messages(workspace_id, id) ON DELETE CASCADE
);

CREATE INDEX message_idempotency_keys_message_idx
    ON message_idempotency_keys(workspace_id, message_id);

-- Same isolation as conversation_presence.
SELECT secure_workspace_table('message_idempotency_keys');
