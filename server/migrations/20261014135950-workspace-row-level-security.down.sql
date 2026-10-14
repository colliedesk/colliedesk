-- Turns row-level security off and removes the app role's grants in this schema. The role itself
-- stays because other schemas of this database may still use it; drop it by hand once no schema does.

DO $$
DECLARE
    schema_name text := current_schema();
    app_role text;
    table_name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = schema_name AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    FOREACH table_name IN ARRAY ARRAY['workspaces', 'agents', 'pairing_links', 'sessions', 'contacts', 'conversations',
        'messages', 'tags', 'conversation_tags', 'attachments', 'time_entries', 'import_items', 'email_channels', 'email_outbox']
    LOOP
        EXECUTE format('DROP POLICY IF EXISTS privileged_access ON %I', table_name);
        EXECUTE format('DROP POLICY IF EXISTS workspace_isolation ON %I', table_name);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', table_name);
    END LOOP;
    IF app_role IS NOT NULL THEN
        EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I REVOKE ALL ON TABLES FROM %I', schema_name, app_role);
        EXECUTE format('REVOKE ALL ON ALL TABLES IN SCHEMA %I FROM %I', schema_name, app_role);
        EXECUTE format('REVOKE USAGE ON SCHEMA %I FROM %I', schema_name, app_role);
    END IF;
END
$$;
