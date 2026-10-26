ALTER TABLE conversation_thread_keys DROP COLUMN message_id;
ALTER TABLE conversations DROP COLUMN merged_into_id;
ALTER TABLE messages DROP COLUMN other_contact_id, DROP COLUMN detached_at, DROP COLUMN detached_by_agent_id;
DROP TABLE message_moves;
