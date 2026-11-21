-- The tags of a knowledge base article: lower case, 1 to 40 characters, at most 20 an article
-- (the server cleans and counts them). They are shared by the article's translations and are not
-- the tags of conversations. These rows are the published tags; a published article's unpublished
-- draft keeps its own list in kb_article_drafts.tags, where NULL keeps the published one.
CREATE TABLE kb_article_tags (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    tag text NOT NULL CHECK (char_length(tag) BETWEEN 1 AND 40 AND tag = btrim(tag)),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, article_id, tag),
    FOREIGN KEY (workspace_id, article_id)
        REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE
);
-- The list filter and the tag counts read the articles of one tag.
CREATE INDEX kb_article_tags_tag_idx ON kb_article_tags(workspace_id, tag);
-- No GIN index for the help center tag search: under row-level security Postgres never uses one
-- for @@, which is not leakproof (docs/internals/knowledge-base.md); the search reads the
-- workspace's tags.

ALTER TABLE kb_article_drafts ADD COLUMN tags text[];

-- Readers see an article's tags on its help center page only when the team turns this on.
ALTER TABLE help_centers ADD COLUMN show_tags boolean NOT NULL DEFAULT false;

SELECT secure_workspace_table('kb_article_tags');

-- AI retrieval reads an article's tags with its title: a tag added or removed queues the article.
-- Deleting a workspace cascades to its tags after the workspace row is gone, so the trigger queues
-- only for a workspace it can see: otherwise the delete fails on the foreign key to workspaces
-- (app.privileged) or on row-level security (a superuser, whose cascade runs as the table owner).
CREATE FUNCTION ai_retrieval_tag_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id)
        SELECT DISTINCT c.workspace_id, c.article_id FROM changed c
        WHERE EXISTS (SELECT 1 FROM workspaces w WHERE w.id = c.workspace_id);
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_retrieval_tag_inserted AFTER INSERT ON kb_article_tags
    REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION ai_retrieval_tag_changed();
CREATE TRIGGER ai_retrieval_tag_deleted AFTER DELETE ON kb_article_tags
    REFERENCING OLD TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION ai_retrieval_tag_changed();
