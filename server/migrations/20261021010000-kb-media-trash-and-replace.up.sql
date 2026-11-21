-- The media library's trash in batches, and replacing an item in every article that shows it.
--
-- kb_media_batches remembers which items one "move to trash" took, so a single Undo brings back
-- exactly those (a "move all unused" of thousands of items runs in several calls under one batch).
--
-- kb_media_replacements records a replacement: which articles got a new version naming to_media_id
-- instead of from_media_id (and its number), and which translations were rewritten (with their text
-- before and after), so the replacement can be undone exactly: an undo puts back the text before,
-- only where nothing changed since. Those versions are of kind media_replace.
CREATE TABLE kb_media_batches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    action text NOT NULL CHECK (action IN ('trash')),
    media_ids uuid[] NOT NULL DEFAULT '{}',
    created_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, created_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE
        SET NULL (created_by_agent_id)
);

CREATE TABLE kb_media_replacements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    from_media_id uuid NOT NULL,
    to_media_id uuid NOT NULL,
    article_ids uuid[] NOT NULL DEFAULT '{}',
    -- The version each article of article_ids got, in the same order.
    article_versions integer[] NOT NULL DEFAULT '{}',
    -- [{"article_id": "...", "locale": "de", "before": "...", "after": "..."}, ...]
    translations jsonb NOT NULL DEFAULT '[]',
    skipped_article_ids uuid[] NOT NULL DEFAULT '{}',
    replaced_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    undone_at timestamptz,
    undone_by_agent_id uuid,
    UNIQUE (workspace_id, id),
    CHECK (from_media_id <> to_media_id),
    CHECK (cardinality(article_ids) = cardinality(article_versions)),
    CHECK (undone_by_agent_id IS NULL OR undone_at IS NOT NULL),
    FOREIGN KEY (workspace_id, replaced_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE
        SET NULL (replaced_by_agent_id),
    FOREIGN KEY (workspace_id, undone_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE
        SET NULL (undone_by_agent_id)
);
-- An undo after the replaced item was purged is refused; the replacement stays as a record.
CREATE INDEX kb_media_replacements_media_idx ON kb_media_replacements(workspace_id, from_media_id);

ALTER TABLE kb_article_versions DROP CONSTRAINT kb_article_versions_kind_check;
ALTER TABLE kb_article_versions ADD CONSTRAINT kb_article_versions_kind_check
    CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace'));

SELECT secure_workspace_table('kb_media_batches');
SELECT secure_workspace_table('kb_media_replacements');
