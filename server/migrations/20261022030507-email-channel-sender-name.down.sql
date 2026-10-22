-- An older image sends with the workspace name again; a sender name set since the upgrade is lost.
ALTER TABLE email_channels DROP COLUMN sender_name;
