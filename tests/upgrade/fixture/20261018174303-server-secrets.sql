-- The key that signs mail image links, as the server stores it on first use.
INSERT INTO server_secrets (name, value, created_at) VALUES ('mail_image_key', sha256('fixture-mail-image-key'), '2026-10-18 18:00:00+00');
