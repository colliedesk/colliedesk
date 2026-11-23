-- Mail a member forwards to the workspace's knowledge base address becomes a draft article. This
-- table remembers which Message-ID made which draft, so the same mail delivered twice (a
-- forwarder retries, or a member forwards and also copies kb@) adds one article. Deleting the
-- article for good forgets the mail, so forwarding it again makes a new draft.
--
-- An older image ignores the table; rolling back drops it.

CREATE TABLE kb_email_articles (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- The Message-ID header without angle brackets, as the email channel stores it.
    message_id text NOT NULL CHECK (char_length(message_id) BETWEEN 1 AND 998),
    article_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (workspace_id, message_id),
    FOREIGN KEY (workspace_id, article_id) REFERENCES kb_articles(workspace_id, id)
        ON DELETE CASCADE
);
CREATE INDEX kb_email_articles_article_idx ON kb_email_articles(workspace_id, article_id);

SELECT secure_workspace_table('kb_email_articles');
