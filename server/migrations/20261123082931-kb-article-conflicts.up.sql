-- Out-of-date articles: a reply a team member sent that contradicts a published article flags the
-- article, so an editor sees that the article may no longer be true.
--
-- A trigger on messages queues every public reply a team member writes in kb_conflict_checks. A
-- server worker matches the reply against the published articles and asks the workspace's small
-- model only when one matches closely. The same table counts the checks of the day, which the
-- worker bounds per workspace: a queued row is pending until the worker claims it (checked_at).
-- A "yes" writes kb_article_conflicts, one row per article and reply, until an editor clears it.
--
-- Both tables are agent-side data, so they use secure_workspace_table: the public help center never
-- reads them. An older image ignores the tables; rolling back drops them with the trigger.

CREATE TABLE kb_conflict_checks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    message_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    -- Set when the worker took the reply, whether or not it asked the model.
    checked_at timestamptz,
    FOREIGN KEY (workspace_id, message_id)
        REFERENCES messages(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX kb_conflict_checks_pending_idx ON kb_conflict_checks(workspace_id, id)
    WHERE checked_at IS NULL;
CREATE INDEX kb_conflict_checks_checked_idx ON kb_conflict_checks(workspace_id, checked_at)
    WHERE checked_at IS NOT NULL;

CREATE TABLE kb_article_conflicts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    message_id uuid NOT NULL,
    -- The model's sentence quoting both sides, masked: an email address or a run of 6 or more
    -- digits never reaches this column.
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    -- Who cleared the flag and when; the row stays so the article keeps its history.
    cleared_at timestamptz,
    cleared_by_agent_id uuid,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, article_id, message_id),
    CHECK (cleared_by_agent_id IS NULL OR cleared_at IS NOT NULL),
    FOREIGN KEY (workspace_id, article_id)
        REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, message_id)
        REFERENCES messages(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, cleared_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (cleared_by_agent_id)
);
-- The knowledge base list counts the open conflicts of each article.
CREATE INDEX kb_article_conflicts_open_idx ON kb_article_conflicts(workspace_id, article_id)
    WHERE cleared_at IS NULL;
CREATE INDEX kb_article_conflicts_article_idx
    ON kb_article_conflicts(workspace_id, article_id, created_at DESC);

SELECT secure_workspace_table('kb_conflict_checks');
SELECT secure_workspace_table('kb_article_conflicts');
SELECT limit_app_role('kb_article_conflicts', 'SELECT', 'INSERT', 'UPDATE', 'DELETE');

-- A public reply a team member wrote in Colliedesk. Imported replies (origin), channel notices and
-- the replies intake sends on its own are not the team's words about an article.
CREATE FUNCTION kb_conflict_reply_sent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO kb_conflict_checks(workspace_id, message_id)
        SELECT NEW.workspace_id, NEW.id
        WHERE EXISTS (SELECT 1 FROM workspaces w WHERE w.id = NEW.workspace_id);
    RETURN NULL;
END
$$;
CREATE TRIGGER kb_conflict_reply_sent AFTER INSERT ON messages
    FOR EACH ROW WHEN (NEW.kind = 'outgoing' AND NEW.origin IS NULL
        AND NEW.author_agent_id IS NOT NULL AND NEW.notice_kind IS NULL
        AND NEW.intake_reply_kind IS NULL)
    EXECUTE FUNCTION kb_conflict_reply_sent();
