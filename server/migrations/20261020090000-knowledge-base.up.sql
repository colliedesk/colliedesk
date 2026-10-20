-- Knowledge base: articles in Markdown, grouped in collections at most three levels deep (a Freshdesk
-- category, folder and subfolder), each change kept as a version, deletes kept in a trash for 30
-- days, images in attachment storage, and the settings of the public help center.
--
-- Collections and articles keep their row with deleted_at set while in the trash; the server purges
-- them after 30 days. A slug is unique only among the live collections of one parent. An article's
-- number grows per workspace (help_centers.last_article_number) and is never reused, so its address
-- /a/<number>-<slug> stays stable when the title or slug changes.
--
-- source_html and source_meta hold what an import read (the original HTML and the fields Colliedesk
-- has no column for); only an import writes them, the export carries them, the help center never
-- renders source_html. Older images ignore these tables.
CREATE TABLE kb_collections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    parent_id uuid,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    slug text NOT NULL CHECK (char_length(slug) BETWEEN 1 AND 120),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    position integer NOT NULL DEFAULT 0 CHECK (position >= 0),
    source_meta jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    deleted_by_agent_id uuid,
    UNIQUE (workspace_id, id),
    CHECK (parent_id IS NULL OR parent_id <> id),
    FOREIGN KEY (workspace_id, parent_id) REFERENCES kb_collections(workspace_id, id),
    FOREIGN KEY (workspace_id, deleted_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (deleted_by_agent_id)
);
CREATE UNIQUE INDEX kb_collections_slug_idx ON kb_collections(workspace_id, COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), slug)
    WHERE deleted_at IS NULL;
CREATE INDEX kb_collections_parent_idx ON kb_collections(workspace_id, parent_id);
-- The daily purge finds rows deleted more than 30 days ago in every workspace.
CREATE INDEX kb_collections_trash_idx ON kb_collections(deleted_at) WHERE deleted_at IS NOT NULL;

-- The words of a text for search, with no language rules ('simple'), so no language is the default.
-- Positions are stripped: ranking needs only which words occur, and the vector is a third smaller.
-- ASCII punctuation other than '.' becomes a space first: with it, the default parser costs the square
-- of a run without whitespace, so 100,000 characters of '_a', '~/', 'a1@' or 'a-1-' took seconds to
-- tens of seconds of one core (docs/internals/knowledge-base.md). Without it the cost is linear. The article column and the
-- search query both call this, so a text and a query split the same way. The SQL body is parsed now,
-- so 'simple' cannot be changed by a search_path.
CREATE FUNCTION kb_search_vector(content text) RETURNS tsvector LANGUAGE sql IMMUTABLE PARALLEL SAFE
    RETURN strip(to_tsvector('simple'::regconfig,
        translate(content, '!"#$%&''()*+,-/:;<=>?@[\]^_`{|}~', '                               ')));

