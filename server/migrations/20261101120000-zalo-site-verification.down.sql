-- The older image serves no verification file.
DROP INDEX IF EXISTS zalo_channels_site_verification;
ALTER TABLE zalo_channels DROP COLUMN IF EXISTS site_verification;
