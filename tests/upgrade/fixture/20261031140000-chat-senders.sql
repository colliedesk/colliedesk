-- beta's chat customer wrote in its one-to-one chat.
INSERT INTO channel_senders (workspace_id, channel_id, sender_kind, sender_value, name, role, last_at, thread_id)
    SELECT t.workspace_id, t.channel_id, 'loop_user', 'beta-chatter', 'Beta Chatter', 'customer', '2026-10-31 09:00:00+00', t.id
    FROM channel_threads t WHERE t.id = md5('thread-beta-1')::uuid;
