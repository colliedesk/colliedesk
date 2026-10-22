ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_ai_draft_fkey;
ALTER TABLE messages DROP COLUMN IF EXISTS ai_draft_id;
DROP TABLE IF EXISTS ai_usage;
DROP TABLE IF EXISTS ai_drafts;
DROP TABLE IF EXISTS workspace_ai_settings;
