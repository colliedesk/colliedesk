-- A saved view belongs to one area: the inbox (conversation filters) or the knowledge base
-- article list (the query of GET /kb/articles as an object). Names and the 50-view limits count
-- per area, so an inbox view and a knowledge base view may share a name. Existing views are inbox
-- views. An older image lists every view as an inbox view; a knowledge base view then refuses to
-- open there.
ALTER TABLE saved_views
    ADD COLUMN area text NOT NULL DEFAULT 'inbox' CHECK (area IN ('inbox', 'kb'));

DROP INDEX saved_views_personal_name_idx;
DROP INDEX saved_views_shared_name_idx;
CREATE UNIQUE INDEX saved_views_personal_name_idx
    ON saved_views(workspace_id, area, owner_agent_id, lower(name))
    WHERE deleted_at IS NULL AND scope = 'personal';
CREATE UNIQUE INDEX saved_views_shared_name_idx ON saved_views(workspace_id, area, lower(name))
    WHERE deleted_at IS NULL AND scope = 'shared';
