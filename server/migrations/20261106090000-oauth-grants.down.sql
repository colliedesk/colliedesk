-- The previous release knows no OAuth grant. Its rows stay, because a note an AI app wrote names its grant
-- (messages.agent_token_id): each becomes what that release reads as a dead token, a revoked legacy read
-- token whose hash no bearer matches. Their credentials go with oauth_tokens.
SELECT set_config('app.privileged', 'on', true);
UPDATE sessions SET scope = 'read', legacy = true, role_id = NULL, revoked_at = COALESCE(revoked_at, now()), role_ceiling = 'agent'
WHERE scope = 'oauth';
DROP TABLE oauth_tokens;
ALTER TABLE sessions DROP CONSTRAINT sessions_oauth_grant;
ALTER TABLE sessions DROP COLUMN user_agent;
ALTER TABLE sessions DROP COLUMN oauth_connected_at;
ALTER TABLE sessions DROP COLUMN oauth_resource;
ALTER TABLE sessions DROP COLUMN oauth_redirect_uri;
ALTER TABLE sessions DROP COLUMN oauth_client_id;
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_role_ceiling;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_role_ceiling
    CHECK (scope IS NULL OR scope IN ('custom', 'role') OR role_ceiling = CASE WHEN scope = 'admin' THEN 'admin' ELSE 'agent' END);
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_check CHECK (scope IN ('read', 'draft', 'send', 'admin', 'custom', 'role'));
