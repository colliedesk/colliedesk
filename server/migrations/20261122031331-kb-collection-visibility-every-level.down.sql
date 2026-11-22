-- Back to the view that reads a collection's parent and grandparent only.
CREATE OR REPLACE VIEW kb_collection_visibility WITH (security_invoker = true) AS
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
