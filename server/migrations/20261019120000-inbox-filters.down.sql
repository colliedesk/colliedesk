-- Saved views are what people made; an older image cannot show them, so they are not dropped silently.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM saved_views) THEN
        RAISE EXCEPTION 'saved views exist (some may be in the trash); export what you need and delete them before rolling back';
    END IF;
END
$$;
DROP TABLE saved_views;

DROP INDEX email_outbox_workspace_failed_idx;
DROP INDEX conversations_workspace_awaiting_team_idx;
DROP TRIGGER messages_last_public_updated ON messages;
DROP TRIGGER messages_last_public_deleted ON messages;
DROP TRIGGER messages_last_public_inserted ON messages;
DROP FUNCTION messages_last_public_updated();
DROP FUNCTION messages_last_public_deleted();
DROP FUNCTION messages_last_public_inserted();
DROP FUNCTION conversations_refresh_last_public(uuid[], uuid[]);
-- Derived from the messages, so nothing is lost; channel is read back from the same rows on the next upgrade.
ALTER TABLE conversations DROP COLUMN last_public_at, DROP COLUMN last_public_kind, DROP COLUMN channel;
