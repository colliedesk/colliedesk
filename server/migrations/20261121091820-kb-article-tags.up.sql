-- The tags of a knowledge base article: lower case, 1 to 40 characters, at most 20 an article (the
-- server cleans and counts them). They are shared by the article's translations and are not the tags
-- of conversations. These rows are the published tags; a published article's unpublished draft keeps
-- its own list in kb_article_drafts.tags, where NULL keeps the published one.
CREATE TABLE kb_article_tags (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    tag text NOT NULL CHECK (char_length(tag) BETWEEN 1 AND 40 AND tag = btrim(tag)),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, article_id, tag),
    FOREIGN KEY (workspace_id, article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE
);
-- The list filter and the tag counts read the articles of one tag.
CREATE INDEX kb_article_tags_tag_idx ON kb_article_tags(workspace_id, tag);
-- The help center search finds the tags that match a word of the search here, then keeps those of its
-- workspace, so a search never builds a vector for every tag of a large knowledge base.
CREATE INDEX kb_article_tags_search_idx ON kb_article_tags USING gin (kb_search_vector(tag));

ALTER TABLE kb_article_drafts ADD COLUMN tags text[];

-- Readers see an article's tags on its help center page only when the team turns this on.
ALTER TABLE help_centers ADD COLUMN show_tags boolean NOT NULL DEFAULT false;

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE kb_article_tags ENABLE ROW LEVEL SECURITY;
    ALTER TABLE kb_article_tags FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON kb_article_tags FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON kb_article_tags FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON kb_article_tags TO %I', app_role);
END
$$;

-- AI retrieval reads an article's tags with its title: a tag added or removed queues the article.
CREATE FUNCTION ai_retrieval_tag_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id)
        SELECT DISTINCT workspace_id, article_id FROM changed;
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_retrieval_tag_inserted AFTER INSERT ON kb_article_tags
    REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION ai_retrieval_tag_changed();
CREATE TRIGGER ai_retrieval_tag_deleted AFTER DELETE ON kb_article_tags
    REFERENCING OLD TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION ai_retrieval_tag_changed();
