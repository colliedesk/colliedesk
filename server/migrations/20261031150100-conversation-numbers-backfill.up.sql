-- The conversations there were before numbering (20261031150000) are numbered in the default series, 1 on
-- in the order they were opened. Its own transaction: conversations stay readable meanwhile, and only a
-- change to one of these rows waits for it (about 40 to 70 s for a million conversations; see
-- docs/self-host/upgrade.md).
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    WITH numbered AS (
        SELECT c.workspace_id, c.id, row_number() OVER (PARTITION BY c.workspace_id ORDER BY c.created_at, c.id) AS n,
            extract(year FROM c.created_at AT TIME ZONE w.timezone)::integer AS year
        FROM conversations c JOIN workspaces w ON w.id = c.workspace_id WHERE c.number IS NULL)
    UPDATE conversations c SET number = numbered.n, number_year = numbered.year, display_ref = '#' || numbered.n, number_format = '#{n}'
        FROM numbered WHERE c.workspace_id = numbered.workspace_id AND c.id = numbered.id;
    PERFORM set_config('app.privileged', 'off', true);
END
$$;
