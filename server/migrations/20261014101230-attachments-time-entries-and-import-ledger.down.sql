DROP TABLE IF EXISTS import_items;
DROP TABLE IF EXISTS time_entries;
DROP TABLE IF EXISTS attachments;
ALTER TABLE contacts DROP COLUMN IF EXISTS organization;
ALTER TABLE conversations DROP COLUMN IF EXISTS origin;
ALTER TABLE messages DROP COLUMN IF EXISTS origin;
ALTER TABLE messages DROP COLUMN IF EXISTS body_html;
ALTER TABLE messages DROP COLUMN IF EXISTS author_name;
-- Drop the key only when no other table's foreign key still uses it (the email outbox does).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE contype = 'f' AND confrelid = 'messages'::regclass
          AND conindid = (SELECT conindid FROM pg_constraint WHERE conrelid = 'messages'::regclass AND conname = 'messages_workspace_id_id_key')
    ) THEN
        ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_workspace_id_id_key;
    END IF;
END $$;
