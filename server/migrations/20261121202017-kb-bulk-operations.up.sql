-- A bulk action on knowledge base articles (POST /api/v1/kb/articles/bulk) is one undo record:
-- the operation, and for each article it changed what undo puts back. Undo works for ten
-- minutes and only once (undone_at). An article changed again since the batch is left as it
-- is: after_token is the article's token just after the batch, so undo sees the newer change.
CREATE TABLE kb_bulk_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    action text NOT NULL CHECK (action IN (
        'move',
        'visibility',
        'author',
        'tags_add',
        'tags_remove',
        'send_for_review',
        'approve',
        'publish',
        'unpublish',
        'trash'
    )),
    params jsonb NOT NULL DEFAULT '{}',
    created_by_agent_id uuid,
    created_by_token_id uuid,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    undone_at timestamptz,
    undone_by_agent_id uuid,
    undone_by_token_id uuid,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, created_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id),
    FOREIGN KEY (workspace_id, created_by_token_id)
        REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (created_by_token_id),
    FOREIGN KEY (workspace_id, undone_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (undone_by_agent_id),
    FOREIGN KEY (workspace_id, undone_by_token_id)
        REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (undone_by_token_id)
);

-- One row per article the batch changed and can put back. undo names how: revert the fields in
-- before, discard the draft the batch made, withdraw the review it asked for (ref), unpublish,
-- publish again or restore from the trash. ordinal keeps the order of the batch's answer. A review
-- withdrawn by undo needs no token (after_token stays empty): the review itself must be open.
CREATE TABLE kb_bulk_operation_articles (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    operation_id uuid NOT NULL,
    article_id uuid NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    before jsonb NOT NULL DEFAULT '{}',
    after_token timestamptz,
    undo text NOT NULL CHECK (undo IN (
        'update',
        'discard_draft',
        'withdraw_review',
        'unpublish',
        'republish',
        'restore'
    )),
    ref text,
    PRIMARY KEY (workspace_id, operation_id, article_id),
    FOREIGN KEY (workspace_id, operation_id)
        REFERENCES kb_bulk_operations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, article_id)
        REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX kb_bulk_operation_articles_article_idx
    ON kb_bulk_operation_articles(workspace_id, article_id);

SELECT secure_workspace_table('kb_bulk_operations');
SELECT secure_workspace_table('kb_bulk_operation_articles');
