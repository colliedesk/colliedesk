-- A knowledge base in acme (imported from Freshdesk, with a translation and the original HTML) and in
-- beta (written in Colliedesk, an article in the trash, an edit an AI agent asked for), and an image
-- in each. A knowledge base image file holds 'colliedesk upgrade fixture kb image: ' || the workspace slug.
INSERT INTO kb_collections (id, workspace_id, parent_id, name, slug, description, position, source_meta, created_at, updated_at, deleted_at, deleted_by_agent_id) VALUES
    (md5('kbc-acme-1')::uuid, md5('ws-acme')::uuid, NULL, 'Getting started', 'getting-started', 'First steps', 0, '{"freshdesk_category_id": 101}', '2024-02-01 10:00:00+00', '2024-02-01 10:00:00+00', NULL, NULL),
    (md5('kbc-acme-2')::uuid, md5('ws-acme')::uuid, md5('kbc-acme-1')::uuid, 'Accounts', 'accounts', '', 1, '{"freshdesk_folder_id": 201}', '2024-02-01 10:00:00+00', '2024-02-01 10:00:00+00', NULL, NULL),
    (md5('kbc-beta-1')::uuid, md5('ws-beta')::uuid, NULL, 'Billing', 'billing', 'Invoices and plans', 0, NULL, '2026-10-19 09:00:00+00', '2026-10-19 09:00:00+00', NULL, NULL),
    (md5('kbc-beta-2')::uuid, md5('ws-beta')::uuid, NULL, 'Old', 'old', '', 2, NULL, '2026-10-19 09:00:00+00', now() - interval '2 days', now() - interval '2 days', md5('agent-beta-admin')::uuid);

INSERT INTO kb_articles (id, workspace_id, collection_id, number, title, slug, body_md, status, published_at, author_agent_id, source_meta, source_html, created_at, updated_at, deleted_at, deleted_by_agent_id) VALUES
    (md5('kba-acme-1')::uuid, md5('ws-acme')::uuid, md5('kbc-acme-2')::uuid, 1, 'Reset your password', 'reset-your-password',
     E'Open **Settings**, then *Password*.\n\n![Screen](/api/v1/kb/images/' || md5('kbi-acme-1')::uuid || ')', 'published', '2024-02-02 10:00:00+00',
     md5('agent-acme-admin')::uuid, '{"freshdesk_id": 9001, "hits": 42}', '<p>Open <b>Settings</b>, then <i>Password</i>.</p>',
     '2024-02-02 10:00:00+00', '2024-03-01 10:00:00+00', NULL, NULL),
    (md5('kba-beta-1')::uuid, md5('ws-beta')::uuid, md5('kbc-beta-1')::uuid, 1, 'Download an invoice', 'download-an-invoice',
     'Invoices are under Billing.', 'draft', NULL, md5('agent-beta-cy')::uuid, NULL, NULL,
     '2026-10-19 09:10:00+00', '2026-10-19 09:30:00+00', NULL, NULL),
    (md5('kba-beta-2')::uuid, md5('ws-beta')::uuid, md5('kbc-beta-2')::uuid, 2, 'Old pricing', 'old-pricing',
     'Prices before 2026.', 'draft', NULL, md5('agent-beta-admin')::uuid, NULL, NULL,
     '2026-10-19 09:20:00+00', now() - interval '3 days', now() - interval '3 days', md5('agent-beta-admin')::uuid);

INSERT INTO kb_article_versions (id, workspace_id, article_id, version, kind, restored_from_version, title, slug, body_md, status, collection_id, changed_by_agent_id, token_id, token_label, agent_request_id, created_at) VALUES
    (md5('kba-acme-1-v1')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 1, 'import', NULL, 'Reset your password', 'reset-your-password',
     'Open **Settings**, then *Password*.', 'published', md5('kbc-acme-2')::uuid, NULL, NULL, NULL, NULL, '2024-03-01 10:00:00+00'),
    (md5('kba-acme-1-v2')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 2, 'restore', 1, 'Reset your password', 'reset-your-password',
     'Open **Settings**, then *Password*.', 'published', md5('kbc-acme-2')::uuid, md5('agent-acme-ben')::uuid, md5('token-acme-read')::uuid, 'Claude (read)', NULL, '2024-03-02 10:00:00+00'),
    (md5('kba-beta-1-v1')::uuid, md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, 1, 'create', NULL, 'Download an invoice', 'download-an-invoice',
     'Invoices are under Billing.', 'draft', md5('kbc-beta-1')::uuid, md5('agent-beta-cy')::uuid, md5('token-beta-send')::uuid, 'Claude (send)', md5('request-beta-saved-reply')::uuid, '2026-10-19 09:30:00+00'),
    (md5('kba-beta-2-v1')::uuid, md5('ws-beta')::uuid, md5('kba-beta-2')::uuid, 1, 'delete', NULL, 'Old pricing', 'old-pricing',
     'Prices before 2026.', 'draft', md5('kbc-beta-2')::uuid, md5('agent-beta-admin')::uuid, NULL, NULL, NULL, now() - interval '3 days');

INSERT INTO kb_article_translations (workspace_id, article_id, locale, title, body_md, source_meta, source_html, created_at, updated_at) VALUES
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'de', 'Passwort zurücksetzen', 'Öffnen Sie **Einstellungen**.', '{"freshdesk_id": 9001}',
     '<p>Öffnen Sie <b>Einstellungen</b>.</p>', '2024-02-03 10:00:00+00', '2024-02-03 10:00:00+00');

INSERT INTO kb_images (id, workspace_id, storage_key, sha256, size, content_type, counted, created_by_agent_id, created_at)
SELECT md5('kbi-' || w || '-1')::uuid, md5('ws-' || w)::uuid,
       md5('ws-' || w)::uuid || '/' || left(h, 2) || '/' || h, h,
       octet_length('colliedesk upgrade fixture kb image: ' || w), 'image/png', w = 'beta', md5('agent-' || w || '-admin')::uuid, '2026-10-19 09:00:00+00'
FROM (VALUES ('acme'), ('beta')) AS v(w),
     LATERAL (SELECT encode(sha256(convert_to('colliedesk upgrade fixture kb image: ' || w, 'UTF8')), 'hex') AS h) s;

INSERT INTO kb_article_images (workspace_id, article_id, image_id) VALUES
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, md5('kbi-acme-1')::uuid),
    (md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, md5('kbi-beta-1')::uuid);

INSERT INTO help_centers (workspace_id, enabled, title, locale, content_version, last_article_number, updated_at) VALUES
    (md5('ws-acme')::uuid, true, 'Acme Help', 'en', 7, 1, '2024-03-02 10:00:00+00'),
    (md5('ws-beta')::uuid, false, '', 'pt-BR', 4, 2, now() - interval '2 days');
