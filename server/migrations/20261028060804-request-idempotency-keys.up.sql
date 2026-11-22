-- The key a client sent with a create request (the Idempotency-Key header) for an article, a saved
-- reply or a test message, so fast repeated clicks create one thing. A repeat finds the stored
-- answer and returns it. operation names the kind of request; fingerprint is a hash of its input
-- and author, so a key reused for other input is refused. response is NULL while the first request
-- still runs (a repeat waits for it). A key is honoured for a day; older rows are removed as new
-- ones are written. Older images ignore this table.
CREATE TABLE request_idempotency_keys (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    operation text NOT NULL CHECK (char_length(operation) BETWEEN 1 AND 64),
    key text NOT NULL CHECK (char_length(key) BETWEEN 1 AND 200),
    fingerprint text NOT NULL,
    response jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, operation, key)
);

CREATE INDEX request_idempotency_keys_age_idx
    ON request_idempotency_keys(workspace_id, created_at);

-- Same isolation as message_idempotency_keys.
SELECT secure_workspace_table('request_idempotency_keys');
