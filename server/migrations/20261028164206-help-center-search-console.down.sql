-- An older image serves no verification tags; the tokens set since the upgrade are lost.
ALTER TABLE help_centers DROP COLUMN IF EXISTS google_site_verification, DROP COLUMN IF EXISTS bing_site_verification;
