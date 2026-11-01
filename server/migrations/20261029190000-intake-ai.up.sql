-- AI decides intake cards (docs/internals/channels.md "Intake"): one task at a time decides a card, and a
-- person's decision says which suggestion of AI it followed or corrected (the labels of intake's eval).

-- A person who decided as AI suggested names that suggestion; one who decided otherwise names it in
-- corrects_decision_id.
ALTER TABLE intake_decisions ADD COLUMN follows_decision_id uuid,
    ADD FOREIGN KEY (workspace_id, follows_decision_id) REFERENCES intake_decisions(workspace_id, id) ON DELETE SET NULL (follows_decision_id),
    ADD CHECK (follows_decision_id IS NULL OR corrects_decision_id IS NULL);

-- Why AI did not act on its own, also: it hesitated between two or more conversations of the customer
-- (attach_requires_single_candidate), or the batch had nothing it could read (no text, no picture sent).
ALTER TABLE intake_decisions DROP CONSTRAINT intake_decisions_not_applied_reason_check,
    ADD CONSTRAINT intake_decisions_not_applied_reason_check CHECK (not_applied_reason IN ('level_suggest', 'below_threshold', 'too_old', 'credential',
        'person_first', 'target_changed', 'lane_blocked', 'gate_closed', 'limit', 'error', 'several_candidates', 'no_content'));

-- One AI task decides a card at a time: the hold, the lane's turn and a person's requeue may all ask.
CREATE UNIQUE INDEX ai_tasks_intake_gathering ON ai_tasks(workspace_id, gathering_id) WHERE kind = 'intake' AND state IN ('queued', 'running');

-- What intake said a customer's message added to a conversation was: an acknowledgement (a thank-you, an
-- "ok") joined to a resolved conversation keeps it resolved and asks for no automatic draft.
ALTER TABLE messages ADD COLUMN intake_reply_kind text CHECK (intake_reply_kind IN ('substantive', 'acknowledgement'));

-- A customer's message queues an automatic draft where drafts are suggested; a thank-you AI kept in a
-- resolved conversation (acknowledgement) asks for no answer.
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
