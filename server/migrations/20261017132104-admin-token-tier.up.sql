-- A fourth agent token tier, admin, above send. An admin token is stored with the admin role ceiling
-- (it acts with its creator's role, checked on every call); every other tier keeps the agent ceiling.
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_check CHECK (scope IN ('read', 'draft', 'send', 'admin'));
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_agent_ceiling;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_role_ceiling
    CHECK (scope IS NULL OR role_ceiling = CASE WHEN scope = 'admin' THEN 'admin' ELSE 'agent' END);
-- An admin token files agent requests too (every tier does on approval-required routes).
ALTER TABLE agent_requests DROP CONSTRAINT agent_requests_token_scope_check;
ALTER TABLE agent_requests ADD CONSTRAINT agent_requests_token_scope_check CHECK (token_scope IN ('read', 'draft', 'send', 'admin'));
