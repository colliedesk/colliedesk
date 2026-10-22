-- The name customers see in the From line belongs to the channel, not the workspace: the workspace name
-- is an internal label. Each existing channel starts with the name its customers already see, the
-- workspace name, made safe for a mail header (control and text-direction characters become spaces) and
-- cut to 100 characters. A workspace name with nothing left after that falls back to its slug.
ALTER TABLE email_channels ADD COLUMN sender_name text;

CREATE FUNCTION email_sender_name_from_workspace(workspace_name text, slug text) RETURNS text
    LANGUAGE sql IMMUTABLE AS $$
    SELECT COALESCE(
        NULLIF(btrim(left(btrim(regexp_replace(workspace_name, '[\x01-\x1f\x7f-\x9f‪-‮⁦-⁩]', ' ', 'g')), 100)), ''),
        left(slug, 100))
$$;

UPDATE email_channels ch SET sender_name = email_sender_name_from_workspace(w.name, w.slug)
FROM workspaces w WHERE w.id = ch.workspace_id;

ALTER TABLE email_channels
    ALTER COLUMN sender_name SET NOT NULL,
    ADD CONSTRAINT email_channels_sender_name_check
        CHECK (char_length(sender_name) BETWEEN 1 AND 100 AND btrim(sender_name) <> '' AND sender_name !~ '[\x01-\x1f\x7f]');

-- An image older than this migration connects email without a sender name. After a rollback of the
-- image that keeps the schema, its insert gets the workspace name the same way instead of failing on
-- NOT NULL, like a column default (which cannot read another table).
CREATE FUNCTION email_channels_default_sender_name() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.sender_name IS NULL THEN
        SELECT email_sender_name_from_workspace(w.name, w.slug) INTO NEW.sender_name
            FROM workspaces w WHERE w.id = NEW.workspace_id;
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER email_channels_default_sender_name BEFORE INSERT ON email_channels
    FOR EACH ROW EXECUTE FUNCTION email_channels_default_sender_name();