CREATE TABLE kb_articles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    collection_id uuid NOT NULL,
    number bigint NOT NULL CHECK (number >= 1),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
    slug text NOT NULL CHECK (char_length(slug) BETWEEN 1 AND 120),
    body_md text NOT NULL CHECK (char_length(body_md) <= 100000),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    published_at timestamptz CHECK ((status = 'published') = (published_at IS NOT NULL)),
    author_agent_id uuid,
    source_meta jsonb,
    source_html text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    deleted_by_agent_id uuid,
    -- No GIN index: under row-level security Postgres never uses one for @@, which is not leakproof
    -- (docs/internals/knowledge-base.md), so a search reads the vectors of the workspace.
    search tsvector GENERATED ALWAYS AS (kb_search_vector(title || ' ' || body_md)) STORED,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, number),
    FOREIGN KEY (workspace_id, collection_id) REFERENCES kb_collections(workspace_id, id),
    FOREIGN KEY (workspace_id, author_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (author_agent_id),
    FOREIGN KEY (workspace_id, deleted_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (deleted_by_agent_id)
);
CREATE INDEX kb_articles_collection_idx ON kb_articles(workspace_id, collection_id);
CREATE INDEX kb_articles_updated_idx ON kb_articles(workspace_id, updated_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX kb_articles_trash_idx ON kb_articles(deleted_at) WHERE deleted_at IS NOT NULL;

-- Append-only for the app role: who made each version (the person, or the person who approved an
-- agent request), through which agent token, and what the article said after it.
CREATE TABLE kb_article_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    version integer NOT NULL CHECK (version >= 1),
    kind text NOT NULL CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import')),
    restored_from_version integer CHECK ((kind = 'restore') = (restored_from_version IS NOT NULL)),
    title text NOT NULL,
    slug text NOT NULL,
    body_md text NOT NULL,
    status text NOT NULL CHECK (status IN ('draft', 'published')),
    collection_id uuid,
    changed_by_agent_id uuid,
    token_id uuid,
    token_label text,
    agent_request_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, article_id, version),
    CHECK ((token_id IS NULL) OR (token_label IS NOT NULL)),
    FOREIGN KEY (workspace_id, article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, collection_id) REFERENCES kb_collections(workspace_id, id) ON DELETE SET NULL (collection_id),
    FOREIGN KEY (workspace_id, changed_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (changed_by_agent_id),
    FOREIGN KEY (workspace_id, token_id) REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (token_id),
    FOREIGN KEY (workspace_id, agent_request_id) REFERENCES agent_requests(workspace_id, id) ON DELETE SET NULL (agent_request_id)
);
CREATE INDEX kb_article_versions_collection_idx ON kb_article_versions(workspace_id, collection_id);

-- Translations an import brings in, one per article and language. Kept and exported; not rendered yet.
CREATE TABLE kb_article_translations (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    locale text NOT NULL CHECK (char_length(locale) BETWEEN 2 AND 35),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
    body_md text NOT NULL CHECK (char_length(body_md) <= 100000),
    source_meta jsonb,
    source_html text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, article_id, locale),
    FOREIGN KEY (workspace_id, article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE
);

-- Images of articles, in attachment storage. The same bytes give one row per workspace. They count
-- toward the storage cap like attachments: counted is false for images an import brings in, which
-- only a plan that counts imports counts (the same rule as attachments.counted).
CREATE TABLE kb_images (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    storage_key text NOT NULL,
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size bigint NOT NULL CHECK (size > 0),
    content_type text NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/gif', 'image/webp')),
    counted boolean NOT NULL DEFAULT true,
    created_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- The last upload of these bytes. The purge counts 30 days from it, so an image uploaded again for
    -- an article not saved yet is not deleted under it.
    last_uploaded_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, sha256),
    FOREIGN KEY (workspace_id, created_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id)
);
-- Deleting an attachment object checks whether an image still points at it.
CREATE INDEX kb_images_storage_key_idx ON kb_images(workspace_id, storage_key);

-- Which images any version of an article points at. Links are added with every version and go only
-- with the article, so an image stays while some version could still show it; the purge deletes an
-- image no article links once it is 30 days old.
CREATE TABLE kb_article_images (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    image_id uuid NOT NULL,
    PRIMARY KEY (workspace_id, article_id, image_id),
    FOREIGN KEY (workspace_id, article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, image_id) REFERENCES kb_images(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX kb_article_images_image_idx ON kb_article_images(workspace_id, image_id);

-- The public help center of a workspace and the knowledge base's own counters. No row means it is
-- off. content_version grows in the same transaction as every write to the knowledge base, so a page
-- cached under an older version is known to be stale.
CREATE TABLE help_centers (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    enabled boolean NOT NULL DEFAULT false,
    title text NOT NULL DEFAULT '' CHECK (char_length(title) <= 200),
    locale text NOT NULL DEFAULT 'en' CHECK (char_length(locale) BETWEEN 2 AND 35),
    content_version bigint NOT NULL DEFAULT 0 CHECK (content_version >= 0),
    last_article_number bigint NOT NULL DEFAULT 0 CHECK (last_article_number >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Images count toward workspaces.counted_storage_bytes and stored_storage_bytes, kept by statement
-- triggers like those of attachments (see the storage-counter migrations).
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

-- Same isolation as saved_replies. The app role may read and add versions, never change or remove one.
DO $$
DECLARE
    app_role text;
    kb_table text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH kb_table IN ARRAY ARRAY['kb_collections', 'kb_articles', 'kb_article_versions', 'kb_article_translations',
                                    'kb_images', 'kb_article_images', 'help_centers'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', kb_table);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', kb_table);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %2$I FOR ALL TO %1$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role, kb_table);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %2$I FOR ALL TO %1$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user, kb_table);
        IF kb_table = 'kb_article_versions' THEN
            -- The schema's default privileges grant UPDATE and DELETE on every new table; take them back.
            EXECUTE format('REVOKE ALL ON %I FROM %I', kb_table, app_role);
            EXECUTE format('GRANT SELECT, INSERT ON %I TO %I', kb_table, app_role);
        ELSE
            EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', kb_table, app_role);
        END IF;
    END LOOP;
END
$$;
