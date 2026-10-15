-- Retention of locked workspaces.
--
-- retention_notice_sent_at: when the last admin of a locked workspace was told it will be deleted.
-- The 30 day clock starts then. Cleared when the workspace gets access again.
ALTER TABLE workspaces ADD COLUMN retention_notice_sent_at timestamptz;

-- Which admins were already told (or whose address the relay refused for good) in the current
-- lock, so a partly failed pass never mails the same admin twice. It holds agent ids, not addresses.
CREATE TABLE workspace_retention_notices (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('sent', 'rejected')),
    sent_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, agent_id),
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE
);

-- One row per deleted workspace: the audit trail, and what the daily deletion cap counts. Platform
-- level and without personal data; it outlives the workspace on purpose.
CREATE TABLE retention_deletions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deleted_workspace_id uuid NOT NULL,
    slug text NOT NULL,
    deleted_at timestamptz NOT NULL
);

CREATE INDEX retention_deletions_deleted_at_idx ON retention_deletions(deleted_at);

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    -- Same isolation as workspace_comp_plans.
    ALTER TABLE workspace_retention_notices ENABLE ROW LEVEL SECURITY;
    ALTER TABLE workspace_retention_notices FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON workspace_retention_notices FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON workspace_retention_notices FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON workspace_retention_notices TO %I', app_role);
    -- Same isolation as cloud_signups: only the privileged retention transaction reads it.
    ALTER TABLE retention_deletions ENABLE ROW LEVEL SECURITY;
    ALTER TABLE retention_deletions FORCE ROW LEVEL SECURITY;
    EXECUTE format('REVOKE ALL ON retention_deletions FROM %I', app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON retention_deletions FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
END
$$;
