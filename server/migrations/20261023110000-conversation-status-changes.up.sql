-- Every change of a conversation's status, for the results page (issue #100, docs/internals/results.md):
-- when a conversation was resolved, and whether it was reopened within 7 days. Kept by a trigger, not
-- by the writers, so every way a status changes is recorded (a person, a bulk update, a customer reply
-- that reopens, an import re-run, and an older image after a rollback). A conversation is created
-- open by every Colliedesk writer, so an insert is not a change; an import creates conversations with
-- their source status, which is history from before the move, not a change.
--
-- source says who changed it: 'colliedesk' (the default) or 'import', which an importer sets with
-- set_config('app.status_source','import',true) for its own transactions. The results page reads only
-- 'colliedesk'; import rows keep the history consistent with the current status and leave in the export.
-- actor_agent_id is the person or token owner, when the writer knows one (app.actor_agent_id).
CREATE TABLE conversation_status_changes (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL,
    from_status text NOT NULL,
    to_status text NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT now(),
    actor_agent_id uuid,
    source text NOT NULL DEFAULT 'colliedesk' CHECK (source IN ('colliedesk', 'import')),
    PRIMARY KEY (workspace_id, id),
    CHECK (from_status <> to_status),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, actor_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (actor_agent_id)
);

-- One conversation's history in order (results, export), and a workspace's changes since a time.
CREATE INDEX conversation_status_changes_conversation ON conversation_status_changes(workspace_id, conversation_id, changed_at);
CREATE INDEX conversation_status_changes_changed ON conversation_status_changes(workspace_id, changed_at);

-- From when a workspace's status history is complete: the upgrade for existing workspaces, the
-- creation for new ones. A resolution or reopen before it was never recorded, so the results page does
-- not show a number that would need it.
ALTER TABLE workspaces ADD COLUMN status_history_since timestamptz NOT NULL DEFAULT now();

CREATE FUNCTION conversation_status_changes_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO conversation_status_changes(workspace_id, conversation_id, from_status, to_status, actor_agent_id, source)
        VALUES (NEW.workspace_id, NEW.id, OLD.status, NEW.status,
                NULLIF(current_setting('app.actor_agent_id', true), '')::uuid,
                COALESCE(NULLIF(current_setting('app.status_source', true), ''), 'colliedesk'));
    RETURN NULL;
END
$$;
CREATE TRIGGER conversation_status_changes_record AFTER UPDATE OF status ON conversations
    FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION conversation_status_changes_record();

-- Same isolation as conversation_presence.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE conversation_status_changes ENABLE ROW LEVEL SECURITY;
    ALTER TABLE conversation_status_changes FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON conversation_status_changes FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON conversation_status_changes FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON conversation_status_changes TO %I', app_role);
END
$$;
