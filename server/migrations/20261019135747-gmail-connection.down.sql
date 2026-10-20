-- Without the transport column a reply queued for Gmail would leave through SMTP from another address, so
-- those replies fail with the reason first; an agent can retry them after the rollback.
UPDATE email_outbox SET status = 'failed', locked_until = NULL, claim_token = NULL, updated_at = now(),
    last_error = 'Gmail sending was removed from this server before the reply was sent. Retry to send it another way.'
    WHERE transport = 'gmail' AND status = 'queued';
ALTER TABLE email_outbox DROP COLUMN IF EXISTS transport;
-- The connections hold only grants Google can issue again: an admin connects Gmail again after an upgrade.
DROP TABLE IF EXISTS mailbox_oauth_states;
DROP TABLE IF EXISTS mailbox_connections;
