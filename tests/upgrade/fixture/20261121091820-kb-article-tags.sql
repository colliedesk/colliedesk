-- acme's password article is tagged "account" and "security"; its unpublished changes drop "security" and
-- add "login". beta's invoice article is tagged "billing". beta turns on tags on its help center pages.
INSERT INTO kb_article_tags (workspace_id, article_id, tag, created_at) VALUES
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'account', '2026-11-21 09:30:00+00'),
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'security', '2026-11-21 09:30:00+00'),
    (md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, 'billing', '2026-11-21 09:35:00+00');

UPDATE kb_article_drafts SET tags = ARRAY['account', 'login'] WHERE id = md5('kbd-acme-1')::uuid;

UPDATE help_centers SET show_tags = true WHERE workspace_id = md5('ws-beta')::uuid;
