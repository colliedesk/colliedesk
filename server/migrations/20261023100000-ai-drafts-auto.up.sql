-- Reply drafts written when a customer writes (#89): an admin lets a workspace's AI draft a reply to
-- every new customer message by itself. The draft waits on the conversation for the team; the AI
-- never sends. Off unless an admin turns it on (ai_slots.automatic on the draft slot).
ALTER TABLE ai_automatic_limits ADD COLUMN draft_per_day integer NOT NULL DEFAULT 100 CHECK (draft_per_day BETWEEN 0 AND 10000);

ALTER TABLE ai_drafts
    -- Written by itself for a customer's message, not asked for by a person: the whole team sees it.
    ADD COLUMN automatic boolean NOT NULL DEFAULT false,
    -- The newest customer message it answers.
    ADD COLUMN answers_message_id uuid,
    -- The model's view that the message needs a reply ("thanks" does not); null for a person's draft.
    ADD COLUMN reply_needed boolean,
    ADD FOREIGN KEY (workspace_id, answers_message_id) REFERENCES messages(workspace_id, id) ON DELETE SET NULL (answers_message_id);
CREATE INDEX ai_drafts_automatic_open ON ai_drafts(workspace_id, conversation_id) WHERE automatic AND outcome IS NULL;
-- At most one automatic draft waits to be written per conversation: messages that come close together
-- are answered by one draft.
CREATE UNIQUE INDEX ai_tasks_automatic_draft_queued ON ai_tasks(workspace_id, conversation_id) WHERE kind = 'draft' AND automatic AND state = 'queued';

-- A customer's message queues an automatic draft when the workspace lets drafts write themselves. It
-- runs a minute later, and a message that comes before it runs pushes it another minute (five at most
-- from the first), so a burst gets one draft. A ready automatic draft nobody decided on is replaced.
-- An imported message (origin set) queues nothing, and neither does mail that failed its sender check.
-- A reply from the team stops the automatic draft still waiting.
CREATE FUNCTION ai_draft_on_customer_message() RETURNS trigger LANGUAGE plpgsql AS $$
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
    IF NEW.origin IS NOT NULL OR NEW.email_meta->>'authentication' = 'fail' THEN
        RETURN NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM ai_slots WHERE workspace_id = NEW.workspace_id AND slot = 'draft' AND enabled AND automatic) THEN
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
    UPDATE ai_drafts SET outcome = 'superseded', decided_at = now()
        WHERE workspace_id = NEW.workspace_id AND conversation_id = NEW.conversation_id AND automatic AND status = 'ready' AND outcome IS NULL;
    INSERT INTO ai_tasks(workspace_id, kind, conversation_id, automatic, state, next_run_at)
        VALUES (NEW.workspace_id, 'draft', NEW.conversation_id, true, 'queued', now() + interval '60 seconds') RETURNING id INTO task;
    INSERT INTO ai_drafts(workspace_id, conversation_id, task_id, status, automatic, answers_message_id)
        VALUES (NEW.workspace_id, NEW.conversation_id, task, 'pending', true, NEW.id);
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_draft_on_customer_message AFTER INSERT ON messages
    FOR EACH ROW WHEN ((NEW.kind = 'incoming' AND NEW.origin IS NULL) OR NEW.kind = 'outgoing')
    EXECUTE FUNCTION ai_draft_on_customer_message();
