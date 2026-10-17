-- An agent request is a call an agent token made that a person must approve first: an action above
-- the token's tier, an action that needs approval for every tier (new credentials, money, data
-- leaving), or a reply drafted by a token that may not send. The row keeps the exact call (method,
-- route, concrete path, JSON body) so approving replays exactly what was asked, as the approving
-- person. Older images ignore this table.
CREATE TABLE agent_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- The token (a sessions row with a scope) and its owner. Both outlive their removal as labels.
    token_id uuid,
    token_label text NOT NULL,
    token_scope text NOT NULL CHECK (token_scope IN ('read', 'draft', 'send')),
    requested_by_agent_id uuid,
    kind text NOT NULL CHECK (kind IN ('reply_draft', 'action')),
    method text NOT NULL CHECK (method IN ('GET', 'POST', 'PUT', 'PATCH', 'DELETE')),
    route text NOT NULL CHECK (char_length(route) BETWEEN 1 AND 200),
    path text NOT NULL CHECK (char_length(path) BETWEEN 1 AND 500),
    -- The call as the token sent it. A reply draft a person edited keeps the agent's version here and
    -- the edited call in edited_body, which is what ran. Both are cleared 90 days after the decision.
    body jsonb CHECK (body IS NULL OR octet_length(body::text) <= 65536),
    edited_body jsonb CHECK (edited_body IS NULL OR octet_length(edited_body::text) <= 65536),
    summary text NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 500),
    -- The conversation a request acts on, so the conversation view can show a reply draft.
    conversation_id uuid,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'failed', 'rejected', 'cancelled', 'expired')),
    decided_by_agent_id uuid,
    decided_at timestamptz,
    reason text CHECK (reason IS NULL OR char_length(reason) <= 1000),
    -- What the approved call answered. A credential or a link is shown once to the approver and
    -- never stored; result_note says so.
    result_status integer,
    result jsonb CHECK (result IS NULL OR octet_length(result::text) <= 65536),
    result_note text,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    UNIQUE (workspace_id, id),
    CHECK ((status = 'pending') = (decided_at IS NULL)),
    FOREIGN KEY (workspace_id, token_id) REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (token_id),
    FOREIGN KEY (workspace_id, requested_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (requested_by_agent_id),
    FOREIGN KEY (workspace_id, decided_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (decided_by_agent_id),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (conversation_id)
);

-- The open tab polls the pending list; the list shows pending first, newest first.
CREATE INDEX agent_requests_workspace_status_idx ON agent_requests(workspace_id, status, created_at DESC);
-- The hourly filing limit counts one token's recent requests; the pending cap counts its waiting ones.
CREATE INDEX agent_requests_token_idx ON agent_requests(workspace_id, token_id, created_at DESC);
CREATE INDEX agent_requests_token_pending_idx ON agent_requests(workspace_id, token_id) WHERE status = 'pending';
-- A conversation shows its reply drafts; the 90-day prune finds old decided rows.
CREATE INDEX agent_requests_conversation_idx ON agent_requests(workspace_id, conversation_id) WHERE conversation_id IS NOT NULL;
CREATE INDEX agent_requests_decided_idx ON agent_requests(decided_at) WHERE status <> 'pending' AND (body IS NOT NULL OR edited_body IS NOT NULL OR result IS NOT NULL);

-- Same isolation as saved_replies.
DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE agent_requests ENABLE ROW LEVEL SECURITY;
    ALTER TABLE agent_requests FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON agent_requests FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON agent_requests FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE ON agent_requests TO %I', app_role);
END
$$;

-- A message sent by approving a request names it, so the timeline can say who asked and who approved.
-- The key is added NOT VALID (the column is new, so every existing row is NULL); the next migration
-- validates it in a transaction of its own.
ALTER TABLE messages ADD COLUMN agent_request_id uuid;
ALTER TABLE messages ADD CONSTRAINT messages_agent_request_fkey
    FOREIGN KEY (workspace_id, agent_request_id) REFERENCES agent_requests(workspace_id, id) ON DELETE SET NULL (agent_request_id) NOT VALID;
