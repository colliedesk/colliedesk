DROP TRIGGER conversations_status_notice_clear ON conversations;
DROP FUNCTION conversations_status_notice_clear();
ALTER TABLE conversations DROP COLUMN status_notice_due;
ALTER TABLE channels DROP COLUMN auto_close_since;
ALTER TABLE agent_requests DROP COLUMN notices;

CREATE OR REPLACE FUNCTION conversation_status_changes_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO conversation_status_changes(workspace_id, conversation_id, from_status, to_status, changed_at, actor_agent_id, source)
        VALUES (NEW.workspace_id, NEW.id, OLD.status, NEW.status, clock_timestamp(),
                NULLIF(current_setting('app.actor_agent_id', true), '')::uuid,
                COALESCE(NULLIF(current_setting('app.status_source', true), ''), 'colliedesk'));
    RETURN NULL;
END
$$;

DROP TRIGGER conversation_status_changes_cause ON conversation_status_changes;
DROP FUNCTION conversation_status_changes_cause();
ALTER TABLE conversation_status_changes DROP COLUMN actor_token_id, DROP COLUMN cause, DROP COLUMN quiet;
