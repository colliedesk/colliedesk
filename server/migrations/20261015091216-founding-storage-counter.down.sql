-- Restore the trigger functions of the storage-counter migration, then drop the second number.
CREATE OR REPLACE FUNCTION attachments_count_inserted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes + d.bytes
        FROM (SELECT workspace_id, sum(size) AS bytes FROM added_files WHERE counted GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
    RETURN NULL;
END
$$;

CREATE OR REPLACE FUNCTION attachments_count_deleted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes - d.bytes
        FROM (SELECT workspace_id, sum(size) AS bytes FROM removed_files WHERE counted GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
    RETURN NULL;
END
$$;

CREATE OR REPLACE FUNCTION attachments_count_updated() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes + d.bytes
        FROM (SELECT n.workspace_id,
                     sum(CASE WHEN n.counted THEN n.size ELSE 0 END - CASE WHEN o.counted THEN o.size ELSE 0 END) AS bytes
              FROM new_files n JOIN old_files o ON o.id = n.id GROUP BY n.workspace_id) d
        WHERE w.id = d.workspace_id AND d.bytes <> 0;
    RETURN NULL;
END
$$;

ALTER TABLE workspaces DROP COLUMN IF EXISTS stored_storage_bytes;
