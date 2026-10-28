-- The key a client sent with a create request (the Idempotency-Key header) for an article, a saved reply or
-- a test message, so fast repeated clicks create one thing. A repeat finds the stored answer and returns it.
-- operation names the kind of request; fingerprint is a hash of its input and author, so a key reused
-- for other input is refused. response is NULL while the first request still runs (a repeat waits for it).
-- A key is honoured for a day; older rows are removed as new ones are written.
-- Older images ignore this table.
CREATE TABLE request_idempotency_keys (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    operation text NOT NULL CHECK (char_length(operation) BETWEEN 1 AND 64),
    key text NOT NULL CHECK (char_length(key) BETWEEN 1 AND 200),
    fingerprint text NOT NULL,
    response jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, operation, key)
);

CREATE INDEX request_idempotency_keys_age_idx ON request_idempotency_keys(workspace_id, created_at);

-- Same isolation as message_idempotency_keys.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE request_idempotency_keys ENABLE ROW LEVEL SECURITY;
    ALTER TABLE request_idempotency_keys FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON request_idempotency_keys FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON request_idempotency_keys FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON request_idempotency_keys TO %I', app_role);
END
$$;
