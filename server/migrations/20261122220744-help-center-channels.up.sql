-- A channel and a conversation name the help center they belong to. A help chat or web form
-- channel names the help center it serves (NULL is the primary), and a conversation it opens keeps
-- that help center, so the inbox and Results tell the help centers apart. Email and the other
-- types leave it NULL. Existing rows keep NULL: every channel serves the primary, as before, and
-- earlier conversations name no help center.
--
-- Help chat reads the knowledge base as the reader of its channel's help center, so it cites only
-- what that help center shows. The reader may now read the columns of kb_article_versions that
-- number a text and the knowledge base rows of ai_retrieval_docs, each for the articles it sees.
-- kb_help_center_chat_articles and kb_help_center_chat_translations decide what help chat may
-- read, like the public views, and return rows only while app.help_chat is on: the public pages
-- never set it, so they cannot show an article kept for help chat.
--
-- An older image ignores the new columns and reads help chat as the app role through
-- kb_help_chat_articles, which stays.
ALTER TABLE channels ADD COLUMN help_center_id uuid,
    ADD CONSTRAINT channels_help_center_fkey FOREIGN KEY (workspace_id, help_center_id)
        REFERENCES help_centers (workspace_id, id),
    ADD CONSTRAINT channels_help_center_type
        CHECK (help_center_id IS NULL OR type IN ('help_chat', 'web_form'));
CREATE INDEX channels_help_center ON channels (workspace_id, help_center_id)
    WHERE help_center_id IS NOT NULL;

ALTER TABLE conversations ADD COLUMN help_center_id uuid,
    ADD CONSTRAINT conversations_help_center_fkey FOREIGN KEY (workspace_id, help_center_id)
        REFERENCES help_centers (workspace_id, id) ON DELETE SET NULL (help_center_id);
CREATE INDEX conversations_help_center ON conversations (workspace_id, help_center_id)
    WHERE help_center_id IS NOT NULL;

SELECT secure_help_center_table('kb_article_versions',
    'article_id IN (SELECT id FROM kb_articles)', 'SELECT');
SELECT secure_help_center_table('ai_retrieval_docs',
    'kb_article_id IN (SELECT id FROM kb_articles)', 'SELECT');
-- Help chat reads only which version an article's text is at, never an older text or its author.
DO $$
BEGIN
    EXECUTE format('REVOKE SELECT ON kb_article_versions FROM %I', reader_role());
    EXECUTE format('GRANT SELECT (workspace_id, article_id, version, kind) '
        'ON kb_article_versions TO %I', reader_role());
END
$$;

-- What help chat may read on a help center: kb_help_chat_articles decided for the whole workspace
-- by the app role, as kb_help_center_public_articles is (help-center-row-security). The reader's
-- own policies on kb_articles then keep the help center's articles. public says the help center
-- shows the article too (kb_help_center_public_articles), so help chat may link it. It tests the
-- ranks as sets the planner hashes once per statement: h.visibility_rank in the output list makes
-- the planner compute that collection's rank again for every row.
CREATE VIEW kb_help_center_chat_articles AS
SELECT a.workspace_id, a.id,
    a.visibility = 'everyone'
        AND NOT EXISTS (SELECT 1 FROM kb_help_center_collection_ranks p
            WHERE p.workspace_id = a.workspace_id AND p.id = a.collection_id
                AND p.visibility_rank > 0)
        AND NOT EXISTS (SELECT 1 FROM kb_article_collections x
            JOIN kb_help_center_collection_ranks v
                ON v.workspace_id = x.workspace_id AND v.id = x.collection_id
            WHERE x.workspace_id = a.workspace_id AND x.article_id = a.id
                AND v.visibility_rank > 0) AS public
FROM kb_articles a
JOIN kb_help_center_collection_ranks h ON h.workspace_id = a.workspace_id AND h.id = a.collection_id
WHERE (SELECT current_setting('app.help_chat', true)) = 'on'
    AND a.workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid)
    AND a.status = 'published' AND a.deleted_at IS NULL
    AND a.visibility IN ('everyone', 'help_chat')
    AND h.visibility_rank <= 1
    AND NOT EXISTS (SELECT 1 FROM kb_article_collections x
        JOIN kb_help_center_collection_ranks v
            ON v.workspace_id = x.workspace_id AND v.id = x.collection_id
        WHERE x.workspace_id = a.workspace_id AND x.article_id = a.id AND v.visibility_rank > 1);

CREATE VIEW kb_help_center_chat_translations AS
SELECT t.workspace_id, t.article_id, t.locale FROM kb_article_translations t
WHERE t.workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid)
    AND t.status = 'published' AND t.deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM kb_help_center_chat_articles a
        WHERE a.workspace_id = t.workspace_id AND a.id = t.article_id);

DO $$
DECLARE
    view_name text;
BEGIN
    -- A new owner needs CREATE on the schema; the app role has it only for these statements.
    EXECUTE format('GRANT CREATE ON SCHEMA %I TO %I', current_schema(), app_role());
    FOREACH view_name IN ARRAY ARRAY['kb_help_center_chat_articles',
        'kb_help_center_chat_translations']
    LOOP
        EXECUTE format('ALTER VIEW %I OWNER TO %I', view_name, app_role());
        EXECUTE format('GRANT SELECT ON %I TO %I', view_name, reader_role());
    END LOOP;
    EXECUTE format('REVOKE CREATE ON SCHEMA %I FROM %I', current_schema(), app_role());
END
$$;
