-- The name customers see in the From line belongs to the channel, not the workspace: the workspace name
-- is an internal label. Each existing channel starts with the name its customers already see, the
-- workspace name, made safe for a mail header (control and text-direction characters become spaces) and
-- cut to 100 characters. A workspace name with nothing left after that falls back to its slug.
ALTER TABLE email_channels ADD COLUMN sender_name text;

UPDATE email_channels ch SET sender_name = COALESCE(
    NULLIF(btrim(left(btrim(regexp_replace(w.name, '[\x01-\x1f\x7f-\x9f‪-‮⁦-⁩]', ' ', 'g')), 100)), ''),
    left(w.slug, 100))
FROM workspaces w WHERE w.id = ch.workspace_id;

ALTER TABLE email_channels
    ALTER COLUMN sender_name SET NOT NULL,
    ADD CONSTRAINT email_channels_sender_name_check
        CHECK (char_length(sender_name) BETWEEN 1 AND 100 AND btrim(sender_name) <> '' AND sender_name !~ '[\x01-\x1f\x7f]');
