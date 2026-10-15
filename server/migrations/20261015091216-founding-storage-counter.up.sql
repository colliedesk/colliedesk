-- The Founding plan caps attachment storage and, unlike the trial, counts imported files too. This
-- adds a second maintained number next to counted_storage_bytes: workspaces.stored_storage_bytes is
-- the sum of the size of every attachment of the workspace, imported or not. The same statement
-- triggers keep both numbers right, so each workspaces row is still updated once per statement.
-- Additive: counted_storage_bytes and attachments.counted keep their meaning.
ALTER TABLE workspaces ADD COLUMN stored_storage_bytes bigint NOT NULL DEFAULT 0;

UPDATE workspaces w SET stored_storage_bytes =
    COALESCE((SELECT sum(a.size) FROM attachments a WHERE a.workspace_id = w.id), 0);

CREATE OR REPLACE FUNCTION attachments_count_inserted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes + d.counted_bytes,
                            stored_storage_bytes = w.stored_storage_bytes + d.all_bytes
        FROM (SELECT workspace_id, COALESCE(sum(size) FILTER (WHERE counted), 0) AS counted_bytes, sum(size) AS all_bytes
              FROM added_files GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
    RETURN NULL;
END
$$;

CREATE OR REPLACE FUNCTION attachments_count_deleted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes - d.counted_bytes,
                            stored_storage_bytes = w.stored_storage_bytes - d.all_bytes
        FROM (SELECT workspace_id, COALESCE(sum(size) FILTER (WHERE counted), 0) AS counted_bytes, sum(size) AS all_bytes
              FROM removed_files GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
    RETURN NULL;
END
$$;

CREATE OR REPLACE FUNCTION attachments_count_updated() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes + d.counted_bytes,
                            stored_storage_bytes = w.stored_storage_bytes + d.all_bytes
        FROM (SELECT n.workspace_id,
                     sum(CASE WHEN n.counted THEN n.size ELSE 0 END - CASE WHEN o.counted THEN o.size ELSE 0 END) AS counted_bytes,
                     sum(n.size - o.size) AS all_bytes
              FROM new_files n JOIN old_files o ON o.id = n.id GROUP BY n.workspace_id) d
        WHERE w.id = d.workspace_id AND (d.counted_bytes <> 0 OR d.all_bytes <> 0);
    RETURN NULL;
END
$$;
