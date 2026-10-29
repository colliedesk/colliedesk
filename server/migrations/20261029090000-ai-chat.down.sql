-- The schema before has no room for AI chat. Refuse to roll back while a workspace holds a chat, a
-- help chat channel or a chat usage row, naming the workspaces, rather than drop transcripts and what
-- they cost (the way back for a server is the backup).
DO $$
DECLARE
    holders text;
BEGIN
    SELECT string_agg(w.slug, ', ' ORDER BY w.slug) INTO holders FROM workspaces w
        WHERE EXISTS (SELECT 1 FROM ai_chat_sessions s WHERE s.workspace_id = w.id)
            OR EXISTS (SELECT 1 FROM channels c WHERE c.workspace_id = w.id AND c.type = 'help_chat')
            OR EXISTS (SELECT 1 FROM ai_usage u WHERE u.workspace_id = w.id AND u.feature = 'chat')
            OR EXISTS (SELECT 1 FROM ai_slots s WHERE s.workspace_id = w.id AND s.slot = 'chat');
    IF holders IS NOT NULL THEN
        RAISE EXCEPTION 'AI chat holds chats, channels or settings in workspaces %; roll back from a backup instead', holders;
    END IF;
END
$$;

DROP TABLE ai_chat_transfers;
DROP TABLE ai_chat_kb_snapshots;
DROP TABLE ai_chat_feedback;
DROP TABLE ai_chat_sources;
DROP TABLE ai_chat_turns;
DROP TABLE ai_chat_sessions;
DROP TABLE help_chat_channels;
DELETE FROM ai_settings_events WHERE slot = 'chat' OR kind = 'chat_deleted';
ALTER TABLE ai_settings_events DROP CONSTRAINT ai_settings_events_kind_check,
    ADD CONSTRAINT ai_settings_events_kind_check CHECK (kind IN ('provider_added', 'provider_changed',
        'provider_key_changed', 'provider_removed', 'slot_changed', 'automatic_limits_changed'));
ALTER TABLE ai_usage DROP CONSTRAINT ai_usage_feature_check,
    ADD CONSTRAINT ai_usage_feature_check CHECK (feature IN ('check', 'triage', 'draft'));
ALTER TABLE ai_settings_events DROP CONSTRAINT ai_settings_events_slot_check,
    ADD CONSTRAINT ai_settings_events_slot_check CHECK (slot IN ('triage', 'draft'));
ALTER TABLE ai_slots DROP COLUMN monthly_cap_micro_usd, DROP CONSTRAINT ai_slots_slot_check,
    ADD CONSTRAINT ai_slots_slot_check CHECK (slot IN ('triage', 'draft'));
DELETE FROM channel_types WHERE type = 'help_chat';
