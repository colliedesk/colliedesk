-- An article can be listed in several collections (issue #472). kb_articles.collection_id stays its
-- home: the breadcrumb, its place among siblings and the collection it is created and moved in. Each row
-- here lists it in one more collection of the same workspace (both foreign keys carry workspace_id, so a
-- row can never point across workspaces). The help center lists the article in each of them at its one
-- address /a/<number>-<slug>; search, sitemap.xml, llms.txt and AI answers read the article once.
--
-- A row stays while its collection is in the trash (the API leaves it out then, and restoring the
-- collection lists the article again); purging the collection or the article removes it. No existing row
-- changes: every article starts in its home collection only.
CREATE TABLE kb_article_collections (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    collection_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, article_id, collection_id),
    FOREIGN KEY (workspace_id, article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, collection_id) REFERENCES kb_collections(workspace_id, id) ON DELETE CASCADE
);
-- A collection's list on the help center reads the articles listed in it.
CREATE INDEX kb_article_collections_collection_idx ON kb_article_collections(workspace_id, collection_id);

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE kb_article_collections ENABLE ROW LEVEL SECURITY;
    ALTER TABLE kb_article_collections FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON kb_article_collections FOR ALL TO %I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON kb_article_collections FOR ALL TO %I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON kb_article_collections TO %I', app_role);
END
$$;
