-- Saved inbox views: personal, shared, and one in the trash.
INSERT INTO saved_views (id, workspace_id, scope, owner_agent_id, name, filters, created_by_agent_id, created_at, updated_at, deleted_at) VALUES
    (md5('view-acme-mine')::uuid, md5('ws-acme')::uuid, 'personal', md5('agent-acme-ben')::uuid, 'My urgent', '{"assignee": "me", "priority": ["urgent"]}', md5('agent-acme-ben')::uuid, '2026-10-19 12:00:00+00', '2026-10-19 12:00:00+00', NULL),
    (md5('view-acme-shared')::uuid, md5('ws-acme')::uuid, 'shared', NULL, 'Imported billing', '{"channel": ["imported"], "tag": ["billing"]}', md5('agent-acme-admin')::uuid, '2026-10-19 12:05:00+00', '2026-10-19 12:05:00+00', NULL),
    (md5('view-beta-trash')::uuid, md5('ws-beta')::uuid, 'shared', NULL, 'Old view', '{"status": ["open"]}', md5('agent-beta-admin')::uuid, '2026-10-19 12:10:00+00', '2026-10-19 12:20:00+00', now() - interval '1 day');
