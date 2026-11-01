-- The names of a Zalo OA's customers as Zalo gives them (their profile, or a group's member list), read
-- once a delivery of theirs was taken: a delivery names its sender by id only. A message parsed after a
-- name is known carries it; looked_up_at keeps a sender Zalo has no name for from being asked again
-- more than once an hour.
CREATE TABLE zalo_users (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    user_id text NOT NULL CHECK (char_length(user_id) BETWEEN 1 AND 100),
    name text CHECK (char_length(name) BETWEEN 1 AND 200),
    looked_up_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, channel_id, user_id),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id) ON DELETE CASCADE
);

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE zalo_users ENABLE ROW LEVEL SECURITY;
    ALTER TABLE zalo_users FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON zalo_users FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON zalo_users FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON zalo_users TO %I', app_role);
END
$$;
