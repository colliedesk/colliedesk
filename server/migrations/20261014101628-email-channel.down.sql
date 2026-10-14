DROP TABLE IF EXISTS email_outbox;
DROP INDEX IF EXISTS conversations_workspace_created_idx;
DROP INDEX IF EXISTS messages_workspace_incoming_recent_idx;
DROP INDEX IF EXISTS messages_workspace_email_message_id_idx;
ALTER TABLE messages
    DROP COLUMN IF EXISTS email_meta,
    DROP COLUMN IF EXISTS email_message_id;
ALTER TABLE conversations DROP COLUMN IF EXISTS email_reply_token;
DROP TABLE IF EXISTS email_channels;
