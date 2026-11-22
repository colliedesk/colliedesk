-- A collection's effective visibility is the most restrictive of itself and every live collection
-- above it. The view read only the parent and the grandparent, from when collections nested three
-- levels; they now nest six, so an article four or more levels under a collection kept for the team
-- or for help chat reached the help center. The view now reads nine parents up, ten levels with the
-- collection itself, as the API's visibility_from does, and stops at the first parent in the trash.
--
-- It joins each parent by key instead of walking the tree with a recursive query: the planner can
-- only compute a recursive view for the whole workspace, and readers that check one article per row
-- (the help chat's list of articles, the API's article list) would then scan it once per article.
-- Its columns stay the same, so the views on it are unchanged. An older image reads it the same
-- way.
CREATE OR REPLACE VIEW kb_collection_visibility WITH (security_invoker = true) AS
SELECT c.workspace_id, c.id,
    GREATEST(kb_visibility_rank(c.visibility),
        CASE WHEN p1.id IS NOT NULL THEN kb_visibility_rank(p1.visibility) END,
        CASE WHEN p2.id IS NOT NULL THEN kb_visibility_rank(p2.visibility) END,
        CASE WHEN p3.id IS NOT NULL THEN kb_visibility_rank(p3.visibility) END,
        CASE WHEN p4.id IS NOT NULL THEN kb_visibility_rank(p4.visibility) END,
        CASE WHEN p5.id IS NOT NULL THEN kb_visibility_rank(p5.visibility) END,
        CASE WHEN p6.id IS NOT NULL THEN kb_visibility_rank(p6.visibility) END,
        CASE WHEN p7.id IS NOT NULL THEN kb_visibility_rank(p7.visibility) END,
        CASE WHEN p8.id IS NOT NULL THEN kb_visibility_rank(p8.visibility) END,
        CASE WHEN p9.id IS NOT NULL THEN kb_visibility_rank(p9.visibility) END) AS visibility_rank
FROM kb_collections c
LEFT JOIN kb_collections p1
    ON p1.workspace_id = c.workspace_id AND p1.id = c.parent_id AND p1.deleted_at IS NULL
LEFT JOIN kb_collections p2
    ON p2.workspace_id = p1.workspace_id AND p2.id = p1.parent_id AND p2.deleted_at IS NULL
LEFT JOIN kb_collections p3
    ON p3.workspace_id = p2.workspace_id AND p3.id = p2.parent_id AND p3.deleted_at IS NULL
LEFT JOIN kb_collections p4
    ON p4.workspace_id = p3.workspace_id AND p4.id = p3.parent_id AND p4.deleted_at IS NULL
LEFT JOIN kb_collections p5
    ON p5.workspace_id = p4.workspace_id AND p5.id = p4.parent_id AND p5.deleted_at IS NULL
LEFT JOIN kb_collections p6
    ON p6.workspace_id = p5.workspace_id AND p6.id = p5.parent_id AND p6.deleted_at IS NULL
LEFT JOIN kb_collections p7
    ON p7.workspace_id = p6.workspace_id AND p7.id = p6.parent_id AND p7.deleted_at IS NULL
LEFT JOIN kb_collections p8
    ON p8.workspace_id = p7.workspace_id AND p8.id = p7.parent_id AND p8.deleted_at IS NULL
LEFT JOIN kb_collections p9
    ON p9.workspace_id = p8.workspace_id AND p9.id = p8.parent_id AND p9.deleted_at IS NULL
WHERE c.deleted_at IS NULL;
