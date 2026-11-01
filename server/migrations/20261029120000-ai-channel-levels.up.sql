-- AI per channel (docs/internals/ai.md "Levels per channel"). A workspace still decides which features
-- may send ticket data to a provider (ai_slots.enabled, its consent, provider, model, effort); a
-- channel, and a scope of it (a chat's one-to-one messages and its groups), decides only how much of
-- that work the AI does by itself: off, suggest or auto. One SQL function, ai_level(), answers it for
-- the triggers and the Go code alike. The levels a workspace had keep their behaviour:
--   triage enabled -> suggest (it already wrote a suggestion for every new conversation),
--   triage enabled and automatic -> auto, draft enabled -> off (a draft only when someone asks),
--   draft enabled and automatic -> suggest (a draft written when a customer writes).
-- Drafts never reach auto (AI never sends), and intake, which only chat channels have
-- (Definition.IntakeScopes), reaches auto only on a channel's scope, never as a workspace default.

-- Intake is another feature, next to the help center's chat (20261029090000-ai-chat): deciding whether a
-- chat message opens a conversation, joins one, or is not a request.
ALTER TABLE ai_slots DROP CONSTRAINT ai_slots_slot_check,
    ADD CONSTRAINT ai_slots_slot_check CHECK (slot IN ('triage', 'draft', 'chat', 'intake'));
ALTER TABLE ai_settings_events DROP CONSTRAINT ai_settings_events_slot_check,
    ADD CONSTRAINT ai_settings_events_slot_check CHECK (slot IN ('triage', 'draft', 'chat', 'intake'));
ALTER TABLE ai_usage DROP CONSTRAINT ai_usage_feature_check,
    ADD CONSTRAINT ai_usage_feature_check CHECK (feature IN ('check', 'triage', 'draft', 'chat', 'intake'));
ALTER TABLE ai_tasks DROP CONSTRAINT ai_tasks_kind_check,
    ADD CONSTRAINT ai_tasks_kind_check CHECK (kind IN ('triage', 'draft', 'intake'));

-- What a feature does by itself where no channel says otherwise, and on conversations of no channel.
ALTER TABLE ai_slots ADD COLUMN default_level text NOT NULL DEFAULT 'off' CHECK (default_level IN ('off', 'suggest', 'auto'));
UPDATE ai_slots SET default_level = CASE
    WHEN slot = 'triage' AND enabled AND automatic THEN 'auto'
    WHEN slot = 'triage' AND enabled THEN 'suggest'
    WHEN slot = 'draft' AND enabled AND automatic THEN 'suggest'
    ELSE 'off' END;
-- The CHECKs on automatic go with it.
ALTER TABLE ai_slots DROP COLUMN automatic,
    ADD CONSTRAINT ai_slots_level_needs_consent CHECK (default_level = 'off' OR enabled),
    -- A draft is never sent by AI; intake runs by itself only where a channel's scope says so.
    ADD CONSTRAINT ai_slots_level_auto CHECK (default_level <> 'auto' OR slot = 'triage'),
    ADD CONSTRAINT ai_slots_triage_auto_parts CHECK (NOT (slot = 'triage' AND default_level = 'auto') OR cardinality(auto_parts) > 0);

-- A channel's level for one feature, on the whole channel (scope '') or on one of its type's scopes.
-- No row: the channel's row for scope '', then the workspace's default_level. Intake rows exist only
-- for a scope the type declares in Definition.IntakeScopes (the store checks; the database does not
-- know the types' definitions).
CREATE TABLE channel_ai_levels (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    scope text NOT NULL DEFAULT '' CHECK (scope ~ '^[a-z_]{0,40}$'),
    slot text NOT NULL CHECK (slot IN ('triage', 'draft', 'intake')),
    level text NOT NULL CHECK (level IN ('off', 'suggest', 'auto')),
    -- Intake only: thresholds, context sources, the channel's instructions (IntakeOptions in the contract).
    options jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(options) = 'object' AND octet_length(options::text) <= 16384),
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, channel_id, scope, slot),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, updated_by) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (updated_by),
    CHECK (NOT (slot = 'draft' AND level = 'auto')),
    CHECK (slot = 'intake' OR options = '{}'),
    CHECK (slot <> 'intake' OR scope <> '')
);

