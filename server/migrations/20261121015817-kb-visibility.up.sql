-- Who can see an article (docs/internals/knowledge-base.md, "Visibility"). An article and a
-- collection each have a visibility: everyone (the help center, help chat and the team),
-- help_chat (help chat may quote it, the help center never shows it) or agents (only the team).
-- An article's effective visibility is the most restrictive of its own and that of every live
-- collection it is in (its home, the others it is listed in, and their parents). A collection in
-- the trash does not count.
--
-- These views are the one definition of what customers may read. Every help center reader goes
-- through kb_public_articles and kb_public_translations; help chat retrieval through
-- kb_help_chat_articles and kb_help_chat_translations; the team's AI drafts through
-- kb_published_articles and kb_published_translations. A Go test fails when a public reader reads
-- the status column directly. An unknown value counts as agents, so a mistake hides an article
-- rather than showing it.
--
-- No existing row changes: every article and collection stays visible to everyone.
ALTER TABLE kb_articles ADD COLUMN visibility text NOT NULL DEFAULT 'everyone'
    CHECK (visibility IN ('everyone', 'help_chat', 'agents'));
ALTER TABLE kb_collections ADD COLUMN visibility text NOT NULL DEFAULT 'everyone'
    CHECK (visibility IN ('everyone', 'help_chat', 'agents'));

-- 0 everyone, 1 help_chat, 2 agents; NULL and anything else 2.
CREATE FUNCTION kb_visibility_rank(visibility text) RETURNS integer
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    RETURN CASE visibility WHEN 'everyone' THEN 0 WHEN 'help_chat' THEN 1 ELSE 2 END;

CREATE FUNCTION kb_visibility_name(visibility_rank integer) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    RETURN CASE visibility_rank WHEN 0 THEN 'everyone' WHEN 1 THEN 'help_chat' ELSE 'agents' END;

-- Each live collection with the most restrictive visibility of itself and its live parents
-- (collections nest three levels at most).
CREATE VIEW kb_collection_visibility WITH (security_invoker = true) AS
SELECT c.workspace_id, c.id,
    GREATEST(kb_visibility_rank(c.visibility),
        CASE WHEN p.id IS NULL THEN 0 ELSE kb_visibility_rank(p.visibility) END,
        CASE WHEN g.id IS NULL THEN 0 ELSE kb_visibility_rank(g.visibility) END) AS visibility_rank
FROM kb_collections c
LEFT JOIN kb_collections p
    ON p.workspace_id = c.workspace_id AND p.id = c.parent_id AND p.deleted_at IS NULL
LEFT JOIN kb_collections g
    ON g.workspace_id = p.workspace_id AND g.id = p.parent_id AND g.deleted_at IS NULL
WHERE c.deleted_at IS NULL;

-- What each collection an article is in allows: one row for its home (a home that is not live
-- counts as agents) and one for each live collection it is also listed in.
CREATE VIEW kb_article_collection_ranks WITH (security_invoker = true) AS
SELECT a.workspace_id, a.id AS article_id, COALESCE(v.visibility_rank, 2) AS visibility_rank
FROM kb_articles a
LEFT JOIN kb_collection_visibility v
    ON v.workspace_id = a.workspace_id AND v.id = a.collection_id
UNION ALL
SELECT x.workspace_id, x.article_id, v.visibility_rank
FROM kb_article_collections x
JOIN kb_collection_visibility v
    ON v.workspace_id = x.workspace_id AND v.id = x.collection_id;

-- Each article with the visibility its collections allow (collection_rank) and its
-- effective visibility.
CREATE VIEW kb_article_visibility WITH (security_invoker = true) AS
SELECT a.workspace_id, a.id, a.status, a.deleted_at, r.collection_rank,
    GREATEST(kb_visibility_rank(a.visibility), r.collection_rank) AS visibility_rank
