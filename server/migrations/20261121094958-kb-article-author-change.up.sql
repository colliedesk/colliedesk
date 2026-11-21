-- A version of kind author_change records whom an article was credited to before and after.
ALTER TABLE kb_article_versions
    ADD COLUMN author_from_agent_id uuid,
    ADD COLUMN author_to_agent_id uuid,
    ADD CONSTRAINT kb_article_versions_author_from_fkey
        FOREIGN KEY (workspace_id, author_from_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (author_from_agent_id),
    ADD CONSTRAINT kb_article_versions_author_to_fkey
        FOREIGN KEY (workspace_id, author_to_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (author_to_agent_id);

ALTER TABLE kb_article_versions DROP CONSTRAINT kb_article_versions_kind_check;
ALTER TABLE kb_article_versions ADD CONSTRAINT kb_article_versions_kind_check
    CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace',
        'ai_exclude', 'ai_include', 'draft',
        'review_request', 'review_approve', 'review_changes', 'review_withdraw', 'author_change'));
