-- Help center article views per day, counts only: nothing about the reader is stored. day is the
-- calendar day in the workspace's timezone when the view was counted.
CREATE TABLE kb_article_views (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kb_article_id uuid NOT NULL,
    locale text NOT NULL,
    day date NOT NULL,
    views bigint NOT NULL CHECK (views > 0),
    PRIMARY KEY (workspace_id, kb_article_id, locale, day),
    FOREIGN KEY (workspace_id, kb_article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX kb_article_views_day ON kb_article_views(workspace_id, day);

-- A sent reply that used an article: it linked the article's help center address (link), or it was an
-- AI draft that quoted the article (ai_source). One row per message, article and kind. conversation_id
-- follows the message when it moves to another conversation.
CREATE TABLE kb_article_uses (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kb_article_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    message_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('link', 'ai_source')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, message_id, kb_article_id, kind),
    FOREIGN KEY (workspace_id, kb_article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX kb_article_uses_article ON kb_article_uses(workspace_id, kb_article_id, created_at);
CREATE INDEX kb_article_uses_created ON kb_article_uses(workspace_id, created_at);
CREATE INDEX kb_article_uses_conversation ON kb_article_uses(workspace_id, conversation_id);

DO $$
DECLARE
    app_role text;
    table_name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH table_name IN ARRAY ARRAY['kb_article_views', 'kb_article_uses'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$I FOR ALL TO %2$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, table_name, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, table_name, current_user);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', table_name, app_role);
    END LOOP;
END $$;
