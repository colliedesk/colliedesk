-- The storage cap and GET /billing read one maintained number instead of summing every attachment
-- of the workspace. attachments.counted is decided when a file is stored: false for files of
-- imported messages, which never count. Triggers keep workspaces.counted_storage_bytes equal to the
-- sum of counted file sizes for every way a file is added, resized or removed, including code that
-- predates this migration, so rolling back to an older image keeps the number right.
ALTER TABLE workspaces ADD COLUMN counted_storage_bytes bigint NOT NULL DEFAULT 0;
ALTER TABLE attachments ADD COLUMN counted boolean NOT NULL DEFAULT true;

UPDATE attachments a SET counted = false FROM messages m
    WHERE m.workspace_id = a.workspace_id AND m.id = a.message_id AND m.origin IS NOT NULL;
UPDATE workspaces w SET counted_storage_bytes =
    COALESCE((SELECT sum(a.size) FROM attachments a WHERE a.workspace_id = w.id AND a.counted), 0);

CREATE FUNCTION attachments_set_counted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.counted := NOT EXISTS (SELECT 1 FROM messages m
        WHERE m.workspace_id = NEW.workspace_id AND m.id = NEW.message_id AND m.origin IS NOT NULL);
    RETURN NEW;
END
$$;

-- Statement-level triggers with transition tables: one workspaces update per statement and workspace,
-- however many files the statement touches (a row-level trigger would rewrite the workspace row once
-- per file and slow a bulk insert quadratically).
CREATE FUNCTION attachments_count_inserted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes + d.bytes
        FROM (SELECT workspace_id, sum(size) AS bytes FROM added_files WHERE counted GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
    RETURN NULL;
END
$$;

CREATE FUNCTION attachments_count_deleted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes - d.bytes
        FROM (SELECT workspace_id, sum(size) AS bytes FROM removed_files WHERE counted GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
    RETURN NULL;
END
$$;

CREATE FUNCTION attachments_count_updated() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes + d.bytes
        FROM (SELECT n.workspace_id,
                     sum(CASE WHEN n.counted THEN n.size ELSE 0 END - CASE WHEN o.counted THEN o.size ELSE 0 END) AS bytes
              FROM new_files n JOIN old_files o ON o.id = n.id GROUP BY n.workspace_id) d
        WHERE w.id = d.workspace_id AND d.bytes <> 0;
    RETURN NULL;
END
$$;

CREATE TRIGGER attachments_set_counted BEFORE INSERT ON attachments
    FOR EACH ROW EXECUTE FUNCTION attachments_set_counted();
CREATE TRIGGER attachments_count_inserted AFTER INSERT ON attachments
    REFERENCING NEW TABLE AS added_files FOR EACH STATEMENT EXECUTE FUNCTION attachments_count_inserted();
CREATE TRIGGER attachments_count_deleted AFTER DELETE ON attachments
    REFERENCING OLD TABLE AS removed_files FOR EACH STATEMENT EXECUTE FUNCTION attachments_count_deleted();
CREATE TRIGGER attachments_count_updated AFTER UPDATE ON attachments
    REFERENCING OLD TABLE AS old_files NEW TABLE AS new_files FOR EACH STATEMENT EXECUTE FUNCTION attachments_count_updated();
