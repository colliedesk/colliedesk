-- A workspace can have several help centers over one knowledge base. Each help center has its own
-- id, theme, address, custom domain, languages, analytics and home order. Exactly one per
-- workspace is the primary: it keeps every address a help center has today, and the existing row
-- of each workspace becomes it with every setting it had. Any other help center has a handle and
-- is served under /b/<handle> (docs/internals/help-center.md, "Several help centers").
--
-- content_version, last_article_number and require_approval stay knowledge-base wide: they are
-- read and written on the primary row only, and are unused on any other row.
--
-- A help center shows the top-level collections placed on it (kb_collection_help_centers) and
-- every collection nested under them. Every top-level collection, trashed ones too so that undo
-- keeps them, is placed on the primary in today's home order, so each help center shows exactly
-- what it showed before. Triggers keep writes that name no help center on the primary: a domain,
-- language or analytics row without help_center_id, and a collection that becomes top level.
--
-- An older image reads and writes the primary through workspace_id alone; it fails on the new
-- keys of ON CONFLICT, so roll back with the down migration before running one.
ALTER TABLE help_centers
    ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN handle text CHECK (handle ~ '^[a-z0-9][a-z0-9-]{0,39}$'),
    ADD COLUMN primary_help_center boolean NOT NULL DEFAULT true,
    ADD COLUMN deleted_at timestamptz,
    ADD CONSTRAINT help_centers_primary_has_no_handle
        CHECK (primary_help_center = (handle IS NULL)),
    ADD CONSTRAINT help_centers_primary_is_live
        CHECK (NOT primary_help_center OR deleted_at IS NULL);
ALTER TABLE help_centers DROP CONSTRAINT help_centers_pkey,
    ADD PRIMARY KEY (id),
    ADD CONSTRAINT help_centers_workspace_id_id_key UNIQUE (workspace_id, id);
CREATE UNIQUE INDEX help_centers_primary ON help_centers (workspace_id) WHERE primary_help_center;
-- A deleted help center frees its handle.
CREATE UNIQUE INDEX help_centers_handle ON help_centers (workspace_id, handle)
    WHERE deleted_at IS NULL;

-- The id of the workspace's primary help center, NULL when it has none yet. Reads and writes for
-- the primary filter the help center tables with it.
CREATE FUNCTION primary_help_center_id(ws uuid) RETURNS uuid LANGUAGE sql STABLE AS $$
    SELECT id FROM help_centers WHERE workspace_id = ws AND primary_help_center
$$;

-- The same id, made (off, with the defaults) when the workspace has none yet, as the code did
-- before for the one row of a workspace.
CREATE FUNCTION ensure_primary_help_center(ws uuid) RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE
    found uuid;
BEGIN
    SELECT id INTO found FROM help_centers WHERE workspace_id = ws AND primary_help_center;
    IF found IS NULL THEN
        INSERT INTO help_centers (workspace_id) VALUES (ws)
            ON CONFLICT (workspace_id) WHERE primary_help_center DO NOTHING;
        SELECT id INTO found FROM help_centers WHERE workspace_id = ws AND primary_help_center;
    END IF;
    RETURN found;
END
$$;

-- A workspace with a domain, a language, analytics or a collection but no help center row gets
-- its primary now; the code made the row lazily, so it may be missing.
INSERT INTO help_centers (workspace_id)
SELECT workspace_id FROM help_center_domains
UNION SELECT workspace_id FROM help_center_languages
UNION SELECT workspace_id FROM help_center_analytics
UNION SELECT workspace_id FROM kb_collections
ON CONFLICT (workspace_id) WHERE primary_help_center DO NOTHING;

-- A row written without a help center belongs to the workspace's primary.
CREATE FUNCTION help_center_row_on_primary() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.help_center_id IS NULL THEN
        NEW.help_center_id := ensure_primary_help_center(NEW.workspace_id);
    END IF;
    RETURN NEW;
END
$$;

-- One custom domain per help center; a host name stays unique on the whole server.
ALTER TABLE help_center_domains ADD COLUMN help_center_id uuid;
UPDATE help_center_domains d SET help_center_id = h.id
FROM help_centers h
WHERE h.workspace_id = d.workspace_id AND h.primary_help_center;
ALTER TABLE help_center_domains ALTER COLUMN help_center_id SET NOT NULL,
    DROP CONSTRAINT help_center_domains_pkey,
    ADD PRIMARY KEY (workspace_id, help_center_id),
    ADD CONSTRAINT help_center_domains_help_center_id_fkey
        FOREIGN KEY (workspace_id, help_center_id)
        REFERENCES help_centers (workspace_id, id) ON DELETE CASCADE;
CREATE TRIGGER help_center_domains_on_primary BEFORE INSERT ON help_center_domains
    FOR EACH ROW EXECUTE FUNCTION help_center_row_on_primary();

