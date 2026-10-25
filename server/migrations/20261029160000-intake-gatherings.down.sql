ALTER TABLE ai_tasks DROP COLUMN gathering_id;
ALTER TABLE channel_thread_items DROP CONSTRAINT IF EXISTS channel_thread_items_workspace_id_gathering_id_fkey;
UPDATE channel_thread_items SET gathering_id = NULL;
ALTER TABLE channel_inbound_events DROP COLUMN gathering_id;
DROP TABLE intake_decisions;
DROP TABLE intake_gatherings;
