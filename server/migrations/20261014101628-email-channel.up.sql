CREATE TABLE email_channels (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    support_address text NOT NULL CHECK (support_address = lower(support_address) AND support_address LIKE '_%@_%'),
    secret_key_id text UNIQUE,
    secret_hash bytea,
    secret_rotated_at timestamptz,
    domain_verification_token text NOT NULL,
    domain_verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((secret_key_id IS NULL) = (secret_hash IS NULL))
);

ALTER TABLE conversations ADD COLUMN email_reply_token text UNIQUE;

ALTER TABLE messages
    ADD COLUMN email_message_id text,
    ADD COLUMN email_meta jsonb;

-- The attachment and import migration that precedes this one owns this key. Add it only when this
-- migration runs without it, so both orders apply cleanly; the down migration leaves it to its owner.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'messages'::regclass AND conname = 'messages_workspace_id_id_key') THEN
        ALTER TABLE messages ADD CONSTRAINT messages_workspace_id_id_key UNIQUE (workspace_id, id);
    END IF;
END $$;

CREATE UNIQUE INDEX messages_workspace_email_message_id_idx
    ON messages(workspace_id, email_message_id) WHERE email_message_id IS NOT NULL;

-- Serves the per-sender inbound rate limit, which counts recent incoming mail.
CREATE INDEX messages_workspace_incoming_recent_idx
    ON messages(workspace_id, created_at) WHERE kind = 'incoming';

-- Serves the per-workspace ceiling on new conversations.
CREATE INDEX conversations_workspace_created_idx ON conversations(workspace_id, created_at);

CREATE TABLE email_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    message_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_until timestamptz,
    claim_token uuid,
    last_error text,
    sent_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, message_id),
    FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE CASCADE
);

CREATE INDEX email_outbox_due_idx ON email_outbox(next_attempt_at) WHERE status = 'queued';
CREATE INDEX email_outbox_workspace_sent_idx ON email_outbox(workspace_id, sent_at) WHERE status = 'sent';
