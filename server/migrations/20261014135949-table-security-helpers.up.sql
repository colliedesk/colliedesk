-- Helpers that secure a table in one call, so no migration spells out row-level security:
--   SELECT secure_workspace_table('notes');                      -- workspace data
--   SELECT secure_server_table('server_jobs');                   -- server-wide data
--   SELECT limit_app_role('note_versions', 'SELECT', 'INSERT');  -- add-only history
-- The next migration sets up row-level security with them; docs/internals/workspace-isolation.md
-- explains the policies.
--
-- Default privileges give the app role SELECT, INSERT, UPDATE and DELETE on every new table, so a
-- GRANT can only add; limit_app_role revokes first and then grants exactly what it is told.
--
-- An install older than this file got the same functions from
-- 20261121132937-workspace-table-security; CREATE OR REPLACE keeps them as they are there.
-- The function bodies are kept as that migration wrote them, so both installs store the same text.

-- The app role is per database (colliedesk_app_<database>); the workspaces policy names it.
CREATE OR REPLACE FUNCTION app_role() RETURNS text LANGUAGE plpgsql STABLE AS $$
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

-- Sequences the table owns (serial columns); identity columns need no grant.
CREATE OR REPLACE FUNCTION owned_sequences(t regclass) RETURNS SETOF regclass LANGUAGE sql STABLE AS $$
    SELECT d.objid::regclass FROM pg_depend d JOIN pg_class s ON s.oid = d.objid
        WHERE d.refobjid = t AND d.classid = 'pg_class'::regclass AND s.relkind = 'S' AND d.deptype = 'a'
$$;

-- Rows belong to the workspace in app.workspace_id; the owner reaches every row on the privileged
-- path.
-- The privileged policy names the owner, so the app role cannot use it by setting app.privileged.
-- Default privileges cover tables only, so the app role also gets the table's serial sequences.
CREATE OR REPLACE FUNCTION secure_workspace_table(t regclass) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    key text := CASE WHEN t = 'workspaces'::regclass THEN 'id' ELSE 'workspace_id' END;
    sequence regclass;
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
    FOR sequence IN SELECT owned_sequences(t) LOOP
        EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %s TO %I', sequence, app_role());
    END LOOP;
END
$$;

-- Server-wide rows: only the owner on the privileged path, and the app role has no grant at all.
CREATE OR REPLACE FUNCTION secure_server_table(t regclass) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    sequence regclass;
BEGIN
    EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %s FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('REVOKE ALL ON %s FROM %I', t, app_role());
    EXECUTE format($policy$CREATE POLICY privileged_access ON %1$s FOR ALL TO %2$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, t, current_user);
    FOR sequence IN SELECT owned_sequences(t) LOOP
        EXECUTE format('REVOKE ALL ON SEQUENCE %s FROM %I', sequence, app_role());
    END LOOP;
END
$$;

CREATE OR REPLACE FUNCTION limit_app_role(t regclass, VARIADIC privileges text[]) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    privilege text;
BEGIN
    FOREACH privilege IN ARRAY privileges LOOP
        IF privilege IS NULL OR privilege NOT IN ('SELECT', 'INSERT', 'UPDATE', 'DELETE') THEN
            RAISE EXCEPTION 'limit_app_role: % is not one of SELECT, INSERT, UPDATE, DELETE', privilege;
        END IF;
    END LOOP;
    EXECUTE format('REVOKE ALL ON %s FROM %I', t, app_role());
    EXECUTE format('GRANT %s ON %s TO %I', array_to_string(privileges, ', '), t, app_role());
END
$$;

REVOKE EXECUTE ON FUNCTION
    app_role(),
    owned_sequences(regclass),
    secure_workspace_table(regclass),
    secure_server_table(regclass),
    limit_app_role(regclass, text[])
FROM PUBLIC;
