-- After a DNS check proves an active custom domain whose help center serves there, the server opens the
-- domain's address once (over HTTPS behind TLS), as a visitor would: address_checked_at is when, and
-- address_error the plain cause when the answer was not this help center's (no certificate yet, another
-- server answered, no answer in time). Both are null while no check opened it.
ALTER TABLE help_center_domains ADD COLUMN address_checked_at timestamptz,
    ADD COLUMN address_error text CHECK (address_error IS NULL OR address_checked_at IS NOT NULL);
