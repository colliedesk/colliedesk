-- Row-level security is the database-enforced second layer of workspace isolation.
-- Request handling switches to the app role inside each transaction and sets app.workspace_id for
-- that transaction only; a missing setting matches no row. The owner (the role running this
-- migration, the DATABASE_URL role) sees rows only in transactions the server marks with
-- app.privileged; a superuser owner bypasses row-level security altogether. Any other role has no
-- policy and sees nothing.
-- The app role is per database (colliedesk_app_<database>), so the owner of one install on a shared
-- cluster cannot use it to act on another install's database. The server reads the name back from
-- the workspace_isolation policy.
-- The settings are read through scalar subqueries so PostgreSQL evaluates them once per query
-- (an InitPlan) instead of once per row.

-- Create the app role, give it this schema's tables, and secure workspaces by hand: app_role(),
-- which every other table's helper call needs, reads the role's name from this policy.
DO $$
DECLARE
    app_role text := 'colliedesk_app_' || current_database();
    schema_name text := current_schema();
BEGIN
    -- Role names stop at 63 bytes, not characters. Longer names are cut on a character boundary and
    -- get an md5 suffix, so two databases that differ only past that point keep distinct roles.
    IF octet_length(app_role) > 63 THEN
        WHILE octet_length(app_role) > 50 LOOP
            app_role := left(app_role, -1);
        END LOOP;
        app_role := app_role || '_' || left(md5(current_database()), 12);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = app_role) THEN
        BEGIN
            EXECUTE format(
                'CREATE ROLE %I NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT',
                app_role);
        EXCEPTION
            -- created by a concurrent migration
            WHEN duplicate_object OR unique_violation THEN NULL;
            WHEN insufficient_privilege THEN
                RAISE EXCEPTION 'cannot create role %: %. As a superuser run: '
                        'CREATE ROLE % NOLOGIN; '
                        'GRANT % TO %; then run colliedesk migrate again',
                    app_role, SQLERRM, quote_ident(app_role), quote_ident(app_role),
                    quote_ident(current_user)
                    USING ERRCODE = 'insufficient_privilege';
        END;
    END IF;
    IF NOT pg_has_role(current_user, app_role, 'SET') THEN
        BEGIN
            EXECUTE format('GRANT %I TO %I', app_role, current_user);
        EXCEPTION
            WHEN insufficient_privilege THEN
                RAISE EXCEPTION 'cannot grant role % to %: %. As a superuser run: GRANT % TO %; '
                        'then run colliedesk migrate again',
                    app_role, current_user, SQLERRM,
                    quote_ident(app_role), quote_ident(current_user)
                    USING ERRCODE = 'insufficient_privilege';
        END;
    END IF;

    EXECUTE format('GRANT USAGE ON SCHEMA %I TO %I', schema_name, app_role);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO %I',
        schema_name, app_role);
    EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I '
        'GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %I', schema_name, app_role);
    EXECUTE format('REVOKE ALL ON schema_migrations FROM %I', app_role);

    ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
    ALTER TABLE workspaces FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON workspaces FOR ALL TO %I
        USING (id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (id =
            (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$,
        app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON workspaces FOR ALL TO %I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$,
        current_user);
END
$$;

SELECT secure_workspace_table('agents');
SELECT secure_workspace_table('pairing_links');
SELECT secure_workspace_table('sessions');
SELECT secure_workspace_table('contacts');
SELECT secure_workspace_table('conversations');
SELECT secure_workspace_table('messages');
SELECT secure_workspace_table('tags');
SELECT secure_workspace_table('conversation_tags');
SELECT secure_workspace_table('attachments');
SELECT secure_workspace_table('time_entries');
SELECT secure_workspace_table('import_items');
SELECT secure_workspace_table('email_channels');
SELECT secure_workspace_table('email_outbox');
