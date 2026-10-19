-- Secrets the server itself needs and that every process must share: today the key that signs image proxy
-- addresses. The first process to start inserts a random value (INSERT ... ON CONFLICT DO NOTHING), every
-- process reads it back, so replicas, overlapping deploys and restarts all sign and verify with the same
-- key. The table has no workspace_id and no grant to the app role: only the privileged owner path reads it
-- (same shape as cloud_signups).
CREATE TABLE server_secrets (
    name text PRIMARY KEY CHECK (char_length(name) BETWEEN 1 AND 100),
    value bytea NOT NULL CHECK (octet_length(value) >= 32),
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE server_secrets ENABLE ROW LEVEL SECURITY;
ALTER TABLE server_secrets FORCE ROW LEVEL SECURITY;
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    -- The app role gets every new table by default; this one is for the privileged path only.
    EXECUTE format('REVOKE ALL ON server_secrets FROM %I', app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON server_secrets FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
END
$$;
