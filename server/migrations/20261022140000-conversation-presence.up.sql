-- Who has a conversation open, and whether their reply box holds a reply not sent yet
-- (docs/internals/presence.md). One row per browser tab (client_id) of one session, or per agent
-- token; a row not renewed before expires_at is gone. Rows are minutes old at most and say nothing
-- that is not true again a heartbeat later, so the table is UNLOGGED: no WAL, emptied by a crash,
-- refilled within a heartbeat. Older images ignore it.
CREATE UNLOGGED TABLE conversation_presence (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    session_id uuid NOT NULL,
    client_id text NOT NULL CHECK (client_id ~ '^[A-Za-z0-9_-]{8,64}$'),
    agent_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    writing_since timestamptz,
    edited_at timestamptz,
    seen_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, session_id, client_id),
    CHECK ((writing_since IS NULL) = (edited_at IS NULL)),
    FOREIGN KEY (workspace_id, session_id) REFERENCES sessions(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE
);

-- Reading who is on one conversation, and removing the expired rows of a workspace.
CREATE INDEX conversation_presence_conversation_idx ON conversation_presence(workspace_id, conversation_id);
CREATE INDEX conversation_presence_expires_idx ON conversation_presence(workspace_id, expires_at);
CREATE INDEX conversation_presence_agent_idx ON conversation_presence(workspace_id, agent_id, seen_at);

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
    ALTER TABLE conversation_presence ENABLE ROW LEVEL SECURITY;
    ALTER TABLE conversation_presence FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON conversation_presence FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON conversation_presence FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON conversation_presence TO %I', app_role);
END
$$;
