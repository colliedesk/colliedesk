-- An older image sends with the workspace name again; a sender name set since the upgrade is lost.
DROP TRIGGER IF EXISTS email_channels_default_sender_name ON email_channels;
DROP FUNCTION IF EXISTS email_channels_default_sender_name();
ALTER TABLE email_channels DROP COLUMN IF EXISTS sender_name;
DROP FUNCTION IF EXISTS email_sender_name_from_workspace(text, text);
