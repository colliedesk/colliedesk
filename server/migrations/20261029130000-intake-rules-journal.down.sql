DROP INDEX channel_inbound_events_message;
ALTER TABLE channel_inbound_events DROP COLUMN decision_rule, DROP COLUMN decision_basis;
