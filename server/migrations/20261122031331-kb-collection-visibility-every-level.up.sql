-- A collection's effective visibility is the most restrictive of itself and every live collection
-- above it. The view read only the parent and the grandparent, from when collections nested three
-- levels; they now nest six, so an article four or more levels under a collection kept for the team
-- or for help chat reached the help center. The view now reads nine parents up, ten levels with the
-- collection itself, as the API's visibility_from does, and stops at the first parent in the trash.
--
-- A function walks the parents, one lookup by key per level, for the rows a reader keeps. A
-- recursive query in the view would be computed for the whole workspace and scanned once per
-- article by readers that check one article per row (the help chat's article list, the API's
-- article list). Nine joins in the view made every query on it slow to plan, which each new
-- database connection pays on its first help center page. The function runs as its caller, so
-- row security still applies. The view's columns stay the same, so the views on it are unchanged.
CREATE FUNCTION kb_parents_visibility_rank(workspace uuid, parent uuid) RETURNS integer
    LANGUAGE plpgsql STABLE PARALLEL SAFE AS $$
DECLARE
    most integer;
    level_rank integer;
    next_parent uuid;
BEGIN
    FOR level IN 1..9 LOOP
        EXIT WHEN parent IS NULL;
        SELECT kb_visibility_rank(c.visibility), c.parent_id INTO level_rank, next_parent
        FROM kb_collections c
        WHERE c.workspace_id = workspace AND c.id = parent AND c.deleted_at IS NULL;
        EXIT WHEN NOT FOUND;
        most := GREATEST(most, level_rank);
        parent := next_parent;
    END LOOP;
    RETURN most;
END $$;

CREATE OR REPLACE VIEW kb_collection_visibility WITH (security_invoker = true) AS
SELECT c.workspace_id, c.id,
    GREATEST(kb_visibility_rank(c.visibility),
        kb_parents_visibility_rank(c.workspace_id, c.parent_id)) AS visibility_rank
FROM kb_collections c
WHERE c.deleted_at IS NULL;
