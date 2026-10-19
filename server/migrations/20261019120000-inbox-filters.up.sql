-- The inbox filters and saved views (docs/internals/inbox-filters.md).

-- How a conversation started. Every writer sets it; the default is for an older image, which knows
-- nothing of the column: rolling back to one labels the conversations it creates 'agent'.
ALTER TABLE conversations ADD COLUMN channel text NOT NULL DEFAULT 'agent'
    CHECK (channel IN ('email', 'web_form', 'imported', 'agent', 'problem_report'));

-- Existing rows are read from what they already hold: an import keeps its origin; otherwise the first
-- message tells. An incoming first message marks how it arrived (email_meta.via); web form messages
-- stored before via existed have no email_meta at all, since every mail and problem report always
-- had one. A first message from the team, or none, is a conversation a team member started.
UPDATE conversations c SET channel = CASE
        WHEN c.origin IS NOT NULL THEN 'imported'
        WHEN f.kind IS DISTINCT FROM 'incoming' THEN 'agent'
        WHEN f.email_meta->>'via' = 'web_form' THEN 'web_form'
        WHEN f.email_meta->>'via' = 'problem_report' THEN 'problem_report'
        WHEN f.email_meta IS NULL AND f.email_message_id IS NULL THEN 'web_form'
        ELSE 'email'
    END
    FROM conversations c2
    LEFT JOIN LATERAL (
        SELECT m.kind, m.email_meta, m.email_message_id FROM messages m
        WHERE m.workspace_id = c2.workspace_id AND m.conversation_id = c2.id
        ORDER BY m.created_at, m.id LIMIT 1
    ) f ON true
    WHERE c2.workspace_id = c.workspace_id AND c2.id = c.id;

-- The newest message that is not a note: incoming means the team owes the customer an answer,
-- outgoing that the customer has it. Kept by the triggers below for every way a message is added,
-- moved or removed, code older than this migration included. They never touch updated_at.
ALTER TABLE conversations
    ADD COLUMN last_public_kind text CHECK (last_public_kind IN ('incoming', 'outgoing')),
    ADD COLUMN last_public_at timestamptz,
    ADD CHECK ((last_public_kind IS NULL) = (last_public_at IS NULL));

UPDATE conversations c SET last_public_kind = l.kind, last_public_at = l.created_at
    FROM (SELECT DISTINCT ON (m.workspace_id, m.conversation_id) m.workspace_id, m.conversation_id, m.kind, m.created_at
          FROM messages m WHERE m.kind <> 'note'
          ORDER BY m.workspace_id, m.conversation_id, m.created_at DESC, m.id DESC) l
    WHERE l.workspace_id = c.workspace_id AND l.conversation_id = c.id;

-- Recomputed, never incremented: an import writes messages with old timestamps in any order.
-- Statement-level with transition tables, so a bulk insert updates each conversation once.
-- Two writers on one conversation: the rows are locked first (FOR NO KEY UPDATE, as the UPDATE would, so
-- the foreign key checks of other inserts still pass), in the order of the bulk update, and the newest
-- message is read by the next statement, whose snapshot then sees what the other writer committed.
-- Reading and updating in one statement would keep that statement's older snapshot and overwrite it.
CREATE FUNCTION conversations_refresh_last_public(workspace_ids uuid[], conversation_ids uuid[]) RETURNS void LANGUAGE plpgsql AS $$
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

CREATE FUNCTION messages_last_public_inserted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM conversations_refresh_last_public(array_agg(workspace_id), array_agg(conversation_id))
        FROM added_messages WHERE kind <> 'note' HAVING count(*) > 0;
    RETURN NULL;
END
$$;

CREATE FUNCTION messages_last_public_deleted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM conversations_refresh_last_public(array_agg(workspace_id), array_agg(conversation_id))
        FROM removed_messages WHERE kind <> 'note' HAVING count(*) > 0;
    RETURN NULL;
END
$$;

-- Only a change of kind, time or conversation can move the newest message; edits of the HTML or the
-- email metadata refresh nothing. The conversation a message left and the one it joined both refresh.
CREATE FUNCTION messages_last_public_updated() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM conversations_refresh_last_public(array_agg(x.workspace_id), array_agg(x.conversation_id))
        FROM old_messages o JOIN new_messages n ON n.id = o.id
        CROSS JOIN LATERAL (VALUES (o.workspace_id, o.conversation_id), (n.workspace_id, n.conversation_id)) AS x(workspace_id, conversation_id)
        WHERE (o.kind, o.created_at, o.conversation_id) IS DISTINCT FROM (n.kind, n.created_at, n.conversation_id)
        HAVING count(*) > 0;
    RETURN NULL;
END
$$;

CREATE TRIGGER messages_last_public_inserted AFTER INSERT ON messages
    REFERENCING NEW TABLE AS added_messages FOR EACH STATEMENT EXECUTE FUNCTION messages_last_public_inserted();
CREATE TRIGGER messages_last_public_deleted AFTER DELETE ON messages
    REFERENCING OLD TABLE AS removed_messages FOR EACH STATEMENT EXECUTE FUNCTION messages_last_public_deleted();
CREATE TRIGGER messages_last_public_updated AFTER UPDATE ON messages
    REFERENCING OLD TABLE AS old_messages NEW TABLE AS new_messages FOR EACH STATEMENT EXECUTE FUNCTION messages_last_public_updated();

-- Sort by the longest wait and the awaiting filter.
CREATE INDEX conversations_workspace_awaiting_team_idx
    ON conversations(workspace_id, last_public_at, id) WHERE last_public_kind = 'incoming';
-- The delivery_failed filter: failed rows are few, so the partial index stays small.
CREATE INDEX email_outbox_workspace_failed_idx ON email_outbox(workspace_id, message_id) WHERE status = 'failed';

-- Saved views: a named set of inbox filters (the query of GET /conversations as an object). A personal
-- view belongs to one agent and goes with them; a shared view belongs to the workspace. A delete waits
-- 30 days in the trash (deleted_at) and can be restored. Older images ignore this table.
CREATE TABLE saved_views (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    scope text NOT NULL CHECK (scope IN ('personal', 'shared')),
    owner_agent_id uuid,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80 AND btrim(name) <> ''),
    filters jsonb NOT NULL CHECK (jsonb_typeof(filters) = 'object' AND octet_length(filters::text) <= 16384),
    created_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CHECK ((scope = 'personal') = (owner_agent_id IS NOT NULL)),
    FOREIGN KEY (workspace_id, owner_agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, created_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id)
);

CREATE UNIQUE INDEX saved_views_personal_name_idx ON saved_views(workspace_id, owner_agent_id, lower(name))
    WHERE deleted_at IS NULL AND scope = 'personal';
CREATE UNIQUE INDEX saved_views_shared_name_idx ON saved_views(workspace_id, lower(name))
    WHERE deleted_at IS NULL AND scope = 'shared';
CREATE INDEX saved_views_trash_idx ON saved_views(workspace_id, deleted_at) WHERE deleted_at IS NOT NULL;

-- Same isolation as saved_replies.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE saved_views ENABLE ROW LEVEL SECURITY;
    ALTER TABLE saved_views FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON saved_views FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON saved_views FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON saved_views TO %I', app_role);
END
$$;
