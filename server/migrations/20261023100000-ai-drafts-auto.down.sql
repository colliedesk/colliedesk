-- The drafts stay; only their automatic marks, the trigger and the setting go.
DROP TRIGGER ai_draft_on_customer_message ON messages;
DROP FUNCTION ai_draft_on_customer_message();
DROP INDEX ai_tasks_automatic_draft_queued;
DROP INDEX ai_drafts_automatic_open;
ALTER TABLE ai_drafts DROP COLUMN reply_needed, DROP COLUMN answers_message_id, DROP COLUMN automatic;
ALTER TABLE ai_automatic_limits DROP COLUMN draft_per_day;
UPDATE ai_slots SET automatic = false WHERE slot = 'draft';
