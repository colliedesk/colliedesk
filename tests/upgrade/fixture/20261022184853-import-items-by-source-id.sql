-- Ledger rows of acme's imported knowledge base, which old Freshdesk help center addresses look up by
-- Freshdesk id, and beta's own import of another account holding the same article id.
INSERT INTO import_items (workspace_id, source, entity, source_id, parent_source_id, target_id, status, error, source_updated_at, meta, updated_at) VALUES
    (md5('ws-acme')::uuid, 'freshdesk:account:424242', 'kb_category', '101', NULL, md5('kbc-acme-1')::uuid, 'done', '', '2024-02-01 10:00:00+00', '{}', '2026-10-20 09:00:00+00'),
    (md5('ws-acme')::uuid, 'freshdesk:account:424242', 'kb_folder', '201', NULL, md5('kbc-acme-2')::uuid, 'done', '', '2024-02-01 10:00:00+00', '{}', '2026-10-20 09:00:00+00'),
    (md5('ws-acme')::uuid, 'freshdesk:account:424242', 'kb_article', '9001', NULL, md5('kba-acme-1')::uuid, 'done', '', '2024-03-01 10:00:00+00', '{}', '2026-10-20 09:00:00+00'),
    (md5('ws-beta')::uuid, 'freshdesk:account:515151', 'kb_article', '9001', NULL, md5('kba-beta-1')::uuid, 'done', '', '2026-10-19 09:30:00+00', '{}', '2026-10-20 09:00:00+00');
