-- A trial offer is a one-time link an operator sends to a team whose trial ran out: an admin of that
-- workspace who accepts it gets 14 more days from that moment. Only a hash of the link is stored, as for
-- invites. An offer is open until it is accepted, revoked or past expires_at.
CREATE TABLE trial_offers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    accepted_by_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL,
    accepted_by text,
    revoked_at timestamptz,
    revoked_by text,
    CHECK (expires_at > created_at),
    CHECK (accepted_at IS NULL OR revoked_at IS NULL)
);

CREATE INDEX trial_offers_workspace_idx ON trial_offers(workspace_id, created_at);

-- Same isolation as workspace_trial_changes.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE trial_offers ENABLE ROW LEVEL SECURITY;
    ALTER TABLE trial_offers FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON trial_offers FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON trial_offers FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON trial_offers TO %I', app_role);
END
$$;