FROM kb_articles a
JOIN (SELECT workspace_id, article_id, max(visibility_rank) AS collection_rank
    FROM kb_article_collection_ranks GROUP BY workspace_id, article_id) r
    ON r.workspace_id = a.workspace_id AND r.article_id = a.id;

CREATE VIEW kb_published_articles WITH (security_invoker = true) AS
SELECT a.workspace_id, a.id FROM kb_articles a
WHERE a.status = 'published' AND a.deleted_at IS NULL;

-- The readers' views say the same as kb_article_visibility as conditions on the article row: its
-- home is live and allows it, and no other collection it is listed in forbids it. The planner joins
-- these set-wise; a rank computed per article (or a union with kb_articles) is read once per outer
-- row. The visibility column is compared by name for the same reason (its CHECK allows only the
-- three names, so this is kb_visibility_rank <= 1 and = 0).
CREATE VIEW kb_help_chat_articles WITH (security_invoker = true) AS
SELECT a.workspace_id, a.id FROM kb_articles a
WHERE a.status = 'published' AND a.deleted_at IS NULL
    AND a.visibility IN ('everyone', 'help_chat')
    AND EXISTS (SELECT 1 FROM kb_collection_visibility h
        WHERE h.workspace_id = a.workspace_id AND h.id = a.collection_id
            AND h.visibility_rank <= 1)
    AND NOT EXISTS (SELECT 1 FROM kb_article_collections x
        JOIN kb_collection_visibility v
            ON v.workspace_id = x.workspace_id AND v.id = x.collection_id
        WHERE x.workspace_id = a.workspace_id AND x.article_id = a.id AND v.visibility_rank > 1);

CREATE VIEW kb_public_articles WITH (security_invoker = true) AS
SELECT a.workspace_id, a.id FROM kb_articles a
WHERE a.status = 'published' AND a.deleted_at IS NULL AND a.visibility = 'everyone'
    AND EXISTS (SELECT 1 FROM kb_collection_visibility h
        WHERE h.workspace_id = a.workspace_id AND h.id = a.collection_id
            AND h.visibility_rank = 0)
    AND NOT EXISTS (SELECT 1 FROM kb_article_collections x
        JOIN kb_collection_visibility v
            ON v.workspace_id = x.workspace_id AND v.id = x.collection_id
        WHERE x.workspace_id = a.workspace_id AND x.article_id = a.id AND v.visibility_rank > 0);

-- A translation is shown where its article is, while it is published itself.
CREATE VIEW kb_published_translations WITH (security_invoker = true) AS
SELECT t.workspace_id, t.article_id, t.locale FROM kb_article_translations t
WHERE t.status = 'published' AND t.deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM kb_published_articles a
        WHERE a.workspace_id = t.workspace_id AND a.id = t.article_id);

CREATE VIEW kb_help_chat_translations WITH (security_invoker = true) AS
SELECT t.workspace_id, t.article_id, t.locale FROM kb_article_translations t
WHERE t.status = 'published' AND t.deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM kb_help_chat_articles a
        WHERE a.workspace_id = t.workspace_id AND a.id = t.article_id);

CREATE VIEW kb_public_translations WITH (security_invoker = true) AS
SELECT t.workspace_id, t.article_id, t.locale FROM kb_article_translations t
WHERE t.status = 'published' AND t.deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM kb_public_articles a
        WHERE a.workspace_id = t.workspace_id AND a.id = t.article_id);

SELECT limit_app_role('kb_collection_visibility', 'SELECT');
SELECT limit_app_role('kb_article_collection_ranks', 'SELECT');
SELECT limit_app_role('kb_article_visibility', 'SELECT');
SELECT limit_app_role('kb_published_articles', 'SELECT');
SELECT limit_app_role('kb_help_chat_articles', 'SELECT');
SELECT limit_app_role('kb_public_articles', 'SELECT');
SELECT limit_app_role('kb_published_translations', 'SELECT');
SELECT limit_app_role('kb_help_chat_translations', 'SELECT');
SELECT limit_app_role('kb_public_translations', 'SELECT');
