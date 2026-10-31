CREATE OR REPLACE FUNCTION conversations_refresh_last_public(workspace_ids uuid[], conversation_ids uuid[]) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM conversations c
        WHERE (c.workspace_id, c.id) IN (SELECT * FROM unnest(workspace_ids, conversation_ids))
        ORDER BY c.workspace_id, c.id FOR NO KEY UPDATE;
    UPDATE conversations c SET last_public_kind = l.kind, last_public_at = l.created_at
        FROM (SELECT DISTINCT t.workspace_id, t.conversation_id
              FROM unnest(workspace_ids, conversation_ids) AS t(workspace_id, conversation_id)) d
        LEFT JOIN LATERAL (
            SELECT m.kind, m.created_at FROM messages m
            WHERE m.workspace_id = d.workspace_id AND m.conversation_id = d.conversation_id AND m.kind <> 'note' AND m.detached_at IS NULL
            ORDER BY m.created_at DESC, m.id DESC LIMIT 1
        ) l ON true
        WHERE c.workspace_id = d.workspace_id AND c.id = d.conversation_id
            AND (c.last_public_kind IS DISTINCT FROM l.kind OR c.last_public_at IS DISTINCT FROM l.created_at);
END
$$;
CREATE OR REPLACE FUNCTION ai_draft_on_customer_message() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    waiting uuid;
    task uuid;
BEGIN
    IF NEW.kind = 'outgoing' THEN
        WITH stopped AS (
            UPDATE ai_tasks SET state = 'cancelled', error_code = 'answered', finished_at = now()
                WHERE workspace_id = NEW.workspace_id AND conversation_id = NEW.conversation_id AND kind = 'draft' AND automatic AND state = 'queued'
                RETURNING id)
        UPDATE ai_drafts SET status = 'cancelled' WHERE workspace_id = NEW.workspace_id AND task_id IN (SELECT id FROM stopped) AND status = 'pending';
        RETURN NULL;
    END IF;
    IF NEW.origin IS NOT NULL OR NEW.email_meta->>'authentication' = 'fail' OR NEW.intake_reply_kind = 'acknowledgement' THEN
        RETURN NULL;
    END IF;
    IF (SELECT ai_level(NEW.workspace_id, c.channel_id, c.channel_scope, 'draft') FROM conversations c
            WHERE c.workspace_id = NEW.workspace_id AND c.id = NEW.conversation_id) IS DISTINCT FROM 'suggest' THEN
        RETURN NULL;
    END IF;
    SELECT id INTO waiting FROM ai_tasks
        WHERE workspace_id = NEW.workspace_id AND conversation_id = NEW.conversation_id AND kind = 'draft' AND automatic AND state = 'queued'
        FOR UPDATE;
    IF waiting IS NOT NULL THEN
        UPDATE ai_tasks SET next_run_at = LEAST(now() + interval '60 seconds', created_at + interval '5 minutes')
            WHERE workspace_id = NEW.workspace_id AND id = waiting;
        UPDATE ai_drafts SET answers_message_id = NEW.id WHERE workspace_id = NEW.workspace_id AND task_id = waiting;
        RETURN NULL;
    END IF;
    -- The ready draft stays usable until the new one is written: a new draft that is never written
    -- (a limit, a failure) leaves the team the one it had.
    INSERT INTO ai_tasks(workspace_id, kind, conversation_id, automatic, state, next_run_at)
        VALUES (NEW.workspace_id, 'draft', NEW.conversation_id, true, 'queued', now() + interval '60 seconds') RETURNING id INTO task;
    INSERT INTO ai_drafts(workspace_id, conversation_id, task_id, status, automatic, answers_message_id)
        VALUES (NEW.workspace_id, NEW.conversation_id, task, 'pending', true, NEW.id);
    RETURN NULL;
END
$$;
ALTER TABLE messages DROP COLUMN notice_kind;
