-- The rows are a send queue: what was sent is in Meta, and what was not yet sent is only a report Meta
-- does not get. No conversation, contact or setting is lost.
DROP TABLE meta_conversion_events;
