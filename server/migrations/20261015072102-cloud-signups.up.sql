-- One row per email that ever started a cloud trial, so deleting a workspace does not hand out a
-- second trial. Only keyed hashes are stored: HMAC-SHA256 under SIGNUP_HASH_KEY of the normalized
-- address (a repeat is recognised; without the key the table cannot be tested against a list of
-- addresses) and of the client IP, which is cleared 30 days after the signup, long after the 24 hour
-- rate window needs it.
-- Platform-level table: no workspace policy, only the privileged signup transaction reads it.
-- Older images ignore it, so rolling back to one only stops the signup checks.
CREATE TABLE cloud_signups (
    email_hash text PRIMARY KEY,
    signup_workspace_id uuid REFERENCES workspaces(id) ON DELETE SET NULL,
    ip_hash text,
    created_at timestamptz NOT NULL
);

CREATE INDEX cloud_signups_ip_idx ON cloud_signups(ip_hash, created_at) WHERE ip_hash IS NOT NULL;

ALTER TABLE cloud_signups ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_signups FORCE ROW LEVEL SECURITY;
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
    EXECUTE format('REVOKE ALL ON cloud_signups FROM %I', app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON cloud_signups FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
END
$$;
