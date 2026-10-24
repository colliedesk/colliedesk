-- An agent token holds either a tier (read, draft, send, admin), whose permissions are worked out on
-- every call, or scope 'custom' with its own list of permissions (resource:action). The list is
-- checked in Go (auth.ValidPermission), not here, so a new permission needs no migration. A custom
-- token that holds an admin-only permission acts with its creator's role, like an admin token.
ALTER TABLE sessions ADD COLUMN permissions text[];
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_check CHECK (scope IN ('read', 'draft', 'send', 'admin', 'custom'));
ALTER TABLE sessions ADD CONSTRAINT sessions_custom_permissions
    CHECK ((scope = 'custom') = (permissions IS NOT NULL AND cardinality(permissions) > 0));
ALTER TABLE sessions DROP CONSTRAINT sessions_scope_role_ceiling;
ALTER TABLE sessions ADD CONSTRAINT sessions_scope_role_ceiling
    CHECK (scope IS NULL OR scope = 'custom' OR role_ceiling = CASE WHEN scope = 'admin' THEN 'admin' ELSE 'agent' END);
-- A request keeps what its token held when it was filed, so a person deciding it sees that.
ALTER TABLE agent_requests ADD COLUMN token_permissions text[];
ALTER TABLE agent_requests DROP CONSTRAINT agent_requests_token_scope_check;
ALTER TABLE agent_requests ADD CONSTRAINT agent_requests_token_scope_check CHECK (token_scope IN ('read', 'draft', 'send', 'admin', 'custom'));
