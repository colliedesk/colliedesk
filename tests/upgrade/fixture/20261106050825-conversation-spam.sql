-- Spam: acme marked one sender as spam, so their conversations are in Spam and their later mail goes there.
UPDATE contacts SET spam_since = timestamptz '2026-11-06 05:00:00+00' WHERE workspace_id = md5('ws-acme')::uuid AND id = md5('contact-acme-30')::uuid;
UPDATE conversations SET status = 'spam' WHERE workspace_id = md5('ws-acme')::uuid AND contact_id = md5('contact-acme-30')::uuid;
