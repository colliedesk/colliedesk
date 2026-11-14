-- An AI app connected with OAuth (Claude, ChatGPT, Codex, Cursor, Claude Code) holds a grant: a sessions
-- row with scope 'oauth' that acts as its person with everything they hold, read from their roles on
-- every call. It is a token (an agent token's machinery: label, last use, sliding expiry, revoke, message
-- attribution), so every person-only route refuses it.
--
-- The grant's id never changes: messages.agent_token_id points at it. Its credentials rotate in
-- oauth_tokens: the one-time authorization code, the short-lived access tokens and the refresh tokens,
-- stored only as hashes in a domain of their own (an older image hashes every bearer plainly and finds
-- none of them). sessions.token_hash of a grant is the hash of a random value nobody ever receives.
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_check CHECK (scope IN ('read', 'draft', 'send', 'admin', 'custom', 'role', 'oauth'));
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_role_ceiling;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_role_ceiling
    CHECK (scope IS NULL OR scope IN ('custom', 'role', 'oauth') OR role_ceiling = CASE WHEN scope = 'admin' THEN 'admin' ELSE 'agent' END);
-- The app that holds the grant (its OAuth client id and the redirect the person allowed), the browser the
-- person allowed it from, the resource its access tokens are bound to (RFC 8707: this server's /mcp), and
-- when the app first exchanged its code: NULL while the code waits, so the grant lists nowhere until then.
ALTER TABLE sessions ADD COLUMN oauth_client_id text;
ALTER TABLE sessions ADD COLUMN oauth_redirect_uri text;
ALTER TABLE sessions ADD COLUMN oauth_resource text;
ALTER TABLE sessions ADD COLUMN oauth_connected_at timestamptz;
ALTER TABLE sessions ADD COLUMN user_agent text;
ALTER TABLE sessions ADD CONSTRAINT sessions_oauth_grant
    CHECK (scope IS DISTINCT FROM 'oauth' OR (oauth_client_id IS NOT NULL AND oauth_redirect_uri IS NOT NULL AND oauth_resource IS NOT NULL AND permissions IS NULL));

-- kind: code (one use, PKCE challenge kept with it), access (an hour), refresh (rotated on each use; the one
-- it replaced still works for a short grace, so a client that lost the answer to a refresh can retry).
CREATE TABLE oauth_tokens (
    workspace_id uuid NOT NULL,
    session_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('code', 'access', 'refresh')),
    hash bytea NOT NULL UNIQUE,
    code_challenge text,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    replaced_at timestamptz,
    FOREIGN KEY (workspace_id, session_id) REFERENCES sessions(workspace_id, id) ON DELETE CASCADE,
    CHECK ((kind = 'code') = (code_challenge IS NOT NULL))
);
CREATE INDEX oauth_tokens_session_idx ON oauth_tokens(workspace_id, session_id);

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE oauth_tokens ENABLE ROW LEVEL SECURITY;
    ALTER TABLE oauth_tokens FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON oauth_tokens FOR ALL TO %I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON oauth_tokens FOR ALL TO %I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON oauth_tokens TO %I', app_role);
END
$$;
