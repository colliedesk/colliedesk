-- One call secures a new table, so a migration never spells out row-level security again:
--   SELECT secure_workspace_table('notes');               -- workspace data
--   SELECT secure_server_table('server_jobs');            -- server-wide data, privileged path only
--   SELECT limit_app_role('note_versions', 'SELECT', 'INSERT');  -- add-only history
-- Default privileges give the app role SELECT, INSERT, UPDATE and DELETE on every new table, so a
-- GRANT can only add; limit_app_role revokes first and then grants exactly what it is told.

-- The app role is per database (colliedesk_app_<database>); the workspaces policy names it.
CREATE FUNCTION app_role() RETURNS text LANGUAGE plpgsql STABLE AS $$
DECLARE
    role text;
BEGIN
    SELECT roles[1]::text INTO role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    RETURN role;
END
$$;

-- Rows belong to the workspace in app.workspace_id; the owner reaches every row on the privileged path.
-- The privileged policy names the owner, so the app role cannot use it by setting app.privileged.
CREATE FUNCTION secure_workspace_table(t regclass) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    key text := CASE WHEN t = 'workspaces'::regclass THEN 'id' ELSE 'workspace_id' END;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = t AND attname = key AND NOT attisdropped) THEN
        RAISE EXCEPTION '% has no % column; use secure_server_table for server-wide data', t, key;
    END IF;
    EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %s FORCE ROW LEVEL SECURITY', t);
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$s FOR ALL TO %2$I
        USING (%3$I = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (%3$I = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$,
        t, app_role(), key);
    EXECUTE format($policy$CREATE POLICY privileged_access ON %1$s FOR ALL TO %2$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, t, current_user);
END
$$;

-- Server-wide rows: only the owner on the privileged path, and the app role has no grant at all.
CREATE FUNCTION secure_server_table(t regclass) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    sequence regclass;
BEGIN
    EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %s FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('REVOKE ALL ON %s FROM %I', t, app_role());
    EXECUTE format($policy$CREATE POLICY privileged_access ON %1$s FOR ALL TO %2$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, t, current_user);
    FOR sequence IN SELECT d.objid::regclass FROM pg_depend d JOIN pg_class s ON s.oid = d.objid
        WHERE d.refobjid = t AND d.classid = 'pg_class'::regclass AND s.relkind = 'S' LOOP
        EXECUTE format('REVOKE ALL ON SEQUENCE %s FROM %I', sequence, app_role());
    END LOOP;
END
$$;

CREATE FUNCTION limit_app_role(t regclass, VARIADIC privileges text[]) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    privilege text;
BEGIN
    FOREACH privilege IN ARRAY privileges LOOP
        IF privilege NOT IN ('SELECT', 'INSERT', 'UPDATE', 'DELETE') THEN
            RAISE EXCEPTION 'limit_app_role: % is not one of SELECT, INSERT, UPDATE, DELETE', privilege;
        END IF;
    END LOOP;
    EXECUTE format('REVOKE ALL ON %s FROM %I', t, app_role());
    EXECUTE format('GRANT %s ON %s TO %I', array_to_string(privileges, ', '), t, app_role());
END
$$;

REVOKE EXECUTE ON FUNCTION app_role(), secure_workspace_table(regclass), secure_server_table(regclass),
    limit_app_role(regclass, text[]) FROM PUBLIC;
