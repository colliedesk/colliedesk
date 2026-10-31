-- A message the channel sends by itself (its received notice) is marked with notice_kind: it is no reply
-- of the team, so it never makes a conversation answered (the inbox's awaiting filter and sort), nor its
-- first reply in Results, nor the setup step and the usage counts of replies sent.
ALTER TABLE messages ADD COLUMN notice_kind text
    CHECK (notice_kind IS NULL OR (kind = 'outgoing' AND notice_kind ~ '^[a-z][a-z_]{1,39}$'));

CREATE OR REPLACE FUNCTION conversations_refresh_last_public(workspace_ids uuid[], conversation_ids uuid[]) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM conversations c
        WHERE (c.workspace_id, c.id) IN (SELECT * FROM unnest(workspace_ids, conversation_ids))
        ORDER BY c.workspace_id, c.id FOR NO KEY UPDATE;
    UPDATE conversations c SET last_public_kind = l.kind, last_public_at = l.created_at
        FROM (SELECT DISTINCT t.workspace_id, t.conversation_id
              FROM unnest(workspace_ids, conversation_ids) AS t(workspace_id, conversation_id)) d
        LEFT JOIN LATERAL (
            SELECT m.kind, m.created_at FROM messages m
            WHERE m.workspace_id = d.workspace_id AND m.conversation_id = d.conversation_id AND m.kind <> 'note' AND m.detached_at IS NULL
                AND m.notice_kind IS NULL
            ORDER BY m.created_at DESC, m.id DESC LIMIT 1
        ) l ON true
        WHERE c.workspace_id = d.workspace_id AND c.id = d.conversation_id
            AND (c.last_public_kind IS DISTINCT FROM l.kind OR c.last_public_at IS DISTINCT FROM l.created_at);
END
$$;

-- A notice the channel sends in a chat where no conversation is (the unmatched notice of a one-to-one
-- chat) is its thread item: the outbox sends either a message or such an item, and the ids the channel
-- gives what it sent name either.
ALTER TABLE channel_outbox ALTER COLUMN message_id DROP NOT NULL,
    ADD COLUMN thread_item_id uuid,
    ADD CONSTRAINT channel_outbox_target CHECK ((message_id IS NULL) <> (thread_item_id IS NULL)),
    ADD CONSTRAINT channel_outbox_thread_item_fkey FOREIGN KEY (workspace_id, thread_item_id)
        REFERENCES channel_thread_items(workspace_id, id) ON DELETE CASCADE;
CREATE UNIQUE INDEX channel_outbox_thread_item_idx ON channel_outbox(workspace_id, thread_item_id) WHERE thread_item_id IS NOT NULL;
ALTER TABLE channel_sent_ids ALTER COLUMN message_id DROP NOT NULL,
    ADD COLUMN thread_item_id uuid,
    ADD CONSTRAINT channel_sent_ids_target CHECK ((message_id IS NULL) <> (thread_item_id IS NULL)),
    ADD CONSTRAINT channel_sent_ids_thread_item_fkey FOREIGN KEY (workspace_id, thread_item_id)
        REFERENCES channel_thread_items(workspace_id, id) ON DELETE CASCADE;
