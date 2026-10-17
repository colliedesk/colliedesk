-- Dropping the table removes its policies. Without it a redelivered relay event can fail a reply
-- again after an agent retried it.
DROP TABLE IF EXISTS relay_webhook_events;
