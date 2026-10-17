-- Rolling back would bring trashed replies back as live ones and drop the history people recorded,
-- so it refuses while any reply is in the trash, any reply has more than the version this migration
-- backfilled, or any agent request keeps a before snapshot. Restore or purge them deliberately first.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM saved_replies WHERE deleted_at IS NOT NULL) THEN
        RAISE EXCEPTION 'saved reply history rollback refused: % saved replies are in the trash',
            (SELECT count(*) FROM saved_replies WHERE deleted_at IS NOT NULL);
    END IF;
    IF EXISTS (SELECT 1 FROM saved_reply_versions WHERE version > 1) THEN
        RAISE EXCEPTION 'saved reply history rollback refused: % versions after the first would be deleted',
            (SELECT count(*) FROM saved_reply_versions WHERE version > 1);
    END IF;
    IF EXISTS (SELECT 1 FROM agent_requests WHERE saved_reply_before IS NOT NULL) THEN
        RAISE EXCEPTION 'saved reply history rollback refused: % agent requests keep a saved reply snapshot',
            (SELECT count(*) FROM agent_requests WHERE saved_reply_before IS NOT NULL);
    END IF;
END $$;

ALTER TABLE agent_requests DROP CONSTRAINT IF EXISTS agent_requests_saved_reply_fkey;
ALTER TABLE agent_requests DROP COLUMN IF EXISTS saved_reply_before;
ALTER TABLE agent_requests DROP COLUMN IF EXISTS saved_reply_id;
DROP TABLE IF EXISTS saved_reply_versions;
DROP INDEX IF EXISTS saved_replies_trash_idx;
DROP INDEX IF EXISTS saved_replies_workspace_title_idx;
CREATE UNIQUE INDEX saved_replies_workspace_title_idx ON saved_replies(workspace_id, lower(title));
ALTER TABLE saved_replies DROP CONSTRAINT IF EXISTS saved_replies_workspace_id_id_key;
ALTER TABLE saved_replies DROP COLUMN IF EXISTS deleted_by_agent_id;
ALTER TABLE saved_replies DROP COLUMN IF EXISTS deleted_at;
