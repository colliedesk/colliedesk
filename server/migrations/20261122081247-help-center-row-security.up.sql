-- Row-level security for the public help center: a third role, the reader, serves one help center's
-- visitors. Public requests switch to it with app.workspace_id and app.help_center_id set for the
-- transaction only. The reader sees, on each table the help center reads, only the rows that help
-- center shows (a RESTRICTIVE help_center_isolation policy beside workspace_isolation), and it has
-- no grant at all on the inbox, contacts, channels, tokens or billing. An unset help center matches
-- no row. docs/internals/workspace-isolation.md explains the policies.
--
-- The reader is per database (colliedesk_reader_<database>) like the app role. The app role may
-- SET ROLE to it without inheriting it, so the owner and the request login reach it through the
-- app role and the reader's RESTRICTIVE policies never bind the app role or the owner.

DO $$
DECLARE
    reader text := 'colliedesk_reader_' || current_database();
    app text := app_role();
BEGIN
    IF octet_length(reader) > 63 THEN
        WHILE octet_length(reader) > 50 LOOP
            reader := left(reader, -1);
        END LOOP;
        reader := reader || '_' || left(md5(current_database()), 12);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = reader) THEN
        BEGIN
            EXECUTE format(
                'CREATE ROLE %I NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT',
                reader);
        EXCEPTION
            -- created by a concurrent migration
            WHEN duplicate_object OR unique_violation THEN NULL;
            WHEN insufficient_privilege THEN
                RAISE EXCEPTION 'cannot create role %: %. As a superuser run: '
                        'CREATE ROLE % NOLOGIN; '
                        'GRANT % TO % WITH INHERIT FALSE, SET TRUE; '
                        'then run colliedesk migrate again',
                    reader, SQLERRM, quote_ident(reader), quote_ident(reader), quote_ident(app)
                    USING ERRCODE = 'insufficient_privilege';
        END;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_auth_members m JOIN pg_roles g ON g.oid = m.roleid
            JOIN pg_roles u ON u.oid = m.member WHERE g.rolname = reader AND u.rolname = app) THEN
        BEGIN
            EXECUTE format('GRANT %I TO %I WITH INHERIT FALSE, SET TRUE', reader, app);
        EXCEPTION
            WHEN insufficient_privilege THEN
                RAISE EXCEPTION 'cannot grant role % to %: %. As a superuser run: '
                        'GRANT % TO % WITH INHERIT FALSE, SET TRUE; '
                        'then run colliedesk migrate again',
                    reader, app, SQLERRM, quote_ident(reader), quote_ident(app)
                    USING ERRCODE = 'insufficient_privilege';
        END;
    END IF;
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO %I', current_schema(), reader);
END
$$;

-- The reader role's name, as the DO block above made it.
CREATE FUNCTION reader_role() RETURNS text LANGUAGE plpgsql STABLE AS $$
DECLARE
    reader text := 'colliedesk_reader_' || current_database();
BEGIN
    IF octet_length(reader) > 63 THEN
        WHILE octet_length(reader) > 50 LOOP
            reader := left(reader, -1);
        END LOOP;
        reader := reader || '_' || left(md5(current_database()), 12);
    END IF;
    RETURN reader;
END
$$;

-- A table the public help center reads: the reader also passes workspace_isolation, sees only the
-- rows for which visible_when holds (an expression over the table's columns that reads the help
-- center from app.help_center_id) and gets exactly the given privileges. Call it after
-- secure_workspace_table.
CREATE FUNCTION secure_help_center_table(t regclass, visible_when text, VARIADIC privileges text[])
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    privilege text;
    sequence regclass;
BEGIN
    FOREACH privilege IN ARRAY privileges LOOP
        IF privilege IS NULL OR privilege NOT IN ('SELECT', 'INSERT', 'UPDATE', 'DELETE') THEN
            RAISE EXCEPTION 'secure_help_center_table: % is not one of '
                'SELECT, INSERT, UPDATE, DELETE', privilege;
        END IF;
    END LOOP;
    EXECUTE format('ALTER POLICY workspace_isolation ON %s TO %I, %I',
        t, app_role(), reader_role());
    EXECUTE format('CREATE POLICY help_center_isolation ON %s AS RESTRICTIVE FOR ALL TO %I '
        'USING (%s) WITH CHECK (%s)', t, reader_role(), visible_when, visible_when);
    EXECUTE format('REVOKE ALL ON %s FROM %I', t, reader_role());
    EXECUTE format('GRANT %s ON %s TO %I', array_to_string(privileges, ', '), t, reader_role());
    IF 'INSERT' = ANY (privileges) THEN
        FOR sequence IN SELECT owned_sequences(t) LOOP
            EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %s TO %I', sequence, reader_role());
        END LOOP;
    END IF;
END
$$;

REVOKE EXECUTE ON FUNCTION reader_role(), secure_help_center_table(regclass, text, text[])
    FROM PUBLIC;

