-- A received notice the channel sent by itself on acme's first conversation.
INSERT INTO messages (id, workspace_id, conversation_id, kind, body, notice_kind, created_at) VALUES
    (md5('msg-acme-received-notice')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 'outgoing', 'We got your message.', 'received', '2026-10-31 09:00:00+00');
