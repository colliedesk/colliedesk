-- The knowledge base media library: images and videos in one table, renamed from kb_images, with
-- what the library shows and edits (name, alt text or title, size in pixels, length, codecs), a
-- thumbnail, a video's poster, a "kept" mark and a trash.
--
-- kept: the purge never deletes a kept item. Uploading in the library or editing its name or alt
-- keeps it; an image pasted into the editor is not kept, so it goes 30 days after its last upload
-- when no article ever saved it (as before).
--
-- kb_article_media.current is true while the current text of the article (or one of its
-- translations) names the media; the other rows are older versions only. "Not used" means no live
-- article has a current link. The backfill looks for the image path the API writes, as the help
-- center's image route does.
--
-- Thumbnails count only toward stored_storage_bytes (disk the server holds); a video's poster counts
-- toward the plan like the original bytes. inspected_at is set once the server read the size in pixels
-- and made the thumbnail, so the daily backfill takes each older row once.
ALTER TABLE kb_images RENAME TO kb_media;
ALTER TABLE kb_media RENAME CONSTRAINT kb_images_pkey TO kb_media_pkey;
ALTER TABLE kb_media RENAME CONSTRAINT kb_images_workspace_id_id_key TO kb_media_workspace_id_id_key;
ALTER TABLE kb_media RENAME CONSTRAINT kb_images_workspace_id_sha256_key TO kb_media_workspace_id_sha256_key;
ALTER TABLE kb_media RENAME CONSTRAINT kb_images_sha256_check TO kb_media_sha256_check;
ALTER TABLE kb_media RENAME CONSTRAINT kb_images_size_check TO kb_media_size_check;
ALTER TABLE kb_media RENAME CONSTRAINT kb_images_workspace_id_fkey TO kb_media_workspace_id_fkey;
ALTER TABLE kb_media RENAME CONSTRAINT kb_images_workspace_id_created_by_agent_id_fkey TO kb_media_workspace_id_created_by_agent_id_fkey;
ALTER TABLE kb_media DROP CONSTRAINT kb_images_content_type_check;
ALTER INDEX kb_images_storage_key_idx RENAME TO kb_media_storage_key_idx;

ALTER TABLE kb_article_images RENAME TO kb_article_media;
ALTER TABLE kb_article_media RENAME COLUMN image_id TO media_id;
ALTER TABLE kb_article_media RENAME CONSTRAINT kb_article_images_pkey TO kb_article_media_pkey;
ALTER TABLE kb_article_media RENAME CONSTRAINT kb_article_images_workspace_id_fkey TO kb_article_media_workspace_id_fkey;
ALTER TABLE kb_article_media RENAME CONSTRAINT kb_article_images_workspace_id_article_id_fkey TO kb_article_media_workspace_id_article_id_fkey;
ALTER TABLE kb_article_media RENAME CONSTRAINT kb_article_images_workspace_id_image_id_fkey TO kb_article_media_workspace_id_media_id_fkey;
ALTER INDEX kb_article_images_image_idx RENAME TO kb_article_media_media_idx;

