-- The count is derived from messages.cc, so dropping it loses no data; the cap counts replies again.
ALTER TABLE email_outbox DROP COLUMN IF EXISTS recipients;
