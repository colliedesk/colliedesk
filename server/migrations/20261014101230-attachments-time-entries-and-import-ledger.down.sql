DROP TABLE IF EXISTS import_items;
DROP TABLE IF EXISTS time_entries;
DROP TABLE IF EXISTS attachments;
ALTER TABLE contacts DROP COLUMN IF EXISTS organization;
ALTER TABLE messages DROP COLUMN IF EXISTS body_html;
ALTER TABLE messages DROP COLUMN IF EXISTS author_name;
ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_workspace_id_id_key;