ALTER TABLE kb_media
    ADD COLUMN kind text NOT NULL DEFAULT 'image' CHECK (kind IN ('image', 'video')),
    ADD COLUMN name text NOT NULL DEFAULT '' CHECK (char_length(name) <= 200),
    -- An image's alt text; a video's title for screen readers.
    ADD COLUMN alt text NOT NULL DEFAULT '' CHECK (char_length(alt) <= 500),
    ADD COLUMN width integer CHECK (width BETWEEN 1 AND 65535),
    ADD COLUMN height integer CHECK (height BETWEEN 1 AND 65535),
    ADD COLUMN duration_ms bigint CHECK (duration_ms >= 0),
    ADD COLUMN codecs text[] NOT NULL DEFAULT '{}',
    ADD COLUMN thumb_storage_key text,
    ADD COLUMN thumb_size bigint CHECK (thumb_size > 0),
    ADD COLUMN poster_storage_key text,
    ADD COLUMN poster_size bigint CHECK (poster_size > 0),
    ADD COLUMN inspected_at timestamptz,
    ADD COLUMN kept boolean NOT NULL DEFAULT false,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN deleted_by_agent_id uuid,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN updated_by_agent_id uuid,
    ADD CONSTRAINT kb_media_content_type_check CHECK (content_type IN ('image/png', 'image/jpeg', 'image/gif', 'image/webp', 'video/mp4', 'video/webm')),
    ADD CONSTRAINT kb_media_kind_check_type CHECK ((kind = 'video') = (content_type LIKE 'video/%')),
    ADD CONSTRAINT kb_media_thumb_check CHECK ((thumb_storage_key IS NULL) = (thumb_size IS NULL)),
    ADD CONSTRAINT kb_media_poster_check CHECK ((poster_storage_key IS NULL) = (poster_size IS NULL) AND (poster_storage_key IS NULL OR kind = 'video')),
    ADD CONSTRAINT kb_media_duration_check CHECK (duration_ms IS NULL OR kind = 'video'),
    ADD CONSTRAINT kb_media_deleted_by_check CHECK (deleted_by_agent_id IS NULL OR deleted_at IS NOT NULL),
    ADD CONSTRAINT kb_media_workspace_id_deleted_by_agent_id_fkey FOREIGN KEY (workspace_id, deleted_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (deleted_by_agent_id),
    ADD CONSTRAINT kb_media_workspace_id_updated_by_agent_id_fkey FOREIGN KEY (workspace_id, updated_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (updated_by_agent_id);
UPDATE kb_media SET updated_at = created_at;

-- The library pages newest first over the media outside the trash. It carries what the unused page
-- and its summary sort and add up, and the name a search reads, so they read the index alone: a
-- workspace's rows are spread over the table between other workspaces' uploads. Not the alt text: 500
-- characters of 4 bytes each with the name could pass the 2,704 bytes a btree entry may hold.
CREATE INDEX kb_media_library_idx ON kb_media(workspace_id, created_at DESC, id) INCLUDE (size, poster_size, kept, kind, name)
    WHERE deleted_at IS NULL;
-- The daily purge finds media in the trash for more than 30 days in every workspace.
CREATE INDEX kb_media_trash_idx ON kb_media(deleted_at) WHERE deleted_at IS NOT NULL;
-- Deleting an attachment object checks whether a thumbnail or a poster still points at it.
CREATE INDEX kb_media_thumb_key_idx ON kb_media(workspace_id, thumb_storage_key) WHERE thumb_storage_key IS NOT NULL;
-- The daily backfill finds the images it has not read yet.
CREATE INDEX kb_media_backfill_idx ON kb_media(created_at) WHERE inspected_at IS NULL;
CREATE INDEX kb_media_poster_key_idx ON kb_media(workspace_id, poster_storage_key) WHERE poster_storage_key IS NOT NULL;

ALTER TABLE kb_article_media ADD COLUMN current boolean NOT NULL DEFAULT false;
UPDATE kb_article_media l SET current = true
    FROM kb_articles a
    WHERE a.workspace_id = l.workspace_id AND a.id = l.article_id
      AND (strpos(a.body_md, '/api/v1/kb/images/' || l.media_id::text) > 0
           OR EXISTS (SELECT 1 FROM kb_article_translations t
                      WHERE t.workspace_id = l.workspace_id AND t.article_id = l.article_id
                        AND strpos(t.body_md, '/api/v1/kb/images/' || l.media_id::text) > 0));
CREATE INDEX kb_article_media_current_idx ON kb_article_media(workspace_id, media_id) WHERE current;

-- The counters follow the original bytes and the poster (counted, unless an import brought the row in
-- and the plan does not count imports) and those plus the thumbnail (stored). A poster or thumbnail is
-- set by an UPDATE, so an update trigger moves the counters by the difference. Postgres refuses
-- transition tables on a trigger with a column list, so it fires on every update and compares rows.
DROP TRIGGER kb_images_count_inserted ON kb_media;
DROP TRIGGER kb_images_count_deleted ON kb_media;
DROP FUNCTION kb_images_count_inserted();
DROP FUNCTION kb_images_count_deleted();

CREATE FUNCTION kb_media_count_inserted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes + d.counted_bytes,
                            stored_storage_bytes = w.stored_storage_bytes + d.all_bytes
        FROM (SELECT workspace_id,
                     COALESCE(sum(size + COALESCE(poster_size, 0)) FILTER (WHERE counted), 0) AS counted_bytes,
                     sum(size + COALESCE(poster_size, 0) + COALESCE(thumb_size, 0)) AS all_bytes
              FROM added_media GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
    RETURN NULL;
END
$$;

CREATE FUNCTION kb_media_count_deleted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes - d.counted_bytes,
                            stored_storage_bytes = w.stored_storage_bytes - d.all_bytes
        FROM (SELECT workspace_id,
                     COALESCE(sum(size + COALESCE(poster_size, 0)) FILTER (WHERE counted), 0) AS counted_bytes,
                     sum(size + COALESCE(poster_size, 0) + COALESCE(thumb_size, 0)) AS all_bytes
              FROM removed_media GROUP BY workspace_id) d
        WHERE w.id = d.workspace_id;
    RETURN NULL;
END
$$;

CREATE FUNCTION kb_media_count_updated() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE workspaces w SET counted_storage_bytes = w.counted_storage_bytes + d.counted_bytes,
                            stored_storage_bytes = w.stored_storage_bytes + d.all_bytes
        FROM (SELECT n.workspace_id,
                     sum(CASE WHEN n.counted THEN n.size + COALESCE(n.poster_size, 0) ELSE 0 END
                       - CASE WHEN o.counted THEN o.size + COALESCE(o.poster_size, 0) ELSE 0 END) AS counted_bytes,
                     sum((n.size + COALESCE(n.poster_size, 0) + COALESCE(n.thumb_size, 0))
                       - (o.size + COALESCE(o.poster_size, 0) + COALESCE(o.thumb_size, 0))) AS all_bytes
              FROM new_media n JOIN old_media o ON o.id = n.id
              GROUP BY n.workspace_id) d
        WHERE w.id = d.workspace_id AND (d.counted_bytes <> 0 OR d.all_bytes <> 0);
    RETURN NULL;
END
$$;

CREATE TRIGGER kb_media_count_inserted AFTER INSERT ON kb_media
    REFERENCING NEW TABLE AS added_media FOR EACH STATEMENT EXECUTE FUNCTION kb_media_count_inserted();
CREATE TRIGGER kb_media_count_deleted AFTER DELETE ON kb_media
    REFERENCING OLD TABLE AS removed_media FOR EACH STATEMENT EXECUTE FUNCTION kb_media_count_deleted();
CREATE TRIGGER kb_media_count_updated AFTER UPDATE ON kb_media
    REFERENCING OLD TABLE AS old_media NEW TABLE AS new_media FOR EACH STATEMENT EXECUTE FUNCTION kb_media_count_updated();
