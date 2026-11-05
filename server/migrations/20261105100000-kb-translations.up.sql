-- Translations of the knowledge base become a language version of the same article
-- (docs/internals/knowledge-base.md, "Translations"): each has its own slug, search fields, status,
-- versions and trash, and is marked outdated when the article's own text changes. The help center's
-- languages are its primary language (help_centers.locale) and the rows of help_center_languages,
-- each shown to customers or hidden (hidden at first). Collections get a name and description per
-- language.
--
-- No row is dropped: every translation an import brought in stays, gains a first version of kind
-- import (so an import rerun and undo can tell it from a person's edit) and its language is added
-- hidden, so the public help center shows exactly what it showed before the upgrade.

CREATE TABLE help_center_languages (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    locale text NOT NULL CHECK (char_length(locale) BETWEEN 2 AND 35),
    visible boolean NOT NULL DEFAULT false,
    position integer NOT NULL DEFAULT 0 CHECK (position >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, locale)
);

ALTER TABLE kb_article_translations
    ADD COLUMN slug text CHECK (char_length(slug) BETWEEN 1 AND 120),
    ADD COLUMN status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    ADD COLUMN published_at timestamptz,
    ADD COLUMN seo_title text NOT NULL DEFAULT '' CHECK (char_length(seo_title) <= 120),
    ADD COLUMN seo_description text NOT NULL DEFAULT '' CHECK (char_length(seo_description) <= 320),
    -- Set when the article's own title or text changed after the translation was last brought up to
    -- date; cleared by a person or an AI agent (PATCH outdated false).
    ADD COLUMN outdated boolean NOT NULL DEFAULT false,
    ADD COLUMN outdated_at timestamptz,
    ADD COLUMN author_agent_id uuid,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN deleted_by_agent_id uuid,
    -- Like kb_articles.search: no GIN index, a search reads the vectors of the workspace.
    ADD COLUMN search tsvector GENERATED ALWAYS AS (kb_search_vector(title || ' ' || body_md)) STORED;

-- Slugs from the title, as domain.Slugify would make them for Latin text; a slug that differs from
-- Slugify only costs a 301 to the canonical address, which the help center sends for any other slug.
UPDATE kb_article_translations SET
    slug = COALESCE(NULLIF(left(btrim(regexp_replace(lower(normalize(title, NFC)), '[^[:alnum:]]+', '-', 'g'), '-'), 120), ''), 'article'),
    -- Freshdesk's status 2 is published. It stays off the help center while the language is hidden.
    status = CASE WHEN source_meta->>'status' = '2' THEN 'published' ELSE 'draft' END,
    published_at = CASE WHEN source_meta->>'status' = '2' THEN updated_at END,
    -- Freshdesk's own search fields, when they fit; a longer one is left for a person, never cut.
    seo_title = CASE WHEN char_length(regexp_replace(btrim(COALESCE(source_meta->'seo_data'->>'meta_title', '')), '\s+', ' ', 'g')) <= 120
        THEN regexp_replace(btrim(COALESCE(source_meta->'seo_data'->>'meta_title', '')), '\s+', ' ', 'g') ELSE '' END,
    seo_description = CASE WHEN char_length(regexp_replace(btrim(COALESCE(source_meta->'seo_data'->>'meta_description', '')), '\s+', ' ', 'g')) <= 320
        THEN regexp_replace(btrim(COALESCE(source_meta->'seo_data'->>'meta_description', '')), '\s+', ' ', 'g') ELSE '' END;
-- btrim of a slug that is only hyphens leaves '', handled above; a left() cut can end in a hyphen.
UPDATE kb_article_translations SET slug = COALESCE(NULLIF(rtrim(slug, '-'), ''), 'article') WHERE slug LIKE '%-';
ALTER TABLE kb_article_translations ALTER COLUMN slug SET NOT NULL;
ALTER TABLE kb_article_translations
    ADD CONSTRAINT kb_article_translations_published_at CHECK ((status = 'published') = (published_at IS NOT NULL)),
    ADD CONSTRAINT kb_article_translations_outdated_at CHECK (outdated = (outdated_at IS NOT NULL)),
    ADD CONSTRAINT kb_article_translations_author_fkey FOREIGN KEY (workspace_id, author_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (author_agent_id),
    ADD CONSTRAINT kb_article_translations_deleted_by_fkey FOREIGN KEY (workspace_id, deleted_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (deleted_by_agent_id);
-- The lists of a language (missing, outdated) and the help center read one locale of the workspace.
CREATE INDEX kb_article_translations_locale_idx ON kb_article_translations(workspace_id, locale) WHERE deleted_at IS NULL;
CREATE INDEX kb_article_translations_trash_idx ON kb_article_translations(deleted_at) WHERE deleted_at IS NOT NULL;

-- Every language a translation has, other than the primary one, is a language of the help center,
-- hidden. A translation in the primary language itself (an import from an account whose primary
-- language differs) keeps its row and is listed for a person to decide.
INSERT INTO help_center_languages(workspace_id, locale, visible, created_at, updated_at)
SELECT t.workspace_id, t.locale, false, min(t.created_at), now()
FROM kb_article_translations t LEFT JOIN help_centers h ON h.workspace_id = t.workspace_id
WHERE t.locale <> COALESCE(h.locale, 'en')
GROUP BY t.workspace_id, t.locale;

-- Append-only for the app role, like kb_article_versions: one history per translation.
CREATE TABLE kb_article_translation_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    locale text NOT NULL,
    version integer NOT NULL CHECK (version >= 1),
    kind text NOT NULL CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'mark_current', 'mark_outdated')),
    restored_from_version integer CHECK ((kind = 'restore') = (restored_from_version IS NOT NULL)),
    title text NOT NULL,
    slug text NOT NULL,
    body_md text NOT NULL,
    seo_title text NOT NULL DEFAULT '',
    seo_description text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('draft', 'published')),
    changed_by_agent_id uuid,
    token_id uuid,
    token_label text,
    agent_request_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, article_id, locale, version),
    CHECK ((token_id IS NULL) OR (token_label IS NOT NULL)),
    FOREIGN KEY (workspace_id, article_id, locale) REFERENCES kb_article_translations(workspace_id, article_id, locale) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, changed_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (changed_by_agent_id),
    FOREIGN KEY (workspace_id, token_id) REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (token_id),
    FOREIGN KEY (workspace_id, agent_request_id) REFERENCES agent_requests(workspace_id, id) ON DELETE SET NULL (agent_request_id)
);

