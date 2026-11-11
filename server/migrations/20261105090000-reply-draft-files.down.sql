-- The previous release knows no draft files: their rows go (the trigger gives their bytes back to the
-- storage count first). Their stored bytes stay until no row references them; nothing a customer
-- received is touched, since sent files are attachments of the reply.
SELECT set_config('app.privileged', 'on', true);
DELETE FROM ai_draft_files;
DROP TABLE ai_draft_files;
DROP FUNCTION ai_draft_files_count();
ALTER TABLE ai_drafts DROP COLUMN restored_by_agent_id;
ALTER TABLE ai_drafts DROP COLUMN restored_at;
ALTER TABLE message_idempotency_keys DROP COLUMN draft_selection;
