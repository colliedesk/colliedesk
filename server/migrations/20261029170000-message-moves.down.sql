CREATE OR REPLACE FUNCTION messages_last_public_updated() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM conversations_refresh_last_public(array_agg(x.workspace_id), array_agg(x.conversation_id))
        FROM old_messages o JOIN new_messages n ON n.id = o.id
        CROSS JOIN LATERAL (VALUES (o.workspace_id, o.conversation_id), (n.workspace_id, n.conversation_id)) AS x(workspace_id, conversation_id)
        WHERE (o.kind, o.created_at, o.conversation_id) IS DISTINCT FROM (n.kind, n.created_at, n.conversation_id)
        HAVING count(*) > 0;
    RETURN NULL;
END
$$;
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
            WHERE m.workspace_id = d.workspace_id AND m.conversation_id = d.conversation_id AND m.kind <> 'note'
            ORDER BY m.created_at DESC, m.id DESC LIMIT 1
        ) l ON true
        WHERE c.workspace_id = d.workspace_id AND c.id = d.conversation_id
            AND (c.last_public_kind IS DISTINCT FROM l.kind OR c.last_public_at IS DISTINCT FROM l.created_at);
END
$$;
ALTER TABLE conversation_thread_keys DROP COLUMN message_id;
ALTER TABLE conversations DROP COLUMN merged_into_id;
ALTER TABLE messages DROP COLUMN other_contact_id, DROP COLUMN detached_at, DROP COLUMN detached_by_agent_id;
DROP TABLE message_moves;
