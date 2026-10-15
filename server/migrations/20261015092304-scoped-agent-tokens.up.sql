-- An agent token is a session row with a tier. NULL scope is a full session, exactly as before.
-- Sessions already carry the label and last_seen_at that the token list shows.
-- A token always has the agent role ceiling, so no tier can reach admin-only permissions.
ALTER TABLE sessions ADD COLUMN scope text;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_check CHECK (scope IN ('read', 'draft', 'send'));
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_agent_ceiling CHECK (scope IS NULL OR role_ceiling = 'agent');
