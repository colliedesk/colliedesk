-- Who wrote the unpublished text of an article (locale NULL) or of its translation, for the
-- four-eyes rule: a writer never reviews it (docs/internals/knowledge-base.md, "Review and
-- approval"). An autosave records no version, so the versions alone cannot say who wrote the text;
-- every write of it adds its writer here, a save, an autosave, a restore or an import alike. A
-- version that puts the text on the help center empties the list: who wrote the next change starts
-- from there.
CREATE TABLE kb_text_writers (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    locale text CHECK (char_length(locale) BETWEEN 2 AND 35),
    agent_id uuid NOT NULL,
    first_written_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    -- locale NULL is the article's own text, so the key treats NULL as one value.
    CONSTRAINT kb_text_writers_key
        UNIQUE NULLS NOT DISTINCT (workspace_id, article_id, locale, agent_id),
    FOREIGN KEY (workspace_id, article_id)
        REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, article_id, locale)
        REFERENCES kb_article_translations(workspace_id, article_id, locale) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE
);

SELECT secure_workspace_table('kb_text_writers');

-- The writers so far are the people who made a version that wrote the text after the last version
-- that put it on the help center: the rule as it read the versions until now.
INSERT INTO kb_text_writers(workspace_id, article_id, locale, agent_id, first_written_at)
SELECT v.workspace_id, v.article_id, NULL, v.changed_by_agent_id, min(v.created_at)
FROM kb_article_versions v
WHERE v.changed_by_agent_id IS NOT NULL
    AND v.kind IN ('create', 'edit', 'restore', 'draft', 'import', 'media_replace')
    AND v.version > COALESCE((SELECT max(p.version) FROM kb_article_versions p
        WHERE p.workspace_id = v.workspace_id AND p.article_id = v.article_id
            AND p.status = 'published'
            AND p.kind NOT IN (
                'draft', 'review_request', 'review_approve', 'review_changes', 'review_withdraw'
            )), 0)
GROUP BY v.workspace_id, v.article_id, v.changed_by_agent_id;
INSERT INTO kb_text_writers(workspace_id, article_id, locale, agent_id, first_written_at)
SELECT v.workspace_id, v.article_id, v.locale, v.changed_by_agent_id, min(v.created_at)
FROM kb_article_translation_versions v
JOIN kb_article_translations t ON t.workspace_id = v.workspace_id
    AND t.article_id = v.article_id AND t.locale = v.locale
WHERE v.changed_by_agent_id IS NOT NULL
    AND v.kind IN ('create', 'edit', 'restore', 'draft', 'import', 'media_replace')
    AND v.version > COALESCE((SELECT max(p.version) FROM kb_article_translation_versions p
        WHERE p.workspace_id = v.workspace_id AND p.article_id = v.article_id
            AND p.locale = v.locale AND p.status = 'published'
            AND p.kind NOT IN (
                'draft', 'review_request', 'review_approve', 'review_changes', 'review_withdraw'
            )), 0)
GROUP BY v.workspace_id, v.article_id, v.locale, v.changed_by_agent_id;