-- Each collection keeps the top-level collection it sits under (itself at the top), so the reader's
-- policy finds a nested collection's help center without walking its parents.
ALTER TABLE kb_collections ADD COLUMN top_collection_id uuid;

WITH RECURSIVE tops(workspace_id, id, top_collection_id) AS (
    SELECT workspace_id, id, id FROM kb_collections WHERE parent_id IS NULL
    UNION ALL
    SELECT c.workspace_id, c.id, t.top_collection_id FROM kb_collections c
        JOIN tops t ON t.workspace_id = c.workspace_id AND t.id = c.parent_id
)
UPDATE kb_collections c SET top_collection_id = t.top_collection_id
FROM tops t WHERE t.workspace_id = c.workspace_id AND t.id = c.id;

-- A parent loop has no top: each collection in one counts as its own.
UPDATE kb_collections SET top_collection_id = id WHERE top_collection_id IS NULL;
ALTER TABLE kb_collections ALTER COLUMN top_collection_id SET NOT NULL;

CREATE FUNCTION kb_collection_top() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.top_collection_id := COALESCE((SELECT p.top_collection_id FROM kb_collections p
        WHERE p.workspace_id = NEW.workspace_id AND p.id = NEW.parent_id), NEW.id);
    RETURN NEW;
END
$$;

CREATE TRIGGER kb_collections_top
    BEFORE INSERT OR UPDATE OF parent_id, top_collection_id ON kb_collections
    FOR EACH ROW EXECUTE FUNCTION kb_collection_top();

-- A moved collection carries its new top down to the collections inside it, one level per update;
-- the cascade stops where the value already matches.
CREATE FUNCTION kb_collection_children_top() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE kb_collections SET top_collection_id = NEW.top_collection_id
    WHERE workspace_id = NEW.workspace_id AND parent_id = NEW.id
        AND top_collection_id IS DISTINCT FROM NEW.top_collection_id;
    RETURN NULL;
END
$$;

CREATE TRIGGER kb_collections_children_top
    AFTER UPDATE OF parent_id, top_collection_id ON kb_collections
    FOR EACH ROW EXECUTE FUNCTION kb_collection_children_top();

-- What each table shows on help center app.help_center_id. Tables lower in the list read the ones
-- above through their own policies, so an article is visible when its home collection or one it is
-- listed in is, and its translations, media, tags and votes follow the article.
SELECT secure_help_center_table('help_centers',
    $$id = (SELECT NULLIF(current_setting('app.help_center_id', true), '')::uuid)$$,
    'SELECT');
SELECT secure_help_center_table('help_center_languages',
    $$help_center_id = (SELECT NULLIF(current_setting('app.help_center_id', true), '')::uuid)$$,
    'SELECT');
SELECT secure_help_center_table('kb_collection_help_centers',
    $$help_center_id = (SELECT NULLIF(current_setting('app.help_center_id', true), '')::uuid)$$,
    'SELECT');
SELECT secure_help_center_table('kb_collections',
    'top_collection_id IN (SELECT collection_id FROM kb_collection_help_centers)', 'SELECT');
SELECT secure_help_center_table('kb_collection_translations',
    'collection_id IN (SELECT id FROM kb_collections)', 'SELECT');
SELECT secure_help_center_table('kb_article_collections',
    'collection_id IN (SELECT id FROM kb_collections)', 'SELECT');
SELECT secure_help_center_table('kb_articles',
    'collection_id IN (SELECT id FROM kb_collections)'
    ' OR id IN (SELECT article_id FROM kb_article_collections)', 'SELECT');
SELECT secure_help_center_table('kb_article_translations',
    'article_id IN (SELECT id FROM kb_articles)', 'SELECT');
SELECT secure_help_center_table('kb_article_media',
    'article_id IN (SELECT id FROM kb_articles)', 'SELECT');
SELECT secure_help_center_table('kb_article_attachments',
    'article_id IN (SELECT id FROM kb_articles)', 'SELECT');
SELECT secure_help_center_table('kb_article_tags',
    'article_id IN (SELECT id FROM kb_articles)', 'SELECT');
SELECT secure_help_center_table('kb_article_vote_resets',
    'article_id IN (SELECT id FROM kb_articles)', 'SELECT');
-- A visitor's vote: recorded, replaced and counted.
SELECT secure_help_center_table('kb_article_votes',
    'article_id IN (SELECT id FROM kb_articles)', 'SELECT', 'INSERT', 'UPDATE');
SELECT secure_help_center_table('kb_article_vote_versions',
    'article_id IN (SELECT id FROM kb_articles)', 'SELECT', 'INSERT', 'UPDATE');
SELECT secure_help_center_table('kb_media',
    'id IN (SELECT media_id FROM kb_article_media)'
    ' OR id IN (SELECT media_id FROM kb_article_attachments)'
    ' OR id IN (SELECT logo_media_id FROM help_centers)'
    ' OR id IN (SELECT favicon_media_id FROM help_centers)', 'SELECT');
