-- acme's visitor had accepted cookies on the website, so its sign-up kept the Meta pixel's browser ID.
UPDATE meta_conversion_events SET fbp = 'fb.1.1792491500000.1234567890123456789' WHERE workspace_id = md5('ws-acme')::uuid;
