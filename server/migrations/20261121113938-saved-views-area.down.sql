-- Rolling back would turn the knowledge base views into inbox views that cannot open, so it refuses
-- while any exists, in the trash too. Export the workspace first.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM saved_views WHERE area = 'kb') THEN
        RAISE EXCEPTION 'saved view area rollback refused: % knowledge base views exist',
            (SELECT count(*) FROM saved_views WHERE area = 'kb');
    END IF;
END $$;

DROP INDEX saved_views_personal_name_idx;
DROP INDEX saved_views_shared_name_idx;
CREATE UNIQUE INDEX saved_views_personal_name_idx
    ON saved_views(workspace_id, owner_agent_id, lower(name))
    WHERE deleted_at IS NULL AND scope = 'personal';
CREATE UNIQUE INDEX saved_views_shared_name_idx ON saved_views(workspace_id, lower(name))
    WHERE deleted_at IS NULL AND scope = 'shared';

ALTER TABLE saved_views DROP COLUMN area;
