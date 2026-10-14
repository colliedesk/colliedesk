-- Row-level security is the database-enforced second layer of workspace isolation.
-- Request handling switches to colliedesk_app inside each transaction and sets app.workspace_id
-- for that transaction only; a missing setting matches no row. The owner (the role running this
-- migration, the DATABASE_URL role) sees rows only in transactions the server marks with
-- app.privileged; a superuser owner bypasses row-level security altogether. Any other role has no
-- policy and sees nothing.
-- The settings are read through scalar subqueries so PostgreSQL evaluates them once per query
-- (an InitPlan) instead of once per row.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'colliedesk_app') THEN
        BEGIN
            CREATE ROLE colliedesk_app NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT;
        EXCEPTION
            WHEN duplicate_object OR unique_violation THEN NULL; -- created by a concurrent migration
            WHEN insufficient_privilege THEN
                RAISE EXCEPTION 'cannot create role colliedesk_app: %', SQLERRM
                    USING HINT = 'As a superuser run: CREATE ROLE colliedesk_app NOLOGIN; GRANT colliedesk_app TO '
                        || quote_ident(current_user) || '; then run colliedesk migrate again.';
        END;
    END IF;
    IF NOT pg_has_role(current_user, 'colliedesk_app', 'SET') THEN
        EXECUTE format('GRANT colliedesk_app TO %I', current_user);
    END IF;
END
$$;

DO $$
DECLARE
    schema_name text := current_schema();
    table_name text;
    key_column text;
BEGIN
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO colliedesk_app', schema_name);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO colliedesk_app', schema_name);
    EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO colliedesk_app', schema_name);
    REVOKE ALL ON schema_migrations FROM colliedesk_app;

    FOREACH table_name IN ARRAY ARRAY['workspaces', 'agents', 'pairing_links', 'sessions', 'contacts', 'conversations',
        'messages', 'tags', 'conversation_tags', 'attachments', 'time_entries', 'import_items', 'email_channels', 'email_outbox']
    LOOP
        key_column := CASE WHEN table_name = 'workspaces' THEN 'id' ELSE 'workspace_id' END;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$I FOR ALL TO colliedesk_app
            USING (%2$I = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (%2$I = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$,
            table_name, key_column);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$,
            table_name, current_user);
    END LOOP;
END
$$;
