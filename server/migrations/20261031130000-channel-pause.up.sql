-- A channel whose processing is paused (docs/user/channels.md "Pause processing"): what arrives is kept in
-- its journal as it came and nothing is decided, opened or sent until it is resumed; then the journal is
-- processed in order by the channel's rules, as if it had just arrived. For moving a channel from another
-- system without losing a message in between.
-- files_fetched_at marks an event whose files were fetched while its channel was paused (their links expire).
ALTER TABLE channel_inbound_events ADD COLUMN files_fetched_at timestamptz;
ALTER TABLE channels ADD COLUMN processing_paused_at timestamptz,
    ADD COLUMN processing_paused_by_agent_id uuid,
    ADD CONSTRAINT channels_processing_paused_by_fkey FOREIGN KEY (workspace_id, processing_paused_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (processing_paused_by_agent_id);
ALTER TABLE channel_events DROP CONSTRAINT channel_events_kind_check,
    ADD CONSTRAINT channel_events_kind_check CHECK (kind IN ('added', 'renamed', 'enabled', 'disabled', 'config_changed', 'credential_changed',
        'removed', 'held_replayed', 'held_discarded', 'default_changed', 'ai_changed', 'paused', 'resumed'));
