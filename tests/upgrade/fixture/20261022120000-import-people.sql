-- acme's Freshdesk account has two agents: Alice, linked by email when she accepted her invite, and Bob,
-- whose link to Ben a person changed, still waiting for the worker.
INSERT INTO import_people (workspace_id, source, external_id, name, email, active, linked_agent_id, linked_by_email, relink_pending, relink_from, created_at, updated_at) VALUES
    (md5('ws-acme')::uuid, 'freshdesk:account:777', '101', 'Alice Agent', 'alice@acme.example', true, md5('agent-acme-admin')::uuid, true, false, NULL,
     '2026-10-22 08:00:00+00', '2026-10-22 08:10:00+00'),
    (md5('ws-acme')::uuid, 'freshdesk:account:777', '102', 'Bob Former', 'bob@old.example', false, md5('agent-acme-ben')::uuid, false, true, md5('agent-acme-admin')::uuid,
     '2026-10-22 08:00:00+00', '2026-10-22 09:00:00+00');
