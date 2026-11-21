-- Who can see an article (docs/internals/knowledge-base.md, "Visibility"). An article and a collection
-- each have a visibility: everyone (the help center, help chat and the team), help_chat (help chat may
-- quote it, the help center never shows it) or agents (only the team). An article's effective
-- visibility is the most restrictive of its own and that of every live collection it is in (its home,
-- the others it is listed in, and their parents). A collection in the trash does not count.
--
-- These views are the one definition of what customers may read. Every help center reader goes through
-- kb_public_articles and kb_public_translations; help chat retrieval through kb_help_chat_articles and
-- kb_help_chat_translations; the team's AI drafts through kb_published_articles and
-- kb_published_translations. A Go test fails when a public reader reads the status column directly.
-- An unknown value counts as agents, so a mistake hides an article rather than showing it.
--
-- No existing row changes: every article and collection stays visible to everyone.
ALTER TABLE kb_articles ADD COLUMN visibility text NOT NULL DEFAULT 'everyone'
    CHECK (visibility IN ('everyone', 'help_chat', 'agents'));
ALTER TABLE kb_collections ADD COLUMN visibility text NOT NULL DEFAULT 'everyone'
    CHECK (visibility IN ('everyone', 'help_chat', 'agents'));

-- 0 everyone, 1 help_chat, 2 agents; NULL and anything else 2.
CREATE FUNCTION kb_visibility_rank(visibility text) RETURNS integer LANGUAGE sql IMMUTABLE PARALLEL SAFE
    RETURN CASE visibility WHEN 'everyone' THEN 0 WHEN 'help_chat' THEN 1 ELSE 2 END;

CREATE FUNCTION kb_visibility_name(visibility_rank integer) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE
    RETURN CASE visibility_rank WHEN 0 THEN 'everyone' WHEN 1 THEN 'help_chat' ELSE 'agents' END;

-- Each live collection with the most restrictive visibility of itself and its live parents (collections
-- nest three levels at most).
CREATE VIEW kb_collection_visibility WITH (security_invoker = true) AS
SELECT c.workspace_id, c.id,
    GREATEST(kb_visibility_rank(c.visibility),
        CASE WHEN p.id IS NULL THEN 0 ELSE kb_visibility_rank(p.visibility) END,
        CASE WHEN g.id IS NULL THEN 0 ELSE kb_visibility_rank(g.visibility) END) AS visibility_rank
FROM kb_collections c
LEFT JOIN kb_collections p ON p.workspace_id = c.workspace_id AND p.id = c.parent_id AND p.deleted_at IS NULL
LEFT JOIN kb_collections g ON g.workspace_id = p.workspace_id AND g.id = p.parent_id AND g.deleted_at IS NULL
WHERE c.deleted_at IS NULL;

-- Each article with the visibility its collections allow (collection_rank) and its effective
-- visibility. A home collection that is not live counts as agents.
CREATE VIEW kb_article_visibility WITH (security_invoker = true) AS
SELECT a.workspace_id, a.id, a.status, a.deleted_at, l.collection_rank,
    GREATEST(kb_visibility_rank(a.visibility), l.collection_rank) AS visibility_rank
FROM kb_articles a
CROSS JOIN LATERAL (SELECT GREATEST(
        COALESCE((SELECT v.visibility_rank FROM kb_collection_visibility v
            WHERE v.workspace_id = a.workspace_id AND v.id = a.collection_id), 2),
        COALESCE((SELECT max(v.visibility_rank) FROM kb_article_collections x
            JOIN kb_collection_visibility v ON v.workspace_id = x.workspace_id AND v.id = x.collection_id
            WHERE x.workspace_id = a.workspace_id AND x.article_id = a.id), 0)) AS collection_rank) l;

CREATE VIEW kb_published_articles WITH (security_invoker = true) AS
SELECT v.workspace_id, v.id FROM kb_article_visibility v WHERE v.status = 'published' AND v.deleted_at IS NULL;

CREATE VIEW kb_help_chat_articles WITH (security_invoker = true) AS
SELECT v.workspace_id, v.id FROM kb_article_visibility v
WHERE v.status = 'published' AND v.deleted_at IS NULL AND v.visibility_rank <= 1;

CREATE VIEW kb_public_articles WITH (security_invoker = true) AS
SELECT v.workspace_id, v.id FROM kb_article_visibility v
WHERE v.status = 'published' AND v.deleted_at IS NULL AND v.visibility_rank = 0;

-- A translation is shown where its article is, while it is published itself.
CREATE VIEW kb_published_translations WITH (security_invoker = true) AS
SELECT t.workspace_id, t.article_id, t.locale FROM kb_article_translations t
WHERE t.status = 'published' AND t.deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM kb_published_articles a WHERE a.workspace_id = t.workspace_id AND a.id = t.article_id);

CREATE VIEW kb_help_chat_translations WITH (security_invoker = true) AS
SELECT t.workspace_id, t.article_id, t.locale FROM kb_article_translations t
WHERE t.status = 'published' AND t.deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM kb_help_chat_articles a WHERE a.workspace_id = t.workspace_id AND a.id = t.article_id);

CREATE VIEW kb_public_translations WITH (security_invoker = true) AS
SELECT t.workspace_id, t.article_id, t.locale FROM kb_article_translations t
WHERE t.status = 'published' AND t.deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM kb_public_articles a WHERE a.workspace_id = t.workspace_id AND a.id = t.article_id);

DO $$
DECLARE
    app_role text;
    kb_view text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH kb_view IN ARRAY ARRAY['kb_collection_visibility', 'kb_article_visibility', 'kb_published_articles',
        'kb_help_chat_articles', 'kb_public_articles', 'kb_published_translations', 'kb_help_chat_translations',
        'kb_public_translations'] LOOP
        EXECUTE format('REVOKE ALL ON %I FROM %I', kb_view, app_role);
        EXECUTE format('GRANT SELECT ON %I TO %I', kb_view, app_role);
    END LOOP;
END
$$;
