-- Whether customer mail has come through a support mailbox (the Reply-To before forwarding and the inbox
-- line about moving the mail) reads the journal entries of one channel that became a message and did not
-- reach it directly (a test message, Gmail's forwarding confirmation). The index on (workspace_id,
-- message_id) made that read every such entry of the workspace, growing with the mail of its other
-- channels; this one holds only those entries, by channel, and skips dropped mail. Its condition is the
-- query's own, so a lookup of one message keeps using channel_inbound_events_message.
CREATE INDEX channel_inbound_events_channel_messages ON channel_inbound_events(workspace_id, channel_id)
    WHERE message_id IS NOT NULL AND NOT COALESCE((raw->>'direct')::boolean, false);
