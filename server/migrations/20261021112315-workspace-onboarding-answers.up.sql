-- What a cloud sign-up said about the team: its first goal, the person's role and where they heard of
-- Colliedesk. One row per workspace created by cloud sign-up; an answer not given is NULL. The goal
-- orders the steps the empty inbox offers first. Older images ignore this table.
CREATE TABLE workspace_onboarding (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    goal text CHECK (goal IN ('leave_helpdesk', 'shared_inbox', 'lower_cost', 'ai_agents', 'other')),
    role text CHECK (role IN ('founder', 'support_lead', 'support_agent', 'developer', 'other')),
    source text CHECK (source IN ('x', 'email', 'search', 'reddit', 'github', 'referral', 'other')),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Same isolation as saved_views.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE workspace_onboarding ENABLE ROW LEVEL SECURITY;
    ALTER TABLE workspace_onboarding FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON workspace_onboarding FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON workspace_onboarding FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON workspace_onboarding TO %I', app_role);
END
$$;