-- The context an intake decision may send besides the conversation's text (which the feature's own
-- consent covers): images, the contact's profile, an integration's data. An admin agrees per class.
CREATE TABLE ai_source_consents (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    source_class text NOT NULL CHECK (source_class ~ '^(images|contact_profile|external:[a-z0-9_]{1,40})$'),
    consented_by uuid,
    consented_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, source_class),
    FOREIGN KEY (workspace_id, consented_by) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (consented_by)
);

ALTER TABLE ai_settings_events DROP CONSTRAINT ai_settings_events_kind_check,
    ADD CONSTRAINT ai_settings_events_kind_check CHECK (kind IN ('provider_added', 'provider_changed', 'provider_key_changed',
        'provider_removed', 'slot_changed', 'automatic_limits_changed', 'chat_deleted', 'channel_level_changed', 'source_consent_changed'));
-- A channel's own history shows its AI levels changing too.
ALTER TABLE channel_events DROP CONSTRAINT channel_events_kind_check,
    ADD CONSTRAINT channel_events_kind_check CHECK (kind IN ('added', 'renamed', 'enabled', 'disabled', 'config_changed', 'credential_changed',
        'removed', 'held_replayed', 'held_discarded', 'default_changed', 'ai_changed'));

-- The scope a channel gave a conversation's first message (Inbound.Scope), so its level can be read.
ALTER TABLE conversations ADD COLUMN channel_scope text CHECK (channel_scope ~ '^[a-z_]{1,40}$');

-- What a feature does by itself on a channel's scope: off when the workspace has not turned the
-- feature on, else the scope's row, the channel's row, then the workspace's default. A conversation of
-- no channel passes NULLs and gets the default. Parameters are named p_*: in a SQL function a name a
-- column also has means the column, and l.scope = COALESCE(scope, '') would always be true.
CREATE FUNCTION ai_level(p_workspace uuid, p_channel uuid, p_scope text, p_slot text) RETURNS text
LANGUAGE sql STABLE AS $$
    SELECT CASE WHEN s.enabled IS NOT TRUE THEN 'off' ELSE COALESCE(
        (SELECT l.level FROM channel_ai_levels l WHERE l.workspace_id = p_workspace AND l.channel_id = p_channel
            AND l.scope = COALESCE(p_scope, '') AND l.slot = p_slot),
        (SELECT l.level FROM channel_ai_levels l WHERE l.workspace_id = p_workspace AND l.channel_id = p_channel
            AND l.scope = '' AND l.slot = p_slot),
        s.default_level) END
    FROM (SELECT 1) one LEFT JOIN ai_slots s ON s.workspace_id = p_workspace AND s.slot = p_slot
$$;

-- The first customer message queues a triage suggestion where triage suggests or applies by itself.
CREATE OR REPLACE FUNCTION ai_triage_on_first_message() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    task uuid;
    conversation record;
BEGIN
    IF NEW.kind <> 'incoming' OR NEW.origin IS NOT NULL THEN
        RETURN NULL;
    END IF;
    SELECT c.channel_id, c.channel_scope, c.priority, c.assignee_id INTO conversation
        FROM conversations c WHERE c.workspace_id = NEW.workspace_id AND c.id = NEW.conversation_id;
    IF ai_level(NEW.workspace_id, conversation.channel_id, conversation.channel_scope, 'triage') NOT IN ('suggest', 'auto') THEN
        RETURN NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM messages WHERE workspace_id = NEW.workspace_id AND conversation_id = NEW.conversation_id
            AND kind = 'incoming' AND id <> NEW.id) THEN
        RETURN NULL;
    END IF;
    INSERT INTO ai_tasks(workspace_id, kind, conversation_id, automatic, state)
        VALUES (NEW.workspace_id, 'triage', NEW.conversation_id, true, 'queued') RETURNING id INTO task;
    INSERT INTO ai_triage_suggestions(workspace_id, conversation_id, task_id, automatic, status, base)
        VALUES (NEW.workspace_id, NEW.conversation_id, task, true, 'pending',
            jsonb_build_object('priority', conversation.priority, 'assignee_agent_id', conversation.assignee_id));
    RETURN NULL;
END
$$;

-- A customer's message queues an automatic draft where drafts are suggested.
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

DO $$
DECLARE
    app_role text;
    name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH name IN ARRAY ARRAY['channel_ai_levels', 'ai_source_consents'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$I FOR ALL TO %2$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, name, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, name, current_user);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', name, app_role);
    END LOOP;
END
$$;
