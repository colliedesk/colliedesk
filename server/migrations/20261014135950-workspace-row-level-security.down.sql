-- Turns row-level security off and removes the app role's grants in this schema. The role itself
-- stays: roles are shared by every database of the cluster and may serve another CollieDesk.

DO $$
DECLARE
    schema_name text := current_schema();
    table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['workspaces', 'agents', 'pairing_links', 'sessions', 'contacts', 'conversations',
        'messages', 'tags', 'conversation_tags', 'attachments', 'time_entries', 'import_items', 'email_channels', 'email_outbox']
    LOOP
        EXECUTE format('DROP POLICY IF EXISTS privileged_access ON %I', table_name);
        EXECUTE format('DROP POLICY IF EXISTS workspace_isolation ON %I', table_name);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', table_name);
    END LOOP;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'colliedesk_app') THEN
        EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I REVOKE ALL ON TABLES FROM colliedesk_app', schema_name);
        EXECUTE format('REVOKE ALL ON ALL TABLES IN SCHEMA %I FROM colliedesk_app', schema_name);
        EXECUTE format('REVOKE USAGE ON SCHEMA %I FROM colliedesk_app', schema_name);
    END IF;
END
$$;
