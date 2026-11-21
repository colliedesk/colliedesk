-- Help center article views per day, counts only: nothing about the reader is stored. day is the
-- calendar day in the workspace's timezone when the view was counted.
CREATE TABLE kb_article_views (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kb_article_id uuid NOT NULL,
    locale text NOT NULL,
    day date NOT NULL,
    views bigint NOT NULL CHECK (views > 0),
    PRIMARY KEY (workspace_id, kb_article_id, locale, day),
    FOREIGN KEY (workspace_id, kb_article_id)
        REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX kb_article_views_day ON kb_article_views(workspace_id, day);

-- A sent reply that used an article: it linked the article's help center address (link), or it was
-- an AI draft that quoted the article (ai_source). One row per message, article and kind.
-- conversation_id stays the reply's: only customer messages move between conversations.
CREATE TABLE kb_article_uses (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kb_article_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    message_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('link', 'ai_source')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, message_id, kb_article_id, kind),
    FOREIGN KEY (workspace_id, kb_article_id)
        REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, conversation_id)
        REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, message_id)
        REFERENCES messages(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX kb_article_uses_article
    ON kb_article_uses(workspace_id, kb_article_id, created_at);
CREATE INDEX kb_article_uses_created ON kb_article_uses(workspace_id, created_at);
CREATE INDEX kb_article_uses_conversation ON kb_article_uses(workspace_id, conversation_id);

SELECT secure_workspace_table('kb_article_views');
SELECT secure_workspace_table('kb_article_uses');