-- An old helpdesk address of an imported item leads to the page it became: the ledger rows whose
-- article or collection this help center shows, and only the columns that find it, never the
-- import's errors or source data.
SELECT secure_help_center_table('import_items',
    'target_id IN (SELECT id FROM kb_articles)'
    ' OR target_id IN (SELECT id FROM kb_collections)', 'SELECT');
DO $$
BEGIN
    EXECUTE format('REVOKE SELECT ON import_items FROM %I', reader_role());
    EXECUTE format('GRANT SELECT (workspace_id, source, entity, source_id, target_id, status, '
        'updated_at) ON import_items TO %I', reader_role());
END
$$;

-- Whether an article is public depends on every collection it is in, and a listing may sit on
-- another help center the reader cannot see. These views decide it for the whole workspace: they
-- belong to the app role and are not security_invoker, so they read the tables as the app role
-- (workspace_isolation alone) whoever selects from them. They read only tables and immutable
-- functions, because a function that reads a table runs as the caller. They match
-- kb_public_articles and kb_public_translations row for row. Each also keeps to the scoped
-- workspace itself, so a view that ends up owned by another role (a restore with --no-owner, a
-- superuser) still returns no other workspace's ids; Verify refuses to serve with such an owner.
-- They are not security_barrier: a barrier keeps each view from being merged into the query that
-- reads it, and the help center pages then take seconds instead of milliseconds. Without it, a
-- function the caller writes into its own WHERE could see a row before the view's filters, but only
-- the columns the view returns (workspace and article ids, locale) of rows the owner's workspace
-- isolation already allows: the scoped workspace's own articles.
CREATE VIEW kb_help_center_collection_ranks AS
SELECT c.workspace_id, c.id,
    GREATEST(kb_visibility_rank(c.visibility), COALESCE((
        -- The parents of this one collection, one lookup by key per level, nine levels up at most,
        -- stopping at the first parent in the trash, as kb_parents_visibility_rank does. A
        -- recursive query over every collection would be computed whole for each article read.
        WITH RECURSIVE up(parent_id, visibility_rank, depth) AS (
            SELECT p.parent_id, kb_visibility_rank(p.visibility), 1 FROM kb_collections p
            WHERE p.workspace_id = c.workspace_id AND p.id = c.parent_id AND p.deleted_at IS NULL
            UNION ALL
            SELECT p.parent_id, kb_visibility_rank(p.visibility), u.depth + 1
            FROM up u JOIN kb_collections p
                ON p.workspace_id = c.workspace_id AND p.id = u.parent_id AND p.deleted_at IS NULL
            WHERE u.depth < 9
        )
        SELECT max(visibility_rank) FROM up), 0)) AS visibility_rank
FROM kb_collections c
WHERE c.workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid)
    AND c.deleted_at IS NULL;

CREATE VIEW kb_help_center_public_articles AS
SELECT a.workspace_id, a.id FROM kb_articles a
WHERE a.workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid)
    AND a.status = 'published' AND a.deleted_at IS NULL AND a.visibility = 'everyone'
    AND EXISTS (SELECT 1 FROM kb_help_center_collection_ranks h
        WHERE h.workspace_id = a.workspace_id AND h.id = a.collection_id AND h.visibility_rank = 0)
    AND NOT EXISTS (SELECT 1 FROM kb_article_collections x
        JOIN kb_help_center_collection_ranks v
            ON v.workspace_id = x.workspace_id AND v.id = x.collection_id
        WHERE x.workspace_id = a.workspace_id AND x.article_id = a.id AND v.visibility_rank > 0);

CREATE VIEW kb_help_center_public_translations AS
SELECT t.workspace_id, t.article_id, t.locale FROM kb_article_translations t
WHERE t.workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid)
    AND t.status = 'published' AND t.deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM kb_help_center_public_articles a
        WHERE a.workspace_id = t.workspace_id AND a.id = t.article_id);

DO $$
DECLARE
    view_name text;
BEGIN
    -- A new owner needs CREATE on the schema; the app role has it only for these statements.
    EXECUTE format('GRANT CREATE ON SCHEMA %I TO %I', current_schema(), app_role());
    FOREACH view_name IN ARRAY ARRAY['kb_help_center_collection_ranks',
        'kb_help_center_public_articles', 'kb_help_center_public_translations']
    LOOP
        EXECUTE format('ALTER VIEW %I OWNER TO %I', view_name, app_role());
    END LOOP;
    EXECUTE format('REVOKE CREATE ON SCHEMA %I FROM %I', current_schema(), app_role());

    -- The views the reader's queries use. kb_public_articles and kb_public_translations stay out of
    -- its reach: read as the reader they would miss the listings it cannot see.
    FOREACH view_name IN ARRAY ARRAY['kb_help_center_public_articles',
        'kb_help_center_public_translations', 'kb_collection_visibility', 'kb_published_articles',
        'kb_published_translations']
    LOOP
        EXECUTE format('GRANT SELECT ON %I TO %I', view_name, reader_role());
    END LOOP;
END
$$;
