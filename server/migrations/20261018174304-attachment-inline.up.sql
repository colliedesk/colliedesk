-- True when the attachment's bytes are embedded in its message's body_html (an inline image the formatted
-- message already shows), so the message view need not list the file again. Set by the writer that did the
-- embedding (the email channel for a cid: image, the Freshdesk importer for a rehosted image), never from a
-- file name. Older rows are false and stay listed.
ALTER TABLE attachments ADD COLUMN inline boolean NOT NULL DEFAULT false;
