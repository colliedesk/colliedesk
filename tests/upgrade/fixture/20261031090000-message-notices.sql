-- A received notice the channel sent by itself on acme's first conversation.
INSERT INTO messages (id, workspace_id, conversation_id, kind, body, notice_kind, created_at) VALUES
    (md5('msg-acme-received-notice')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 'outgoing', 'We got your message.', 'received', '2026-10-31 09:00:00+00');

-- beta's chat answered a message that named no command with the unmatched notice, sent by the channel.
INSERT INTO channel_thread_items (id, workspace_id, channel_id, thread_id, direction, role, external_id, quote_external_id, body, disposition, occurred_at, created_at)
    SELECT md5('item-beta-notice')::uuid, t.workspace_id, t.channel_id, t.id, 'out', 'bot', 'chat-notice-1', 'chat-1', 'Write a command to open a request.',
        'bot', '2026-10-31 09:00:00+00', '2026-10-31 09:00:00+00'
    FROM channel_threads t WHERE t.id = md5('thread-beta-1')::uuid;
INSERT INTO channel_outbox (workspace_id, thread_item_id, status, attempts, sent_at, external_id, recipients, channel_type, channel_id, created_at, updated_at)
    SELECT t.workspace_id, md5('item-beta-notice')::uuid, 'sent', 1, '2026-10-31 09:00:05+00', 'chat-notice-1', 1, c.type, c.id, '2026-10-31 09:00:00+00', '2026-10-31 09:00:05+00'
    FROM channel_threads t JOIN channels c ON c.workspace_id = t.workspace_id AND c.id = t.channel_id WHERE t.id = md5('thread-beta-1')::uuid;
INSERT INTO channel_sent_ids (workspace_id, channel_id, external_id, thread_item_id, created_at)
    SELECT t.workspace_id, t.channel_id, 'chat-notice-1', md5('item-beta-notice')::uuid, '2026-10-31 09:00:05+00'
    FROM channel_threads t WHERE t.id = md5('thread-beta-1')::uuid;
