DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM channel_events WHERE kind IN ('paused', 'resumed')) THEN
        RAISE EXCEPTION 'channel pause rollback refused: channels were paused';
    END IF;
END
$$;
ALTER TABLE channel_events DROP CONSTRAINT channel_events_kind_check,
    ADD CONSTRAINT channel_events_kind_check CHECK (kind IN ('added', 'renamed', 'enabled', 'disabled', 'config_changed', 'credential_changed',
        'removed', 'held_replayed', 'held_discarded', 'default_changed', 'ai_changed'));
ALTER TABLE channels DROP COLUMN processing_paused_by_agent_id, DROP COLUMN processing_paused_at;
ALTER TABLE channel_inbound_events DROP COLUMN files_fetched_at;
