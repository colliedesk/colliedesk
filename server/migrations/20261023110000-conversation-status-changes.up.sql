-- Every change of a conversation's status, for the results page (issue #100,
-- docs/internals/results.md): when a conversation was resolved, and whether it was reopened within
-- 7 days. Kept by a trigger, not by the writers, so every way a status changes is recorded (a
-- person, a bulk update, a customer reply that reopens, an import re-run, and an older image after
-- a rollback). A conversation is created open by every Colliedesk writer, so an insert is not a
-- change; an import creates conversations with their source status, which is history from before
-- the move, not a change.
--
-- source says who changed it: 'colliedesk' (the default) or 'import', which an importer sets
-- with set_config('app.status_source','import',true) for its own transactions. The results
-- page reads only 'colliedesk'; import rows keep the history consistent with the current
-- status and leave in the export. actor_agent_id is the person or token owner, when the writer
-- knows one (app.actor_agent_id).
CREATE TABLE conversation_status_changes (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL,
    from_status text NOT NULL,
    to_status text NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    actor_agent_id uuid,
    source text NOT NULL DEFAULT 'colliedesk' CHECK (source IN ('colliedesk', 'import')),
    PRIMARY KEY (workspace_id, id),
    CHECK (from_status <> to_status),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id)
        ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, actor_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (actor_agent_id)
);

-- One conversation's history in order (results, export), and a workspace's changes since a time.
CREATE INDEX conversation_status_changes_conversation
    ON conversation_status_changes(workspace_id, conversation_id, changed_at);
CREATE INDEX conversation_status_changes_changed
    ON conversation_status_changes(workspace_id, changed_at);

-- From when a workspace's status history is complete: the upgrade for existing workspaces, the
-- creation for new ones. A resolution or reopen before it was never recorded, so the results page
-- does not show a number that would need it.
ALTER TABLE workspaces ADD COLUMN status_history_since timestamptz NOT NULL DEFAULT now();

CREATE FUNCTION conversation_status_changes_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- clock_timestamp(), not now(): now() is when the transaction began, so a change committed after
    -- another (a customer reply whose transaction started first, or several changes in one transaction)
    -- would sort before it.
    INSERT INTO conversation_status_changes(workspace_id, conversation_id, from_status, to_status, changed_at, actor_agent_id, source)
        VALUES (NEW.workspace_id, NEW.id, OLD.status, NEW.status, clock_timestamp(),
                NULLIF(current_setting('app.actor_agent_id', true), '')::uuid,
                COALESCE(NULLIF(current_setting('app.status_source', true), ''), 'colliedesk'));
    RETURN NULL;
END
$$;
CREATE TRIGGER conversation_status_changes_record AFTER UPDATE OF status ON conversations
    FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION conversation_status_changes_record();

-- Same isolation as conversation_presence.
SELECT secure_workspace_table('conversation_status_changes');
