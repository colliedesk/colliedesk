-- acme runs a second help center for its hardware at /b/hardware, showing its first collection
-- (also on the primary), and once had a third one it deleted.
INSERT INTO help_centers (id, workspace_id, handle, primary_help_center, enabled, title, locale,
        deleted_at) VALUES
    (md5('hc-acme-2')::uuid, md5('ws-acme')::uuid, 'hardware', false, true, 'Acme Hardware', 'en',
        NULL),
    (md5('hc-acme-3')::uuid, md5('ws-acme')::uuid, 'legacy', false, false, 'Acme Legacy', 'en',
        '2026-11-22 03:00:00+00');
INSERT INTO kb_collection_help_centers (workspace_id, collection_id, help_center_id, position)
VALUES (md5('ws-acme')::uuid, md5('kbc-acme-1')::uuid, md5('hc-acme-2')::uuid, 0);
