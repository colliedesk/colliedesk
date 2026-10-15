DROP TABLE IF EXISTS retention_deletions;
DROP TABLE IF EXISTS workspace_retention_notices;
ALTER TABLE workspaces DROP COLUMN IF EXISTS retention_notice_sent_at;
