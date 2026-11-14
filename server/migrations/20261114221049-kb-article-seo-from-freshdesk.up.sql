-- Articles imported from Freshdesk before the import read its search title and description kept them
-- in source_meta->'seo_data'. A rerun of the import skips an article Freshdesk did not change, so this
-- fills them once, as the import cleans them (domain.CleanKBSEO): runs of white space become one space,
-- the ends are trimmed, and a value longer than the limit stays empty for a person, never cut. A value
-- that is not a string is ignored, as the import ignores it.
--
-- Only an article whose newest version is an import, as a rerun of the import decides
-- (store.KBArticleImportedLast), and only a field that is still empty: a change made in Colliedesk
-- stays. The newest version gets the same fields, so restoring it keeps them.
WITH source AS (
    SELECT a.workspace_id, a.id, a.seo_title AS old_title, a.seo_description AS old_description,
        -- The class is what Go's unicode.IsSpace counts as white space, which strings.Fields splits on.
        btrim(regexp_replace(CASE WHEN jsonb_typeof(a.source_meta->'seo_data'->'meta_title') = 'string'
            THEN a.source_meta->'seo_data'->>'meta_title' ELSE '' END,
            '[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+', ' ', 'g'), ' ') AS title,
        btrim(regexp_replace(CASE WHEN jsonb_typeof(a.source_meta->'seo_data'->'meta_description') = 'string'
            THEN a.source_meta->'seo_data'->>'meta_description' ELSE '' END,
            '[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+', ' ', 'g'), ' ') AS description
    FROM kb_articles a
    WHERE jsonb_typeof(a.source_meta->'seo_data') = 'object'
        AND (a.seo_title = '' OR a.seo_description = '')
        AND (SELECT v.kind FROM kb_article_versions v WHERE v.workspace_id = a.workspace_id AND v.article_id = a.id
            ORDER BY v.version DESC LIMIT 1) = 'import'
), filled AS (
    SELECT workspace_id, id,
        CASE WHEN old_title <> '' THEN old_title WHEN char_length(title) <= 120 THEN title ELSE '' END AS seo_title,
        CASE WHEN old_description <> '' THEN old_description WHEN char_length(description) <= 320 THEN description ELSE '' END AS seo_description
    FROM source
), articles AS (
    UPDATE kb_articles a SET seo_title = f.seo_title, seo_description = f.seo_description
    FROM filled f
    WHERE a.workspace_id = f.workspace_id AND a.id = f.id
        AND (a.seo_title, a.seo_description) IS DISTINCT FROM (f.seo_title, f.seo_description)
    RETURNING a.workspace_id, a.id, a.seo_title, a.seo_description
)
UPDATE kb_article_versions v SET seo_title = a.seo_title, seo_description = a.seo_description
FROM articles a
WHERE v.workspace_id = a.workspace_id AND v.article_id = a.id
    AND v.version = (SELECT max(n.version) FROM kb_article_versions n WHERE n.workspace_id = a.workspace_id AND n.article_id = a.id);
