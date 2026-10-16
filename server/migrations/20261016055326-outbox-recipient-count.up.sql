-- How many people a delivery reaches: the contact in To plus every Cc address. The trial's daily cap
-- sums it, so copying ten people costs eleven, not one. Rows queued before Cc existed reached one
-- person; replies already queued with a Cc list are counted from their message.
ALTER TABLE email_outbox ADD COLUMN recipients integer NOT NULL DEFAULT 1 CHECK (recipients >= 1);

UPDATE email_outbox o SET recipients = 1 + cardinality(m.cc)
FROM messages m
WHERE m.workspace_id = o.workspace_id AND m.id = o.message_id AND m.cc IS NOT NULL;
