-- acme's Zalo channel holds the code its app was given to verify the webhook's URL prefix (a fixture value).
UPDATE zalo_channels SET site_verification = 'FixtureSiteVerification_Code-0001'
WHERE workspace_id = md5('ws-acme')::uuid AND channel_id = md5('channel-acme-zalo')::uuid;
