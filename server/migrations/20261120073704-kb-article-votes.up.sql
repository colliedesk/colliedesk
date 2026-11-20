-- "Was this article helpful?" on the public help center (docs/internals/knowledge-base.md, "Votes").
-- One vote per reader and article: the reader is a signed-in contact, or else a first-party cookie
-- id; only its SHA-256 is kept (voter). A second vote changes the first. A "No" may carry one line,
-- "What was missing?", stored masked (no email, no run of 6+ digits).
--
-- A reset by the team closes the current votes (reset_id) and keeps, append-only, the counts the page
-- showed before it, so the history of the counts stays readable.

CREATE TABLE kb_article_vote_resets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    -- What the page counted just before the reset, votes imported with the article included.
    helpful bigint NOT NULL CHECK (helpful >= 0),
    not_helpful bigint NOT NULL CHECK (not_helpful >= 0),
    reset_by_agent_id uuid,
    token_id uuid,
    token_label text,
    agent_request_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    CHECK ((token_id IS NULL) OR (token_label IS NOT NULL)),
    FOREIGN KEY (workspace_id, article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, reset_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (reset_by_agent_id),
    FOREIGN KEY (workspace_id, token_id) REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (token_id),
    FOREIGN KEY (workspace_id, agent_request_id) REFERENCES agent_requests(workspace_id, id) ON DELETE SET NULL (agent_request_id)
);
CREATE INDEX kb_article_vote_resets_article_idx ON kb_article_vote_resets(workspace_id, article_id, created_at);

CREATE TABLE kb_article_votes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    voter bytea NOT NULL CHECK (octet_length(voter) = 32),
    -- The signed-in contact who voted; null for a reader the help center does not know.
    contact_id uuid,
    helpful boolean NOT NULL,
    -- "What was missing?", masked; only a "No" has one.
    missing text CHECK (char_length(missing) BETWEEN 1 AND 500),
    reset_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (missing IS NULL OR NOT helpful),
    FOREIGN KEY (workspace_id, article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, contact_id) REFERENCES contacts(workspace_id, id) ON DELETE SET NULL (contact_id),
    FOREIGN KEY (workspace_id, reset_id) REFERENCES kb_article_vote_resets(workspace_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX kb_article_votes_voter_idx ON kb_article_votes(workspace_id, article_id, voter) WHERE reset_id IS NULL;
CREATE INDEX kb_article_votes_article_idx ON kb_article_votes(workspace_id, article_id, updated_at DESC);

-- Grows with every vote and reset, so a cached article page knows its count is stale without the
-- knowledge base's content_version moving.
ALTER TABLE help_centers ADD COLUMN votes_version bigint NOT NULL DEFAULT 0 CHECK (votes_version >= 0);

DO $$
DECLARE
    app_role text;
    table_name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH table_name IN ARRAY ARRAY['kb_article_vote_resets', 'kb_article_votes'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %2$I FOR ALL TO %1$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role, table_name);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %2$I FOR ALL TO %1$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user, table_name);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', table_name, app_role);
    END LOOP;
    -- A reset is history: written and read, never changed (it goes by cascade, which runs as the owner).
    EXECUTE format('REVOKE UPDATE, DELETE ON kb_article_vote_resets FROM %I', app_role);
END
$$;
