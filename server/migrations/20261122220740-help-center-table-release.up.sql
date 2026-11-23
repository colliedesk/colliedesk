-- release_help_center_table undoes secure_help_center_table, for a down migration: it drops the
-- reader's help_center_isolation policy, gives workspace_isolation back to the app role alone and
-- takes every grant on the table and its sequences from the reader (a table-level REVOKE ALL also
-- takes column grants). The table is then as secure_workspace_table left it.
CREATE FUNCTION release_help_center_table(t regclass) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    sequence regclass;
BEGIN
    EXECUTE format('DROP POLICY help_center_isolation ON %s', t);
    EXECUTE format('ALTER POLICY workspace_isolation ON %s TO %I', t, app_role());
    EXECUTE format('REVOKE ALL ON %s FROM %I', t, reader_role());
    FOR sequence IN SELECT owned_sequences(t) LOOP
        EXECUTE format('REVOKE ALL ON SEQUENCE %s FROM %I', sequence, reader_role());
    END LOOP;
END
$$;

REVOKE EXECUTE ON FUNCTION release_help_center_table(regclass) FROM PUBLIC;
