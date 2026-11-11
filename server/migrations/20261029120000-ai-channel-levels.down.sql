-- For tests only: a workspace that set a level on a channel, a scope, the intake feature or a source
-- consent cannot be put back without losing it, so the down migration refuses it.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM channel_ai_levels) OR EXISTS (SELECT 1 FROM ai_source_consents)
            OR EXISTS (SELECT 1 FROM ai_slots WHERE slot = 'intake')
            OR EXISTS (SELECT 1 FROM ai_settings_events WHERE slot = 'intake' OR kind IN ('channel_level_changed', 'source_consent_changed'))
            OR EXISTS (SELECT 1 FROM ai_usage WHERE feature = 'intake') OR EXISTS (SELECT 1 FROM ai_tasks WHERE kind = 'intake')
            OR EXISTS (SELECT 1 FROM channel_events WHERE kind = 'ai_changed') THEN
        RAISE EXCEPTION 'AI levels per channel or the intake feature are in use; restore the backup taken before the upgrade instead';
    END IF;
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
    INSERT INTO ai_tasks(workspace_id, kind, conversation_id, automatic, state, next_run_at)
        VALUES (NEW.workspace_id, 'draft', NEW.conversation_id, true, 'queued', now() + interval '60 seconds') RETURNING id INTO task;
    INSERT INTO ai_drafts(workspace_id, conversation_id, task_id, status, automatic, answers_message_id)
        VALUES (NEW.workspace_id, NEW.conversation_id, task, 'pending', true, NEW.id);
    RETURN NULL;
END
$$;

CREATE OR REPLACE FUNCTION ai_triage_on_first_message() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    task uuid;
BEGIN
    IF NEW.kind <> 'incoming' OR NEW.origin IS NOT NULL THEN
        RETURN NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM ai_slots WHERE workspace_id = NEW.workspace_id AND slot = 'triage' AND enabled) THEN
        RETURN NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM messages WHERE workspace_id = NEW.workspace_id AND conversation_id = NEW.conversation_id
            AND kind = 'incoming' AND id <> NEW.id) THEN
        RETURN NULL;
    END IF;
    INSERT INTO ai_tasks(workspace_id, kind, conversation_id, automatic, state)
        VALUES (NEW.workspace_id, 'triage', NEW.conversation_id, true, 'queued') RETURNING id INTO task;
    INSERT INTO ai_triage_suggestions(workspace_id, conversation_id, task_id, automatic, status, base)
        SELECT NEW.workspace_id, NEW.conversation_id, task, true, 'pending',
            jsonb_build_object('priority', c.priority, 'assignee_agent_id', c.assignee_id)
        FROM conversations c WHERE c.workspace_id = NEW.workspace_id AND c.id = NEW.conversation_id;
    RETURN NULL;
END
$$;
DROP FUNCTION ai_level(uuid, uuid, text, text);
ALTER TABLE conversations DROP COLUMN channel_scope;
ALTER TABLE channel_events DROP CONSTRAINT channel_events_kind_check,
    ADD CONSTRAINT channel_events_kind_check CHECK (kind IN ('added', 'renamed', 'enabled', 'disabled', 'config_changed', 'credential_changed',
        'removed', 'held_replayed', 'held_discarded', 'default_changed'));
ALTER TABLE ai_settings_events DROP CONSTRAINT ai_settings_events_kind_check,
    ADD CONSTRAINT ai_settings_events_kind_check CHECK (kind IN ('provider_added', 'provider_changed', 'provider_key_changed',
        'provider_removed', 'slot_changed', 'automatic_limits_changed', 'chat_deleted'));
DROP TABLE ai_source_consents;
DROP TABLE channel_ai_levels;
ALTER TABLE ai_slots ADD COLUMN automatic boolean NOT NULL DEFAULT false;
-- Triage that suggests by itself is triage that is on; draft that suggests by itself is automatic.
UPDATE ai_slots SET automatic = enabled AND ((slot = 'triage' AND default_level = 'auto') OR (slot = 'draft' AND default_level = 'suggest'));
ALTER TABLE ai_slots DROP CONSTRAINT ai_slots_triage_auto_parts,
    DROP CONSTRAINT ai_slots_level_auto,
    DROP CONSTRAINT ai_slots_level_needs_consent,
    DROP COLUMN default_level,
    ADD CHECK (NOT automatic OR enabled),
    ADD CHECK (NOT (automatic AND slot = 'triage') OR cardinality(auto_parts) > 0);
ALTER TABLE ai_tasks DROP CONSTRAINT ai_tasks_kind_check,
    ADD CONSTRAINT ai_tasks_kind_check CHECK (kind IN ('triage', 'draft'));
ALTER TABLE ai_usage DROP CONSTRAINT ai_usage_feature_check,
    ADD CONSTRAINT ai_usage_feature_check CHECK (feature IN ('check', 'triage', 'draft', 'chat'));
ALTER TABLE ai_settings_events DROP CONSTRAINT ai_settings_events_slot_check,
    ADD CONSTRAINT ai_settings_events_slot_check CHECK (slot IN ('triage', 'draft', 'chat'));
ALTER TABLE ai_slots DROP CONSTRAINT ai_slots_slot_check,
    ADD CONSTRAINT ai_slots_slot_check CHECK (slot IN ('triage', 'draft', 'chat'));
