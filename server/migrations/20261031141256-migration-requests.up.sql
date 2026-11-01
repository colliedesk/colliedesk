-- The Freshdesk data moves an admin asked Colliedesk to do. The email to support is the request itself;
-- this row is the admin's proof that it went out, and what Plan and billing shows afterwards. The row
-- stays when the admin who asked is removed. It holds the Freshdesk host name and the times the admin
-- typed, no ticket data.
CREATE TABLE workspace_migration_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    requested_by uuid,
    freshdesk_address text NOT NULL CHECK (freshdesk_address <> ''),
    times text NOT NULL DEFAULT '',
    requested_at timestamptz NOT NULL,
    FOREIGN KEY (workspace_id, requested_by) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (requested_by)
);
CREATE INDEX workspace_migration_requests_workspace_time ON workspace_migration_requests (workspace_id, requested_at DESC);

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE workspace_migration_requests ENABLE ROW LEVEL SECURITY;
    ALTER TABLE workspace_migration_requests FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON workspace_migration_requests FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON workspace_migration_requests FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON workspace_migration_requests TO %I', app_role);
END
$$;
