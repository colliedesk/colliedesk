-- What readers search for on the public help center (docs/internals/knowledge-base.md,
-- "Searches"). One row per search a reader made: the words masked (no email, no run of 6+
-- digits), their folded form for grouping, how many articles the page listed, and the article
-- the reader opened from the results, if any. Nothing about the reader is kept: no address,
-- cookie or email. A daily job deletes rows older than kb_searches.retention_days (180).

CREATE TABLE kb_searches (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    query text NOT NULL CHECK (query <> ''),
    query_key text NOT NULL,
    results integer NOT NULL CHECK (results >= 0),
    opened_article_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- A purged article leaves the search: it still counts, with nothing opened.
    FOREIGN KEY (workspace_id, opened_article_id)
        REFERENCES kb_articles(workspace_id, id) ON DELETE SET NULL (opened_article_id)
);
-- The weekly report, the per-minute cap and the retention delete all read by time.
CREATE INDEX kb_searches_workspace_created_idx ON kb_searches(workspace_id, created_at);
CREATE INDEX kb_searches_created_idx ON kb_searches(created_at);
CREATE INDEX kb_searches_opened_idx ON kb_searches(workspace_id, opened_article_id)
    WHERE opened_article_id IS NOT NULL;

SELECT secure_workspace_table('kb_searches');
