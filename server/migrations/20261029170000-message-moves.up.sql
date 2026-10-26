-- Moving customer messages between conversations (docs/internals/message-moves.md): a
-- person, an AI agent or intake moves a message added to the wrong conversation, splits it into a new
-- one, or takes a chat message back to the intake queue. Every move is a row, undone by its own row's
-- undone_at, so no move is a one-way door; the messages themselves are never deleted.
CREATE TABLE message_moves (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- The order moves were made in: only a message's newest move is undone.
    seq bigint GENERATED ALWAYS AS IDENTITY,
    message_ids uuid[] NOT NULL CHECK (cardinality(message_ids) BETWEEN 1 AND 100),
    from_conversation_id uuid NOT NULL,
    -- NULL: back to the intake queue (the card is gathering_id).
    to_conversation_id uuid,
    -- The move opened to_conversation_id (a split).
    created_conversation boolean NOT NULL DEFAULT false,
    -- The move took every customer message of the source and closed it ("not a new request").
    closed_source boolean NOT NULL DEFAULT false,
    -- The statuses the move changed, given back by its undo while nobody changed them since.
    source_status_before text CHECK (source_status_before IN ('open', 'pending', 'resolved', 'closed')),
    target_status_before text CHECK (target_status_before IN ('open', 'pending', 'resolved', 'closed')),
    -- The thread keys the moved messages recorded, [{"channel_type": ..., "key": ...}], moved with them.
    thread_keys_moved jsonb NOT NULL DEFAULT '[]',
    gathering_id uuid,
    -- The person's decision it records when it corrects what AI applied.
    decision_id uuid,
    moved_by_agent_id uuid,
    moved_by_token_id uuid,
    moved_at timestamptz NOT NULL DEFAULT now(),
    undone_at timestamptz,
    undone_by_agent_id uuid,
    undone_by_token_id uuid,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, from_conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, to_conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, gathering_id) REFERENCES intake_gatherings(workspace_id, id) ON DELETE SET NULL (gathering_id),
    FOREIGN KEY (workspace_id, decision_id) REFERENCES intake_decisions(workspace_id, id) ON DELETE SET NULL (decision_id),
    FOREIGN KEY (workspace_id, moved_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (moved_by_agent_id),
    FOREIGN KEY (workspace_id, undone_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (undone_by_agent_id),
    CHECK (to_conversation_id IS DISTINCT FROM from_conversation_id),
    CHECK (NOT created_conversation OR to_conversation_id IS NOT NULL),
    CHECK (jsonb_typeof(thread_keys_moved) = 'array')
);
CREATE INDEX message_moves_from ON message_moves(workspace_id, from_conversation_id, moved_at DESC);
CREATE INDEX message_moves_to ON message_moves(workspace_id, to_conversation_id, moved_at DESC) WHERE to_conversation_id IS NOT NULL;
-- Whether a later move took one of a move's messages (only the newest move of a message is undone).
CREATE INDEX message_moves_messages ON message_moves USING gin (message_ids);

-- A customer message moved into a conversation of another contact: the contact whose conversation it came
-- from, so replies never go to them. NULL in a conversation of that contact, and for every message a
-- channel added (a copied colleague answering by email is not moved).
ALTER TABLE messages ADD COLUMN other_contact_id uuid,
    ADD FOREIGN KEY (workspace_id, other_contact_id) REFERENCES contacts(workspace_id, id) ON DELETE SET NULL (other_contact_id),
    -- A chat message taken back to the intake queue: out of the conversation's timeline, never deleted.
    ADD COLUMN detached_at timestamptz,
    ADD COLUMN detached_by_agent_id uuid,
    ADD FOREIGN KEY (workspace_id, detached_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (detached_by_agent_id);

-- A conversation closed because every customer message of it was moved to another: where they went.
ALTER TABLE conversations ADD COLUMN merged_into_id uuid,
    ADD FOREIGN KEY (workspace_id, merged_into_id) REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (merged_into_id),
    ADD CHECK (merged_into_id IS DISTINCT FROM id);

-- The message that recorded a thread key, so the key moves with it; NULL for keys recorded before.
ALTER TABLE conversation_thread_keys ADD COLUMN message_id uuid,
    ADD FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE SET NULL (message_id);
CREATE INDEX conversation_thread_keys_message ON conversation_thread_keys(workspace_id, message_id) WHERE message_id IS NOT NULL;

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE message_moves ENABLE ROW LEVEL SECURITY;
    ALTER TABLE message_moves FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON message_moves FOR ALL TO %I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON message_moves FOR ALL TO %I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON message_moves TO %I', app_role);
END
$$;

-- A chat message taken back to the intake queue leaves the conversation's timeline: the newest public
-- message (the inbox's sort and the awaiting filter) is the newest one still in it, and taking one back or
-- putting it in again refreshes it.
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
            ORDER BY m.created_at DESC, m.id DESC LIMIT 1
        ) l ON true
        WHERE c.workspace_id = d.workspace_id AND c.id = d.conversation_id
            AND (c.last_public_kind IS DISTINCT FROM l.kind OR c.last_public_at IS DISTINCT FROM l.created_at);
END
$$;

CREATE OR REPLACE FUNCTION messages_last_public_updated() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM conversations_refresh_last_public(array_agg(x.workspace_id), array_agg(x.conversation_id))
        FROM old_messages o JOIN new_messages n ON n.id = o.id
        CROSS JOIN LATERAL (VALUES (o.workspace_id, o.conversation_id), (n.workspace_id, n.conversation_id)) AS x(workspace_id, conversation_id)
        WHERE (o.kind, o.created_at, o.conversation_id, o.detached_at IS NULL) IS DISTINCT FROM (n.kind, n.created_at, n.conversation_id, n.detached_at IS NULL)
        HAVING count(*) > 0;
    RETURN NULL;
END
$$;
