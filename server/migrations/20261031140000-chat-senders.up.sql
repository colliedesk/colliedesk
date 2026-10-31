-- The people who wrote in a channel's chat places (GET /api/v1/channels/{id}/senders), one row each, kept
-- as items are written so listing them reads a few hundred rows whatever the channel holds. The channel
-- itself (its own account, external_account when the item was written) is never one of them: an item it
-- sent is left out when it is written, so moving the channel to another account later lets none back in.
CREATE TABLE channel_senders (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    sender_kind text NOT NULL CHECK (char_length(sender_kind) BETWEEN 1 AND 40),
    sender_value text NOT NULL CHECK (char_length(sender_value) BETWEEN 1 AND 200),
    name text CHECK (char_length(name) BETWEEN 1 AND 200),
    role text NOT NULL CHECK (role IN ('customer', 'staff')),
    last_at timestamptz NOT NULL,
    thread_id uuid NOT NULL,
    PRIMARY KEY (workspace_id, channel_id, sender_kind, sender_value),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    FOREIGN KEY (workspace_id, thread_id) REFERENCES channel_threads(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX channel_senders_recent ON channel_senders(workspace_id, channel_id, last_at DESC, sender_value);

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE channel_senders ENABLE ROW LEVEL SECURITY;
    ALTER TABLE channel_senders FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON channel_senders FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON channel_senders FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON channel_senders TO %I', app_role);
    -- What was written before: only the channel's current account can be told apart from a person now.
    PERFORM set_config('app.privileged', 'on', true);
    INSERT INTO channel_senders (workspace_id, channel_id, sender_kind, sender_value, name, role, last_at, thread_id)
    SELECT DISTINCT ON (i.workspace_id, i.channel_id, i.sender_kind, i.sender_value)
        i.workspace_id, i.channel_id, i.sender_kind, i.sender_value,
        (SELECT n.sender_name FROM channel_thread_items n WHERE n.workspace_id = i.workspace_id AND n.channel_id = i.channel_id
            AND n.sender_kind = i.sender_kind AND n.sender_value = i.sender_value AND n.sender_name IS NOT NULL ORDER BY n.occurred_at DESC, n.id DESC LIMIT 1),
        i.role, i.occurred_at, i.thread_id
    FROM channel_thread_items i JOIN channels c ON c.workspace_id = i.workspace_id AND c.id = i.channel_id
    WHERE i.role IN ('customer', 'staff') AND i.sender_kind IS NOT NULL AND i.sender_value IS NOT NULL
        AND i.sender_value IS DISTINCT FROM c.external_account
    ORDER BY i.workspace_id, i.channel_id, i.sender_kind, i.sender_value, i.occurred_at DESC, i.id DESC;
    PERFORM set_config('app.privileged', 'off', true);
END
$$;
