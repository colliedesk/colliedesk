-- A Microsoft 365 or Outlook.com mailbox connected for sending (issue #95, step 1), beside Gmail. An email
-- channel still has one mailbox connection: connecting through the other provider replaces it.
ALTER TABLE mailbox_connections DROP CONSTRAINT mailbox_connections_provider_check,
    ADD CONSTRAINT mailbox_connections_provider_check CHECK (provider IN ('google', 'microsoft'));
ALTER TABLE mailbox_oauth_states DROP CONSTRAINT mailbox_oauth_states_provider_check,
    ADD CONSTRAINT mailbox_oauth_states_provider_check CHECK (provider IN ('google', 'microsoft'));
-- mailbox_unavailable: the Microsoft account has no Exchange Online mailbox (any more), so Graph refuses to
-- send from it.
ALTER TABLE mailbox_connections DROP CONSTRAINT mailbox_connections_error_code_check,
    ADD CONSTRAINT mailbox_connections_error_code_check CHECK (error_code IN ('revoked', 'permission', 'mailbox_unavailable'));
-- The provider's own id of the account that signed in (Microsoft: the user's object id; Google: none). It
-- names the account when its address changes, and keeps apart, later, the account that signs in from the
-- address it sends as (a shared mailbox).
ALTER TABLE mailbox_connections ADD COLUMN account_id text CHECK (char_length(account_id) BETWEEN 1 AND 200);
-- The outbox table kept the name of its transport check when it was renamed from email_outbox.
ALTER TABLE channel_outbox DROP CONSTRAINT email_outbox_transport_check,
    ADD CONSTRAINT channel_outbox_transport_check CHECK (transport IN ('smtp', 'gmail', 'microsoft'));
