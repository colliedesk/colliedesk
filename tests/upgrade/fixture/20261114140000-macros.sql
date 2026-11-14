-- Acme shares a refund macro and keeps a personal one; the personal one was deleted and can be restored.
-- Beta has its own shared macro. One apply on each desk is recorded, and Acme's was undone.
INSERT INTO macros (id, workspace_id, scope, owner_agent_id, title, body, actions, created_by_agent_id, created_at, updated_at, deleted_at, deleted_by_agent_id) VALUES
    (md5('macro-acme-shared')::uuid, md5('ws-acme')::uuid, 'shared', NULL, 'Refund and wait',
     'Hi {{customer.name}}, this is {{agent.name}} on {{ticket.id}}. The refund is on its way.',
     '{"status":"pending","priority":"high"}', md5('agent-acme-admin')::uuid,
     '2026-11-14 09:00:00+00', '2026-11-14 09:10:00+00', NULL, NULL),
    (md5('macro-acme-personal')::uuid, md5('ws-acme')::uuid, 'personal', md5('agent-acme-ben')::uuid, 'My closing note',
     'Thanks for waiting.', '{}', md5('agent-acme-ben')::uuid,
     '2026-11-14 09:20:00+00', '2026-11-14 09:25:00+00', '2026-11-14 09:40:00+00', md5('agent-acme-admin')::uuid),
    (md5('macro-beta-shared')::uuid, md5('ws-beta')::uuid, 'shared', NULL, 'Refund and wait',
     'Hi {{customer.name}}, we are looking into this.', '{"status":"pending"}', md5('agent-beta-admin')::uuid,
     '2026-11-14 09:05:00+00', '2026-11-14 09:05:00+00', NULL, NULL);

INSERT INTO macro_applications (id, workspace_id, conversation_id, macro_id, macro_title, agent_id, rendered_body, before, applied, created_at, undone_at, undone_by_agent_id) VALUES
    (md5('macro-app-acme-1')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, md5('macro-acme-shared')::uuid,
     'Refund and wait', md5('agent-acme-admin')::uuid, 'Hi Ada, this is Ada Owner on #1. The refund is on its way.',
     '{"status":"open","priority":"normal","touched":["status","priority"]}',
     '{"status":"pending","priority":"high","touched":["status","priority"]}',
     '2026-11-14 10:00:00+00', '2026-11-14 10:15:00+00', md5('agent-acme-ben')::uuid),
    (md5('macro-app-beta-1')::uuid, md5('ws-beta')::uuid, md5('conv-beta-1')::uuid, md5('macro-beta-shared')::uuid,
     'Refund and wait', md5('agent-beta-cy')::uuid, 'Hi Beta Customer 2, we are looking into this.',
     '{"status":"open","touched":["status"]}', '{"status":"pending","touched":["status"]}',
     '2026-11-14 10:05:00+00', NULL, NULL);
