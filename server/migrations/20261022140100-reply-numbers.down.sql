-- The numbers are derived from the order of the replies; the next upgrade numbers them again.
DROP TRIGGER messages_number_reply ON messages;
DROP FUNCTION messages_number_reply();
DROP INDEX messages_reply_number_idx;
ALTER TABLE messages DROP CONSTRAINT messages_reply_number_kind, DROP COLUMN reply_number;
