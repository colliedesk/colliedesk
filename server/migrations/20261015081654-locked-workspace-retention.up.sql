-- When the admins of a locked workspace were told it will be deleted. The retention job sets it once
-- per lock and clears it when the workspace gets access again. A notice older than the current lock
-- does not count. Older images ignore the column, so rolling back only stops the notices.
ALTER TABLE workspaces ADD COLUMN retention_notice_sent_at timestamptz;
