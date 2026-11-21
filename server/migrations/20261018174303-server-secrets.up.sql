-- Secrets the server itself needs and that every process must share: today the key that signs
-- image proxy addresses. The first process to start inserts a random value (INSERT ... ON CONFLICT
-- DO NOTHING), every process reads it back, so replicas, overlapping deploys and restarts all sign
-- and verify with the same key. The table has no workspace_id and no grant to the app role: only
-- the privileged owner path reads it (same shape as cloud_signups).
CREATE TABLE server_secrets (
    name text PRIMARY KEY CHECK (char_length(name) BETWEEN 1 AND 100),
    value bytea NOT NULL CHECK (octet_length(value) >= 32),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- The app role gets every new table by default; this one is for the privileged path only.
SELECT secure_server_table('server_secrets');
