-- The code Zalo gave a channel's app to verify the URL prefix of its webhook with the HTML file method
-- (docs/user/zalo.md). Not a secret: the server serves Zalo's file for it to anyone below
-- /api/v1/channels/zalo/. Two channels of one app may hold the same code.
ALTER TABLE zalo_channels ADD COLUMN site_verification text CHECK (site_verification ~ '^[A-Za-z0-9_-]{8,100}$');
CREATE INDEX zalo_channels_site_verification ON zalo_channels (site_verification) WHERE site_verification IS NOT NULL;
