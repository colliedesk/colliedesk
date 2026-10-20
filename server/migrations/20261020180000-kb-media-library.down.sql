-- Rolling back would drop the videos, the trash of the library, and the names and alt texts people
-- wrote, so it refuses while any of them exists. Thumbnails are derived: their files stay in
-- attachment storage, and the counters stop counting them.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM kb_media WHERE kind = 'video' OR deleted_at IS NOT NULL OR name <> '' OR alt <> '' OR kept) THEN
        RAISE EXCEPTION 'knowledge base media rollback refused: % videos, % items in the trash and % named, described or kept items would be lost',
            (SELECT count(*) FROM kb_media WHERE kind = 'video'), (SELECT count(*) FROM kb_media WHERE deleted_at IS NOT NULL),
            (SELECT count(*) FROM kb_media WHERE name <> '' OR alt <> '' OR kept);
    END IF;
    UPDATE workspaces w SET stored_storage_bytes = w.stored_storage_bytes - d.thumb_bytes
        FROM (SELECT workspace_id, sum(thumb_size) AS thumb_bytes FROM kb_media WHERE thumb_size IS NOT NULL GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
END $$;

DROP TRIGGER kb_media_count_inserted ON kb_media;
DROP TRIGGER kb_media_count_deleted ON kb_media;
DROP TRIGGER kb_media_count_updated ON kb_media;
DROP FUNCTION kb_media_count_inserted();
DROP FUNCTION kb_media_count_deleted();
DROP FUNCTION kb_media_count_updated();

DROP INDEX kb_article_media_current_idx;
ALTER TABLE kb_article_media DROP COLUMN current;
DROP INDEX kb_media_library_idx;
DROP INDEX kb_media_trash_idx;
DROP INDEX kb_media_thumb_key_idx;
DROP INDEX kb_media_poster_key_idx;
DROP INDEX kb_media_backfill_idx;
ALTER TABLE kb_media
    DROP CONSTRAINT kb_media_content_type_check,
    DROP CONSTRAINT kb_media_kind_check_type,
    DROP COLUMN kind, DROP COLUMN name, DROP COLUMN alt, DROP COLUMN width, DROP COLUMN height,
    DROP COLUMN duration_ms, DROP COLUMN codecs, DROP COLUMN thumb_storage_key, DROP COLUMN thumb_size,
    DROP COLUMN poster_storage_key, DROP COLUMN poster_size, DROP COLUMN inspected_at, DROP COLUMN kept,
    DROP COLUMN deleted_at, DROP COLUMN deleted_by_agent_id, DROP COLUMN updated_at, DROP COLUMN updated_by_agent_id,
    ADD CONSTRAINT kb_images_content_type_check CHECK (content_type IN ('image/png', 'image/jpeg', 'image/gif', 'image/webp'));

ALTER INDEX kb_article_media_media_idx RENAME TO kb_article_images_image_idx;
ALTER TABLE kb_article_media RENAME CONSTRAINT kb_article_media_workspace_id_media_id_fkey TO kb_article_images_workspace_id_image_id_fkey;
ALTER TABLE kb_article_media RENAME CONSTRAINT kb_article_media_workspace_id_article_id_fkey TO kb_article_images_workspace_id_article_id_fkey;
ALTER TABLE kb_article_media RENAME CONSTRAINT kb_article_media_workspace_id_fkey TO kb_article_images_workspace_id_fkey;
ALTER TABLE kb_article_media RENAME CONSTRAINT kb_article_media_pkey TO kb_article_images_pkey;
ALTER TABLE kb_article_media RENAME COLUMN media_id TO image_id;
ALTER TABLE kb_article_media RENAME TO kb_article_images;

ALTER INDEX kb_media_storage_key_idx RENAME TO kb_images_storage_key_idx;
ALTER TABLE kb_media RENAME CONSTRAINT kb_media_workspace_id_created_by_agent_id_fkey TO kb_images_workspace_id_created_by_agent_id_fkey;
ALTER TABLE kb_media RENAME CONSTRAINT kb_media_workspace_id_fkey TO kb_images_workspace_id_fkey;
ALTER TABLE kb_media RENAME CONSTRAINT kb_media_size_check TO kb_images_size_check;
ALTER TABLE kb_media RENAME CONSTRAINT kb_media_sha256_check TO kb_images_sha256_check;
ALTER TABLE kb_media RENAME CONSTRAINT kb_media_workspace_id_sha256_key TO kb_images_workspace_id_sha256_key;
ALTER TABLE kb_media RENAME CONSTRAINT kb_media_workspace_id_id_key TO kb_images_workspace_id_id_key;
ALTER TABLE kb_media RENAME CONSTRAINT kb_media_pkey TO kb_images_pkey;
ALTER TABLE kb_media RENAME TO kb_images;

CREATE FUNCTION kb_images_count_inserted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes + d.counted_bytes,
                            stored_storage_bytes = w.stored_storage_bytes + d.all_bytes
        FROM (SELECT workspace_id, COALESCE(sum(size) FILTER (WHERE counted), 0) AS counted_bytes, sum(size) AS all_bytes
              FROM added_images GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
    RETURN NULL;
END
$$;

CREATE FUNCTION kb_images_count_deleted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes - d.counted_bytes,
                            stored_storage_bytes = w.stored_storage_bytes - d.all_bytes
        FROM (SELECT workspace_id, COALESCE(sum(size) FILTER (WHERE counted), 0) AS counted_bytes, sum(size) AS all_bytes
              FROM removed_images GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
    RETURN NULL;
END
$$;

CREATE TRIGGER kb_images_count_inserted AFTER INSERT ON kb_images
    REFERENCING NEW TABLE AS added_images FOR EACH STATEMENT EXECUTE FUNCTION kb_images_count_inserted();
CREATE TRIGGER kb_images_count_deleted AFTER DELETE ON kb_images
    REFERENCING OLD TABLE AS removed_images FOR EACH STATEMENT EXECUTE FUNCTION kb_images_count_deleted();