-- The languages a help center shows besides its own locale.
ALTER TABLE help_center_languages ADD COLUMN help_center_id uuid;
UPDATE help_center_languages l SET help_center_id = h.id
FROM help_centers h
WHERE h.workspace_id = l.workspace_id AND h.primary_help_center;
ALTER TABLE help_center_languages ALTER COLUMN help_center_id SET NOT NULL,
    DROP CONSTRAINT help_center_languages_pkey,
    ADD PRIMARY KEY (workspace_id, help_center_id, locale),
    ADD CONSTRAINT help_center_languages_help_center_id_fkey
        FOREIGN KEY (workspace_id, help_center_id)
        REFERENCES help_centers (workspace_id, id) ON DELETE CASCADE;
CREATE TRIGGER help_center_languages_on_primary BEFORE INSERT ON help_center_languages
    FOR EACH ROW EXECUTE FUNCTION help_center_row_on_primary();

-- Where a help center sends its page views.
ALTER TABLE help_center_analytics ADD COLUMN help_center_id uuid;
UPDATE help_center_analytics a SET help_center_id = h.id
FROM help_centers h
WHERE h.workspace_id = a.workspace_id AND h.primary_help_center;
ALTER TABLE help_center_analytics ALTER COLUMN help_center_id SET NOT NULL,
    DROP CONSTRAINT help_center_analytics_pkey,
    ADD PRIMARY KEY (workspace_id, help_center_id),
    ADD CONSTRAINT help_center_analytics_help_center_id_fkey
        FOREIGN KEY (workspace_id, help_center_id)
        REFERENCES help_centers (workspace_id, id) ON DELETE CASCADE;
CREATE TRIGGER help_center_analytics_on_primary BEFORE INSERT ON help_center_analytics
    FOR EACH ROW EXECUTE FUNCTION help_center_row_on_primary();

-- The top-level collections a help center shows, at its own home position. A nested collection
-- follows its top, so it is never placed itself.
CREATE TABLE kb_collection_help_centers (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    collection_id uuid NOT NULL,
    help_center_id uuid NOT NULL,
    position integer NOT NULL DEFAULT 0 CHECK (position >= 0),
    PRIMARY KEY (workspace_id, help_center_id, collection_id),
    FOREIGN KEY (workspace_id, collection_id)
        REFERENCES kb_collections (workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, help_center_id)
        REFERENCES help_centers (workspace_id, id) ON DELETE CASCADE
);
-- Placing and unplacing a collection finds its rows by collection.
CREATE INDEX kb_collection_help_centers_collection
    ON kb_collection_help_centers (workspace_id, collection_id);

SELECT secure_workspace_table('kb_collection_help_centers');

INSERT INTO kb_collection_help_centers (workspace_id, collection_id, help_center_id, position)
SELECT c.workspace_id, c.id, h.id, c.position
FROM kb_collections c
JOIN help_centers h ON h.workspace_id = c.workspace_id AND h.primary_help_center
WHERE c.parent_id IS NULL;

-- A collection that becomes top level is placed on the primary; one nested under another leaves
-- every help center. Until each help center has its own home order, a top-level collection's
-- position is its home position on the primary.
CREATE FUNCTION kb_collection_placed_on_primary() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.parent_id IS NOT NULL THEN
        IF TG_OP = 'UPDATE' AND OLD.parent_id IS NULL THEN
            DELETE FROM kb_collection_help_centers
            WHERE workspace_id = NEW.workspace_id AND collection_id = NEW.id;
        END IF;
        RETURN NULL;
    END IF;
    IF TG_OP = 'INSERT' OR OLD.parent_id IS NOT NULL THEN
        INSERT INTO kb_collection_help_centers (workspace_id, collection_id, help_center_id,
            position)
        VALUES (NEW.workspace_id, NEW.id, ensure_primary_help_center(NEW.workspace_id),
            NEW.position)
        ON CONFLICT (workspace_id, help_center_id, collection_id)
            DO UPDATE SET position = EXCLUDED.position;
    ELSIF NEW.position IS DISTINCT FROM OLD.position THEN
        UPDATE kb_collection_help_centers l SET position = NEW.position
        FROM help_centers h
        WHERE l.workspace_id = NEW.workspace_id AND l.collection_id = NEW.id
            AND h.workspace_id = l.workspace_id AND h.id = l.help_center_id
            AND h.primary_help_center;
    END IF;
    RETURN NULL;
END
$$;

CREATE TRIGGER kb_collections_placed_on_primary
    AFTER INSERT OR UPDATE OF parent_id, position ON kb_collections
    FOR EACH ROW EXECUTE FUNCTION kb_collection_placed_on_primary();
