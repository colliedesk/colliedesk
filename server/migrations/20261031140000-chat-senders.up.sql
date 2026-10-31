-- The people who wrote in a channel's chats (GET /api/v1/channels/{id}/senders) are read by sender.
CREATE INDEX channel_thread_items_sender ON channel_thread_items(workspace_id, channel_id, sender_kind, sender_value, occurred_at DESC)
    WHERE sender_value IS NOT NULL AND role <> 'bot';
