-- The older image does not open a custom domain's address; it only checks DNS.
ALTER TABLE help_center_domains DROP COLUMN IF EXISTS address_error, DROP COLUMN IF EXISTS address_checked_at;
