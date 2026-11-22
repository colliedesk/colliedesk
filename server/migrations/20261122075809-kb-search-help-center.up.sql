-- A help center search keeps the help center that served it. Searches kept before belong to the
-- workspace's primary help center, made now when the workspace has none yet. A search written
-- without a help center goes to the primary, as the other help center tables do.
ALTER TABLE kb_searches ADD COLUMN help_center_id uuid;
UPDATE kb_searches s SET help_center_id = ensure_primary_help_center(s.workspace_id);
ALTER TABLE kb_searches ALTER COLUMN help_center_id SET NOT NULL,
    ADD CONSTRAINT kb_searches_help_center_id_fkey
        FOREIGN KEY (workspace_id, help_center_id)
        REFERENCES help_centers (workspace_id, id) ON DELETE CASCADE;
CREATE TRIGGER kb_searches_on_primary BEFORE INSERT ON kb_searches
    FOR EACH ROW EXECUTE FUNCTION help_center_row_on_primary();
