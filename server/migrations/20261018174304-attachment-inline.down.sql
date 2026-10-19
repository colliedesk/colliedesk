-- The flag is derived from how a message was written; the files stay.
ALTER TABLE attachments DROP COLUMN IF EXISTS inline;
