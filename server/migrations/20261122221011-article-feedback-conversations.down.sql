-- The schema before has no origin for article feedback. Refuse to roll back while a workspace
-- holds such a conversation, naming the workspaces, rather than drop what readers wrote (the way
-- back for a server is the backup).
DO $$
DECLARE
    holders text;
BEGIN
    SELECT string_agg(w.slug, ', ' ORDER BY w.slug) INTO holders FROM workspaces w
        WHERE EXISTS (
            SELECT 1 FROM conversations c
            WHERE c.workspace_id = w.id AND c.channel = 'article_feedback'
        );
    IF holders IS NOT NULL THEN
        RAISE EXCEPTION 'article feedback opened conversations in workspaces %; '
            'roll back from a backup instead', holders;
    END IF;
END
$$;

DROP INDEX kb_article_votes_conversation_idx;
ALTER TABLE kb_article_votes DROP COLUMN conversation_id;
ALTER TABLE help_centers DROP COLUMN feedback_conversations;
DELETE FROM channel_types WHERE type = 'article_feedback';
