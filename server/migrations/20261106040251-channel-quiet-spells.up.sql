-- A quiet mailbox (docs/user/email-channel.md "When no email arrives"): an email channel that received
-- mail on at least 5 of 7 days and then none for 24 hours. A spell is keyed by the time of the last email
-- before the silence, so the next email starts a new state without a write: the channel is quiet only
-- while that time is still its last email. Each admin is emailed once per spell; a claim is written
-- before the email goes out, so two servers never send it twice, and a send that failed for a passing
-- reason deletes its claim so the next pass tries again. The tables hold agent ids, not addresses.
CREATE TABLE channel_quiet_spells (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    since timestamptz NOT NULL,
    detected_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, channel_id, since),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id)
);

CREATE TABLE channel_quiet_emails (
    workspace_id uuid NOT NULL,
    channel_id uuid NOT NULL,
    since timestamptz NOT NULL,
    agent_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('claimed', 'sent', 'rejected')),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, channel_id, since, agent_id),
    FOREIGN KEY (workspace_id, channel_id, since) REFERENCES channel_quiet_spells(workspace_id, channel_id, since) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE
);

DO $$
DECLARE
    app_role text;
    quiet_table text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    -- Same isolation as workspace_trial_reminders.
    FOREACH quiet_table IN ARRAY ARRAY['channel_quiet_spells', 'channel_quiet_emails'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', quiet_table);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', quiet_table);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %2$I FOR ALL TO %1$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role, quiet_table);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %2$I FOR ALL TO %1$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user, quiet_table);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', quiet_table, app_role);
    END LOOP;
END
$$;
