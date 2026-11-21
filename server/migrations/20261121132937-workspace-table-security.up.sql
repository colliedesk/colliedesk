-- The table security helpers (secure_workspace_table, secure_server_table, limit_app_role) are
-- defined in 20261014135949-table-security-helpers, so every migration can call them.

-- These tables were meant to refuse the app role a DELETE (history is only added; roles and number
-- series are marked deleted or reset), but their GRANT ran on top of the default privileges.
SELECT limit_app_role('saved_reply_versions', 'SELECT', 'INSERT');
SELECT limit_app_role('roles', 'SELECT', 'INSERT', 'UPDATE');
SELECT limit_app_role('agent_requests', 'SELECT', 'INSERT', 'UPDATE');
SELECT limit_app_role('response_targets', 'SELECT', 'INSERT', 'UPDATE');
SELECT limit_app_role('contact_merges', 'SELECT', 'INSERT', 'UPDATE');
SELECT limit_app_role('conversation_number_series', 'SELECT', 'INSERT', 'UPDATE');
SELECT limit_app_role('conversation_number_formats', 'SELECT', 'INSERT', 'UPDATE');
