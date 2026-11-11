-- Zalo Official Account channels (docs/user/zalo.md): a channel is one OA (channels.external_account is
-- its oa_id, unique on the server, so the app's one webhook routes by it). What is not a secret is here:
-- the Zalo app it is reached through, the team's Zalo user ids (their messages in a group are the team's,
-- never a customer's request) and the organization each group stands for. Its secrets (the app's and
-- the OA's keys, the access and refresh tokens) are channel_credentials.
INSERT INTO channel_types (type, connectable) VALUES ('zalo', true);

CREATE TABLE zalo_channels (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    app_id text NOT NULL CHECK (app_id ~ '^[0-9]{1,40}$'),
    staff_user_ids text[] NOT NULL DEFAULT '{}' CHECK (cardinality(staff_user_ids) <= 200),
    -- Group id to the organization its members are customers of.
    groups jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(groups) = 'object' AND octet_length(groups::text) <= 65536),
    -- The last delivery whose signature did not match, so the channel says why events are lost.
    signature_failed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, channel_id),
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
    ALTER TABLE zalo_channels ENABLE ROW LEVEL SECURITY;
    ALTER TABLE zalo_channels FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON zalo_channels FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON zalo_channels FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON zalo_channels TO %I', app_role);
END
$$;
