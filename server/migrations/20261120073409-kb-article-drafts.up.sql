-- A published article, or a published translation, can hold one set of unpublished changes
-- (docs/internals/knowledge-base.md, "Drafts"). A person or an AI agent edits it as often as they
-- like; the help center, search, sitemap.xml, llms.txt and AI answers keep reading the published
-- row until someone publishes the draft. Publishing copies it into the live row, discarding deletes
-- it.
--
-- locale NULL is the article's own text; otherwise the row is the draft of that translation. A
-- draft row exists only while its article or translation is published: a draft article is edited in
-- place. collection_id and extra_collection_ids NULL mean "keep where it is listed now";
-- show_contents NULL keeps the article's on-this-page switch as it is.
--
-- No existing row changes and no draft is created: every article and translation stays as it was.
CREATE TABLE kb_article_drafts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    locale text CHECK (char_length(locale) BETWEEN 2 AND 35),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
    slug text NOT NULL CHECK (char_length(slug) BETWEEN 1 AND 120),
    body_md text NOT NULL CHECK (char_length(body_md) <= 100000),
    seo_title text NOT NULL DEFAULT '' CHECK (char_length(seo_title) <= 120),
    seo_description text NOT NULL DEFAULT '' CHECK (char_length(seo_description) <= 320),
    collection_id uuid,
    extra_collection_ids uuid[],
    show_contents boolean,
    updated_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        locale IS NULL
        OR (collection_id IS NULL AND extra_collection_ids IS NULL AND show_contents IS NULL)
    ),
    FOREIGN KEY (workspace_id, article_id)
        REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, article_id, locale)
        REFERENCES kb_article_translations(workspace_id, article_id, locale) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, collection_id)
        REFERENCES kb_collections(workspace_id, id) ON DELETE SET NULL (collection_id),
    FOREIGN KEY (workspace_id, updated_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (updated_by_agent_id)
);
CREATE UNIQUE INDEX kb_article_drafts_article_idx ON kb_article_drafts(workspace_id, article_id)
    WHERE locale IS NULL;
CREATE UNIQUE INDEX kb_article_drafts_translation_idx
    ON kb_article_drafts(workspace_id, article_id, locale)
    WHERE locale IS NOT NULL;

-- Saving a draft is a version of the article, so the history shows who changed what before it went
-- live. Its status is the live one: the version is not on the help center.
ALTER TABLE kb_article_versions DROP CONSTRAINT kb_article_versions_kind_check;
ALTER TABLE kb_article_versions ADD CONSTRAINT kb_article_versions_kind_check
    CHECK (kind IN (
        'create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'ai_exclude',
        'ai_include', 'draft'
    ));
ALTER TABLE kb_article_translation_versions
    DROP CONSTRAINT kb_article_translation_versions_kind_check;
ALTER TABLE kb_article_translation_versions
    ADD CONSTRAINT kb_article_translation_versions_kind_check
    CHECK (kind IN (
        'create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace',
        'mark_current', 'mark_outdated', 'draft'
    ));

SELECT secure_workspace_table('kb_article_drafts');