INSERT INTO kb_article_translation_versions(workspace_id, article_id, locale, version, kind, title, slug, body_md, seo_title, seo_description, status, created_at)
SELECT workspace_id, article_id, locale, 1, 'import', title, slug, body_md, seo_title, seo_description, status, updated_at FROM kb_article_translations;

-- A collection's name and description in another language. source_meta is set while the row is as an
-- import wrote it; a person's write clears it, so a rerun of the import leaves that row alone.
CREATE TABLE kb_collection_translations (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    collection_id uuid NOT NULL,
    locale text NOT NULL CHECK (char_length(locale) BETWEEN 2 AND 35),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    source_meta jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, collection_id, locale),
    FOREIGN KEY (workspace_id, collection_id) REFERENCES kb_collections(workspace_id, id) ON DELETE CASCADE
);

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
    FOREACH kb_table IN ARRAY ARRAY['help_center_languages', 'kb_article_translation_versions', 'kb_collection_translations'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', kb_table);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', kb_table);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %2$I FOR ALL TO %1$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role, kb_table);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %2$I FOR ALL TO %1$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user, kb_table);
        IF kb_table = 'kb_article_translation_versions' THEN
            EXECUTE format('REVOKE ALL ON %I FROM %I', kb_table, app_role);
            EXECUTE format('GRANT SELECT, INSERT ON %I TO %I', kb_table, app_role);
        ELSE
            EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', kb_table, app_role);
        END IF;
    END LOOP;
END
$$;

-- AI reads a translation only while it is published and outside the trash, like the article itself
-- (docs/internals/ai-retrieval.md). One that stops being readable loses its row now.
CREATE OR REPLACE FUNCTION ai_retrieval_translation_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status <> 'published' OR NEW.deleted_at IS NOT NULL THEN
        DELETE FROM ai_retrieval_docs WHERE workspace_id = NEW.workspace_id AND kb_article_id = NEW.article_id AND kb_locale = NEW.locale;
    END IF;
    INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id) VALUES (NEW.workspace_id, NEW.article_id);
    RETURN NULL;
END
$$;
DROP TRIGGER ai_retrieval_translation_updated ON kb_article_translations;
CREATE TRIGGER ai_retrieval_translation_updated AFTER UPDATE OF title, body_md, status, deleted_at ON kb_article_translations
    FOR EACH ROW WHEN ((OLD.title, OLD.body_md, OLD.status, OLD.deleted_at) IS DISTINCT FROM (NEW.title, NEW.body_md, NEW.status, NEW.deleted_at))
    EXECUTE FUNCTION ai_retrieval_translation_changed();
-- Rows an older build made for translations that are drafts stay: a search skips them
-- (aiPublishedTranslationOf in store/ai-retrieval.go) and the next build of their article drops them.
